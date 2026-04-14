//go:build cgo

package main

import (
	"fmt"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	symindex "github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
)

type elixirPreloadAfterGetChain struct {
	Line          int
	RepoCall      string
	PreloadCall   string
	PreloadTarget string
	Preview       string
}

type elixirPostTransactionPreload struct {
	Line           int
	PreloadTargets []string
	Preview        string
}

type elixirTransactionScript struct {
	Line           int
	StatementCount int
	RepoCallCount  int
	BranchCount    int
	RepoCalls      []string
	Preview        string
	PipelineStages []string
	MultiStepCount int
}

func analyzeElixirPreloadAfterGetChains(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	if len(symbols) == 0 {
		return nil
	}
	findings := make([]finding, 0, 4)
	for _, sym := range symbols {
		if !supportsObservedFunctionSignals(sym, lang, content) {
			continue
		}
		body, ok := extractObservedSymbolBytes(sym, content)
		if !ok {
			continue
		}
		tree, ok := parseElixirSlopTree(body)
		if !ok {
			continue
		}
		chains := collectElixirPreloadAfterGetChains(tree.RootNode(), body)
		tree.Close()
		if len(chains) == 0 {
			continue
		}
		line := sym.StartLine + chains[0].Line - 1
		previews := make([]string, 0, len(chains))
		repoCalls := make([]string, 0, len(chains))
		preloadTargets := make([]string, 0, len(chains))
		for _, chain := range chains {
			if strings.TrimSpace(chain.Preview) != "" {
				previews = append(previews, chain.Preview)
			}
			if strings.TrimSpace(chain.RepoCall) != "" {
				repoCalls = append(repoCalls, chain.RepoCall)
			}
			if strings.TrimSpace(chain.PreloadTarget) != "" {
				preloadTargets = append(preloadTargets, chain.PreloadTarget)
			}
		}
		score := scorePreloadAfterGetChain(len(chains))
		findings = append(findings, finding{
			RuleID:            "preload_after_get_chain",
			Category:          "function",
			Severity:          severityFor(score),
			Score:             score,
			Title:             "Function loads a record and immediately preloads it",
			Detail:            sym.Name + " uses Repo.get/get_by and then immediately pipes into Repo.preload, which is a strong sign that this load shape wants one named loader/helper instead of ad hoc query chaining.",
			SuggestedRefactor: "Extract a dedicated loader that owns the exact fetch + preload contract so callsites stop rebuilding the same persistence shape inline.",
			File:              relPath,
			Line:              line,
			Symbol:            sym.Name,
			Language:          lang,
			Confidence:        "high",
			Signals:           []string{"tree_sitter", "ecto_query_shape"},
			Evidence: map[string]any{
				"chain_count":      len(chains),
				"repo_calls":       appendUniquePatternStrings(nil, repoCalls...),
				"preload_targets":  appendUniquePatternStrings(nil, preloadTargets...),
				"chain_samples":    sampleStrings(previews, 4),
				"normalized_shape": "Repo.get |> Repo.preload",
			},
		})
	}
	return findings
}

