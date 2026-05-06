package semanticanchors

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type OwnerResolver interface {
	ResolveFileOwner(path string) AnchorOwner
	ResolveSymbolOwner(path string, lang string, span Span, qualifiedName string) (AnchorOwner, bool)
}

type ExtractionResult struct {
	Language    string
	Support     LanguageAnchorSupport
	Comments    []CommentSpan
	Occurrences []AnchorOccurrence
	Findings    []Finding
}

func ExtractAnchorsFromSource(ctx context.Context, policy AnchorPolicy, resolver OwnerResolver, path string, src []byte) (ExtractionResult, error) {
	lang, support := languageSupport(path)
	result := ExtractionResult{Language: lang, Support: support}
	var comments []CommentSpan
	var symbols []symbolDecl
	var packageLine int
	var err error
	switch lang {
	case "go":
		comments, symbols, packageLine, err = extractGoCommentsAndSymbols(path, src)
	case "typescript":
		comments = scanLineAndBlockComments(path, string(src), "//", "/*", "*/")
		symbols = extractTypeScriptSymbols(src)
	case "python":
		comments = scanPythonComments(path, string(src))
		symbols = extractPythonSymbols(src)
	case "rust":
		comments = scanLineAndBlockComments(path, string(src), "//", "/*", "*/")
		symbols = extractRustSymbols(src)
	default:
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Comments = comments
	var generatedFinding *Finding
	if isGeneratedOrVendor(path, string(src)) {
		finding := newFinding(AnchorFindingGeneratedOrVendor, AnchorFindingError)
		generatedFinding = &finding
	}
	for _, comment := range comments {
		anchors := anchorsFromComment(policy, comment)
		for _, occ := range anchors {
			if generatedFinding != nil {
				occ.Findings = append(occ.Findings, *generatedFinding)
			}
			if hasErrorFinding(occ.Findings) {
				occ.ValidationStatus = AnchorValidationLintError
				result.Occurrences = append(result.Occurrences, occ)
				continue
			}
			if support == AnchorSupportGraphBinding {
				occ = bindOccurrence(policy, resolver, lang, packageLine, symbols, comment, occ, src)
			} else {
				occ.ValidationStatus = AnchorValidationLintError
				occ.Findings = append(occ.Findings, newFinding(AnchorFindingUnsupportedOwner, AnchorFindingWarning))
			}
			result.Occurrences = append(result.Occurrences, occ)
		}
	}
	if generatedFinding != nil && len(result.Occurrences) > 0 {
		result.Findings = append(result.Findings, *generatedFinding)
	}
	result.Findings = append(result.Findings, ValidateOccurrenceSet(policy, result.Occurrences)...)
	_ = ctx
	return result, nil
}

func LanguageSupportForPath(path string) LanguageAnchorSupport {
	_, support := languageSupport(path)
	return support
}

func StripAnchorOnlyDocLines(doc string) string {
	lines := strings.Split(doc, "\n")
	out := lines[:0]
	for _, line := range lines {
		if isAnchorOnlyText(strings.TrimSpace(line)) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func ValidateOccurrenceSet(policy AnchorPolicy, occurrences []AnchorOccurrence) []Finding {
	var findings []Finding
	byOwnerTarget := map[string]struct{}{}
	byOwnerCount := map[string]int{}
	byOwnerBeacon := map[string]int{}
	byOwnerSupported := map[string]bool{}
	for _, occ := range occurrences {
		ownerKey := ownerKey(occ)
		if ownerKey == "" {
			findings = append(findings, newFinding(AnchorFindingUnboundOwner, AnchorFindingError))
			continue
		}
		key := ownerKey + "\x00" + string(occ.TargetID)
		if _, ok := byOwnerTarget[key]; ok {
			findings = append(findings, newFinding(AnchorFindingDuplicateOwnerTarget, AnchorFindingError))
		}
		byOwnerTarget[key] = struct{}{}
		byOwnerCount[ownerKey]++
		if occ.Type == AnchorTypeBeacon {
			byOwnerBeacon[ownerKey]++
		} else {
			byOwnerSupported[ownerKey] = true
		}
	}
	maxAnchors := policy.MaxAnchorsPerOwner
	if maxAnchors <= 0 {
		maxAnchors = DefaultMaxAnchorsPerOwner
	}
	maxBeacons := policy.MaxBeaconsPerOwner
	if maxBeacons <= 0 {
		maxBeacons = DefaultMaxBeaconsPerOwner
	}
	for owner, count := range byOwnerCount {
		if count > maxAnchors {
			_ = owner
			findings = append(findings, newFinding(AnchorFindingTooManyAnchors, AnchorFindingError))
		}
	}
	for owner, count := range byOwnerBeacon {
		if count > maxBeacons {
			findings = append(findings, newFinding(AnchorFindingTooManyBeacons, AnchorFindingError))
		}
		if !byOwnerSupported[owner] {
			findings = append(findings, newFinding(AnchorFindingBeaconWithoutSupport, AnchorFindingError))
		}
	}
	return findings
}

type symbolDecl struct {
	span Span
	name string
}

func extractGoCommentsAndSymbols(path string, src []byte) ([]CommentSpan, []symbolDecl, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, 0, err
	}
	var comments []CommentSpan
	for _, group := range file.Comments {
		start := fset.Position(group.Pos())
		end := fset.Position(group.End())
		comments = append(comments, CommentSpan{Path: path, Span: Span{LineStart: start.Line, LineEnd: end.Line, ColStart: start.Column, ColEnd: end.Column}, Text: group.Text()})
	}
	var symbols []symbolDecl
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start := fset.Position(d.Pos())
			end := fset.Position(d.End())
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = receiverName(d.Recv.List[0].Type) + "." + name
			}
			symbols = append(symbols, symbolDecl{span: Span{LineStart: start.Line, LineEnd: end.Line, ColStart: start.Column, ColEnd: end.Column}, name: name})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					start := fset.Position(s.Pos())
					end := fset.Position(s.End())
					symbols = append(symbols, symbolDecl{span: Span{LineStart: start.Line, LineEnd: end.Line, ColStart: start.Column, ColEnd: end.Column}, name: s.Name.Name})
				case *ast.ValueSpec:
					for _, ident := range s.Names {
						if ident.Name == "_" {
							continue
						}
						start := fset.Position(ident.Pos())
						end := fset.Position(s.End())
						symbols = append(symbols, symbolDecl{span: Span{LineStart: start.Line, LineEnd: end.Line, ColStart: start.Column, ColEnd: end.Column}, name: ident.Name})
					}
				}
			}
		}
	}
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].span.LineStart < symbols[j].span.LineStart })
	return comments, symbols, fset.Position(file.Package).Line, nil
}

