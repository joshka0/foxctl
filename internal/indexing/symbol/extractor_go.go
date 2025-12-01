package symbol

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// GoExtractor extracts symbols from Go source code using the standard AST parser.
//
// This is the v1 "Go-first" reference implementation per code_symbol_index_and_swe_grep.md
// §4.1. It uses go/ast instead of Tree-sitter, producing the same data model without
// requiring CGO (works with CGO_ENABLED=0).
//
// # Extracted Symbol Kinds
//
// The extractor identifies the following symbol kinds:
//   - [KindFunction]: Package-level functions (func Foo())
//   - [KindMethod]: Methods with receivers (func (r *Receiver) Method())
//   - [KindStruct]: Struct type declarations
//   - [KindInterface]: Interface type declarations
//   - [KindType]: Other type declarations (type aliases, etc.)
//   - [KindVariable]: Package-level var declarations
//   - [KindConstant]: Package-level const declarations
//
// # Symbol Naming
//
// For methods, the symbol name includes the receiver type: "ReceiverType.MethodName".
// This ensures unique IDs when the same method name exists on different types.
//
// # Body Digest
//
// The [Symbol.BodyDigest] is computed over the full declaration body (from Pos to End),
// which includes the signature and implementation. This means signature-only changes
// (e.g. adding a parameter) will trigger re-embedding even if the body is unchanged.
//
// # Call Extraction
//
// [ExtractCalls] identifies function/method calls within a symbol's body by parsing
// call expressions. It extracts:
//   - Direct function calls: "FunctionName"
//   - Method calls: "pkg.Function" or "receiver.Method"
//
// Call extraction is best-effort; resolution is name-based and may produce
// false positives for shadowed names or dynamic calls.
//
// # Error Handling
//
// Unparseable files return nil symbols and nil error (not an error) to avoid
// blocking the indexing pipeline. This follows the [Extractor] contract.
type GoExtractor struct{}

// NewGoExtractor creates a new Go symbol extractor.
func NewGoExtractor() *GoExtractor {
	return &GoExtractor{}
}

// SupportedLanguages returns ["go"].
func (e *GoExtractor) SupportedLanguages() []string {
	return []string{"go"}
}

// Extract parses Go source code and returns symbols.
func (e *GoExtractor) Extract(_ context.Context, filePath string, content []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		// Return empty symbols for unparseable files rather than failing
		return nil, nil
	}

	var symbols []Symbol

	// Extract package-level declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sym := e.extractFunc(fset, d, filePath, content)
			symbols = append(symbols, sym)

		case *ast.GenDecl:
			syms := e.extractGenDecl(fset, d, filePath, content)
			symbols = append(symbols, syms...)
		}
	}

	return symbols, nil
}

// extractFunc extracts a function or method symbol.
func (e *GoExtractor) extractFunc(fset *token.FileSet, fn *ast.FuncDecl, filePath string, content []byte) Symbol {
	name := fn.Name.Name
	kind := KindFunction

	// Check if it's a method (has receiver)
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		kind = KindMethod
		// Include receiver type in name for unique ID
		recvType := e.extractReceiverType(fn.Recv.List[0].Type)
		if recvType != "" {
			name = recvType + "." + name
		}
	}

	startPos := fset.Position(fn.Pos())
	endPos := fset.Position(fn.End())

	// Extract signature
	signature := e.extractFuncSignature(fn, content)

	// Extract documentation
	var doc string
	if fn.Doc != nil {
		doc = fn.Doc.Text()
	}

	// Extract body for digest (with bounds checking to prevent panic)
	var body []byte
	bodyStart := int(fn.Pos()) - 1
	bodyEnd := int(fn.End()) - 1
	if bodyStart >= 0 && bodyEnd <= len(content) && bodyStart < bodyEnd {
		body = content[bodyStart:bodyEnd]
	}

	return Symbol{
		ID:            ID(filePath, name),
		FilePath:      filePath,
		Name:          name,
		Language:      "go",
		Kind:          kind,
		StartByte:     int(fn.Pos() - 1),
		EndByte:       int(fn.End() - 1),
		StartLine:     startPos.Line,
		EndLine:       endPos.Line,
		Signature:     signature,
		BodyDigest:    ComputeDigest(body),
		Documentation: strings.TrimSpace(doc),
	}
}

