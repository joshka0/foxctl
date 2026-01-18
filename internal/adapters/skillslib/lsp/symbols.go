package lsp

import "strconv"

// FlattenDocumentSymbols flattens a hierarchical symbol tree into a list.
func FlattenDocumentSymbols(symbols []DocumentSymbol) []DocumentSymbol {
	result := make([]DocumentSymbol, 0, len(symbols))
	var walk func([]DocumentSymbol)
	walk = func(syms []DocumentSymbol) {
		for _, s := range syms {
			result = append(result, s)
			if len(s.Children) > 0 {
				walk(s.Children)
			}
		}
	}
	walk(symbols)
	return result
}

// SymbolKindToString maps LSP symbol kind integers to descriptive names.
func SymbolKindToString(kind int) string {
	kinds := map[int]string{
		1:  "File",
		2:  "Module",
		3:  "Namespace",
		4:  "Package",
		5:  "Class",
		6:  "Method",
		7:  "Property",
		8:  "Field",
		9:  "Constructor",
		10: "Enum",
		11: "Interface",
		12: "Function",
		13: "Variable",
		14: "Constant",
		15: "String",
		16: "Number",
		17: "Boolean",
		18: "Array",
		19: "Object",
		20: "Key",
		21: "Null",
		22: "EnumMember",
		23: "Struct",
		24: "Event",
		25: "Operator",
		26: "TypeParameter",
	}
	if s, ok := kinds[kind]; ok {
		return s
	}
	return strconv.Itoa(kind)
}
