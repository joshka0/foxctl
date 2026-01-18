package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestDefaultMaxSymbols(t *testing.T) {
	assert.Equal(t, 3, defaultMaxSymbols)
}

func TestDefaultMaxRefs(t *testing.T) {
	assert.Equal(t, 5, defaultMaxRefs)
}

func TestDefaultTimeout(t *testing.T) {
	assert.Equal(t, 45*time.Second, defaultTimeout)
}

func TestDebounceCooldown(t *testing.T) {
	assert.Equal(t, 10*time.Second, debounceCooldown)
}

// Tests for environment variable constants

func TestEnvConstants(t *testing.T) {
	assert.Equal(t, "AGENTCTL_IMPACT_DISABLED", EnvImpactDisabled)
	assert.Equal(t, "AGENTCTL_IMPACT_MAX_SYMBOLS", EnvImpactMaxSymbols)
	assert.Equal(t, "AGENTCTL_IMPACT_MAX_REFS", EnvImpactMaxRefs)
	assert.Equal(t, "AGENTCTL_IMPACT_TIMEOUT", EnvImpactTimeout)
	assert.Equal(t, "AGENTCTL_BIN", EnvAgentctlBin)
}

// Tests for Config structure

func TestConfig_AllFields(t *testing.T) {
	cfg := Config{
		MaxSymbols: 5,
		MaxRefs:    10,
		Timeout:    30 * time.Second,
		Disabled:   true,
	}

	assert.Equal(t, 5, cfg.MaxSymbols)
	assert.Equal(t, 10, cfg.MaxRefs)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.True(t, cfg.Disabled)
}

func TestConfig_JSONSerialization(t *testing.T) {
	cfg := Config{
		MaxSymbols: 3,
		MaxRefs:    5,
		Disabled:   false,
	}

	data, err := json.Marshal(cfg)
	assert.NoError(t, err)

	var decoded Config
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, cfg.MaxSymbols, decoded.MaxSymbols)
	assert.Equal(t, cfg.MaxRefs, decoded.MaxRefs)
	assert.Equal(t, cfg.Disabled, decoded.Disabled)
}

// Tests for ConfigFromMap helper

func TestConfigFromMap_Defaults(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{})

	assert.Equal(t, defaultMaxSymbols, cfg.MaxSymbols)
	assert.Equal(t, defaultMaxRefs, cfg.MaxRefs)
	assert.Equal(t, defaultTimeout, cfg.Timeout)
	assert.False(t, cfg.Disabled)
}

func TestConfigFromMap_Disabled(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactDisabled: "1",
	})

	assert.True(t, cfg.Disabled)
}

func TestConfigFromMap_NotDisabled(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactDisabled: "0",
	})

	assert.False(t, cfg.Disabled)
}

func TestConfigFromMap_MaxSymbols(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxSymbols: "10",
	})

	assert.Equal(t, 10, cfg.MaxSymbols)
}

func TestConfigFromMap_MaxSymbolsInvalid(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxSymbols: "invalid",
	})

	// Should fall back to default
	assert.Equal(t, defaultMaxSymbols, cfg.MaxSymbols)
}

func TestConfigFromMap_MaxSymbolsZero(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxSymbols: "0",
	})

	// Zero should use default
	assert.Equal(t, defaultMaxSymbols, cfg.MaxSymbols)
}

func TestConfigFromMap_MaxSymbolsNegative(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxSymbols: "-5",
	})

	// Negative should use default
	assert.Equal(t, defaultMaxSymbols, cfg.MaxSymbols)
}

func TestConfigFromMap_MaxRefs(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxRefs: "15",
	})

	assert.Equal(t, 15, cfg.MaxRefs)
}

func TestConfigFromMap_MaxRefsInvalid(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxRefs: "invalid",
	})

	assert.Equal(t, defaultMaxRefs, cfg.MaxRefs)
}

func TestConfigFromMap_Timeout(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactTimeout: "60",
	})

	assert.Equal(t, 60*time.Second, cfg.Timeout)
}

func TestConfigFromMap_TimeoutInvalid(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactTimeout: "invalid",
	})

	assert.Equal(t, defaultTimeout, cfg.Timeout)
}

