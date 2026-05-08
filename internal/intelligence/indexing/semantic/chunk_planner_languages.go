package semantic

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/codecontext/expander"
)

const (
	chunkKindTypeScriptFunction = "typescript_function"
	chunkKindTypeScriptClass    = "typescript_class"
	chunkKindTypeScriptType     = "typescript_type"
	chunkKindJavaScriptFunction = "javascript_function"
	chunkKindJavaScriptClass    = "javascript_class"
	chunkKindJavaScriptType     = "javascript_type"
	chunkKindPythonFunction     = "python_function"
	chunkKindPythonClass        = "python_class"
	chunkKindRustFunction       = "rust_function"
	chunkKindRustMethod         = "rust_method"
	chunkKindRustType           = "rust_type"
	chunkKindElixirModule       = "elixir_module"
	chunkKindElixirFunction     = "elixir_function"
	chunkKindElixirType         = "elixir_type"
	chunkKindCSharpClass        = "csharp_class"
	chunkKindCSharpFunction     = "csharp_function"
	chunkKindCSharpType         = "csharp_type"
)

var (
	jstsFunctionPattern  = regexp.MustCompile(`^\s*(?:export\s+(?:default\s+)?)?(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	jstsClassPattern     = regexp.MustCompile(`^\s*(?:export\s+(?:default\s+)?)?(?:abstract\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	jstsInterfacePattern = regexp.MustCompile(
		`^\s*(?:export\s+)?interface\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`,
	)
	jstsTypePattern      = regexp.MustCompile(`^\s*(?:export\s+)?(?:type|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	jstsConstFuncPattern = regexp.MustCompile(
		`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:async\s*)?(?:function\b|\([^)]*\)\s*=>|[A-Za-z_$][A-Za-z0-9_$]*\s*=>)`,
	)

	pythonFunctionPattern = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pythonClassPattern    = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)

	rustFunctionPattern = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?(?:const\s+)?(?:unsafe\s+)?(?:extern\s+"[^"]+"\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*[<(]`)
	rustTypePattern     = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait|type)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rustImplPattern     = regexp.MustCompile(`^\s*(?:unsafe\s+)?impl\b`)
	rustIdentifier      = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

	elixirModulePattern   = regexp.MustCompile(`^\s*(?:defmodule|defprotocol|defimpl)\s+([A-Z][A-Za-z0-9_.]*)\s+do\b`)
	elixirFunctionPattern = regexp.MustCompile(`^\s*(?:def|defp|defmacro|defmacrop)\s+([a-z_][a-z0-9_?!]*)\b`)
	elixirTypePattern     = regexp.MustCompile(`^\s*@(?:type|typep|callback)\s+([a-z_][a-z0-9_?!]*)\b`)
	elixirEndPattern      = regexp.MustCompile(`^\s*end\b`)
	elixirBlockPattern    = regexp.MustCompile(`\b(do|fn)\b`)

	csharpTypePattern = regexp.MustCompile(
		`^\s*(?:(?:public|private|protected|internal|sealed|static|abstract|partial|readonly|unsafe)\s+)*(class|interface|struct|enum|record)\s+([A-Za-z_][A-Za-z0-9_]*)\b`,
	)
	csharpMethodPattern = regexp.MustCompile(
		`^\s*(?:(?:public|private|protected|internal|static|async|virtual|override|sealed|partial|readonly|unsafe|extern)\s+)*(?:[A-Za-z_][A-Za-z0-9_<>,\[\]?\.]*\s+)+([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	)
)

type chunkLineSpan struct {
	kind      string
	symbol    string
	startLine int
	endLine   int
}

type chunkContainerRange struct {
	name      string
	startLine int
	endLine   int
	indent    int
}

func chunkPlannerFor(path, language string) chunkPlannerAdapter {
	switch normalizeChunkPlannerLanguage(path, language) {
	case "go":
		return goChunkPlanner{}
	case "typescript":
		return jstsChunkPlanner{language: "typescript"}
	case "javascript":
		return jstsChunkPlanner{language: "javascript"}
	case "python":
		return pythonChunkPlanner{}
	case "rust":
		return rustChunkPlanner{}
	case "elixir":
		return elixirChunkPlanner{}
	case "csharp":
		return csharpChunkPlanner{}
	default:
		return nil
	}
}

func normalizeChunkPlannerLanguage(path, language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "":
		return chunkLanguageFromPath(path)
	case "auto", "text":
		if lang := chunkLanguageFromPath(path); lang != "" {
			return lang
		}
		return strings.ToLower(strings.TrimSpace(language))
	case "go", "python", "typescript", "javascript", "elixir", "rust", "csharp":
		return strings.ToLower(strings.TrimSpace(language))
	case "py":
		return "python"
	case "ts", "tsx", "mts", "cts":
		return "typescript"
	case "js", "jsx", "mjs", "cjs":
		return "javascript"
	case "ex", "exs":
		return "elixir"
	case "rs":
		return "rust"
	case "cs", "c#":
		return "csharp"
	default:
		return strings.ToLower(strings.TrimSpace(language))
	}
}

func chunkLanguageFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py", ".pyw", ".pyi":
		return "python"
	case ".ts", ".tsx", ".mts", ".cts":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".ex", ".exs":
		return "elixir"
	case ".rs":
		return "rust"
	case ".cs":
		return "csharp"
	default:
		return ""
	}
}

type jstsChunkPlanner struct {
	language string
}

func (p jstsChunkPlanner) Plan(path, digest string, content []byte, maxBytes int) []Chunk {
	lines, offsets := splitChunkPlannerLines(content)
	spans := make([]chunkLineSpan, 0, 8)
	for i, line := range lines {
		name, kind, blockLike, ok := p.parseDeclaration(line)
		if !ok {
			continue
		}
		endLine := i
		if blockLike {
			if braceLine := findBraceLine(lines, i); braceLine >= 0 {
				if end := expander.FindBraceEnd(lines, braceLine, expander.JSBraceStyle()); end >= braceLine {
					endLine = end
				}
			}
		}
		if (kind == chunkKindTypeScriptClass || kind == chunkKindJavaScriptClass ||
			kind == chunkKindTypeScriptType || kind == chunkKindJavaScriptType) &&
			!lineSpanFitsMax(lines, offsets, len(content), i, endLine, maxBytes) {
			continue
		}
		spans = append(spans, chunkLineSpan{
			kind:      kind,
			symbol:    name,
			startLine: i,
			endLine:   endLine,
		})
	}
	return chunksFromLineSpans(path, digest, content, lines, offsets, spans)
}

func (p jstsChunkPlanner) parseDeclaration(line string) (string, string, bool, bool) {
	if match := jstsFunctionPattern.FindStringSubmatch(line); match != nil {
		return match[1], languageChunkKind(p.language, "function"), true, true
	}
	if match := jstsClassPattern.FindStringSubmatch(line); match != nil {
		return match[1], languageChunkKind(p.language, "class"), true, true
	}
	if match := jstsInterfacePattern.FindStringSubmatch(line); match != nil {
		return match[1], languageChunkKind(p.language, "type"), true, true
	}
	if match := jstsTypePattern.FindStringSubmatch(line); match != nil {
		return match[1], languageChunkKind(p.language, "type"), strings.Contains(line, "{"), true
	}
	if match := jstsConstFuncPattern.FindStringSubmatch(line); match != nil {
		return match[1], languageChunkKind(p.language, "function"), strings.Contains(line, "{"), true
	}
	return "", "", false, false
}

type pythonChunkPlanner struct{}

func (pythonChunkPlanner) Plan(path, digest string, content []byte, maxBytes int) []Chunk {
	lines, offsets := splitChunkPlannerLines(content)
	spans := make([]chunkLineSpan, 0, 8)
	classes := make([]chunkContainerRange, 0, 4)
	for i, line := range lines {
		match := pythonClassPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		startLine := includePythonDecorators(lines, i)
		endLine := expander.FindBlockByIndentation(lines, i, "#")
		classes = append(classes, chunkContainerRange{
			name:      match[1],
			startLine: i,
			endLine:   endLine,
			indent:    expander.CountLeadingWhitespace(line),
		})
		if lineSpanFitsMax(lines, offsets, len(content), startLine, endLine, maxBytes) {
			spans = append(spans, chunkLineSpan{
				kind:      chunkKindPythonClass,
				symbol:    match[1],
				startLine: startLine,
				endLine:   endLine,
			})
		}
	}

	for i, line := range lines {
		match := pythonFunctionPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		symbol := match[1]
		if className := containingIndentContainer(classes, i, expander.CountLeadingWhitespace(line)); className != "" {
			symbol = className + "." + symbol
		}
		spans = append(spans, chunkLineSpan{
			kind:      chunkKindPythonFunction,
			symbol:    symbol,
			startLine: includePythonDecorators(lines, i),
			endLine:   expander.FindBlockByIndentation(lines, i, "#"),
		})
	}
	return chunksFromLineSpans(path, digest, content, lines, offsets, spans)
}

type rustChunkPlanner struct{}

func (rustChunkPlanner) Plan(path, digest string, content []byte, maxBytes int) []Chunk {
	lines, offsets := splitChunkPlannerLines(content)
	spans := make([]chunkLineSpan, 0, 8)
	impls := make([]chunkContainerRange, 0, 4)
	for i, line := range lines {
		trimmed := strings.TrimSpace(stripLineComment(line, "//"))
		if !rustImplPattern.MatchString(trimmed) {
			continue
		}
		name := parseRustImplName(trimmed)
		if name == "" {
			continue
		}
		endLine := braceBlockEndLine(lines, i, expander.DefaultBraceStyle())
		impls = append(impls, chunkContainerRange{name: name, startLine: i, endLine: endLine})
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(stripLineComment(line, "//"))
		if match := rustFunctionPattern.FindStringSubmatch(trimmed); match != nil {
			symbol := match[1]
			kind := chunkKindRustFunction
			if implName := containingLineContainer(impls, i); implName != "" {
				symbol = implName + "." + symbol
				kind = chunkKindRustMethod
			}
			spans = append(spans, chunkLineSpan{
				kind:      kind,
				symbol:    symbol,
				startLine: i,
				endLine:   braceBlockEndLine(lines, i, expander.DefaultBraceStyle()),
			})
			continue
		}
		if match := rustTypePattern.FindStringSubmatch(trimmed); match != nil {
			endLine := braceBlockEndLine(lines, i, expander.DefaultBraceStyle())
			if !lineSpanFitsMax(lines, offsets, len(content), i, endLine, maxBytes) {
				continue
			}
			spans = append(spans, chunkLineSpan{
				kind:      chunkKindRustType,
				symbol:    match[1],
				startLine: i,
				endLine:   endLine,
			})
		}
	}
	return chunksFromLineSpans(path, digest, content, lines, offsets, spans)
}

type elixirChunkPlanner struct{}

func (elixirChunkPlanner) Plan(path, digest string, content []byte, maxBytes int) []Chunk {
	lines, offsets := splitChunkPlannerLines(content)
	spans := make([]chunkLineSpan, 0, 8)
	for i, line := range lines {
		if match := elixirModulePattern.FindStringSubmatch(line); match != nil {
			endLine := elixirBlockEndLine(lines, i)
			if lineSpanFitsMax(lines, offsets, len(content), i, endLine, maxBytes) {
				spans = append(spans, chunkLineSpan{
					kind:      chunkKindElixirModule,
					symbol:    match[1],
					startLine: i,
					endLine:   endLine,
				})
			}
			continue
		}
		if match := elixirFunctionPattern.FindStringSubmatch(line); match != nil {
			spans = append(spans, chunkLineSpan{
				kind:      chunkKindElixirFunction,
				symbol:    match[1],
				startLine: i,
				endLine:   elixirBlockEndLine(lines, i),
			})
			continue
		}
		if match := elixirTypePattern.FindStringSubmatch(line); match != nil {
			spans = append(spans, chunkLineSpan{
				kind:      chunkKindElixirType,
				symbol:    match[1],
				startLine: i,
				endLine:   i,
			})
		}
	}
	return chunksFromLineSpans(path, digest, content, lines, offsets, spans)
}

type csharpChunkPlanner struct{}

func (csharpChunkPlanner) Plan(path, digest string, content []byte, maxBytes int) []Chunk {
	lines, offsets := splitChunkPlannerLines(content)
	spans := make([]chunkLineSpan, 0, 8)
	types := make([]chunkContainerRange, 0, 4)
	for i, line := range lines {
		match := csharpTypePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		endLine := braceBlockEndLine(lines, i, expander.DefaultBraceStyle())
		types = append(types, chunkContainerRange{name: match[2], startLine: i, endLine: endLine})
		kind := chunkKindCSharpType
		if match[1] == "class" || match[1] == "record" {
			kind = chunkKindCSharpClass
		}
		if lineSpanFitsMax(lines, offsets, len(content), i, endLine, maxBytes) {
			spans = append(spans, chunkLineSpan{
				kind:      kind,
				symbol:    match[2],
				startLine: i,
				endLine:   endLine,
			})
		}
	}
	for i, line := range lines {
		match := csharpMethodPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		symbol := match[1]
		if typeName := containingLineContainer(types, i); typeName != "" {
			symbol = typeName + "." + symbol
		}
		spans = append(spans, chunkLineSpan{
			kind:      chunkKindCSharpFunction,
			symbol:    symbol,
			startLine: i,
			endLine:   braceBlockEndLine(lines, i, expander.DefaultBraceStyle()),
		})
	}
	return chunksFromLineSpans(path, digest, content, lines, offsets, spans)
}

func chunksFromLineSpans(path, digest string, content []byte, lines []string, offsets []int, spans []chunkLineSpan) []Chunk {
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].startLine != spans[j].startLine {
			return spans[i].startLine < spans[j].startLine
		}
		if spans[i].endLine != spans[j].endLine {
			return spans[i].endLine < spans[j].endLine
		}
		if spans[i].kind != spans[j].kind {
			return spans[i].kind < spans[j].kind
		}
		return spans[i].symbol < spans[j].symbol
	})
	chunks := make([]Chunk, 0, len(spans))
	for _, span := range spans {
		kind := strings.TrimSpace(span.kind)
		symbol := strings.TrimSpace(span.symbol)
		start, end, ok := lineRangeByteSpan(lines, offsets, len(content), span.startLine, span.endLine)
		if !ok || kind == "" || symbol == "" {
			continue
		}
		symbols := []string{symbol}
		chunks = append(chunks, Chunk{
			ID:                stableChunkID(path, digest, kind, start, end, symbols),
			Kind:              kind,
			Content:           content[start:end],
			Start:             start,
			End:               end,
			SymbolIdentifiers: symbols,
		})
	}
	return chunks
}

func splitChunkPlannerLines(content []byte) ([]string, []int) {
	lines := strings.Split(string(content), "\n")
	offsets := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		offsets[i] = offset
		offset += len(line)
		if i < len(lines)-1 {
			offset++
		}
	}
	return lines, offsets
}

func lineRangeByteSpan(lines []string, offsets []int, contentLen, startLine, endLine int) (int, int, bool) {
	if startLine < 0 || startLine >= len(lines) || len(lines) != len(offsets) {
		return 0, 0, false
	}
	if endLine < startLine {
		endLine = startLine
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	start := offsets[startLine]
	end := offsets[endLine] + len(lines[endLine])
	if start < 0 {
		start = 0
	}
	if end > contentLen {
		end = contentLen
	}
	if start > contentLen {
		start = contentLen
	}
	return start, end, start < end
}

func lineSpanFitsMax(lines []string, offsets []int, contentLen, startLine, endLine, maxBytes int) bool {
	if maxBytes <= 0 {
		return true
	}
	start, end, ok := lineRangeByteSpan(lines, offsets, contentLen, startLine, endLine)
	return ok && end-start <= maxBytes
}

func languageChunkKind(language, category string) string {
	switch language {
	case "typescript":
		switch category {
		case "function":
			return chunkKindTypeScriptFunction
		case "class":
			return chunkKindTypeScriptClass
		default:
			return chunkKindTypeScriptType
		}
	case "javascript":
		switch category {
		case "function":
			return chunkKindJavaScriptFunction
		case "class":
			return chunkKindJavaScriptClass
		default:
			return chunkKindJavaScriptType
		}
	default:
		return language + "_" + category
	}
}

func findBraceLine(lines []string, startLine int) int {
	for i := startLine; i < len(lines) && i <= startLine+20; i++ {
		line := stripLineComment(lines[i], "//")
		if strings.Contains(line, "{") {
			return i
		}
		if strings.Contains(line, ";") {
			return -1
		}
	}
	return -1
}

func braceBlockEndLine(lines []string, startLine int, style expander.BraceStyle) int {
	braceLine := findBraceLine(lines, startLine)
	if braceLine < 0 {
		return startLine
	}
	if end := expander.FindBraceEnd(lines, braceLine, style); end >= braceLine {
		return end
	}
	return startLine
}

func stripLineComment(line, marker string) string {
	if idx := strings.Index(line, marker); idx >= 0 {
		return line[:idx]
	}
	return line
}

func includePythonDecorators(lines []string, defLine int) int {
	start := defLine
	for i := defLine - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			start = i
			continue
		}
		break
	}
	return start
}

func containingIndentContainer(containers []chunkContainerRange, line, indent int) string {
	name := ""
	bestIndent := -1
	for _, container := range containers {
		if line <= container.startLine || line > container.endLine {
			continue
		}
		if container.indent >= indent || container.indent < bestIndent {
			continue
		}
		name = container.name
		bestIndent = container.indent
	}
	return name
}

func containingLineContainer(containers []chunkContainerRange, line int) string {
	for i := len(containers) - 1; i >= 0; i-- {
		container := containers[i]
		if line > container.startLine && line <= container.endLine {
			return container.name
		}
	}
	return ""
}

func parseRustImplName(line string) string {
	header := strings.TrimSpace(line)
	if idx := strings.Index(header, "{"); idx >= 0 {
		header = strings.TrimSpace(header[:idx])
	}
	header = strings.TrimPrefix(header, "unsafe ")
	header = strings.TrimSpace(strings.TrimPrefix(header, "impl"))
	if strings.HasPrefix(header, "<") {
		if idx := strings.Index(header, ">"); idx >= 0 {
			header = strings.TrimSpace(header[idx+1:])
		}
	}
	if idx := strings.LastIndex(header, " for "); idx >= 0 {
		header = strings.TrimSpace(header[idx+5:])
	}
	return lastRustIdentifier(header)
}

func lastRustIdentifier(value string) string {
	matches := rustIdentifier.FindAllString(value, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func elixirBlockEndLine(lines []string, startLine int) int {
	line := stripLineComment(lines[startLine], "#")
	if !startsElixirBlock(line) {
		return startLine
	}
	depth := 0
	for i := startLine; i < len(lines); i++ {
		trimmed := strings.TrimSpace(stripLineComment(lines[i], "#"))
		if strings.Contains(trimmed, " do:") {
			continue
		}
		depth += len(elixirBlockPattern.FindAllString(trimmed, -1))
		if elixirEndPattern.MatchString(trimmed) {
			depth--
			if depth <= 0 {
				return i
			}
		}
	}
	return startLine
}

func startsElixirBlock(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.Contains(trimmed, " do:") {
		return false
	}
	return strings.HasSuffix(trimmed, " do") || strings.Contains(trimmed, " do ")
}