// extractReceiverType extracts the receiver type name from an AST expression.
func (e *GoExtractor) extractReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return e.extractReceiverType(t.X)
	default:
		return ""
	}
}

// extractFuncSignature extracts a function signature string.
func (e *GoExtractor) extractFuncSignature(fn *ast.FuncDecl, _ []byte) string {
	// Find the signature up to the opening brace
	var buf bytes.Buffer
	buf.WriteString("func ")

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		buf.WriteString("(")
		recvType := e.extractReceiverType(fn.Recv.List[0].Type)
		if len(fn.Recv.List[0].Names) > 0 {
			buf.WriteString(fn.Recv.List[0].Names[0].Name)
			buf.WriteString(" ")
		}
		if _, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
			buf.WriteString("*")
		}
		buf.WriteString(recvType)
		buf.WriteString(") ")
	}

	buf.WriteString(fn.Name.Name)
	buf.WriteString("(")

	// Parameters
	if fn.Type.Params != nil {
		var params []string
		for _, field := range fn.Type.Params.List {
			paramType := e.exprToString(field.Type)
			if len(field.Names) > 0 {
				for _, name := range field.Names {
					params = append(params, name.Name+" "+paramType)
				}
			} else {
				params = append(params, paramType)
			}
		}
		buf.WriteString(strings.Join(params, ", "))
	}
	buf.WriteString(")")

	// Results
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		buf.WriteString(" ")
		if len(fn.Type.Results.List) == 1 && len(fn.Type.Results.List[0].Names) == 0 {
			buf.WriteString(e.exprToString(fn.Type.Results.List[0].Type))
		} else {
			buf.WriteString("(")
			var results []string
			for _, field := range fn.Type.Results.List {
				resultType := e.exprToString(field.Type)
				if len(field.Names) > 0 {
					for _, name := range field.Names {
						results = append(results, name.Name+" "+resultType)
					}
				} else {
					results = append(results, resultType)
				}
			}
			buf.WriteString(strings.Join(results, ", "))
			buf.WriteString(")")
		}
	}

	return buf.String()
}

// extractGenDecl extracts symbols from general declarations (types, consts, vars).
func (e *GoExtractor) extractGenDecl(fset *token.FileSet, decl *ast.GenDecl, filePath string, content []byte) []Symbol {
	var symbols []Symbol

	for _, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			sym := e.extractTypeSpec(fset, decl, s, filePath, content)
			symbols = append(symbols, sym)

		case *ast.ValueSpec:
			syms := e.extractValueSpec(fset, decl, s, filePath, content)
			symbols = append(symbols, syms...)
		}
	}

	return symbols
}

// extractTypeSpec extracts a type declaration symbol.
func (e *GoExtractor) extractTypeSpec(fset *token.FileSet, decl *ast.GenDecl, spec *ast.TypeSpec, filePath string, content []byte) Symbol {
	startPos := fset.Position(spec.Pos())
	endPos := fset.Position(spec.End())

	kind := KindType
	switch spec.Type.(type) {
	case *ast.StructType:
		kind = KindStruct
	case *ast.InterfaceType:
		kind = KindInterface
	}

	var doc string
	if spec.Doc != nil {
		doc = spec.Doc.Text()
	} else if decl.Doc != nil {
		doc = decl.Doc.Text()
	}

	// Bounds check before slicing
	var body []byte
	start := int(spec.Pos()) - 1
	end := int(spec.End()) - 1
	if start >= 0 && end <= len(content) && start < end {
		body = content[start:end]
	}

	return Symbol{
		ID:            ID(filePath, spec.Name.Name),
		FilePath:      filePath,
		Name:          spec.Name.Name,
		Language:      "go",
		Kind:          kind,
		StartByte:     int(spec.Pos() - 1),
		EndByte:       int(spec.End() - 1),
		StartLine:     startPos.Line,
		EndLine:       endPos.Line,
		Signature:     "type " + spec.Name.Name,
		BodyDigest:    ComputeDigest(body),
		Documentation: strings.TrimSpace(doc),
	}
}