func TestConfigFromMap_AllSettings(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactDisabled:   "1",
		EnvImpactMaxSymbols: "8",
		EnvImpactMaxRefs:    "20",
		EnvImpactTimeout:    "90",
	})

	assert.True(t, cfg.Disabled)
	assert.Equal(t, 8, cfg.MaxSymbols)
	assert.Equal(t, 20, cfg.MaxRefs)
	assert.Equal(t, 90*time.Second, cfg.Timeout)
}

// Tests for Symbol structure

func TestSymbol_AllFields(t *testing.T) {
	sym := Symbol{
		Name:    "MyFunction",
		Type:    "function",
		Line:    25,
		EndLine: 50,
	}

	assert.Equal(t, "MyFunction", sym.Name)
	assert.Equal(t, "function", sym.Type)
	assert.Equal(t, 25, sym.Line)
	assert.Equal(t, 50, sym.EndLine)
}

func TestSymbol_JSONSerialization(t *testing.T) {
	sym := Symbol{
		Name: "Test",
		Type: "struct",
		Line: 10,
	}

	data, err := json.Marshal(sym)
	assert.NoError(t, err)

	var decoded Symbol
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, sym.Name, decoded.Name)
	assert.Equal(t, sym.Type, decoded.Type)
	assert.Equal(t, sym.Line, decoded.Line)
}

// Tests for Impact structure

func TestImpact_AllFields(t *testing.T) {
	imp := Impact{
		Symbol:     "Handler",
		SymbolType: "interface",
		RefCount:   3,
		RefFiles:   []string{"file1.go", "file2.go", "file3.go"},
		ImplCount:  2,
		ImplFiles:  []string{"impl1.go", "impl2.go"},
	}

	assert.Equal(t, "Handler", imp.Symbol)
	assert.Equal(t, "interface", imp.SymbolType)
	assert.Equal(t, 3, imp.RefCount)
	assert.Len(t, imp.RefFiles, 3)
	assert.Equal(t, 2, imp.ImplCount)
	assert.Len(t, imp.ImplFiles, 2)
}

func TestImpact_JSONSerialization(t *testing.T) {
	imp := Impact{
		Symbol:     "Test",
		SymbolType: "function",
		RefCount:   2,
		RefFiles:   []string{"a.go", "b.go"},
	}

	data, err := json.Marshal(imp)
	assert.NoError(t, err)

	var decoded Impact
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, imp.Symbol, decoded.Symbol)
	assert.Equal(t, imp.RefCount, decoded.RefCount)
}

func TestImpact_NoImplementations(t *testing.T) {
	imp := Impact{
		Symbol:     "Helper",
		SymbolType: "function",
		RefCount:   1,
		RefFiles:   []string{"main.go"},
	}

	assert.Zero(t, imp.ImplCount)
	assert.Nil(t, imp.ImplFiles)
}

// Tests for Language structure and languages map

func TestLanguages_GoIsPublic(t *testing.T) {
	goLang := languages[".go"]

	// Uppercase first letter = public in Go
	assert.True(t, goLang.IsPublic("Handler"))
	assert.True(t, goLang.IsPublic("MyFunc"))
	assert.True(t, goLang.IsPublic("A"))

	// Lowercase = private
	assert.False(t, goLang.IsPublic("handler"))
	assert.False(t, goLang.IsPublic("myFunc"))
	assert.False(t, goLang.IsPublic("a"))

	// Empty string
	assert.False(t, goLang.IsPublic(""))
}

func TestLanguages_PythonIsPublic(t *testing.T) {
	pyLang := languages[".py"]

	// No underscore prefix = public
	assert.True(t, pyLang.IsPublic("handler"))
	assert.True(t, pyLang.IsPublic("MyClass"))
	assert.True(t, pyLang.IsPublic("func"))

	// Underscore prefix = private
	assert.False(t, pyLang.IsPublic("_private"))
	assert.False(t, pyLang.IsPublic("__dunder"))
	assert.False(t, pyLang.IsPublic("_"))
}

func TestLanguages_TypeScriptIsPublic(t *testing.T) {
	tsLang := languages[".ts"]

	assert.True(t, tsLang.IsPublic("handler"))
	assert.True(t, tsLang.IsPublic("MyClass"))

	assert.False(t, tsLang.IsPublic("_private"))
}

