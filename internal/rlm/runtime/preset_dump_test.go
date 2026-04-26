package runtime

import (
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/tooling/skillrun/ephemeral"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

func TestPresetSourcesValidateAsGoSkill(t *testing.T) {
	cases := map[string]string{
		"hm":     hindleyMilnerPresetSource(),
		"math":   backtrackingDAGPresetSource(),
		"smiles": smilesPropertyPresetSource(),
		"fen":    fenSearchPresetSource(),
	}
	for name, src := range cases {
		err := ephemeral.ValidateGoSkillSource(src)
		if err != nil {
			t.Errorf("preset %s ValidateGoSkillSource err: %v", name, err)
			// Re-parse with all errors visible
			full := "package ephemeral\n" + src
			lines := strings.Split(full, "\n")
			fset := token.NewFileSet()
			_, perr := parser.ParseFile(fset, "ephemeral_skill.go", full, parser.AllErrors)
			if errs, ok := perr.(scanner.ErrorList); ok {
				for i, e := range errs {
					if i >= 5 {
						t.Logf("...and %d more", len(errs)-i)
						break
					}
					t.Logf("ERR[%d]: %v", i, e)
					ln := e.Pos.Line
					if ln-1 >= 0 && ln-1 < len(lines) {
						t.Logf("  line %d: %q", ln, lines[ln-1])
					}
				}
			} else {
				t.Logf("non-list err: %v", perr)
			}
		} else {
			t.Logf("preset %s OK", name)
		}
	}
}

// TestPresetSourcesEvalUnderYaegi confirms each preset can be loaded by
// yaegi (the actual runtime used in production), catching forward-reference
// or other interpreter-level issues that go/parser does not flag.
func TestPresetSourcesEvalUnderYaegi(t *testing.T) {
	cases := map[string]string{
		"hm":     hindleyMilnerPresetSource(),
		"math":   backtrackingDAGPresetSource(),
		"smiles": smilesPropertyPresetSource(),
		"fen":    fenSearchPresetSource(),
	}
	for name, src := range cases {
		_, err := ephemeral.NewGoSkillRunner(ephemeral.GoSkillSpec{
			Name:   "preset_" + name,
			Source: src,
		})
		if err != nil {
			t.Errorf("preset %s NewGoSkillRunner err: %v", name, err)
			continue
		}
		i := interp.New(interp.Options{})
		if err := i.Use(stdlib.Symbols); err != nil {
			t.Fatalf("preset %s: load stdlib: %v", name, err)
		}
		// Mirror the import injection done by addMissingAllowedImports.
		injected := "import \"strings\"\nimport \"strconv\"\nimport \"sort\"\nimport \"math\"\nimport \"fmt\"\n" + src
		if _, err := i.Eval(injected); err != nil {
			t.Errorf("preset %s yaegi Eval err: %v", name, err)
		}
	}
}
