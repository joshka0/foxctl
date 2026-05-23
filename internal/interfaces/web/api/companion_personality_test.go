package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestCompanionPersonalityDimensionPatchHandler_EmitsTypedResponse(t *testing.T) {
	cfg := config.Config{
		Storage: config.StorageSettings{Root: t.TempDir()},
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"/api/companion/conversations/conversation-1/personality/dimension",
		strings.NewReader(`{"name":"verbosity","value":0.82}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	CompanionPersonalityDimensionPatchHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body CompanionPersonalityDimensionPatchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if !body.Success {
		t.Fatalf("success=%v want true", body.Success)
	}
	if body.Name != "verbosity" {
		t.Fatalf("name=%q want verbosity", body.Name)
	}
	if math.Abs(body.Value-0.82) > 0.000001 {
		t.Fatalf("value=%v want 0.82", body.Value)
	}
}