func TestLanguages_SupportedExtensions(t *testing.T) {
	supportedExts := []string{".go", ".py", ".ts", ".tsx", ".js", ".jsx"}

	for _, ext := range supportedExts {
		lang, ok := languages[ext]
		assert.True(t, ok, "extension %s should be supported", ext)
		assert.NotEmpty(t, lang.Name)
		assert.NotEmpty(t, lang.Skill)
		assert.NotNil(t, lang.IsPublic)
	}
}

func TestLanguages_UnsupportedExtension(t *testing.T) {
	unsupportedExts := []string{".java", ".rb", ".rs", ".c", ".cpp", ".h"}

	for _, ext := range unsupportedExts {
		_, ok := languages[ext]
		assert.False(t, ok, "extension %s should not be supported", ext)
	}
}

func TestLanguages_GoSkill(t *testing.T) {
	assert.Equal(t, "lsp/gopls", languages[".go"].Skill)
}

func TestLanguages_PythonSkill(t *testing.T) {
	assert.Equal(t, "lsp/pylsp", languages[".py"].Skill)
}

func TestLanguages_TypeScriptSkill(t *testing.T) {
	assert.Equal(t, "lsp/tsserver", languages[".ts"].Skill)
	assert.Equal(t, "lsp/tsserver", languages[".tsx"].Skill)
	assert.Equal(t, "lsp/tsserver", languages[".js"].Skill)
	assert.Equal(t, "lsp/tsserver", languages[".jsx"].Skill)
}

// Tests for hashPath helper

func TestHashPath_Deterministic(t *testing.T) {
	path := "/path/to/file.go"

	hash1 := hashPath(path)
	hash2 := hashPath(path)

	assert.Equal(t, hash1, hash2)
}

func TestHashPath_Different(t *testing.T) {
	hash1 := hashPath("/path/to/file1.go")
	hash2 := hashPath("/path/to/file2.go")

	assert.NotEqual(t, hash1, hash2)
}

func TestHashPath_EmptyString(t *testing.T) {
	hash := hashPath("")
	assert.Equal(t, "0", hash) // Empty string should produce "0"
}

func TestHashPath_LongPath(t *testing.T) {
	longPath := "/very/long/path/to/some/deeply/nested/directory/structure/with/file.go"
	hash := hashPath(longPath)
	assert.NotEmpty(t, hash)
}

// Tests for isSameFile helper

func TestIsSameFile_ExactMatch(t *testing.T) {
	result := isSameFile("/path/to/file.go", "/path/to/file.go", "/workspace")
	assert.True(t, result)
}

func TestIsSameFile_Different(t *testing.T) {
	result := isSameFile("/path/to/file1.go", "/path/to/file2.go", "/workspace")
	assert.False(t, result)
}

func TestIsSameFile_RelativeMatch(t *testing.T) {
	result := isSameFile("/workspace/src/file.go", "src/file.go", "/workspace")
	assert.True(t, result)
}

func TestIsSameFile_SuffixMatch(t *testing.T) {
	result := isSameFile("/workspace/pkg/handler.go", "handler.go", "/workspace")
	assert.True(t, result)
}

func TestIsSameFile_NoMatch(t *testing.T) {
	result := isSameFile("/workspace/src/file.go", "other/file.go", "/workspace")
	assert.False(t, result)
}

// Tests for workspaceArgs helper

func TestWorkspaceArgs_WithWorkspace(t *testing.T) {
	args := workspaceArgs("/my/workspace")
	assert.Equal(t, []string{"--workspace", "/my/workspace"}, args)
}

func TestWorkspaceArgs_EmptyWorkspace(t *testing.T) {
	args := workspaceArgs("")
	assert.Nil(t, args)
}

// Tests for formatImpactContext helper

func TestFormatImpactContext_SingleImpact(t *testing.T) {
	impacts := []Impact{
		{
			Symbol:     "Handler",
			SymbolType: "interface",
			RefCount:   2,
			RefFiles:   []string{"a.go", "b.go"},
		},
	}

	result := formatImpactContext("service.go", impacts)

	assert.Contains(t, result, "Impact:")
	assert.Contains(t, result, "service.go")
	assert.Contains(t, result, "Handler")
	assert.Contains(t, result, "interface")
	assert.Contains(t, result, "2 refs")
	assert.Contains(t, result, "a.go")
	assert.Contains(t, result, "b.go")
}

