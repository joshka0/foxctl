package embeddingtext

import (
	"path/filepath"
	"strings"
)

type profiledSource struct {
	Code    string
	Members []string
}

type sourceProfile struct {
	clean   func(string) string
	members func(string, string) []string
}

func profileSymbolSource(info SymbolInfo) profiledSource {
	profile := sourceProfileFor(info.FilePath, info.Language)
	code := profile.clean(info.Code)
	return profiledSource{
		Code:    code,
		Members: profile.members(code, info.Kind),
	}
}

func sourceProfileFor(filePath, language string) sourceProfile {
	lang := strings.ToLower(strings.TrimSpace(language))
	ext := strings.ToLower(filepath.Ext(filePath))
	switch {
	case lang == "go" || ext == ".go":
		return sourceProfile{clean: cleanSlashCommentSource, members: goMemberHints}
	case lang == "typescript" || lang == "javascript" || isTypeScriptLikeExt(ext):
		return sourceProfile{clean: cleanSlashCommentSource, members: tsMemberHints}
	case lang == "python" || ext == ".py":
		return sourceProfile{clean: cleanPythonSource, members: pythonMemberHints}
	case lang == "elixir" || ext == ".ex" || ext == ".exs":
		return sourceProfile{clean: cleanHashCommentSource, members: elixirMemberHints}
	default:
		return sourceProfile{clean: compactBlankLines, members: noMemberHints}
	}
}

func isTypeScriptLikeExt(ext string) bool {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return true
	default:
		return false
	}
}

func noMemberHints(string, string) []string {
	return nil
}