func receiverName(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return receiverName(v.X)
	case *ast.IndexExpr:
		return receiverName(v.X)
	case *ast.IndexListExpr:
		return receiverName(v.X)
	default:
		return "receiver"
	}
}

func bindOccurrence(policy AnchorPolicy, resolver OwnerResolver, lang string, packageLine int, symbols []symbolDecl, comment CommentSpan, occ AnchorOccurrence, src []byte) AnchorOccurrence {
	typePolicy := policy.TypePolicies[occ.Type]
	if symbol, ok := nearestFollowingSymbol(policy, symbols, comment, src); ok {
		if resolver != nil {
			if owner, ok := resolver.ResolveSymbolOwner(occ.Span.Path, lang, symbol.span, symbol.name); ok {
				occ.OwnerBinding = bindingFromOwner(owner)
				return occ
			}
		}
		occ.Findings = append(occ.Findings, newFinding(AnchorFindingUnsupportedOwner, AnchorFindingWarning))
		return occ
	}
	if typePolicy.SymbolOnly {
		occ.Findings = append(occ.Findings, newFinding(AnchorFindingUnboundOwner, AnchorFindingError))
		return occ
	}
	if isTopOfFileComment(comment, packageLine) {
		if resolver != nil {
			owner := resolver.ResolveFileOwner(occ.Span.Path)
			if owner.NodeID != "" {
				occ.OwnerBinding = bindingFromOwner(owner)
				return occ
			}
		}
		occ.Findings = append(occ.Findings, newFinding(AnchorFindingUnsupportedOwner, AnchorFindingWarning))
		return occ
	}
	occ.Findings = append(occ.Findings, newFinding(AnchorFindingUnboundOwner, AnchorFindingError))
	return occ
}