func analyzeElixirTransactionScriptHotspots(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	if len(symbols) == 0 {
		return nil
	}
	findings := make([]finding, 0, 4)
	for _, sym := range symbols {
		if !supportsObservedFunctionSignals(sym, lang, content) {
			continue
		}
		body, ok := extractObservedSymbolBytes(sym, content)
		if !ok {
			continue
		}
		tree, ok := parseElixirSlopTree(body)
		if !ok {
			continue
		}
		scripts := collectElixirTransactionScripts(tree.RootNode(), body)
		tree.Close()
		if len(scripts) == 0 {
			continue
		}
		for _, script := range scripts {
			if script.StatementCount < 3 || (script.RepoCallCount < 2 && script.MultiStepCount < 2) {
				continue
			}
			score := scoreTransactionScriptHotspot(script.StatementCount, script.RepoCallCount, script.BranchCount)
			findings = append(findings, finding{
				RuleID:            "transaction_script_hotspot",
				Category:          "function",
				Severity:          severityFor(score),
				Score:             score,
				Title:             "Function packs a complex script inside Repo.transaction",
				Detail:            sym.Name + " runs a multi-step anonymous transaction body with persistence and control flow mixed together, which is a strong sign that the transaction boundary wants smaller named steps or a dedicated workflow helper.",
				SuggestedRefactor: "Keep the transaction boundary, but extract the fetch/update/write steps into named helpers so the transaction body reads like a short workflow instead of an inlined script.",
				File:              relPath,
				Line:              sym.StartLine + script.Line - 1,
				Symbol:            sym.Name,
				Language:          lang,
				Confidence:        "high",
				Signals:           []string{"tree_sitter", "ecto_transaction_script"},
				Evidence: map[string]any{
					"statement_count":  script.StatementCount,
					"repo_call_count":  script.RepoCallCount,
					"branch_count":     script.BranchCount,
					"repo_calls":       script.RepoCalls,
					"script_preview":   script.Preview,
					"pipeline_stages":  script.PipelineStages,
					"multi_step_count": script.MultiStepCount,
				},
			})
		}
	}
	return findings
}

func analyzeElixirPostTransactionPreloads(_ string, relPath, lang string, content []byte, symbols []symindex.Symbol) []finding {
	if len(symbols) == 0 {
		return nil
	}
	findings := make([]finding, 0, 4)
	for _, sym := range symbols {
		if !supportsObservedFunctionSignals(sym, lang, content) {
			continue
		}
		body, ok := extractObservedSymbolBytes(sym, content)
		if !ok {
			continue
		}
		tree, ok := parseElixirSlopTree(body)
		if !ok {
			continue
		}
		candidates := collectElixirPostTransactionPreloads(tree.RootNode(), body)
		tree.Close()
		if len(candidates) == 0 {
			continue
		}
		for _, candidate := range candidates {
			score := scorePostTransactionPreload(len(candidate.PreloadTargets))
			findings = append(findings, finding{
				RuleID:            "post_transaction_preload",
				Category:          "function",
				Severity:          severityFor(score),
				Score:             score,
				Title:             "Function preloads records after the transaction result unwraps",
				Detail:            sym.Name + " unwraps a transaction result and then calls Repo.preload on the returned record, which is a strong sign that the write workflow and read-shaping contract want to be separated.",
				SuggestedRefactor: "Keep the transaction focused on writes, then move the post-transaction preload into a dedicated loader or explicit read-shaping helper at the boundary.",
				File:              relPath,
				Line:              sym.StartLine + candidate.Line - 1,
				Symbol:            sym.Name,
				Language:          lang,
				Confidence:        "high",
				Signals:           []string{"tree_sitter", "ecto_transaction_result_shape"},
				Evidence: map[string]any{
					"preload_targets": candidate.PreloadTargets,
					"script_preview":  candidate.Preview,
				},
			})
		}
	}
	return findings
}

