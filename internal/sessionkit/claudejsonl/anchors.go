package claudejsonl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	codePreviewLen  = 200
	errorPreviewLen = 300
)

var (
	codeFenceRe = regexp.MustCompile("(?s)```([A-Za-z0-9_.+-]*)[ \\t]*\\n(.*?)\\n```")

	textPathRe = regexp.MustCompile(`(?:/[A-Za-z0-9._~\-/*?\[\]]+|internal/[A-Za-z0-9._~\-/*?\[\]]+)`)

	goFuncRe   = regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	goStructRe = regexp.MustCompile(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\s+struct\b`)
	goTypeRe   = regexp.MustCompile(`(?m)^\s*type\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	goConstRe  = regexp.MustCompile(`(?m)^\s*const\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	goVarRe    = regexp.MustCompile(`(?m)^\s*var\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

	tsFunctionRe  = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	tsClassRe     = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:default\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	tsInterfaceRe = regexp.MustCompile(`(?m)^\s*(?:export\s+)?interface\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	tsExportRe    = regexp.MustCompile(`(?m)^\s*export\s+(?:const|let|var|type|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
)

// SliceAnchor represents a deterministic extraction from a conversation turn.
type SliceAnchor struct {
	Type    AnchorType `json:"type"`
	Content string     `json:"content,omitempty"`
	Meta    AnchorMeta `json:"meta,omitempty"`
}

// AnchorType is the type of extracted slice anchor.
type AnchorType string

const (
	AnchorCodeBlock AnchorType = "code_block"
	AnchorCommand   AnchorType = "command"
	AnchorError     AnchorType = "error"
	AnchorFilePath  AnchorType = "file_path"
	AnchorSymbol    AnchorType = "symbol"
)

// AnchorMeta contains optional type-specific metadata for an anchor.
type AnchorMeta struct {
	Language    string `json:"language,omitempty"`
	Tool        string `json:"tool,omitempty"`
	ExitCode    *int   `json:"exit_code,omitempty"`
	ErrorType   string `json:"error_type,omitempty"`
	SymbolKind  string `json:"symbol_kind,omitempty"`
	FilePath    string `json:"file_path,omitempty"`
	Preview     string `json:"preview,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// BashInput is the typed input for the Bash tool.
type BashInput struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

// ReadInput is the typed input for the Read tool.
type ReadInput struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// WriteInput is the typed input for the Write tool.
type WriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// EditInput is the typed input for the Edit tool.
type EditInput struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// GlobInput is the typed input for the Glob tool.
type GlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// GrepInput is the typed input for the Grep tool.
type GrepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Type    string `json:"type,omitempty"`
}

type rawContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Name      string          `json:"name,omitempty"`
	ID        string          `json:"id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type extractedCodeBlock struct {
	Language string
	Content  string
	Start    int
}

type extractedSymbol struct {
	Kind string
	Name string
}

// ExtractCodeBlocks extracts fenced code blocks from assistant text.
func ExtractCodeBlocks(msg *Message) []SliceAnchor {
	text := extractAssistantText(msg)
	if text == "" {
		return nil
	}

	codeBlocks := extractFencedCodeBlocks(text)
	if len(codeBlocks) == 0 {
		return nil
	}

	anchors := make([]SliceAnchor, 0, len(codeBlocks))
	for _, block := range codeBlocks {
		content := block.Content
		hash := sha256.Sum256([]byte(content))
		anchors = append(anchors, SliceAnchor{
			Type:    AnchorCodeBlock,
			Content: content,
			Meta: AnchorMeta{
				Language:    normalizeLanguage(block.Language),
				Preview:     truncate(content, codePreviewLen),
				ContentHash: hex.EncodeToString(hash[:]),
			},
		})
	}

	return anchors
}

// ExtractCommands extracts Bash command anchors from tool_use blocks.
func ExtractCommands(msg *Message) []SliceAnchor {
	if msg == nil {
		return nil
	}

	anchors := make([]SliceAnchor, 0, 4)
	seen := make(map[string]struct{})

	appendCommand := func(toolName string, input json.RawMessage) {
		command := parseBashCommand(input)
		if command == "" {
			return
		}
		key := toolName + "|" + command
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		anchors = append(anchors, SliceAnchor{
			Type:    AnchorCommand,
			Content: command,
			Meta: AnchorMeta{
				Tool: toolName,
			},
		})
	}

	if msg.ToolUse != nil && strings.EqualFold(msg.ToolUse.Name, "bash") {
		appendCommand(msg.ToolUse.Name, msg.ToolUse.Input)
	}

	_, blocks, ok := parseNestedRawBlocks(msg)
	if ok {
		for _, block := range blocks {
			if block.Type != "tool_use" || !strings.EqualFold(block.Name, "bash") {
				continue
			}
			appendCommand(block.Name, block.Input)
		}
	}

	if len(anchors) == 0 {
		return nil
	}

	return anchors
}

// ExtractFilePaths extracts file path anchors from tool inputs and assistant text.
func ExtractFilePaths(msg *Message) []SliceAnchor {
	if msg == nil {
		return nil
	}

	paths := make(map[string]struct{})
	addPath := func(candidate string) {
		path := normalizePath(candidate)
		if path == "" {
			return
		}
		paths[path] = struct{}{}
	}

	if msg.ToolUse != nil {
		for _, path := range extractPathsFromToolInput(msg.ToolUse.Name, msg.ToolUse.Input) {
			addPath(path)
		}
	}

	_, blocks, ok := parseNestedRawBlocks(msg)
	if ok {
		for _, block := range blocks {
			if block.Type != "tool_use" {
				continue
			}
			for _, path := range extractPathsFromToolInput(block.Name, block.Input) {
				addPath(path)
			}
		}
	}

	text := extractAssistantText(msg)
	for _, path := range extractPathsFromText(text) {
		addPath(path)
	}

	if len(paths) == 0 {
		return nil
	}

	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	anchors := make([]SliceAnchor, 0, len(ordered))
	for _, path := range ordered {
		anchors = append(anchors, SliceAnchor{
			Type:    AnchorFilePath,
			Content: path,
			Meta: AnchorMeta{
				FilePath: path,
			},
		})
	}

	return anchors
}

// ExtractErrors extracts error anchors from tool_result payloads.
func ExtractErrors(msg *Message) []SliceAnchor {
	if msg == nil {
		return nil
	}

	anchors := make([]SliceAnchor, 0, 2)
	seen := make(map[string]struct{})
	toolNames := buildToolNameLookup(msg)

	appendError := func(content, toolUseID string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		toolName := toolNames[toolUseID]
		if toolName == "" && msg.ToolUse != nil {
			toolName = msg.ToolUse.Name
		}
		errorType := classifyErrorContent(content)
		key := errorType + "|" + toolName + "|" + content
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		anchors = append(anchors, SliceAnchor{
			Type:    AnchorError,
			Content: content,
			Meta: AnchorMeta{
				Tool:      toolName,
				ErrorType: errorType,
				Preview:   truncate(content, errorPreviewLen),
			},
		})
	}

	if msg.ToolResult != nil && msg.ToolResult.IsError {
		appendError(msg.ToolResult.Content, msg.ToolResult.ToolUseID)
	}

	_, blocks, ok := parseNestedRawBlocks(msg)
	if ok {
		for _, block := range blocks {
			if block.Type != "tool_result" || !block.IsError {
				continue
			}
			appendError(rawContentToString(block.Content), block.ToolUseID)
		}
	}

	if len(anchors) == 0 {
		return nil
	}

	return anchors
}

// ExtractSymbols extracts language symbols from fenced code blocks.
func ExtractSymbols(msg *Message) []SliceAnchor {
	text := extractAssistantText(msg)
	if text == "" {
		return nil
	}

	codeBlocks := extractFencedCodeBlocks(text)
	if len(codeBlocks) == 0 {
		return nil
	}

	anchors := make([]SliceAnchor, 0, 8)
	seen := make(map[string]struct{})
	for _, codeBlock := range codeBlocks {
		language := normalizeLanguage(codeBlock.Language)
		filePath := findNearestFilePath(text, codeBlock.Start, language)

		var symbols []extractedSymbol
		switch {
		case isGoLanguage(language):
			symbols = extractGoSymbols(codeBlock.Content)
		case isTypeScriptLanguage(language):
			symbols = extractTypeScriptSymbols(codeBlock.Content)
		default:
			if looksLikeGo(codeBlock.Content) {
				symbols = append(symbols, extractGoSymbols(codeBlock.Content)...)
			}
			if looksLikeTypeScript(codeBlock.Content) {
				symbols = append(symbols, extractTypeScriptSymbols(codeBlock.Content)...)
			}
		}

		for _, symbol := range symbols {
			key := symbol.Kind + "|" + symbol.Name + "|" + filePath
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			anchors = append(anchors, SliceAnchor{
				Type:    AnchorSymbol,
				Content: symbol.Name,
				Meta: AnchorMeta{
					SymbolKind: symbol.Kind,
					FilePath:   filePath,
				},
			})
		}
	}

	if len(anchors) == 0 {
		return nil
	}

	return anchors
}

// ExtractAllAnchors extracts and deduplicates all anchor types for a message.
func ExtractAllAnchors(msg *Message) []SliceAnchor {
	if msg == nil {
		return nil
	}

	var all []SliceAnchor
	all = append(all, ExtractCodeBlocks(msg)...)
	all = append(all, ExtractCommands(msg)...)
	all = append(all, ExtractFilePaths(msg)...)
	all = append(all, ExtractErrors(msg)...)
	all = append(all, ExtractSymbols(msg)...)

	if len(all) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	deduped := make([]SliceAnchor, 0, len(all))
	for _, anchor := range all {
		key := anchorKey(anchor)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, anchor)
	}

	return deduped
}

// parseNestedContent extracts content blocks from the Message.Message field.
func parseNestedContent(msg *Message) (role string, blocks []ContentBlock, text string, ok bool) {
	if msg == nil || len(msg.Message) == 0 {
		return "", nil, "", false
	}

	var nested NestedMessage
	if err := json.Unmarshal(msg.Message, &nested); err != nil {
		return "", nil, "", false
	}

	role = nested.Role
	if len(nested.Content) == 0 {
		return role, nil, "", true
	}

	if err := json.Unmarshal(nested.Content, &text); err == nil {
		return role, nil, strings.TrimSpace(text), true
	}

	if err := json.Unmarshal(nested.Content, &blocks); err == nil {
		textParts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		}
		return role, blocks, strings.TrimSpace(strings.Join(textParts, "\n")), true
	}

	return role, nil, "", false
}

func parseNestedRawBlocks(msg *Message) (role string, blocks []rawContentBlock, ok bool) {
	if msg == nil || len(msg.Message) == 0 {
		return "", nil, false
	}

	var nested NestedMessage
	if err := json.Unmarshal(msg.Message, &nested); err != nil {
		return "", nil, false
	}

	role = nested.Role
	if len(nested.Content) == 0 {
		return role, nil, true
	}

	if err := json.Unmarshal(nested.Content, &blocks); err != nil {
		return role, nil, false
	}

	return role, blocks, true
}

func extractAssistantText(msg *Message) string {
	if msg == nil {
		return ""
	}

	role, blocks, text, ok := parseNestedContent(msg)
	if ok && isAssistant(role, msg) {
		if text != "" {
			return text
		}
		if len(blocks) > 0 {
			textParts := make([]string, 0, len(blocks))
			for _, block := range blocks {
				if block.Type == "text" && block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			}
			return strings.TrimSpace(strings.Join(textParts, "\n"))
		}
	}

	if !isAssistant("", msg) || len(msg.Content) == 0 {
		return ""
	}

	var topLevelText string
	if err := json.Unmarshal(msg.Content, &topLevelText); err == nil {
		return strings.TrimSpace(topLevelText)
	}

	var topBlocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &topBlocks); err == nil {
		textParts := make([]string, 0, len(topBlocks))
		for _, block := range topBlocks {
			if block.Type == "text" && block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		}
		return strings.TrimSpace(strings.Join(textParts, "\n"))
	}

	return ""
}

func isAssistant(nestedRole string, msg *Message) bool {
	switch {
	case nestedRole != "":
		return nestedRole == "assistant"
	case msg == nil:
		return false
	default:
		return msg.Role == "assistant" || msg.Type == "assistant"
	}
}

func extractFencedCodeBlocks(text string) []extractedCodeBlock {
	if text == "" {
		return nil
	}

	matches := codeFenceRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}

	codeBlocks := make([]extractedCodeBlock, 0, len(matches))
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		codeBlocks = append(codeBlocks, extractedCodeBlock{
			Language: text[match[2]:match[3]],
			Content:  text[match[4]:match[5]],
			Start:    match[0],
		})
	}

	return codeBlocks
}