func nearestFollowingSymbol(policy AnchorPolicy, symbols []symbolDecl, comment CommentSpan, src []byte) (symbolDecl, bool) {
	maxLookahead := policy.MaxOwnerLookaheadLines
	if maxLookahead <= 0 {
		maxLookahead = DefaultMaxOwnerLookaheadLines
	}
	maxBlank := policy.MaxBlankLinesToOwner
	if maxBlank <= 0 {
		maxBlank = DefaultMaxBlankLinesToOwner
	}
	for _, symbol := range symbols {
		if symbol.span.LineStart <= comment.Span.LineEnd {
			continue
		}
		if symbol.span.LineStart-comment.Span.LineEnd > maxLookahead {
			return symbolDecl{}, false
		}
		if blankLineCount(src, comment.Span.LineEnd+1, symbol.span.LineStart-1) > maxBlank {
			return symbolDecl{}, false
		}
		if hasNonCommentCodeBetween(src, comment.Span.LineEnd+1, symbol.span.LineStart-1) {
			return symbolDecl{}, false
		}
		return symbol, true
	}
	return symbolDecl{}, false
}

func anchorsFromComment(policy AnchorPolicy, comment CommentSpan) []AnchorOccurrence {
	matches := inlineAnchorRE.FindAllString(comment.Text, -1)
	occurrences := make([]AnchorOccurrence, 0, len(matches))
	for _, match := range matches {
		occ, findings := ParseInlineAnchor(policy, match)
		occ.Span = SourceSpan{Path: comment.Path, LineStart: comment.Span.LineStart, LineEnd: comment.Span.LineEnd}
		occ.Findings = append(occ.Findings, findings...)
		if len(findings) == 0 {
			occ, _, _ = CanonicalizeAnchor(policy, occ)
		}
		occurrences = append(occurrences, occ)
	}
	return occurrences
}

func languageSupport(path string) (string, LanguageAnchorSupport) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go", AnchorSupportGraphBinding
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript", AnchorSupportGraphBinding
	case ".py":
		return "python", AnchorSupportGraphBinding
	case ".rs":
		return "rust", AnchorSupportGraphBinding
	default:
		return "", ""
	}
}

func extractTypeScriptSymbols(src []byte) []symbolDecl {
	var symbols []symbolDecl
	for i, line := range strings.Split(string(src), "\n") {
		name, ok := parseTypeScriptOwnerLine(line)
		if !ok {
			continue
		}
		lineNo := i + 1
		symbols = append(symbols, symbolDecl{span: Span{LineStart: lineNo, LineEnd: lineNo}, name: name})
	}
	return symbols
}

func parseTypeScriptOwnerLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return "", false
	}
	for {
		next := strings.TrimSpace(trimmed)
		switch {
		case strings.HasPrefix(next, "export "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "export "))
		case strings.HasPrefix(next, "default "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "default "))
		case strings.HasPrefix(next, "declare "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "declare "))
		case strings.HasPrefix(next, "abstract "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "abstract "))
		case strings.HasPrefix(next, "async "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "async "))
		default:
			trimmed = next
			goto parsedPrefixes
		}
	}

parsedPrefixes:
	for _, prefix := range []string{"function ", "class ", "interface ", "type ", "enum "} {
		if strings.HasPrefix(trimmed, prefix) {
			return identifierAfter(strings.TrimPrefix(trimmed, prefix))
		}
	}
	for _, prefix := range []string{"const ", "let ", "var "} {
		if !strings.HasPrefix(trimmed, prefix) || !typeScriptVariableLooksCallable(trimmed) {
			continue
		}
		return identifierAfter(strings.TrimPrefix(trimmed, prefix))
	}
	return "", false
}

