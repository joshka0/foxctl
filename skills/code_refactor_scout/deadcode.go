package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	symindex "github.com/jkatigb/agentctl/internal/indexing/symbol"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
)

const (
	maxDeadCodeEdgeScan      = 256
	maxDeadCodeSourceSamples = 5
)

var deadCodeEdgeTypes = []repoindex.EdgeType{
	repoindex.EdgeCalls,
	repoindex.EdgeUsesSymbol,
	repoindex.EdgeRefersTo,
}

type deadInboundSummary struct {
	TotalCount      int
	SameFileNonTest int
	ExternalNonTest int
	TestCount       int
	SourceSample    []string
}

type deadSymbolInfo struct {
	Node     repoindex.Node
	Locator  *repoindex.LocatorEntry
	Incoming []repoindex.Edge
	Summary  deadInboundSummary
}

type deadStructuralRoots struct {
	LiveMethodFiles map[string]string
	LiveSymbols     map[string]map[string]string
}

type deadFileInboundSummary struct {
	TotalCount      int
	ExternalNonTest int
	TestCount       int
	SourceSample    []string
}

type deadPackageInboundSummary struct {
	TotalCount      int
	ExternalNonTest int
	TestCount       int
	SourceSample    []string
}

type deadReachability int

const (
	deadReachabilityNone deadReachability = iota
	deadReachabilityTestOnly
	deadReachabilityLive
)

func buildDeadCodeFindings(ctx context.Context, storageRoot string, scope refscope.Scope, status refstatus.Status, focus string) ([]finding, error) {
	if !focusWantsDeadCode(focus) || status.Mode != refstatus.ModeIndexBacked {
		return nil, nil
	}

	store, err := repoindex.Open(ctx, storageRoot, scope.Workspace)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	limit := status.RepoIndex.Stats.NodesByKind[repoindex.NodeSymbol]
	if limit <= 0 {
		limit = 50000
	}
	fileLimit := status.RepoIndex.Stats.NodesByKind[repoindex.NodeFile]
	if fileLimit <= 0 {
		fileLimit = 10000
	}
	fileNodes, err := store.ListNodesByKind(ctx, repoindex.NodeFile, fileLimit)
	if err != nil {
		return nil, err
	}
	structuralRoots, err := buildDeadStructuralRoots(ctx, store, scope, fileNodes)
	if err != nil {
		return nil, err
	}
	nodes, err := store.ListNodesByKind(ctx, repoindex.NodeSymbol, limit)
	if err != nil {
		return nil, err
	}

	locatorCache := make(map[string][]repoindex.LocatorEntry)
	nodeCache := make(map[string]repoindex.Node)
	infos := make(map[string]*deadSymbolInfo)
	for _, node := range nodes {
		if !deadCodeScopeIncludes(scope, node.File) {
			continue
		}
		if !scope.IncludeTests && fsutil.IsTestFile(filepath.Base(node.File)) {
			continue
		}

		locator := deadCodeLocatorForNode(ctx, store, locatorCache, node)
		if !deadCodeSupportsSymbol(node, locator) || deadCodeExempt(node, locator) {
			continue
		}

		incoming, err := store.GetIncomingEdges(ctx, node.ID, deadCodeEdgeTypes, maxDeadCodeEdgeScan)
		if err != nil {
			return nil, err
		}
		summary, err := summarizeDeadCodeInbounds(ctx, store, node, incoming)
		if err != nil {
			return nil, err
		}

		nodeCache[node.ID] = node
		infos[node.ID] = &deadSymbolInfo{
			Node:     node,
			Locator:  locator,
			Incoming: incoming,
			Summary:  summary,
		}
	}

	out := make([]finding, 0, 32)
	symbolRuleByID := make(map[string]string, len(infos))
	memo := make(map[string]deadReachability, len(infos))
	for _, info := range infos {
		reachability := classifyDeadReachability(ctx, store, info.Node.ID, infos, nodeCache, memo, structuralRoots, nil)
		switch {
		case !info.Node.Exported && reachability == deadReachabilityNone:
			out = append(out, makeUnreachablePrivateFinding(info.Node, info.Locator, info.Summary))
			symbolRuleByID[info.Node.ID] = "unreachable_private_symbol"
		case !info.Node.Exported && reachability == deadReachabilityTestOnly:
			out = append(out, makeTestOnlyHelperFinding(info.Node, info.Locator, info.Summary))
			symbolRuleByID[info.Node.ID] = "test_only_helper"
		case info.Node.Exported && reachability == deadReachabilityNone && info.Summary.ExternalNonTest == 0 && info.Summary.TestCount == 0:
			out = append(out, makeStaleExportFinding(info.Node, info.Locator, info.Summary))
			symbolRuleByID[info.Node.ID] = "stale_export_candidate"
		}
	}

	fileFindings, fileRuleByPath, err := buildDeadFileFindings(ctx, store, scope, fileNodes, symbolRuleByID)
	if err != nil {
		return nil, err
	}
	out = append(out, fileFindings...)

	pkgLimit := status.RepoIndex.Stats.NodesByKind[repoindex.NodePackage]
	if pkgLimit <= 0 {
		pkgLimit = 10000
	}
	packageNodes, err := store.ListNodesByKind(ctx, repoindex.NodePackage, pkgLimit)
	if err != nil {
		return nil, err
	}
	packageFindings, packageRuleByFile, err := buildDeadPackageFindings(ctx, store, scope, packageNodes, fileNodes, fileRuleByPath)
	if err != nil {
		return nil, err
	}
	out = append(out, packageFindings...)
	if shouldSuppressDeadFamilyFindings(focus) {
		out = suppressDeadFamilyFindings(out, fileRuleByPath, packageRuleByFile)
	}

	sortFindings(out)
	return out, nil
}