func collectElixirPreloadAfterGetChains(root *sitter.Node, content []byte) []elixirPreloadAfterGetChain {
	if root == nil {
		return nil
	}
	out := make([]elixirPreloadAfterGetChain, 0, 4)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if chain, ok := elixirPreloadAfterGetChainCandidate(node, content); ok {
			out = append(out, chain)
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

func elixirPreloadAfterGetChainCandidate(node *sitter.Node, content []byte) (elixirPreloadAfterGetChain, bool) {
	if node == nil || node.Kind() != "binary_operator" {
		return elixirPreloadAfterGetChain{}, false
	}
	children := elixirNamedChildren(node)
	if len(children) < 2 {
		return elixirPreloadAfterGetChain{}, false
	}
	left := &children[0]
	right := &children[len(children)-1]
	if strings.TrimSpace(elixirBinaryOperator(node, left, right, content)) != "|>" {
		return elixirPreloadAfterGetChain{}, false
	}
	if left.Kind() != "call" || right.Kind() != "call" {
		return elixirPreloadAfterGetChain{}, false
	}
	repoCall := strings.TrimSpace(elixirCallTargetName(left, content))
	if repoCall != "Repo.get" && repoCall != "Repo.get!" && repoCall != "Repo.get_by" && repoCall != "Repo.get_by!" {
		return elixirPreloadAfterGetChain{}, false
	}
	preloadCall := strings.TrimSpace(elixirCallTargetName(right, content))
	if preloadCall != "Repo.preload" {
		return elixirPreloadAfterGetChain{}, false
	}
	preloadTarget := ""
	if args := elixirCallArgumentsLocal(right); args != nil {
		children := elixirNamedChildren(args)
		if len(children) > 0 {
			last := &children[len(children)-1]
			preloadTarget = previewElixirText(elixirNodeText(last, content))
		}
	}
	return elixirPreloadAfterGetChain{
		Line:          int(node.StartPosition().Row) + 1,
		RepoCall:      repoCall,
		PreloadCall:   preloadCall,
		PreloadTarget: preloadTarget,
		Preview:       previewElixirText(elixirNodeText(node, content)),
	}, true
}

func collectElixirTransactionScripts(root *sitter.Node, content []byte) []elixirTransactionScript {
	if root == nil {
		return nil
	}
	index := make(map[string]elixirTransactionScript)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if script, ok := elixirTransactionScriptCandidate(node, content); ok {
			key := fmt.Sprintf("%d:%s", script.Line, script.Preview)
			if existing, exists := index[key]; exists {
				if script.MultiStepCount > existing.MultiStepCount || (script.MultiStepCount == existing.MultiStepCount && script.StatementCount > existing.StatementCount) {
					index[key] = script
				}
			} else {
				index[key] = script
			}
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)
	out := make([]elixirTransactionScript, 0, len(index))
	for _, script := range index {
		out = append(out, script)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

func collectElixirPostTransactionPreloads(root *sitter.Node, content []byte) []elixirPostTransactionPreload {
	if root == nil {
		return nil
	}
	index := make(map[string]elixirPostTransactionPreload)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		if candidate, ok := elixirPostTransactionPreloadCandidate(node, content); ok {
			key := fmt.Sprintf("%d:%s", candidate.Line, candidate.Preview)
			index[key] = candidate
		}
		cursor := node.Walk()
		for _, child := range node.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)
	out := make([]elixirPostTransactionPreload, 0, len(index))
	for _, item := range index {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

func elixirPostTransactionPreloadCandidate(node *sitter.Node, content []byte) (elixirPostTransactionPreload, bool) {
	if node == nil || node.Kind() != "binary_operator" {
		return elixirPostTransactionPreload{}, false
	}
	children := elixirNamedChildren(node)
	if len(children) < 2 {
		return elixirPostTransactionPreload{}, false
	}
	left := &children[0]
	right := &children[len(children)-1]
	if strings.TrimSpace(elixirBinaryOperator(node, left, right, content)) != "|>" {
		return elixirPostTransactionPreload{}, false
	}
	if right.Kind() != "call" || strings.TrimSpace(elixirCallTargetName(right, content)) != "case" {
		return elixirPostTransactionPreload{}, false
	}
	doBlock := elixirCallDoBlock(right)
	if doBlock == nil {
		return elixirPostTransactionPreload{}, false
	}
	stages := elixirPipeStages(left, content)
	hasTransaction := false
	for _, stage := range stages {
		if stage.target == "Repo.transaction" {
			hasTransaction = true
			break
		}
	}
	if !hasTransaction {
		return elixirPostTransactionPreload{}, false
	}
	targets := make([]string, 0, 2)
	doChildren := elixirNamedChildren(doBlock)
	for i := range doChildren {
		if doChildren[i].Kind() != "stab_clause" {
			continue
		}
		stabChildren := elixirNamedChildren(&doChildren[i])
		for j := range stabChildren {
			if stabChildren[j].Kind() != "body" {
				continue
			}
			bodyChildren := elixirNamedChildren(&stabChildren[j])
			for k := range bodyChildren {
				targets = append(targets, elixirRepoPreloadTargets(&bodyChildren[k], content)...)
			}
		}
	}
	targets = appendUniquePatternStrings(nil, targets...)
	if len(targets) == 0 {
		return elixirPostTransactionPreload{}, false
	}
	return elixirPostTransactionPreload{
		Line:           int(node.StartPosition().Row) + 1,
		PreloadTargets: targets,
		Preview:        previewElixirText(elixirNodeText(node, content)),
	}, true
}

func elixirRepoPreloadTargets(node *sitter.Node, content []byte) []string {
	if node == nil {
		return nil
	}
	out := make([]string, 0, 2)
	var walk func(*sitter.Node)
	walk = func(current *sitter.Node) {
		if current == nil {
			return
		}
		if current.Kind() == "call" && strings.TrimSpace(elixirCallTargetName(current, content)) == "Repo.preload" {
			if args := elixirCallArgumentsLocal(current); args != nil {
				children := elixirNamedChildren(args)
				if len(children) > 0 {
					last := &children[len(children)-1]
					out = append(out, previewElixirText(elixirNodeText(last, content)))
				} else {
					out = append(out, "Repo.preload")
				}
			} else {
				out = append(out, "Repo.preload")
			}
		}
		cursor := current.Walk()
		for _, child := range current.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(node)
	return appendUniquePatternStrings(nil, out...)
}

func elixirTransactionScriptCandidate(node *sitter.Node, content []byte) (elixirTransactionScript, bool) {
	if node == nil {
		return elixirTransactionScript{}, false
	}
	if script, ok := elixirAnonymousTransactionScriptCandidate(node, content); ok {
		return script, true
	}
	return elixirMultiTransactionScriptCandidate(node, content)
}

func elixirAnonymousTransactionScriptCandidate(node *sitter.Node, content []byte) (elixirTransactionScript, bool) {
	if node == nil || node.Kind() != "call" {
		return elixirTransactionScript{}, false
	}
	if strings.TrimSpace(elixirCallTargetName(node, content)) != "Repo.transaction" {
		return elixirTransactionScript{}, false
	}
	args := elixirCallArgumentsLocal(node)
	if args == nil {
		return elixirTransactionScript{}, false
	}
	argChildren := elixirNamedChildren(args)
	if len(argChildren) == 0 {
		return elixirTransactionScript{}, false
	}
	fnNode := &argChildren[0]
	if fnNode.Kind() != "anonymous_function" {
		return elixirTransactionScript{}, false
	}
	body := elixirAnonymousFunctionBody(fnNode)
	if body == nil {
		return elixirTransactionScript{}, false
	}
	statements := elixirNamedChildren(body)
	statementCount := len(statements)
	if statementCount == 0 {
		return elixirTransactionScript{}, false
	}
	repoCalls, branchCount := elixirTransactionBodyStats(body, content)
	return elixirTransactionScript{
		Line:           int(node.StartPosition().Row) + 1,
		StatementCount: statementCount,
		RepoCallCount:  len(repoCalls),
		BranchCount:    branchCount,
		RepoCalls:      sampleStrings(repoCalls, 6),
		Preview:        previewElixirText(elixirNodeText(node, content)),
	}, true
}

func elixirMultiTransactionScriptCandidate(node *sitter.Node, content []byte) (elixirTransactionScript, bool) {
	if node == nil || node.Kind() != "binary_operator" {
		return elixirTransactionScript{}, false
	}
	stages := elixirPipeStages(node, content)
	if len(stages) < 3 {
		return elixirTransactionScript{}, false
	}
	transactionIdx := -1
	for i, stage := range stages {
		if stage.target == "Repo.transaction" {
			transactionIdx = i
			break
		}
	}
	if transactionIdx <= 0 {
		return elixirTransactionScript{}, false
	}
	multiStageCount := 0
	repoCalls := make([]string, 0, 6)
	pipelineNames := make([]string, 0, len(stages))
	for i, stage := range stages[:transactionIdx] {
		if strings.HasPrefix(stage.target, "Ecto.Multi.") || strings.HasPrefix(stage.target, "Multi.") {
			if i > 0 || !strings.HasSuffix(stage.target, ".new") {
				multiStageCount++
			}
		}
		if strings.HasPrefix(stage.target, "Repo.") {
			repoCalls = appendUniquePatternStrings(repoCalls, stage.target)
		}
		if stage.target != "" {
			pipelineNames = append(pipelineNames, stage.target)
		}
	}
	repoCalls = appendUniquePatternStrings(repoCalls, "Repo.transaction")
	if multiStageCount < 2 {
		return elixirTransactionScript{}, false
	}
	runSteps := 0
	for _, stage := range stages[:transactionIdx] {
		if strings.HasSuffix(stage.target, ".run") {
			runSteps++
		}
	}
	return elixirTransactionScript{
		Line:           stages[transactionIdx].line,
		StatementCount: transactionIdx + 1,
		RepoCallCount:  len(repoCalls),
		BranchCount:    runSteps,
		RepoCalls:      sampleStrings(repoCalls, 6),
		Preview:        previewElixirText(elixirNodeText(node, content)),
		PipelineStages: sampleStrings(pipelineNames, 8),
		MultiStepCount: multiStageCount,
	}, true
}

type elixirPipeStage struct {
	line   int
	target string
	node   *sitter.Node
}

func elixirPipeStages(node *sitter.Node, content []byte) []elixirPipeStage {
	if node == nil {
		return nil
	}
	if node.Kind() != "binary_operator" {
		return []elixirPipeStage{{
			line:   int(node.StartPosition().Row) + 1,
			target: strings.TrimSpace(elixirCallTargetName(node, content)),
			node:   node,
		}}
	}
	children := elixirNamedChildren(node)
	if len(children) < 2 {
		return []elixirPipeStage{{
			line:   int(node.StartPosition().Row) + 1,
			target: strings.TrimSpace(elixirCallTargetName(node, content)),
			node:   node,
		}}
	}
	left := &children[0]
	right := &children[len(children)-1]
	if strings.TrimSpace(elixirBinaryOperator(node, left, right, content)) != "|>" {
		return []elixirPipeStage{{
			line:   int(node.StartPosition().Row) + 1,
			target: strings.TrimSpace(elixirCallTargetName(node, content)),
			node:   node,
		}}
	}
	stages := elixirPipeStages(left, content)
	stages = append(stages, elixirPipeStage{
		line:   int(right.StartPosition().Row) + 1,
		target: strings.TrimSpace(elixirCallTargetName(right, content)),
		node:   right,
	})
	return stages
}

func elixirAnonymousFunctionBody(node *sitter.Node) *sitter.Node {
	if node == nil || node.Kind() != "anonymous_function" {
		return nil
	}
	children := elixirNamedChildren(node)
	for i := range children {
		if children[i].Kind() != "stab_clause" {
			continue
		}
		stabChildren := elixirNamedChildren(&children[i])
		for j := range stabChildren {
			if stabChildren[j].Kind() == "body" {
				c := stabChildren[j]
				return &c
			}
		}
	}
	return nil
}

func elixirTransactionBodyStats(node *sitter.Node, content []byte) ([]string, int) {
	repoCalls := make([]string, 0, 4)
	branchCount := 0
	var walk func(*sitter.Node)
	walk = func(current *sitter.Node) {
		if current == nil {
			return
		}
		if current.Kind() == "call" {
			target := strings.TrimSpace(elixirCallTargetName(current, content))
			switch target {
			case "Repo.get", "Repo.get!", "Repo.get_by", "Repo.get_by!", "Repo.preload",
				"Repo.insert", "Repo.insert!", "Repo.insert_or_update", "Repo.insert_or_update!",
				"Repo.update", "Repo.update!", "Repo.delete", "Repo.delete!", "Repo.delete_all",
				"Repo.update_all", "Repo.all", "Repo.one", "Repo.exists?", "Repo.rollback":
				repoCalls = appendUniquePatternStrings(repoCalls, target)
			case "if", "case", "cond", "with":
				branchCount++
			}
		}
		cursor := current.Walk()
		for _, child := range current.NamedChildren(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(node)
	return repoCalls, branchCount
}