func parseBashCommand(input json.RawMessage) string {
	var bashInput BashInput
	if err := json.Unmarshal(input, &bashInput); err == nil && strings.TrimSpace(bashInput.Command) != "" {
		return strings.TrimSpace(bashInput.Command)
	}

	var generic map[string]any
	if err := json.Unmarshal(input, &generic); err == nil {
		if command, ok := generic["command"].(string); ok {
			return strings.TrimSpace(command)
		}
	}

	return ""
}

func extractPathsFromToolInput(toolName string, input json.RawMessage) []string {
	normalizedName := strings.ToLower(strings.TrimSpace(toolName))
	paths := make([]string, 0, 2)

	switch normalizedName {
	case "read":
		var in ReadInput
		if json.Unmarshal(input, &in) == nil && in.FilePath != "" {
			paths = append(paths, in.FilePath)
		}
	case "write":
		var in WriteInput
		if json.Unmarshal(input, &in) == nil && in.FilePath != "" {
			paths = append(paths, in.FilePath)
		}
	case "edit":
		var in EditInput
		if json.Unmarshal(input, &in) == nil && in.FilePath != "" {
			paths = append(paths, in.FilePath)
		}
	case "glob":
		var in GlobInput
		if json.Unmarshal(input, &in) == nil {
			if in.Path != "" {
				paths = append(paths, in.Path)
			}
			if in.Pattern != "" {
				paths = append(paths, in.Pattern)
			}
		}
	case "grep":
		var in GrepInput
		if json.Unmarshal(input, &in) == nil {
			if in.Path != "" {
				paths = append(paths, in.Path)
			}
			if in.Pattern != "" {
				paths = append(paths, in.Pattern)
			}
		}
	}

	if len(paths) > 0 {
		return paths
	}

	var generic map[string]any
	if err := json.Unmarshal(input, &generic); err != nil {
		return nil
	}

	for _, key := range []string{"file_path", "path", "pattern"} {
		if raw, exists := generic[key]; exists {
			if str, ok := raw.(string); ok {
				paths = append(paths, str)
			}
		}
	}

	return paths
}