func buildDeadFileFindings(ctx context.Context, store *repoindex.Store, scope refscope.Scope, files []repoindex.Node, symbolRules map[string]string) ([]finding, map[string]string, error) {
	out := make([]finding, 0, 16)
	fileRules := make(map[string]string)
	for _, fileNode := range files {
		if !deadCodeScopeIncludes(scope, fileNode.File) {
			continue
		}
		if deadCodeExemptFile(fileNode.File) {
			continue
		}

		contains, err := store.GetOutgoingEdges(ctx, fileNode.ID, []repoindex.EdgeType{repoindex.EdgeContains}, 512)
		if err != nil {
			return nil, nil, err
		}
		if len(contains) == 0 {
			continue
		}

		contained := make([]string, 0, len(contains))
		allDeadish := true
		allTestOnly := true
		hasTestOnly := false
		hasPrivateDead := false
		for _, edge := range contains {
			ruleID, ok := symbolRules[edge.Dst]
			if !ok {
				allDeadish = false
				allTestOnly = false
				continue
			}
			contained = append(contained, edge.Dst)
			switch ruleID {
			case "unreachable_private_symbol":
				hasPrivateDead = true
				allTestOnly = false
			case "test_only_helper":
				hasTestOnly = true
			case "stale_export_candidate":
				// allowed in both conservative file families
			default:
				allDeadish = false
				allTestOnly = false
			}
		}
		if len(contained) == 0 {
			continue
		}

		incoming, err := store.GetIncomingEdges(ctx, fileNode.ID, []repoindex.EdgeType{repoindex.EdgeImports, repoindex.EdgeTests, repoindex.EdgeRefersTo}, maxDeadCodeEdgeScan)
		if err != nil {
			return nil, nil, err
		}
		summary, err := summarizeDeadFileInbounds(ctx, store, fileNode, incoming)
		if err != nil {
			return nil, nil, err
		}

		switch {
		case allTestOnly && hasTestOnly && summary.ExternalNonTest == 0:
			out = append(out, makeTestOnlyFileFinding(fileNode, len(contained), summary))
			fileRules[fileNode.File] = "test_only_file"
		case allDeadish && hasPrivateDead && summary.ExternalNonTest == 0 && summary.TestCount == 0:
			out = append(out, makeOrphanFileFinding(fileNode, len(contained), summary))
			fileRules[fileNode.File] = "orphan_file"
		}
	}
	return out, fileRules, nil
}

func buildDeadPackageFindings(ctx context.Context, store *repoindex.Store, scope refscope.Scope, packages []repoindex.Node, files []repoindex.Node, fileRules map[string]string) ([]finding, map[string]string, error) {
	fileNodeByID := make(map[string]repoindex.Node, len(files))
	for _, node := range files {
		fileNodeByID[node.ID] = node
	}

	out := make([]finding, 0, 8)
	packageRuleByFile := make(map[string]string)
	for _, pkgNode := range packages {
		contains, err := store.GetOutgoingEdges(ctx, pkgNode.ID, []repoindex.EdgeType{repoindex.EdgeContains}, 512)
		if err != nil {
			return nil, nil, err
		}

		containedPaths := make([]string, 0, len(contains))
		allOrphan := true
		allTestOnly := true
		hasTestOnly := false
		for _, edge := range contains {
			fileNode, ok := fileNodeByID[edge.Dst]
			if !ok || !deadCodeScopeIncludes(scope, fileNode.File) || deadCodeExemptFile(fileNode.File) {
				continue
			}
			ruleID, ok := fileRules[fileNode.File]
			if !ok {
				allOrphan = false
				allTestOnly = false
				containedPaths = append(containedPaths, fileNode.File)
				continue
			}
			containedPaths = append(containedPaths, fileNode.File)
			switch ruleID {
			case "orphan_file":
				allTestOnly = false
			case "test_only_file":
				hasTestOnly = true
				allOrphan = false
			default:
				allOrphan = false
				allTestOnly = false
			}
		}
		if len(containedPaths) == 0 {
			continue
		}

		incoming, err := store.GetIncomingEdges(ctx, pkgNode.ID, []repoindex.EdgeType{repoindex.EdgeImports, repoindex.EdgeTests}, maxDeadCodeEdgeScan)
		if err != nil {
			return nil, nil, err
		}
		summary, err := summarizeDeadPackageInbounds(ctx, store, pkgNode, incoming)
		if err != nil {
			return nil, nil, err
		}

		switch {
		case allTestOnly && hasTestOnly && summary.ExternalNonTest == 0:
			out = append(out, makeTestOnlyPackageFinding(pkgNode, len(containedPaths), summary))
			for _, path := range containedPaths {
				packageRuleByFile[path] = "test_only_package"
			}
		case allOrphan && summary.ExternalNonTest == 0 && summary.TestCount == 0:
			out = append(out, makeStalePackageFinding(pkgNode, len(containedPaths), summary))
			for _, path := range containedPaths {
				packageRuleByFile[path] = "stale_package_candidate"
			}
		}
	}
	return out, packageRuleByFile, nil
}

