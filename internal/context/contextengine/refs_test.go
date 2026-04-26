package contextengine

import (
	"encoding/json"
	"testing"
)

func TestRefType_IsValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		refType RefType
		valid   bool
	}{
		{RefTypePath, true},
		{RefTypeSymbol, true},
		{RefTypeTask, true},
		{RefTypeSession, true},
		{RefTypeMemoryClaim, true},
		{RefTypeNote, true},
		{RefTypeArtifact, true},
		{RefTypeTrajectory, true},
		{RefTypeCommit, true},
		{RefTypeEvent, true},
		{RefTypeRun, true},
		{RefTypeToolCall, true},
		{RefType("invalid"), false},
		{RefType(""), false},
	}

	for _, tc := range tests {
		t.Run(string(tc.refType), func(t *testing.T) {
			if got := tc.refType.IsValid(); got != tc.valid {
				t.Errorf("IsValid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestParseEvidenceRef(t *testing.T) {
	t.Parallel()

	t.Run("valid_path", func(t *testing.T) {
		ref, err := ParseEvidenceRef("path:src/main.go")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.Type != RefTypePath {
			t.Errorf("Type: got %q, want %q", ref.Type, RefTypePath)
		}
		if ref.Ref != "src/main.go" {
			t.Errorf("Ref: got %q, want %q", ref.Ref, "src/main.go")
		}
	})

	t.Run("valid_symbol", func(t *testing.T) {
		ref, err := ParseEvidenceRef("symbol:main")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ref.Type != RefTypeSymbol {
			t.Errorf("Type: got %q, want %q", ref.Type, RefTypeSymbol)
		}
	})

	t.Run("empty_string", func(t *testing.T) {
		_, err := ParseEvidenceRef("")
		if err == nil {
			t.Error("expected error for empty string")
		}
	})

	t.Run("missing_colon", func(t *testing.T) {
		_, err := ParseEvidenceRef("pathwithoutcolon")
		if err == nil {
			t.Error("expected error for missing colon")
		}
	})

	t.Run("empty_ref_value", func(t *testing.T) {
		_, err := ParseEvidenceRef("path:")
		if err == nil {
			t.Error("expected error for empty ref value")
		}
	})

	t.Run("invalid_ref_type", func(t *testing.T) {
		_, err := ParseEvidenceRef("invalid:value")
		if err == nil {
			t.Error("expected error for invalid ref type")
		}
	})

	t.Run("colon_at_start", func(t *testing.T) {
		_, err := ParseEvidenceRef(":value")
		if err == nil {
			t.Error("expected error for colon at start")
		}
	})
}

func TestFormatEvidenceRef(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		got := FormatEvidenceRef(ref)
		if got != "path:src/main.go" {
			t.Errorf("got %q, want %q", got, "path:src/main.go")
		}
	})

	t.Run("empty_type", func(t *testing.T) {
		ref := EvidenceRef{Type: "", Ref: "src/main.go"}
		if got := FormatEvidenceRef(ref); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("empty_ref", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: ""}
		if got := FormatEvidenceRef(ref); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestNormalizeEvidenceRef(t *testing.T) {
	t.Parallel()

	t.Run("fills_empty_workspace_id", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		got := NormalizeEvidenceRef(ref, "ws-1")
		if got.WorkspaceID != "ws-1" {
			t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, "ws-1")
		}
	})

	t.Run("preserves_existing_workspace_id", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: "src/main.go", WorkspaceID: "ws-existing"}
		got := NormalizeEvidenceRef(ref, "ws-new")
		if got.WorkspaceID != "ws-existing" {
			t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, "ws-existing")
		}
	})

	t.Run("empty_workspace_id_param", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		got := NormalizeEvidenceRef(ref, "")
		if got.WorkspaceID != "" {
			t.Errorf("WorkspaceID: got %q, want empty", got.WorkspaceID)
		}
	})
}

func TestValidateEvidenceRef(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		if err := ValidateEvidenceRef(ref); err != nil {
			t.Errorf("expected valid, got %v", err)
		}
	})

	t.Run("invalid_type", func(t *testing.T) {
		ref := EvidenceRef{Type: "invalid", Ref: "x"}
		if err := ValidateEvidenceRef(ref); err == nil {
			t.Error("expected error for invalid type")
		}
	})

	t.Run("empty_ref", func(t *testing.T) {
		ref := EvidenceRef{Type: RefTypePath, Ref: ""}
		if err := ValidateEvidenceRef(ref); err == nil {
			t.Error("expected error for empty ref")
		}
	})
}

func TestEvidenceRef_Equal(t *testing.T) {
	t.Parallel()

	t.Run("equal", func(t *testing.T) {
		a := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		b := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		if !a.Equal(b) {
			t.Error("expected equal")
		}
	})

	t.Run("different_type", func(t *testing.T) {
		a := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		b := EvidenceRef{Type: RefTypeSymbol, Ref: "src/main.go"}
		if a.Equal(b) {
			t.Error("expected not equal")
		}
	})

	t.Run("different_ref", func(t *testing.T) {
		a := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
		b := EvidenceRef{Type: RefTypePath, Ref: "src/other.go"}
		if a.Equal(b) {
			t.Error("expected not equal")
		}
	})
}

func TestEvidenceRef_RoundTrip(t *testing.T) {
	t.Parallel()
	orig := EvidenceRef{
		Type:        RefTypeSymbol,
		Ref:         "AuthenticationService",
		WorkspaceID: "ws-1",
		Title:       "Auth Service",
		Excerpt:     "Handles user authentication...",
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got EvidenceRef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != orig.Type {
		t.Errorf("Type: got %q, want %q", got.Type, orig.Type)
	}
	if got.Ref != orig.Ref {
		t.Errorf("Ref: got %q, want %q", got.Ref, orig.Ref)
	}
	if got.WorkspaceID != orig.WorkspaceID {
		t.Errorf("WorkspaceID: got %q, want %q", got.WorkspaceID, orig.WorkspaceID)
	}
}

func TestEvidenceRef_Validate_Method(t *testing.T) {
	t.Parallel()
	// Test that the Validate method delegates correctly
	valid := EvidenceRef{Type: RefTypePath, Ref: "src/main.go"}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid, got %v", err)
	}

	invalid := EvidenceRef{Type: "bad", Ref: "x"}
	if err := invalid.Validate(); err == nil {
		t.Error("expected error for invalid ref")
	}
}