func extractPathsFromText(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	matches := textPathRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	paths := make([]string, 0, len(matches))
	seen := make(map[string]struct{})
	for _, match := range matches {
		path := normalizePath(match)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	return paths
}

func normalizePath(path string) string {
	trimmed := strings.TrimSpace(path)
	trimmed = strings.Trim(trimmed, "`\"'")
	trimmed = strings.TrimRight(trimmed, ".,;:!?)]}")
	if trimmed == "" {
		return ""
	}

	switch {
	case strings.HasPrefix(trimmed, "/"):
		return trimmed
	case strings.HasPrefix(trimmed, "internal/"):
		return trimmed
	case strings.Contains(trimmed, "/"):
		return trimmed
	case strings.ContainsAny(trimmed, "*?["):
		return trimmed
	default:
		return ""
	}
}

func buildToolNameLookup(msg *Message) map[string]string {
	lookup := make(map[string]string)
	if msg == nil {
		return lookup
	}

	if msg.ToolUse != nil && msg.ToolUse.ID != "" && msg.ToolUse.Name != "" {
		lookup[msg.ToolUse.ID] = msg.ToolUse.Name
	}

	_, blocks, ok := parseNestedRawBlocks(msg)
	if !ok {
		return lookup
	}

	for _, block := range blocks {
		if block.Type != "tool_use" || block.ID == "" || block.Name == "" {
			continue
		}
		lookup[block.ID] = block.Name
	}

	return lookup
}

func rawContentToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err == nil {
		textParts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			if value, ok := block["text"].(string); ok && strings.TrimSpace(value) != "" {
				textParts = append(textParts, strings.TrimSpace(value))
			}
			if value, ok := block["content"].(string); ok && strings.TrimSpace(value) != "" {
				textParts = append(textParts, strings.TrimSpace(value))
			}
		}
		return strings.Join(textParts, "\n")
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		if value, ok := obj["text"].(string); ok {
			return strings.TrimSpace(value)
		}
		if value, ok := obj["content"].(string); ok {
			return strings.TrimSpace(value)
		}
	}

	fallback := strings.TrimSpace(string(raw))
	if fallback == "null" {
		return ""
	}
	return fallback
}