func suppressDeadFamilyFindings(items []finding, fileRuleByPath map[string]string, packageRuleByFile map[string]string) []finding {
	if len(items) == 0 {
		return items
	}
	filtered := items[:0]
	for _, item := range items {
		if deadSymbolRuleID(item.RuleID) {
			if _, ok := packageRuleByFile[item.File]; ok {
				continue
			}
			if _, ok := fileRuleByPath[item.File]; ok {
				continue
			}
		}
		if deadFileRuleID(item.RuleID) {
			if _, ok := packageRuleByFile[item.File]; ok {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func deadSymbolRuleID(ruleID string) bool {
	switch ruleID {
	case "unreachable_private_symbol", "test_only_helper", "stale_export_candidate":
		return true
	default:
		return false
	}
}

func deadFileRuleID(ruleID string) bool {
	switch ruleID {
	case "orphan_file", "test_only_file":
		return true
	default:
		return false
	}
}

func shouldSuppressDeadFamilyFindings(focus string) bool {
	switch strings.TrimSpace(focus) {
	case "", "all":
		return true
	default:
		return false
	}
}

func buildDeadStructuralRoots(ctx context.Context, store *repoindex.Store, scope refscope.Scope, files []repoindex.Node) (deadStructuralRoots, error) {
	out := deadStructuralRoots{
		LiveMethodFiles: make(map[string]string),
		LiveSymbols:     make(map[string]map[string]string),
	}
	for _, fileNode := range files {
		if !deadCodeScopeIncludes(scope, fileNode.File) {
			continue
		}
		outgoing, err := store.GetOutgoingEdges(ctx, fileNode.ID, []repoindex.EdgeType{repoindex.EdgeImplements, repoindex.EdgeEmbeds, repoindex.EdgeRefersTo}, maxDeadCodeEdgeScan)
		if err != nil {
			return deadStructuralRoots{}, err
		}
		targetIDs := make([]string, 0, len(outgoing))
		targetSeen := make(map[string]struct{}, len(outgoing))
		for _, edge := range outgoing {
			if edge.Dst == "" {
				continue
			}
			if _, ok := targetSeen[edge.Dst]; ok {
				continue
			}
			targetSeen[edge.Dst] = struct{}{}
			targetIDs = append(targetIDs, edge.Dst)
		}
		targetNodes, err := store.GetNodes(ctx, targetIDs)
		if err != nil {
			return deadStructuralRoots{}, err
		}
		targetNodeByID := make(map[string]repoindex.Node, len(targetNodes))
		for _, node := range targetNodes {
			targetNodeByID[node.ID] = node
		}
		for _, edge := range outgoing {
			switch edge.Type {
			case repoindex.EdgeImplements:
				out.LiveMethodFiles[fileNode.File] = "file_implements"
				addElixirStructuralRootCallbacks(out.LiveSymbols, fileNode.File, targetNodeByID[edge.Dst].Name)
			case repoindex.EdgeEmbeds:
				if _, ok := out.LiveMethodFiles[fileNode.File]; !ok {
					out.LiveMethodFiles[fileNode.File] = "file_embeds"
				}
			case repoindex.EdgeRefersTo:
				addElixirStructuralRootCallbacks(out.LiveSymbols, fileNode.File, targetNodeByID[edge.Dst].Name)
			}
		}
	}
	return out, nil
}

var elixirCallbackRootRegistry = map[string][]string{
	"Application":            {"start", "config_change"},
	"GenServer":              {"init", "handle_call", "handle_cast", "handle_continue", "handle_info", "terminate", "code_change", "format_status", "child_spec"},
	"Supervisor":             {"init", "child_spec"},
	"Plug":                   {"init", "call"},
	"Phoenix.Channel":        {"join", "handle_in", "handle_info", "terminate"},
	"Phoenix.LiveView":       {"mount", "render", "handle_params", "handle_event", "handle_info", "terminate"},
	"Phoenix.LiveComponent":  {"mount", "render", "update", "handle_event", "handle_info", "terminate"},
	"Ecto.Type":              {"type", "cast", "load", "dump", "embed_as", "equal"},
	"Ecto.ParameterizedType": {"type", "init", "cast", "load", "dump", "embed_as", "equal"},
}

func addElixirStructuralRootCallbacks(liveSymbols map[string]map[string]string, filePath, targetName string) {
	filePath = filepath.ToSlash(strings.TrimSpace(filePath))
	targetName = strings.TrimSpace(targetName)
	if filePath == "" || targetName == "" {
		return
	}
	callbacks := elixirCallbackRootRegistry[targetName]
	if len(callbacks) == 0 {
		return
	}
	if _, ok := liveSymbols[filePath]; !ok {
		liveSymbols[filePath] = make(map[string]string, len(callbacks))
	}
	for _, callback := range callbacks {
		if callback = strings.TrimSpace(callback); callback != "" {
			liveSymbols[filePath][callback] = targetName
		}
	}
}

func focusWantsDeadCode(focus string) bool {
	switch strings.TrimSpace(focus) {
	case "", "all", "dead":
		return true
	default:
		return false
	}
}

func deadCodeScopeIncludes(scope refscope.Scope, file string) bool {
	file = filepath.ToSlash(strings.TrimSpace(file))
	if file == "" {
		return false
	}
	scopePath := filepath.ToSlash(strings.TrimSpace(scope.Path))
	if scopePath == "" || scopePath == "." {
		return true
	}
	if scope.IsDir {
		return file == scopePath || strings.HasPrefix(file, scopePath+"/")
	}
	return file == scopePath
}

func deadCodeLocatorForNode(ctx context.Context, store *repoindex.Store, cache map[string][]repoindex.LocatorEntry, node repoindex.Node) *repoindex.LocatorEntry {
	file := filepath.ToSlash(strings.TrimSpace(node.File))
	if file == "" {
		return nil
	}
	locators, ok := cache[file]
	if !ok {
		loaded, err := store.LookupLocatorsByFile(ctx, file)
		if err != nil {
			cache[file] = nil
			return nil
		}
		locators = loaded
		cache[file] = loaded
	}
	expectedKinds := deadExpectedLocatorKinds(node)
	for i := range locators {
		loc := &locators[i]
		if strings.TrimSpace(loc.Name) != strings.TrimSpace(node.Name) {
			continue
		}
		if !deadLocatorKindAllowed(expectedKinds, loc.Kind) {
			continue
		}
		if loc.SpanStart == node.SpanStart {
			return loc
		}
	}
	for i := range locators {
		loc := &locators[i]
		if strings.TrimSpace(loc.Name) == strings.TrimSpace(node.Name) && deadLocatorKindAllowed(expectedKinds, loc.Kind) {
			return loc
		}
	}
	return nil
}

func deadExpectedLocatorKinds(node repoindex.Node) map[string]struct{} {
	signature := strings.TrimSpace(node.Signature)
	if signature == "" {
		return nil
	}
	switch {
	case strings.HasPrefix(signature, "defmodule "), strings.HasPrefix(signature, "defprotocol "), strings.HasPrefix(signature, "defimpl "):
		return map[string]struct{}{string(symindex.KindClass): {}}
	case strings.HasPrefix(signature, "@type "), strings.HasPrefix(signature, "@typep "):
		return map[string]struct{}{string(symindex.KindType): {}}
	case strings.HasPrefix(signature, "@callback "):
		return map[string]struct{}{string(symindex.KindInterface): {}}
	case strings.HasPrefix(signature, "def "), strings.HasPrefix(signature, "defp "), strings.HasPrefix(signature, "defmacro "), strings.HasPrefix(signature, "defmacrop "):
		return map[string]struct{}{string(symindex.KindFunction): {}}
	case strings.HasPrefix(signature, "func ("):
		return map[string]struct{}{string(symindex.KindMethod): {}}
	case strings.HasPrefix(signature, "func "):
		return map[string]struct{}{string(symindex.KindFunction): {}, string(symindex.KindMethod): {}}
	default:
		return nil
	}
}

func deadLocatorKindAllowed(expected map[string]struct{}, kind string) bool {
	if len(expected) == 0 {
		return true
	}
	_, ok := expected[strings.TrimSpace(kind)]
	return ok
}

func deadCodeSupportsSymbol(node repoindex.Node, locator *repoindex.LocatorEntry) bool {
	if locator != nil {
		switch strings.TrimSpace(locator.Kind) {
		case string(symindex.KindFunction), string(symindex.KindMethod):
			return true
		default:
			return false
		}
	}
	expectedKinds := deadExpectedLocatorKinds(node)
	if len(expectedKinds) > 0 {
		_, functionOK := expectedKinds[string(symindex.KindFunction)]
		_, methodOK := expectedKinds[string(symindex.KindMethod)]
		return functionOK || methodOK
	}
	signature := strings.TrimSpace(node.Signature)
	return signature != "" && strings.Contains(signature, "(")
}

func deadCodeExempt(node repoindex.Node, locator *repoindex.LocatorEntry) bool {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return true
	}
	switch name {
	case "main", "init", "TestMain":
		return true
	}
	return false
}

func deadCodeExemptFile(path string) bool {
	base := filepath.Base(strings.TrimSpace(path))
	if base == "" || fsutil.IsTestFile(base) {
		return true
	}
	switch base {
	case "main.go", "doc.go":
		return true
	}
	for _, suffix := range []string{"_linux.go", "_darwin.go", "_windows.go", "_stub.go", "_cgo.go", "_nocgo.go"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func summarizeDeadCodeInbounds(ctx context.Context, store *repoindex.Store, target repoindex.Node, incoming []repoindex.Edge) (deadInboundSummary, error) {
	if len(incoming) == 0 {
		return deadInboundSummary{}, nil
	}
	sourceIDs := make([]string, 0, len(incoming))
	seenIDs := make(map[string]struct{}, len(incoming))
	for _, edge := range incoming {
		if strings.TrimSpace(edge.Src) == "" || edge.Src == target.ID {
			continue
		}
		if _, ok := seenIDs[edge.Src]; ok {
			continue
		}
		seenIDs[edge.Src] = struct{}{}
		sourceIDs = append(sourceIDs, edge.Src)
	}
	nodes, err := store.GetNodes(ctx, sourceIDs)
	if err != nil {
		return deadInboundSummary{}, err
	}
	nodeByID := make(map[string]repoindex.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}

	targetFile := filepath.ToSlash(strings.TrimSpace(target.File))
	summary := deadInboundSummary{}
	sampleSeen := make(map[string]struct{})
	for _, edge := range incoming {
		if edge.Src == target.ID {
			continue
		}
		source, ok := nodeByID[edge.Src]
		if !ok {
			continue
		}
		sourceFile := filepath.ToSlash(strings.TrimSpace(source.File))
		if sourceFile == "" {
			continue
		}
		summary.TotalCount++
		if fsutil.IsTestFile(filepath.Base(sourceFile)) {
			summary.TestCount++
		} else if sourceFile == targetFile {
			summary.SameFileNonTest++
		} else {
			summary.ExternalNonTest++
		}
		label := sourceFile
		if strings.TrimSpace(source.Name) != "" {
			label = fmt.Sprintf("%s:%s", sourceFile, source.Name)
		}
		if len(summary.SourceSample) < maxDeadCodeSourceSamples {
			if _, ok := sampleSeen[label]; !ok {
				sampleSeen[label] = struct{}{}
				summary.SourceSample = append(summary.SourceSample, label)
			}
		}
	}
	return summary, nil
}

func summarizeDeadFileInbounds(ctx context.Context, store *repoindex.Store, target repoindex.Node, incoming []repoindex.Edge) (deadFileInboundSummary, error) {
	if len(incoming) == 0 {
		return deadFileInboundSummary{}, nil
	}
	sourceIDs := make([]string, 0, len(incoming))
	seenIDs := make(map[string]struct{}, len(incoming))
	for _, edge := range incoming {
		if strings.TrimSpace(edge.Src) == "" || edge.Src == target.ID {
			continue
		}
		if _, ok := seenIDs[edge.Src]; ok {
			continue
		}
		seenIDs[edge.Src] = struct{}{}
		sourceIDs = append(sourceIDs, edge.Src)
	}
	nodes, err := store.GetNodes(ctx, sourceIDs)
	if err != nil {
		return deadFileInboundSummary{}, err
	}
	nodeByID := make(map[string]repoindex.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}

	summary := deadFileInboundSummary{}
	sampleSeen := make(map[string]struct{})
	for _, edge := range incoming {
		source, ok := nodeByID[edge.Src]
		if !ok {
			continue
		}
		summary.TotalCount++
		sourceFile := filepath.ToSlash(strings.TrimSpace(source.File))
		if edge.Type == repoindex.EdgeTests || fsutil.IsTestFile(filepath.Base(sourceFile)) {
			summary.TestCount++
		} else {
			summary.ExternalNonTest++
		}
		label := sourceFile
		if label == "" {
			label = source.Name
		}
		if label == "" {
			label = source.ID
		}
		if len(summary.SourceSample) < maxDeadCodeSourceSamples {
			if _, ok := sampleSeen[label]; !ok {
				sampleSeen[label] = struct{}{}
				summary.SourceSample = append(summary.SourceSample, label)
			}
		}
	}
	return summary, nil
}

func summarizeDeadPackageInbounds(ctx context.Context, store *repoindex.Store, target repoindex.Node, incoming []repoindex.Edge) (deadPackageInboundSummary, error) {
	if len(incoming) == 0 {
		return deadPackageInboundSummary{}, nil
	}
	sourceIDs := make([]string, 0, len(incoming))
	seenIDs := make(map[string]struct{}, len(incoming))
	for _, edge := range incoming {
		if strings.TrimSpace(edge.Src) == "" || edge.Src == target.ID {
			continue
		}
		if _, ok := seenIDs[edge.Src]; ok {
			continue
		}
		seenIDs[edge.Src] = struct{}{}
		sourceIDs = append(sourceIDs, edge.Src)
	}
	nodes, err := store.GetNodes(ctx, sourceIDs)
	if err != nil {
		return deadPackageInboundSummary{}, err
	}
	nodeByID := make(map[string]repoindex.Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.ID] = node
	}

	summary := deadPackageInboundSummary{}
	sampleSeen := make(map[string]struct{})
	for _, edge := range incoming {
		source, ok := nodeByID[edge.Src]
		if !ok {
			continue
		}
		summary.TotalCount++
		if edge.Type == repoindex.EdgeTests || fsutil.IsTestFile(filepath.Base(strings.TrimSpace(source.File))) {
			summary.TestCount++
		} else {
			summary.ExternalNonTest++
		}
		label := strings.TrimSpace(source.Name)
		if label == "" {
			label = strings.TrimSpace(source.File)
		}
		if label == "" {
			label = source.ID
		}
		if len(summary.SourceSample) < maxDeadCodeSourceSamples {
			if _, ok := sampleSeen[label]; !ok {
				sampleSeen[label] = struct{}{}
				summary.SourceSample = append(summary.SourceSample, label)
			}
		}
	}
	return summary, nil
}

func classifyDeadReachability(ctx context.Context, store *repoindex.Store, nodeID string, infos map[string]*deadSymbolInfo, nodeCache map[string]repoindex.Node, memo map[string]deadReachability, roots deadStructuralRoots, visiting map[string]struct{}) deadReachability {
	if reachability, ok := memo[nodeID]; ok {
		return reachability
	}
	info, ok := infos[nodeID]
	if !ok {
		return deadReachabilityNone
	}
	if direct := deadSelfRootReachability(info.Node, info.Locator, roots); direct != deadReachabilityNone {
		memo[nodeID] = direct
		return direct
	}
	if visiting == nil {
		visiting = make(map[string]struct{}, 8)
	}
	if _, ok := visiting[nodeID]; ok {
		return deadReachabilityNone
	}
	visiting[nodeID] = struct{}{}
	defer delete(visiting, nodeID)

	best := deadReachabilityNone
	for _, edge := range info.Incoming {
		source, sourceLocator, ok := deadSourceNode(ctx, store, edge.Src, infos, nodeCache)
		if !ok {
			continue
		}
		switch direct := deadDirectRootReachability(source, sourceLocator, infos[edge.Src]); direct {
		case deadReachabilityLive:
			memo[nodeID] = deadReachabilityLive
			return deadReachabilityLive
		case deadReachabilityTestOnly:
			best = deadReachabilityTestOnly
			continue
		}
		if _, nested := infos[edge.Src]; nested {
			switch classifyDeadReachability(ctx, store, edge.Src, infos, nodeCache, memo, roots, visiting) {
			case deadReachabilityLive:
				memo[nodeID] = deadReachabilityLive
				return deadReachabilityLive
			case deadReachabilityTestOnly:
				best = deadReachabilityTestOnly
			}
		}
	}
	memo[nodeID] = best
	return best
}

func deadSelfRootReachability(node repoindex.Node, locator *repoindex.LocatorEntry, roots deadStructuralRoots) deadReachability {
	if locator == nil || strings.TrimSpace(locator.Kind) != string(symindex.KindMethod) {
		file := filepath.ToSlash(strings.TrimSpace(node.File))
		if callbacks := roots.LiveSymbols[file]; len(callbacks) > 0 {
			if _, ok := callbacks[strings.TrimSpace(node.Name)]; ok {
				return deadReachabilityLive
			}
		}
		return deadReachabilityNone
	}
	if _, ok := roots.LiveMethodFiles[filepath.ToSlash(strings.TrimSpace(node.File))]; ok {
		return deadReachabilityLive
	}
	return deadReachabilityNone
}

func deadSourceNode(ctx context.Context, store *repoindex.Store, sourceID string, infos map[string]*deadSymbolInfo, nodeCache map[string]repoindex.Node) (repoindex.Node, *repoindex.LocatorEntry, bool) {
	if info, ok := infos[sourceID]; ok {
		return info.Node, info.Locator, true
	}
	if cached, ok := nodeCache[sourceID]; ok {
		return cached, nil, true
	}
	node, err := store.GetNode(ctx, sourceID)
	if err != nil {
		return repoindex.Node{}, nil, false
	}
	nodeCache[sourceID] = node
	return node, nil, true
}

func deadDirectRootReachability(node repoindex.Node, locator *repoindex.LocatorEntry, info *deadSymbolInfo) deadReachability {
	file := filepath.Base(strings.TrimSpace(node.File))
	if fsutil.IsTestFile(file) {
		return deadReachabilityTestOnly
	}
	if info == nil && node.Kind == repoindex.NodeSymbol {
		return deadReachabilityLive
	}
	if node.Kind == repoindex.NodeFile {
		return deadReachabilityLive
	}
	name := strings.TrimSpace(node.Name)
	switch name {
	case "main", "init":
		return deadReachabilityLive
	case "TestMain":
		return deadReachabilityTestOnly
	}
	if node.Exported {
		return deadReachabilityLive
	}
	if info != nil && info.Summary.ExternalNonTest > 0 {
		return deadReachabilityLive
	}
	return deadReachabilityNone
}

func makeUnreachablePrivateFinding(node repoindex.Node, locator *repoindex.LocatorEntry, summary deadInboundSummary) finding {
	score := 84
	return finding{
		RuleID:            "unreachable_private_symbol",
		Category:          "function",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "Private symbol has no incoming references",
		Detail:            fmt.Sprintf("%s has no incoming calls or references in the covered repo graph, so it is a strong dead-code candidate.", deadCodeDisplayName(node)),
		SuggestedRefactor: "Delete the helper if it is truly obsolete, or add the missing call path if it is still intended to be reachable.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          deadCodeLanguage(node, locator),
		Confidence:        "high",
		Signals:           []string{"repo_graph", "dead_code_reachability"},
		Evidence: map[string]any{
			"incoming_ref_count":      summary.TotalCount,
			"same_file_non_test_refs": summary.SameFileNonTest,
			"external_non_test_refs":  summary.ExternalNonTest,
			"test_ref_count":          summary.TestCount,
			"source_sample":           append([]string(nil), summary.SourceSample...),
			"exported":                node.Exported,
			"symbol_kind":             deadCodeKind(locator),
		},
	}
}

func makeTestOnlyHelperFinding(node repoindex.Node, locator *repoindex.LocatorEntry, summary deadInboundSummary) finding {
	score := 74
	return finding{
		RuleID:            "test_only_helper",
		Category:          "function",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "Symbol is only referenced from tests",
		Detail:            fmt.Sprintf("%s is only reached from test code in the covered repo graph, which usually means the production surface no longer needs it.", deadCodeDisplayName(node)),
		SuggestedRefactor: "Inline or move the helper into test-only code if production callers are gone.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          deadCodeLanguage(node, locator),
		Confidence:        "medium",
		Signals:           []string{"repo_graph", "dead_code_reachability"},
		Evidence: map[string]any{
			"incoming_ref_count":      summary.TotalCount,
			"same_file_non_test_refs": summary.SameFileNonTest,
			"external_non_test_refs":  summary.ExternalNonTest,
			"test_ref_count":          summary.TestCount,
			"source_sample":           append([]string(nil), summary.SourceSample...),
			"exported":                node.Exported,
			"symbol_kind":             deadCodeKind(locator),
		},
	}
}

func makeStaleExportFinding(node repoindex.Node, locator *repoindex.LocatorEntry, summary deadInboundSummary) finding {
	score := 66
	return finding{
		RuleID:            "stale_export_candidate",
		Category:          "function",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "Exported symbol has no external non-test references",
		Detail:            fmt.Sprintf("%s is exported, but the covered repo graph shows no external non-test callers. It may be an internal helper that no longer needs to be public.", deadCodeDisplayName(node)),
		SuggestedRefactor: "Consider unexporting or removing this symbol after confirming it is not part of an intentional external API.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          deadCodeLanguage(node, locator),
		Confidence:        "medium",
		Signals:           []string{"repo_graph", "dead_code_reachability"},
		Evidence: map[string]any{
			"incoming_ref_count":      summary.TotalCount,
			"same_file_non_test_refs": summary.SameFileNonTest,
			"external_non_test_refs":  summary.ExternalNonTest,
			"test_ref_count":          summary.TestCount,
			"source_sample":           append([]string(nil), summary.SourceSample...),
			"exported":                node.Exported,
			"symbol_kind":             deadCodeKind(locator),
		},
	}
}

func makeOrphanFileFinding(node repoindex.Node, containedCount int, summary deadFileInboundSummary) finding {
	score := 76
	return finding{
		RuleID:            "orphan_file",
		Category:          "file",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "File appears orphaned from the covered graph",
		Detail:            fmt.Sprintf("%s has %d dead-ish contained symbols and no non-test inbound file references in the covered repo graph, so it is a strong file-level dead-code candidate.", node.File, containedCount),
		SuggestedRefactor: "Delete the file if it is obsolete, or restore the intended import or registration path if it should still be reachable.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          langutil.DetectAllowed(node.File, langutil.CommonCodeLanguages),
		Confidence:        "high",
		Signals:           []string{"repo_graph", "dead_code_file"},
		Evidence: map[string]any{
			"incoming_ref_count":     summary.TotalCount,
			"external_non_test_refs": summary.ExternalNonTest,
			"test_ref_count":         summary.TestCount,
			"source_sample":          append([]string(nil), summary.SourceSample...),
			"contained_symbol_count": containedCount,
		},
	}
}

func makeTestOnlyFileFinding(node repoindex.Node, containedCount int, summary deadFileInboundSummary) finding {
	score := 70
	return finding{
		RuleID:            "test_only_file",
		Category:          "file",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "File appears test-only from the covered graph",
		Detail:            fmt.Sprintf("%s contains %d dead-ish/test-only symbols and no non-test inbound file references in the covered repo graph. It likely belongs in test-only code or can be removed.", node.File, containedCount),
		SuggestedRefactor: "Move the helper into test-only code or delete the file if the production surface no longer needs it.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          langutil.DetectAllowed(node.File, langutil.CommonCodeLanguages),
		Confidence:        "medium",
		Signals:           []string{"repo_graph", "dead_code_file"},
		Evidence: map[string]any{
			"incoming_ref_count":     summary.TotalCount,
			"external_non_test_refs": summary.ExternalNonTest,
			"test_ref_count":         summary.TestCount,
			"source_sample":          append([]string(nil), summary.SourceSample...),
			"contained_symbol_count": containedCount,
		},
	}
}

func makeStalePackageFinding(node repoindex.Node, containedCount int, summary deadPackageInboundSummary) finding {
	score := 68
	return finding{
		RuleID:            "stale_package_candidate",
		Category:          "package",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "Package appears orphaned from the covered graph",
		Detail:            fmt.Sprintf("%s only contains orphaned files in the covered scope and has no non-test inbound package references in the covered repo graph.", deadCodeDisplayName(node)),
		SuggestedRefactor: "Delete the package if it is obsolete, or restore the intended import path if it should still be reachable.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          deadCodeLanguage(node, nil),
		Confidence:        "medium",
		Signals:           []string{"repo_graph", "dead_code_package"},
		Evidence: map[string]any{
			"incoming_ref_count":     summary.TotalCount,
			"external_non_test_refs": summary.ExternalNonTest,
			"test_ref_count":         summary.TestCount,
			"source_sample":          append([]string(nil), summary.SourceSample...),
			"contained_file_count":   containedCount,
		},
	}
}

func makeTestOnlyPackageFinding(node repoindex.Node, containedCount int, summary deadPackageInboundSummary) finding {
	score := 64
	return finding{
		RuleID:            "test_only_package",
		Category:          "package",
		Severity:          severityFor(score),
		Score:             score,
		Title:             "Package appears test-only from the covered graph",
		Detail:            fmt.Sprintf("%s only contains test-only files in the covered scope and has no non-test inbound package references in the covered repo graph.", deadCodeDisplayName(node)),
		SuggestedRefactor: "Move the package under test-only code or remove it if the production surface no longer needs it.",
		File:              node.File,
		Line:              node.SpanStart,
		Symbol:            node.Name,
		Language:          deadCodeLanguage(node, nil),
		Confidence:        "medium",
		Signals:           []string{"repo_graph", "dead_code_package"},
		Evidence: map[string]any{
			"incoming_ref_count":     summary.TotalCount,
			"external_non_test_refs": summary.ExternalNonTest,
			"test_ref_count":         summary.TestCount,
			"source_sample":          append([]string(nil), summary.SourceSample...),
			"contained_file_count":   containedCount,
		},
	}
}

func deadCodeDisplayName(node repoindex.Node) string {
	name := strings.TrimSpace(node.Name)
	if name != "" {
		return name
	}
	if strings.TrimSpace(node.File) != "" {
		return node.File
	}
	return "symbol"
}

func deadCodeLanguage(node repoindex.Node, locator *repoindex.LocatorEntry) string {
	if locator != nil && strings.TrimSpace(locator.Pkg) != "" {
		if idx := strings.Index(locator.Pkg, ":"); idx > 0 {
			return locator.Pkg[:idx]
		}
	}
	return langutil.DetectAllowed(node.File, langutil.CommonCodeLanguages)
}

func deadCodeKind(locator *repoindex.LocatorEntry) string {
	if locator == nil {
		return ""
	}
	return strings.TrimSpace(locator.Kind)
}
