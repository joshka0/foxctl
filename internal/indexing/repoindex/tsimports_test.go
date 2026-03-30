package repoindex

import (
	"reflect"
	"testing"
)

func TestExtractTSImports(t *testing.T) {
	source := []byte(`import foo, { bar as baz } from "./lib"
export { qux } from "pkg"
export * from "./other"
const mod = await import("./dyn")
const req = require("./req")
`)

	got := extractTSImports("src/example.ts", source)
	want := []string{"./dyn", "./lib", "./other", "./req", "pkg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractTSImports() = %v, want %v", got, want)
	}
}

func TestExtractTSImportBindings(t *testing.T) {
	source := []byte(`import defaultThing, { bar as baz, qux } from "./lib"
import { type Kind, value } from "./types"
`)

	got := extractTSImportBindings("src/example.ts", source)
	want := []tsImportBinding{
		{ImportPath: "./lib", TargetName: "bar"},
		{ImportPath: "./lib", TargetName: "default"},
		{ImportPath: "./lib", TargetName: "qux"},
		{ImportPath: "./types", TargetName: "Kind"},
		{ImportPath: "./types", TargetName: "value"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractTSImportBindings() = %#v, want %#v", got, want)
	}
}