// extractValueSpec extracts const or var declarations.
func (e *GoExtractor) extractValueSpec(fset *token.FileSet, decl *ast.GenDecl, spec *ast.ValueSpec, filePath string, content []byte) []Symbol {
	var symbols []Symbol

	kind := KindVariable
	if decl.Tok == token.CONST {
		kind = KindConstant
	}

	for _, name := range spec.Names {
		// Skip blank identifiers
		if name.Name == "_" {
			continue
		}

		startPos := fset.Position(name.Pos())
		endPos := fset.Position(spec.End())

		var doc string
		if spec.Doc != nil {
			doc = spec.Doc.Text()
		} else if decl.Doc != nil {
			doc = decl.Doc.Text()
		}

		// Bounds check before slicing
		var body []byte
		start := int(spec.Pos()) - 1
		end := int(spec.End()) - 1
		if start >= 0 && end <= len(content) && start < end {
			body = content[start:end]
		}

		sym := Symbol{
			ID:            ID(filePath, name.Name),
			FilePath:      filePath,
			Name:          name.Name,
			Language:      "go",
			Kind:          kind,
			StartByte:     int(name.Pos() - 1),
			EndByte:       int(spec.End() - 1),
			StartLine:     startPos.Line,
			EndLine:       endPos.Line,
			BodyDigest:    ComputeDigest(body),
			Documentation: strings.TrimSpace(doc),
		}
		symbols = append(symbols, sym)
	}

	return symbols
}

// ExtractCalls extracts function call identifiers from a symbol's body.
// It parses the full source file once and locates the AST node corresponding
// to the symbol's byte range, then extracts call expressions from that node.
func (e *GoExtractor) ExtractCalls(_ context.Context, symbol Symbol, content []byte) ([]string, error) {
	// Validate bounds
	if symbol.StartByte < 0 || symbol.EndByte > len(content) || symbol.StartByte >= symbol.EndByte {
		return nil, nil
	}

	// Parse the full source file once with a single FileSet
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, symbol.FilePath, content, 0)
	if err != nil {
		return nil, fmt.Errorf("parse file for call extraction: %w", err)
	}

	// Find the AST node that corresponds to our symbol's byte range
	var targetNode ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		// Convert token positions to byte offsets
		nodeStart := int(n.Pos()) - 1 // token.Pos is 1-indexed
		nodeEnd := int(n.End()) - 1

		// Check if this node encompasses our symbol's range
		if nodeStart <= symbol.StartByte && nodeEnd >= symbol.EndByte {
			// Prefer the most specific (innermost) matching node
			switch n.(type) {
			case *ast.FuncDecl, *ast.FuncLit:
				targetNode = n
			}
		}
		return true
	})

	if targetNode == nil {
		// Fallback: if we can't find the exact node, return empty
		return nil, nil
	}

	// Extract calls from the target node only
	var calls []string
	seenCalls := make(map[string]bool)
	ast.Inspect(targetNode, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			callName := e.extractCallName(call)
			if callName != "" && !seenCalls[callName] {
				calls = append(calls, callName)
				seenCalls[callName] = true
			}
		}
		return true
	})

	return calls, nil
}

// extractCallName extracts the function name from a call expression.
func (e *GoExtractor) extractCallName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		// pkg.Func or receiver.Method
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	default:
		return ""
	}
}

// exprToString converts an AST expression to a string representation.
func (e *GoExtractor) exprToString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.BasicLit:
		return t.Value
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + e.exprToString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + e.exprToString(t.Elt)
		}
		// Fixed-size array: include the length expression
		return "[" + e.exprToString(t.Len) + "]" + e.exprToString(t.Elt)
	case *ast.MapType:
		return "map[" + e.exprToString(t.Key) + "]" + e.exprToString(t.Value)
	case *ast.SelectorExpr:
		return e.exprToString(t.X) + "." + t.Sel.Name
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ellipsis:
		return "..." + e.exprToString(t.Elt)
	case *ast.FuncType:
		return "func(...)"
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + e.exprToString(t.Value)
		case ast.RECV:
			return "<-chan " + e.exprToString(t.Value)
		default:
			return "chan " + e.exprToString(t.Value)
		}
	default:
		return "unknown"
	}
}