func typeScriptVariableLooksCallable(line string) bool {
	return strings.Contains(line, "=>") || strings.Contains(line, "function(") || strings.Contains(line, "function ")
}

func extractPythonSymbols(src []byte) []symbolDecl {
	var symbols []symbolDecl
	for i, line := range strings.Split(string(src), "\n") {
		name, ok := parsePythonOwnerLine(line)
		if !ok {
			continue
		}
		lineNo := i + 1
		symbols = append(symbols, symbolDecl{span: Span{LineStart: lineNo, LineEnd: lineNo}, name: name})
	}
	return symbols
}

func parsePythonOwnerLine(line string) (string, bool) {
	if leadingWhitespace(line) != 0 {
		return "", false
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "async def ") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "async "))
	}
	for _, prefix := range []string{"def ", "class "} {
		if strings.HasPrefix(trimmed, prefix) {
			return identifierAfter(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return "", false
}

func extractRustSymbols(src []byte) []symbolDecl {
	var symbols []symbolDecl
	for i, line := range strings.Split(string(src), "\n") {
		name, ok := parseRustOwnerLine(line)
		if !ok {
			continue
		}
		lineNo := i + 1
		symbols = append(symbols, symbolDecl{span: Span{LineStart: lineNo, LineEnd: lineNo}, name: name})
	}
	return symbols
}

func parseRustOwnerLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(stripRustLineComment(line))
	if trimmed == "" {
		return "", false
	}
	for {
		next := strings.TrimSpace(trimmed)
		switch {
		case strings.HasPrefix(next, "pub("):
			end := strings.Index(next, ")")
			if end < 0 {
				return "", false
			}
			trimmed = strings.TrimSpace(next[end+1:])
		case strings.HasPrefix(next, "pub "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "pub "))
		case strings.HasPrefix(next, "default "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "default "))
		case strings.HasPrefix(next, "async "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "async "))
		case strings.HasPrefix(next, "const "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "const "))
		case strings.HasPrefix(next, "unsafe "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "unsafe "))
		case strings.HasPrefix(next, "extern "):
			trimmed = strings.TrimSpace(strings.TrimPrefix(next, "extern "))
		default:
			trimmed = next
			goto parsedPrefixes
		}
	}