func classifyErrorContent(content string) string {
	switch {
	case strings.Contains(content, "TypeError"):
		return "TypeError"
	case strings.Contains(content, "SyntaxError"):
		return "SyntaxError"
	case strings.Contains(strings.ToLower(content), "compile"), strings.Contains(strings.ToLower(content), "build"):
		return "CompileError"
	default:
		return "ToolError"
	}
}

func extractGoSymbols(code string) []extractedSymbol {
	symbols := make([]extractedSymbol, 0, 8)
	seen := make(map[string]struct{})
	add := func(kind, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := kind + "|" + name
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		symbols = append(symbols, extractedSymbol{Kind: kind, Name: name})
	}

	for _, match := range goFuncRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("func", match[1])
		}
	}
	for _, match := range goStructRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("struct", match[1])
		}
	}
	for _, match := range goTypeRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("type", match[1])
		}
	}
	for _, match := range goConstRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("const", match[1])
		}
	}
	for _, match := range goVarRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("var", match[1])
		}
	}

	return symbols
}

func extractTypeScriptSymbols(code string) []extractedSymbol {
	symbols := make([]extractedSymbol, 0, 8)
	seen := make(map[string]struct{})
	add := func(kind, name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := kind + "|" + name
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		symbols = append(symbols, extractedSymbol{Kind: kind, Name: name})
	}

	for _, match := range tsFunctionRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("function", match[1])
		}
	}
	for _, match := range tsClassRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("class", match[1])
		}
	}
	for _, match := range tsInterfaceRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("interface", match[1])
		}
	}
	for _, match := range tsExportRe.FindAllStringSubmatch(code, -1) {
		if len(match) > 1 {
			add("export", match[1])
		}
	}

	return symbols
}