func TestFormatImpactContext_WithImplementations(t *testing.T) {
	impacts := []Impact{
		{
			Symbol:     "Store",
			SymbolType: "interface",
			RefCount:   1,
			RefFiles:   []string{"handler.go"},
			ImplCount:  2,
			ImplFiles:  []string{"memory_store.go", "db_store.go"},
		},
	}

	result := formatImpactContext("store.go", impacts)

	assert.Contains(t, result, "impls:")
	assert.Contains(t, result, "memory_store.go")
	assert.Contains(t, result, "db_store.go")
}

func TestFormatImpactContext_MultipleImpacts(t *testing.T) {
	impacts := []Impact{
		{Symbol: "Func1", SymbolType: "function", RefCount: 1, RefFiles: []string{"a.go"}},
		{Symbol: "Func2", SymbolType: "function", RefCount: 2, RefFiles: []string{"b.go", "c.go"}},
	}

	result := formatImpactContext("utils.go", impacts)

	assert.Contains(t, result, "Func1")
	assert.Contains(t, result, "Func2")
}

func TestFormatImpactContext_NoRefs(t *testing.T) {
	impacts := []Impact{
		{
			Symbol:     "Type",
			SymbolType: "struct",
			ImplCount:  1,
			ImplFiles:  []string{"impl.go"},
		},
	}

	result := formatImpactContext("types.go", impacts)

	assert.Contains(t, result, "Type")
	assert.Contains(t, result, "impls:")
}

// Tests for symbol types

func TestSymbolTypes(t *testing.T) {
	validTypes := []string{"function", "method", "type", "struct", "interface", "class"}

	for _, st := range validTypes {
		sym := Symbol{Name: "Test", Type: st, Line: 1}
		assert.Equal(t, st, sym.Type)
	}
}

// Edge case tests

func TestConfigFromMap_NilMap(t *testing.T) {
	// Should handle nil map gracefully
	cfg := ConfigFromMap(nil)

	assert.Equal(t, defaultMaxSymbols, cfg.MaxSymbols)
	assert.Equal(t, defaultMaxRefs, cfg.MaxRefs)
}

func TestImpact_EmptyRefFiles(t *testing.T) {
	imp := Impact{
		Symbol:     "Test",
		SymbolType: "function",
		RefCount:   0,
		RefFiles:   []string{},
	}

	assert.NotNil(t, imp.RefFiles)
	assert.Len(t, imp.RefFiles, 0)
}

func TestLanguages_LanguageNames(t *testing.T) {
	assert.Equal(t, "go", languages[".go"].Name)
	assert.Equal(t, "python", languages[".py"].Name)
	assert.Equal(t, "typescript", languages[".ts"].Name)
	assert.Equal(t, "typescript", languages[".tsx"].Name)
	assert.Equal(t, "javascript", languages[".js"].Name)
	assert.Equal(t, "javascript", languages[".jsx"].Name)
}

func TestHashPath_Unicode(t *testing.T) {
	hash := hashPath("/path/to/日本語/file.go")
	assert.NotEmpty(t, hash)
}

func TestFormatImpactContext_EmptyFilename(t *testing.T) {
	impacts := []Impact{
		{Symbol: "Test", SymbolType: "function", RefCount: 1, RefFiles: []string{"a.go"}},
	}

	result := formatImpactContext("", impacts)

	assert.Contains(t, result, "Impact:")
}

func TestIsSameFile_WindowsStylePaths(t *testing.T) {
	// Test with forward slashes (normalized paths)
	result := isSameFile("/workspace/src/file.go", "src/file.go", "/workspace")
	assert.True(t, result)
}

func TestConfigFromMap_EmptyValues(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		EnvImpactMaxSymbols: "",
		EnvImpactMaxRefs:    "",
		EnvImpactTimeout:    "",
	})

	// Empty strings should use defaults
	assert.Equal(t, defaultMaxSymbols, cfg.MaxSymbols)
	assert.Equal(t, defaultMaxRefs, cfg.MaxRefs)
	assert.Equal(t, defaultTimeout, cfg.Timeout)
}