parsedPrefixes:
	for _, prefix := range []string{"fn ", "struct ", "enum ", "trait "} {
		if strings.HasPrefix(trimmed, prefix) {
			return identifierAfter(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return "", false
}

func scanPythonComments(path, src string) []CommentSpan {
	var comments []CommentSpan
	for i, line := range strings.Split(src, "\n") {
		idx := pythonCommentIndex(line)
		if idx < 0 {
			continue
		}
		comments = append(comments, CommentSpan{Path: path, Span: Span{LineStart: i + 1, LineEnd: i + 1, ColStart: idx + 1, ColEnd: len(line) + 1}, Text: line[idx:]})
	}
	return comments
}

func scanLineAndBlockComments(path, src, linePrefix, blockStart, blockEnd string) []CommentSpan {
	var comments []CommentSpan
	lines := strings.Split(src, "\n")
	inBlock := false
	var block []string
	blockStartLine := 0
	for i, line := range lines {
		lineNo := i + 1
		if inBlock {
			block = append(block, line)
			if strings.Contains(line, blockEnd) {
				comments = append(comments, CommentSpan{Path: path, Span: Span{LineStart: blockStartLine, LineEnd: lineNo}, Text: strings.Join(block, "\n")})
				inBlock = false
				block = nil
			}
			continue
		}
		lineIdx := commentIndexOutsideStrings(line, linePrefix)
		blockIdx := commentIndexOutsideStrings(line, blockStart)
		switch {
		case blockIdx >= 0 && (lineIdx < 0 || blockIdx < lineIdx):
			if end := strings.Index(line[blockIdx+len(blockStart):], blockEnd); end >= 0 {
				comments = append(comments, CommentSpan{Path: path, Span: Span{LineStart: lineNo, LineEnd: lineNo, ColStart: blockIdx + 1, ColEnd: blockIdx + end + len(blockStart) + len(blockEnd) + 1}, Text: line[blockIdx:]})
			} else {
				inBlock = true
				blockStartLine = lineNo
				block = []string{line[blockIdx:]}
			}
		case lineIdx >= 0:
			comments = append(comments, CommentSpan{Path: path, Span: Span{LineStart: lineNo, LineEnd: lineNo, ColStart: lineIdx + 1, ColEnd: len(line) + 1}, Text: line[lineIdx:]})
		}
	}
	return comments
}

func hasNonCommentCodeBetween(src []byte, startLine, endLine int) bool {
	if startLine > endLine {
		return false
	}
	lines := strings.Split(string(src), "\n")
	for lineNo := startLine; lineNo <= endLine && lineNo <= len(lines); lineNo++ {
		line := strings.TrimSpace(lines[lineNo-1])
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "*/") {
			continue
		}
		return true
	}
	return false
}

func blankLineCount(src []byte, startLine, endLine int) int {
	lines := strings.Split(string(src), "\n")
	count := 0
	for lineNo := startLine; lineNo <= endLine && lineNo <= len(lines); lineNo++ {
		if strings.TrimSpace(lines[lineNo-1]) == "" {
			count++
		}
	}
	return count
}

func isTopOfFileComment(comment CommentSpan, packageLine int) bool {
	return packageLine > 0 && comment.Span.LineEnd < packageLine
}

func bindingFromOwner(owner AnchorOwner) AnchorOwnerBinding {
	return AnchorOwnerBinding{OwnerNodeID: owner.NodeID, OwnerKind: owner.Kind, OwnerStableKey: owner.StableKey}
}

func ownerKey(occ AnchorOccurrence) string {
	if occ.OwnerBinding.OwnerNodeID != "" {
		return occ.OwnerBinding.OwnerNodeID
	}
	return occ.OwnerBinding.OwnerStableKey
}

func hasErrorFinding(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Severity == AnchorFindingError {
			return true
		}
	}
	return false
}

func isGeneratedOrVendor(path, src string) bool {
	clean := filepath.ToSlash(path)
	if strings.Contains(clean, "/vendor/") || strings.HasPrefix(clean, "vendor/") {
		return true
	}
	head := src
	if len(head) > 4096 {
		head = head[:4096]
	}
	return strings.Contains(strings.ToLower(head), "code generated") && strings.Contains(strings.ToLower(head), "do not edit")
}

func isAnchorOnlyText(text string) bool {
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "//"), "#"))
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(text, "*/"), "/*"))
	return inlineAnchorRE.MatchString(text) && strings.TrimSpace(inlineAnchorRE.ReplaceAllString(text, "")) == ""
}

func pythonCommentIndex(line string) int {
	inSingle, inDouble := false, false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return i
			}
		}
	}
	return -1
}

func commentIndexOutsideStrings(line, marker string) int {
	inSingle, inDouble, inBacktick := false, false, false
	escaped := false
	for i := 0; i <= len(line)-len(marker); i++ {
		ch := line[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		switch ch {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		}
		if !inSingle && !inDouble && !inBacktick && strings.HasPrefix(line[i:], marker) {
			return i
		}
	}
	return -1
}

func identifierAfter(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", false
	}
	for i, r := range input {
		if i == 0 {
			if isIdentStart(r) {
				continue
			}
			return "", false
		}
		if !isIdentPart(r) {
			return input[:i], true
		}
	}
	return input, true
}

func isIdentStart(r rune) bool {
	return r == '_' || r == '$' || ('A' <= r && r <= 'Z') || ('a' <= r && r <= 'z')
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || ('0' <= r && r <= '9')
}

func leadingWhitespace(line string) int {
	count := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ', '\t':
			count++
		default:
			return count
		}
	}
	return count
}

func stripRustLineComment(line string) string {
	idx := commentIndexOutsideStrings(line, "//")
	if idx < 0 {
		return line
	}
	return line[:idx]
}

var inlineAnchorRE = regexp.MustCompile(`\[\[[^\]\n\r]+:[^\]\n\r]+\]\]`)