func findNearestFilePath(text string, codeStart int, language string) string {
	if text == "" {
		return ""
	}

	windowStart := codeStart - 500
	if windowStart < 0 {
		windowStart = 0
	}

	paths := extractPathsFromText(text[windowStart:codeStart])
	if len(paths) == 0 {
		paths = extractPathsFromText(text)
	}
	if len(paths) == 0 {
		return ""
	}

	preferredExt := ""
	switch {
	case isGoLanguage(language):
		preferredExt = ".go"
	case isTypeScriptLanguage(language):
		preferredExt = ".ts"
	}

	if preferredExt != "" {
		for index := len(paths) - 1; index >= 0; index-- {
			path := paths[index]
			if strings.HasSuffix(path, preferredExt) || strings.HasSuffix(path, preferredExt+"x") {
				return path
			}
		}
	}

	return paths[len(paths)-1]
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func isGoLanguage(language string) bool {
	return language == "go" || language == "golang"
}

func isTypeScriptLanguage(language string) bool {
	switch language {
	case "ts", "tsx", "typescript", "js", "jsx", "javascript":
		return true
	default:
		return false
	}
}

func looksLikeGo(code string) bool {
	return goFuncRe.MatchString(code) || goTypeRe.MatchString(code) || goConstRe.MatchString(code)
}

func looksLikeTypeScript(code string) bool {
	return tsFunctionRe.MatchString(code) || tsClassRe.MatchString(code) || tsInterfaceRe.MatchString(code) || tsExportRe.MatchString(code)
}

func truncate(content string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen]
}

func anchorKey(anchor SliceAnchor) string {
	exitCode := ""
	if anchor.Meta.ExitCode != nil {
		exitCode = fmt.Sprintf("%d", *anchor.Meta.ExitCode)
	}
	return strings.Join([]string{
		string(anchor.Type),
		anchor.Content,
		anchor.Meta.Language,
		anchor.Meta.Tool,
		exitCode,
		anchor.Meta.ErrorType,
		anchor.Meta.SymbolKind,
		anchor.Meta.FilePath,
		anchor.Meta.Preview,
		anchor.Meta.ContentHash,
	}, "|")
}
