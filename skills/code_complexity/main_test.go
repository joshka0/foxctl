package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestCalculateGoCognitiveComplexity(t *testing.T) {
	tests := []struct {
		name string
		code string
		want int
	}{
		{
			name: "simple",
			code: `package main
func f() {
	if true {
		return
	}
}`,
			want: 1, // if +1
		},
		{
			name: "nested",
			code: `package main
func f() {
	if true {
		if true {
			return
		}
	}
}`,
			want: 3, // if +1, nested if +2 (1 + 1)
		},
		{
			name: "switch",
			code: `package main
func f() {
	switch x {
	case 1:
		if true {
			return
		}
	}
}`,
			want: 3, // switch +1, if +2 (1 + 1)
		},
		{
			name: "for loop",
			code: `package main
func f() {
	for i := 0; i < 10; i++ {
		if true {
			continue
		}
	}
}`,
			want: 3, // for +1, if +2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			
			fn := file.Decls[0].(*ast.FuncDecl)
			got := calculateGoCognitiveComplexity(fn)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculateGoNestingDepth(t *testing.T) {
	tests := []struct {
		name string
		code string
		want int
	}{
		{
			name: "flat",
			code: `package main
func f() {
	x := 1
}`,
			want: 0,
		},
		{
			name: "one level",
			code: `package main
func f() {
	if true {
		x := 1
	}
}`,
			want: 1,
		},
		{
			name: "two levels",
			code: `package main
func f() {
	if true {
		for {
			break
		}
	}
}`,
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			
			fn := file.Decls[0].(*ast.FuncDecl)
			got := calculateGoNestingDepth(fn)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}