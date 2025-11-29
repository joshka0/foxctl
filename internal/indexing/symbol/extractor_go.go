package symbol

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// GoExtractor extracts symbols from Go source code using the standard AST parser.
// This implementation does not require CGO and works with CGO_ENABLED=0.
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

	// Extract body for digest
	bodyStart := fn.Pos()
	bodyEnd := fn.End()
	body := content[bodyStart-1 : bodyEnd-1]

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

	body := content[spec.Pos()-1 : spec.End()-1]

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

		body := content[spec.Pos()-1 : spec.End()-1]

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
func (e *GoExtractor) ExtractCalls(_ context.Context, symbol Symbol, content []byte) ([]string, error) {
	// Parse just the function body
	if symbol.EndByte > len(content) || symbol.StartByte >= symbol.EndByte {
		return nil, nil
	}

	// Wrap the body in a valid Go file for parsing
	body := content[symbol.StartByte:symbol.EndByte]

	// Parse as expression list to find call expressions
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", "package p\n"+string(body), 0)
	if err != nil {
		// Try parsing just as source
		file, err = parser.ParseFile(fset, "", content, 0)
		if err != nil {
			return nil, nil
		}
	}

	var calls []string
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			callName := e.extractCallName(call)
			if callName != "" {
				calls = append(calls, callName)
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
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + e.exprToString(t.X)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + e.exprToString(t.Elt)
		}
		return "[...]" + e.exprToString(t.Elt)
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
