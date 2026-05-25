package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"
)

func TestFilteredToolExecutor_BlocksDisallowed(t *testing.T) {
	inner := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return "ok:" + name, nil
		},
		Tools: []ToolDef{
			{Name: "a"},
			{Name: "b"},
		},
	}

	exec := NewFilteredToolExecutor(inner, []string{"b"})
	if exec == nil {
		t.Fatalf("expected executor")
	}

	if _, err := exec.Execute(context.Background(), "a", nil); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}

	out, err := exec.Execute(context.Background(), "b", nil)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if out != "ok:b" {
		t.Fatalf("unexpected output %q", out)
	}

	defs := exec.List()
	if len(defs) != 1 || defs[0].Name != "b" {
		t.Fatalf("unexpected defs %#v", defs)
	}
}

func TestFilterToolDefs(t *testing.T) {
	defs := []ToolDef{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	filtered := FilterToolDefs(defs, []string{"c", "a"})
	if len(filtered) != 2 || filtered[0].Name != "a" || filtered[1].Name != "c" {
		t.Fatalf("unexpected filtered %#v", filtered)
	}
}

func TestFilterToolDefsPropertyNormalizesAllowlistAndPreservesOrder(t *testing.T) {
	defs := filteredToolTestDefs()

	property := func(mask uint8, duplicateSeed uint8) bool {
		allowlist, allowed := filteredToolAllowlistForMask(defs, mask, duplicateSeed)

		filtered := FilterToolDefs(defs, allowlist)
		if len(allowed) == 0 {
			if !sameToolDefNames(filtered, defs) {
				t.Logf("empty normalized allowlist changed defs: got=%v want=%v", toolDefNames(filtered), toolDefNames(defs))
				return false
			}
			return true
		}

		want := make([]ToolDef, 0, len(allowed))
		for _, def := range defs {
			if _, ok := allowed[def.Name]; ok {
				want = append(want, def)
			}
		}
		if !sameToolDefNames(filtered, want) {
			t.Logf("filtered defs mismatch for allowlist %q: got=%v want=%v", allowlist, toolDefNames(filtered), toolDefNames(want))
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestFilteredToolExecutorPropertyBlocksDisallowedBeforeDelegation(t *testing.T) {
	defs := filteredToolTestDefs()

	property := func(allowSeed uint8, denySeed uint8, mask uint8, duplicateSeed uint8) bool {
		allowIndex := int(allowSeed) % len(defs)
		denyIndex := (allowIndex + 1 + int(denySeed)%(len(defs)-1)) % len(defs)
		mask &^= uint8(1 << denyIndex)
		allowlist, allowed := filteredToolAllowlistForMask(defs, mask, duplicateSeed)
		allowed[defs[allowIndex].Name] = struct{}{}
		allowlist = append(allowlist, "\t"+defs[allowIndex].Name+"\n")

		var called []string
		inner := &MockToolExecutor{
			ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
				called = append(called, name)
				return "ok:" + name, nil
			},
			Tools: defs,
		}

		exec := NewFilteredToolExecutor(inner, allowlist)
		if exec == inner {
			t.Logf("non-empty allowlist returned unfiltered executor: %q", allowlist)
			return false
		}

		if out, err := exec.Execute(context.Background(), defs[denyIndex].Name, nil); err == nil || out != "" {
			t.Logf("disallowed execute result out=%q err=%v", out, err)
			return false
		}
		if len(called) != 0 {
			t.Logf("disallowed tool delegated calls=%v", called)
			return false
		}

		out, err := exec.Execute(context.Background(), defs[allowIndex].Name, json.RawMessage(`{}`))
		if err != nil || out != "ok:"+defs[allowIndex].Name {
			t.Logf("allowed execute result out=%q err=%v", out, err)
			return false
		}
		if len(called) != 1 || called[0] != defs[allowIndex].Name {
			t.Logf("allowed tool delegation mismatch calls=%v", called)
			return false
		}

		wantListed := make([]ToolDef, 0, len(allowed))
		for _, def := range defs {
			if _, ok := allowed[def.Name]; ok {
				wantListed = append(wantListed, def)
			}
		}
		if !sameToolDefNames(exec.List(), wantListed) {
			t.Logf("filtered list mismatch: got=%v want=%v", toolDefNames(exec.List()), toolDefNames(wantListed))
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func filteredToolTestDefs() []ToolDef {
	return []ToolDef{
		{Name: "alpha.read"},
		{Name: "beta.write"},
		{Name: "gamma.search"},
		{Name: "delta.apply"},
		{Name: "epsilon.exec"},
	}
}

func filteredToolAllowlistForMask(defs []ToolDef, mask uint8, duplicateSeed uint8) ([]string, map[string]struct{}) {
	allowlist := []string{" ", "\n\t"}
	allowed := make(map[string]struct{})
	for i, def := range defs {
		if mask&(1<<i) == 0 {
			continue
		}
		allowed[def.Name] = struct{}{}
		allowlist = append(allowlist, paddedToolName(def.Name, int(duplicateSeed)+i))
		if (int(duplicateSeed)+i)%3 == 0 {
			allowlist = append(allowlist, "\t"+def.Name+" ")
		}
	}
	return allowlist, allowed
}

func paddedToolName(name string, seed int) string {
	switch seed % 4 {
	case 0:
		return name
	case 1:
		return " " + name
	case 2:
		return name + "\n"
	default:
		return "\t" + name + " "
	}
}

func sameToolDefNames(got []ToolDef, want []ToolDef) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].Name != want[i].Name {
			return false
		}
	}
	return true
}

func toolDefNames(defs []ToolDef) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}
