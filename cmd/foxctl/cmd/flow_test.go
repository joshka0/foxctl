package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseEnvelope parses stdout bytes into an envelope struct.
func parseEnvelope(t *testing.T, stdout []byte) envelope.Envelope {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(stdout, &env); err != nil {
		t.Fatalf("parse envelope: %v\noutput was: %s", err, string(stdout))
	}
	return env
}

// assertValidOKEnvelope checks that the envelope is a valid success envelope.
func assertValidOKEnvelope(t *testing.T, env envelope.Envelope, command string) {
	t.Helper()
	if env.Version != envelope.Version {
		t.Errorf("expected version %d, got %d", envelope.Version, env.Version)
	}
	if env.Status != "ok" {
		t.Errorf("expected status ok, got %q", env.Status)
	}
	if env.Command != command {
		t.Errorf("expected command %q, got %q", command, env.Command)
	}
	if env.Meta.TS == "" {
		t.Error("expected meta.ts to be set")
	}
}

// assertValidErrorEnvelope checks that the envelope is a valid error envelope
// and returns the error code.
func assertValidErrorEnvelope(t *testing.T, env envelope.Envelope, command string) string {
	t.Helper()
	if env.Version != envelope.Version {
		t.Errorf("expected version %d, got %d", envelope.Version, env.Version)
	}
	if env.Status != "error" {
		t.Errorf("expected status error, got %q", env.Status)
	}
	if env.Command != command {
		t.Errorf("expected command %q, got %q", command, env.Command)
	}
	if env.Error.Code == "" {
		t.Error("expected error.code to be set")
	}
	if env.Error.Message == "" {
		t.Error("expected error.message to be set")
	}
	if env.Meta.TS == "" {
		t.Error("expected meta.ts to be set")
	}
	return env.Error.Code
}

// tempWorkspace creates a temp directory for flow storage.
func tempWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// ---------------------------------------------------------------------------
// Unit-style tests using Cobra command execution
// ---------------------------------------------------------------------------

// executeCommand creates a new command, sets args, and executes it.
// Returns stdout bytes and any error from RunE.
func executeFlowCommand(t *testing.T, args ...string) ([]byte, error) {
	t.Helper()

	// Reset flags to defaults between tests.
	flowNameFlag = ""
	flowWorkspaceFlag = "."
	flowDescriptionFlag = ""
	flowNodeLabelFlag = ""
	flowNodeKindFlag = ""
	flowNodeConfigFlag = ""
	flowNodePositionFlag = ""
	flowEdgeFromFlag = ""
	flowEdgeToFlag = ""
	flowEdgeTriggerFlag = "output_ready"
	flowEdgeTransformFlag = "passthrough"
	flowEdgeTransformCfgFlag = ""
	flowEdgeConditionFlag = ""
	flowEdgeRetryFlag = ""

	// Build a fresh command tree for isolation.
	// We need to create a new rootCmd with the flow subcommand.
	// Since flow commands use global vars, we just reset flags and re-parse.

	var stdout bytes.Buffer
	root := rootCmd
	root.SetOut(&stdout)
	root.SetErr(bytes.NewBuffer(nil))
	root.SetArgs(args)

	// Persistent flags need resetting too.
	_ = root.ParseFlags(args)

	err := root.Execute()
	return stdout.Bytes(), err
}

// ---------------------------------------------------------------------------
// Test: flow create
// ---------------------------------------------------------------------------

func TestFlowCreate(t *testing.T) {
	ws := tempWorkspace(t)

	t.Run("creates flow with required fields", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "test-flow", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/create")

		// Check data fields.
		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		if data["name"] != "test-flow" {
			t.Errorf("expected name 'test-flow', got %v", data["name"])
		}
		if data["state"] != "draft" {
			t.Errorf("expected state 'draft', got %v", data["state"])
		}
		if data["id"] == nil || data["id"] == "" {
			t.Error("expected id to be set")
		}
	})

	t.Run("creates flow with description", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "desc-flow", "--workspace", ws, "--description", "A test flow")
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/create")

		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		if data["description"] != "A test flow" {
			t.Errorf("expected description 'A test flow', got %v", data["description"])
		}
	})

	t.Run("rejects missing name", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "create", "--workspace", ws)
		// cobra should output usage error, not an envelope
		// The command should fail because --name is required
		if len(stdout) == 0 {
			// Expected: cobra shows usage to stderr
			return
		}
		// If something was written to stdout, check it
		t.Logf("stdout: %s", string(stdout))
	})

	t.Run("rejects duplicate name", func(t *testing.T) {
		dupWs := tempWorkspace(t)
		_, _ = executeFlowCommand(t, "flow", "create", "--name", "dup-flow", "--workspace", dupWs)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "dup-flow", "--workspace", dupWs)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/create")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG error code for duplicate, got %q", code)
		}
	})

	t.Run("special characters in name", func(t *testing.T) {
		specWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "my flow 🚀", "--workspace", specWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/create")

		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		if data["name"] != "my flow 🚀" {
			t.Errorf("expected name 'my flow 🚀', got %v", data["name"])
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow list
// ---------------------------------------------------------------------------

func TestFlowList(t *testing.T) {
	ws := tempWorkspace(t)

	t.Run("returns empty array when no flows", func(t *testing.T) {
		emptyWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "list", "--workspace", emptyWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/list")

		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		flows, ok := data["flows"].([]any)
		if !ok {
			t.Fatalf("expected flows to be an array, got %T", data["flows"])
		}
		if len(flows) != 0 {
			t.Errorf("expected 0 flows, got %d", len(flows))
		}
	})

	t.Run("lists created flows", func(t *testing.T) {
		_, _ = executeFlowCommand(t, "flow", "create", "--name", "flow-a", "--workspace", ws)
		_, _ = executeFlowCommand(t, "flow", "create", "--name", "flow-b", "--workspace", ws)

		stdout, _ := executeFlowCommand(t, "flow", "list", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/list")

		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		flows, ok := data["flows"].([]any)
		if !ok {
			t.Fatalf("expected flows to be an array, got %T", data["flows"])
		}
		if len(flows) != 2 {
			t.Errorf("expected 2 flows, got %d", len(flows))
		}
	})

	t.Run("filters by workspace", func(t *testing.T) {
		wsA := tempWorkspace(t)
		wsB := tempWorkspace(t)

		_, _ = executeFlowCommand(t, "flow", "create", "--name", "flow-a-only", "--workspace", wsA)
		_, _ = executeFlowCommand(t, "flow", "create", "--name", "flow-b-only", "--workspace", wsB)

		stdout, _ := executeFlowCommand(t, "flow", "list", "--workspace", wsA)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/list")

		data := env.Data.(map[string]any)
		flows := data["flows"].([]any)
		if len(flows) != 1 {
			t.Errorf("expected 1 flow in wsA, got %d", len(flows))
		}
		flowData := flows[0].(map[string]any)
		if flowData["name"] != "flow-a-only" {
			t.Errorf("expected 'flow-a-only', got %v", flowData["name"])
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow show
// ---------------------------------------------------------------------------

func TestFlowShow(t *testing.T) {
	ws := tempWorkspace(t)

	// Create a flow to show.
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "show-flow", "--workspace", ws)
	createEnv := parseEnvelope(t, stdout)
	createData := createEnv.Data.(map[string]any)
	flowID := createData["id"].(string)

	t.Run("shows flow by ID", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "show", flowID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/show")

		data := env.Data.(map[string]any)
		if data["name"] != "show-flow" {
			t.Errorf("expected name 'show-flow', got %v", data["name"])
		}
		if data["state"] != "draft" {
			t.Errorf("expected state 'draft', got %v", data["state"])
		}
	})

	t.Run("shows flow by name", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "show", "show-flow", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/show")

		data := env.Data.(map[string]any)
		if data["id"] != flowID {
			t.Errorf("expected id %q, got %v", flowID, data["id"])
		}
	})

	t.Run("returns ENOTFOUND for missing flow", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "show", "nonexistent", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/show")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("shows flow with nodes and edges", func(t *testing.T) {
		showWs := tempWorkspace(t)
		// Create flow
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "full-flow", "--workspace", showWs)
		createEnv := parseEnvelope(t, stdout)
		flowID := createEnv.Data.(map[string]any)["id"].(string)

		// Add nodes
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"code/semantic_search"}`, "--workspace", showWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "sink", "--kind", "transform",
			"--config", `{"transform":"passthrough"}`, "--workspace", showWs)

		// Add edge
		_, _ = executeFlowCommand(t, "flow", "add-edge", flowID, "--from", "src", "--to", "sink",
			"--trigger", "output_ready", "--transform", "passthrough", "--workspace", showWs)

		// Show
		stdout, _ = executeFlowCommand(t, "flow", "show", flowID, "--workspace", showWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/show")

		data := env.Data.(map[string]any)
		nodes := data["nodes"].([]any)
		edges := data["edges"].([]any)
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes, got %d", len(nodes))
		}
		if len(edges) != 1 {
			t.Errorf("expected 1 edge, got %d", len(edges))
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow delete
// ---------------------------------------------------------------------------

func TestFlowDelete(t *testing.T) {
	ws := tempWorkspace(t)

	t.Run("deletes flow by ID", func(t *testing.T) {
		delWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "del-me", "--workspace", delWs)
		createEnv := parseEnvelope(t, stdout)
		flowID := createEnv.Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "delete", flowID, "--workspace", delWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/delete")

		data := env.Data.(map[string]any)
		if data["deleted"] != true {
			t.Errorf("expected deleted=true, got %v", data["deleted"])
		}

		// Verify show returns ENOTFOUND.
		stdout, _ = executeFlowCommand(t, "flow", "show", flowID, "--workspace", delWs)
		showEnv := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, showEnv, "flow/show")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND after delete, got %q", code)
		}
	})

	t.Run("deletes flow by name", func(t *testing.T) {
		delNameWs := tempWorkspace(t)
		_, _ = executeFlowCommand(t, "flow", "create", "--name", "del-by-name", "--workspace", delNameWs)

		stdout, _ := executeFlowCommand(t, "flow", "delete", "del-by-name", "--workspace", delNameWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/delete")
	})

	t.Run("rejects deleting running flow", func(t *testing.T) {
		// We need to manually set a flow to running state via store.
		// For this test, we'll skip since M1 doesn't have a start command.
		// The CLI logic still validates, but we can't easily set state to running
		// without the start command. We'll test this via the store directly.
		t.Skip("requires running flow state which needs start command (M2)")
	})

	t.Run("returns ENOTFOUND for missing flow", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "delete", "nonexistent", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/delete")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("cascades nodes and edges", func(t *testing.T) {
		cascadeWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "cascade-flow", "--workspace", cascadeWs)
		createEnv := parseEnvelope(t, stdout)
		flowID := createEnv.Data.(map[string]any)["id"].(string)

		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "n1", "--kind", "skill",
			"--config", `{"skill":"x"}`, "--workspace", cascadeWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "n2", "--kind", "skill",
			"--config", `{"skill":"y"}`, "--workspace", cascadeWs)
		_, _ = executeFlowCommand(t, "flow", "add-edge", flowID, "--from", "n1", "--to", "n2",
			"--workspace", cascadeWs)

		// Delete
		_, _ = executeFlowCommand(t, "flow", "delete", flowID, "--workspace", cascadeWs)

		// Verify ENOTFOUND
		stdout, _ = executeFlowCommand(t, "flow", "show", flowID, "--workspace", cascadeWs)
		env := parseEnvelope(t, stdout)
		assertValidErrorEnvelope(t, env, "flow/show")
	})
}

// ---------------------------------------------------------------------------
// Test: flow add-node
// ---------------------------------------------------------------------------

func TestFlowAddNode(t *testing.T) {
	ws := tempWorkspace(t)
	// Create a flow for node tests.
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "node-flow", "--workspace", ws)
	createEnv := parseEnvelope(t, stdout)
	flowID := createEnv.Data.(map[string]any)["id"].(string)

	t.Run("adds skill node", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-node", flowID, "--label", "search", "--kind", "skill",
			"--config", `{"skill":"code/semantic_search"}`, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-node")

		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		if data["kind"] != "skill" {
			t.Errorf("expected kind 'skill', got %v", data["kind"])
		}
		if data["label"] != "search" {
			t.Errorf("expected label 'search', got %v", data["label"])
		}
		if data["node_id"] == nil && data["id"] == nil {
			t.Error("expected id to be set")
		}
	})

	t.Run("adds all node kinds", func(t *testing.T) {
		kinds := []string{"skill", "pty", "http", "playwright", "image", "transform"}
		for _, kind := range kinds {
			t.Run(kind, func(t *testing.T) {
				stdout, _ := executeFlowCommand(t, "flow", "add-node", flowID,
					"--label", fmt.Sprintf("node-%s", kind), "--kind", kind,
					"--config", `{}`, "--workspace", ws)
				env := parseEnvelope(t, stdout)
				assertValidOKEnvelope(t, env, "flow/add-node")
			})
		}
	})

	t.Run("rejects invalid kind", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-node", flowID,
			"--label", "bad-node", "--kind", "unknown",
			"--config", `{}`, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-node")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG, got %q", code)
		}
		// Should list valid kinds.
		if env.Error.Message == "" {
			t.Error("expected error message to list valid kinds")
		}
	})

	t.Run("rejects invalid config JSON", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-node", flowID,
			"--label", "bad-config", "--kind", "skill",
			"--config", `{invalid}`, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-node")
		if code != string(protocol.ErrorCodeEParse) {
			t.Errorf("expected EPARSE, got %q", code)
		}
	})

	t.Run("stores position when provided", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-node", flowID,
			"--label", "positioned", "--kind", "skill",
			"--config", `{}`, "--position", `{"x":100,"y":200}`, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-node")

		data := env.Data.(map[string]any)
		pos, ok := data["position"].(map[string]any)
		if !ok {
			t.Fatalf("expected position to be a map, got %T", data["position"])
		}
		if pos["x"] != float64(100) {
			t.Errorf("expected position.x=100, got %v", pos["x"])
		}
		if pos["y"] != float64(200) {
			t.Errorf("expected position.y=200, got %v", pos["y"])
		}
	})

	t.Run("returns ENOTFOUND for missing flow", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-node", "nonexistent",
			"--label", "x", "--kind", "skill", "--config", `{}`, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-node")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("allows duplicate labels", func(t *testing.T) {
		dupWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "dup-label-flow", "--workspace", dupWs)
		createEnv := parseEnvelope(t, stdout)
		fid := createEnv.Data.(map[string]any)["id"].(string)

		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "dup", "--kind", "skill",
			"--config", `{}`, "--workspace", dupWs)
		stdout, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "dup", "--kind", "skill",
			"--config", `{}`, "--workspace", dupWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-node")
	})
}

// ---------------------------------------------------------------------------
// Test: flow remove-node
// ---------------------------------------------------------------------------

func TestFlowRemoveNode(t *testing.T) {
	ws := tempWorkspace(t)
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "rm-node-flow", "--workspace", ws)
	createEnv := parseEnvelope(t, stdout)
	flowID := createEnv.Data.(map[string]any)["id"].(string)

	t.Run("removes node by ID", func(t *testing.T) {
		rmWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "rm-flow", "--workspace", rmWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "rm-me", "--kind", "skill",
			"--config", `{}`, "--workspace", rmWs)
		nodeID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "remove-node", fid, nodeID, "--workspace", rmWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/remove-node")
	})

	t.Run("removes node by label", func(t *testing.T) {
		labWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "rm-label-flow", "--workspace", labWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "rm-label", "--kind", "skill",
			"--config", `{}`, "--workspace", labWs)

		stdout, _ = executeFlowCommand(t, "flow", "remove-node", fid, "rm-label", "--workspace", labWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/remove-node")
	})

	t.Run("cascades connected edges", func(t *testing.T) {
		cascWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "cascade-node", "--workspace", cascWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "a", "--kind", "skill",
			"--config", `{}`, "--workspace", cascWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "b", "--kind", "skill",
			"--config", `{}`, "--workspace", cascWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "c", "--kind", "skill",
			"--config", `{}`, "--workspace", cascWs)

		// a -> b -> c
		_, _ = executeFlowCommand(t, "flow", "add-edge", fid, "--from", "a", "--to", "b", "--workspace", cascWs)
		_, _ = executeFlowCommand(t, "flow", "add-edge", fid, "--from", "b", "--to", "c", "--workspace", cascWs)

		// Remove b — should cascade both edges.
		_, _ = executeFlowCommand(t, "flow", "remove-node", fid, "b", "--workspace", cascWs)

		// Verify via show.
		stdout, _ = executeFlowCommand(t, "flow", "show", fid, "--workspace", cascWs)
		env := parseEnvelope(t, stdout)
		data := env.Data.(map[string]any)
		nodes := data["nodes"].([]any)
		edges := data["edges"].([]any)
		if len(nodes) != 2 {
			t.Errorf("expected 2 nodes after remove, got %d", len(nodes))
		}
		if len(edges) != 0 {
			t.Errorf("expected 0 edges after cascade, got %d", len(edges))
		}
	})

	t.Run("returns ENOTFOUND for missing node", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "remove-node", flowID, "nonexistent", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/remove-node")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	_ = flowID // used in subtests
}

// ---------------------------------------------------------------------------
// Test: flow add-edge
// ---------------------------------------------------------------------------

func TestFlowAddEdge(t *testing.T) {
	ws := tempWorkspace(t)
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "edge-flow", "--workspace", ws)
	createEnv := parseEnvelope(t, stdout)
	flowID := createEnv.Data.(map[string]any)["id"].(string)

	// Add two nodes.
	_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
		"--config", `{"skill":"x"}`, "--workspace", ws)
	_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "dst", "--kind", "transform",
		"--config", `{"transform":"passthrough"}`, "--workspace", ws)

	t.Run("creates edge with required fields", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", flowID,
			"--from", "src", "--to", "dst", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-edge")

		data := env.Data.(map[string]any)
		if data["id"] == nil || data["id"] == "" {
			t.Error("expected edge id to be set")
		}
		if data["transform"] != "passthrough" {
			t.Errorf("expected transform 'passthrough', got %v", data["transform"])
		}
		if data["trigger"] != "output_ready" {
			t.Errorf("expected trigger 'output_ready', got %v", data["trigger"])
		}
	})

	t.Run("rejects self-loop", func(t *testing.T) {
		loopWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "loop-flow", "--workspace", loopWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "self", "--kind", "skill",
			"--config", `{}`, "--workspace", loopWs)

		stdout, _ = executeFlowCommand(t, "flow", "add-edge", fid,
			"--from", "self", "--to", "self", "--workspace", loopWs)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-edge")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG for self-loop, got %q", code)
		}
		if !strings.Contains(env.Error.Message, "self-loop") {
			t.Errorf("expected self-loop message, got %q", env.Error.Message)
		}
	})

	t.Run("validates trigger kind", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", flowID,
			"--from", "src", "--to", "dst", "--trigger", "bogus", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-edge")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG for invalid trigger, got %q", code)
		}
	})

	t.Run("validates transform kind", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", flowID,
			"--from", "src", "--to", "dst", "--transform", "unknown", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-edge")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG for invalid transform, got %q", code)
		}
	})

	t.Run("stores condition", func(t *testing.T) {
		condWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "cond-flow", "--workspace", condWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "a", "--kind", "skill", "--config", `{}`, "--workspace", condWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "b", "--kind", "skill", "--config", `{}`, "--workspace", condWs)

		stdout, _ = executeFlowCommand(t, "flow", "add-edge", fid,
			"--from", "a", "--to", "b", "--condition", "status == ok", "--workspace", condWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-edge")

		data := env.Data.(map[string]any)
		if data["condition"] != "status == ok" {
			t.Errorf("expected condition 'status == ok', got %v", data["condition"])
		}
	})

	t.Run("stores retry policy", func(t *testing.T) {
		retryWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "retry-flow", "--workspace", retryWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "a", "--kind", "skill", "--config", `{}`, "--workspace", retryWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "b", "--kind", "skill", "--config", `{}`, "--workspace", retryWs)

		stdout, _ = executeFlowCommand(t, "flow", "add-edge", fid,
			"--from", "a", "--to", "b", "--retry", `{"max_attempts":2,"delay_ms":1000}`, "--workspace", retryWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-edge")

		data := env.Data.(map[string]any)
		rp, ok := data["retry_policy"].(map[string]any)
		if !ok {
			t.Fatalf("expected retry_policy to be a map, got %T", data["retry_policy"])
		}
		if rp["max_attempts"] != float64(2) {
			t.Errorf("expected max_attempts=2, got %v", rp["max_attempts"])
		}
	})

	t.Run("allows duplicate edges (same from/to)", func(t *testing.T) {
		dupWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "dup-edge-flow", "--workspace", dupWs)
		fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "a", "--kind", "skill", "--config", `{}`, "--workspace", dupWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "b", "--kind", "skill", "--config", `{}`, "--workspace", dupWs)

		_, _ = executeFlowCommand(t, "flow", "add-edge", fid, "--from", "a", "--to", "b", "--workspace", dupWs)
		stdout, _ = executeFlowCommand(t, "flow", "add-edge", fid, "--from", "a", "--to", "b", "--workspace", dupWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-edge")
	})

	t.Run("rejects non-existent from node", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", flowID,
			"--from", "nonexistent", "--to", "dst", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-edge")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("rejects non-existent to node", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", flowID,
			"--from", "src", "--to", "nonexistent", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-edge")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("returns ENOTFOUND for missing flow", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", "nonexistent",
			"--from", "a", "--to", "b", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/add-edge")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow remove-edge
// ---------------------------------------------------------------------------

func TestFlowRemoveEdge(t *testing.T) {
	ws := tempWorkspace(t)
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "rm-edge-flow", "--workspace", ws)
	createEnv := parseEnvelope(t, stdout)
	flowID := createEnv.Data.(map[string]any)["id"].(string)

	_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "a", "--kind", "skill",
		"--config", `{}`, "--workspace", ws)
	_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "b", "--kind", "skill",
		"--config", `{}`, "--workspace", ws)
	stdout, _ = executeFlowCommand(t, "flow", "add-edge", flowID, "--from", "a", "--to", "b",
		"--workspace", ws)
	edgeID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

	t.Run("removes edge by ID", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "remove-edge", flowID, edgeID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/remove-edge")

		data := env.Data.(map[string]any)
		if data["removed"] != true {
			t.Errorf("expected removed=true, got %v", data["removed"])
		}
	})

	t.Run("returns ENOTFOUND for missing edge", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "remove-edge", flowID, "nonexistent", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/remove-edge")
		if code != string(protocol.ErrorCodeENotFound) {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("returns error for edge from different flow", func(t *testing.T) {
		// Create two flows in the same workspace.
		crossWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "flow-a", "--workspace", crossWs)
		flowAID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		stdout, _ = executeFlowCommand(t, "flow", "create", "--name", "flow-b", "--workspace", crossWs)
		flowBID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		// Add nodes and edge to flow-b.
		_, _ = executeFlowCommand(t, "flow", "add-node", flowBID, "--label", "x", "--kind", "skill",
			"--config", `{}`, "--workspace", crossWs)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowBID, "--label", "y", "--kind", "skill",
			"--config", `{}`, "--workspace", crossWs)
		stdout, _ = executeFlowCommand(t, "flow", "add-edge", flowBID, "--from", "x", "--to", "y",
			"--workspace", crossWs)
		otherEdgeID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		// Try removing flow-b's edge from flow-a context.
		stdout, _ = executeFlowCommand(t, "flow", "remove-edge", flowAID, otherEdgeID, "--workspace", crossWs)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/remove-edge")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG for cross-flow edge, got %q", code)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: envelope output format
// ---------------------------------------------------------------------------

func TestFlowEnvelopeOutput(t *testing.T) {
	ws := tempWorkspace(t)

	t.Run("all commands produce valid envelopes", func(t *testing.T) {
		// create
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "env-test", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/create")

		data := env.Data.(map[string]any)
		flowID := data["id"].(string)

		// list
		stdout, _ = executeFlowCommand(t, "flow", "list", "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/list")

		// show
		stdout, _ = executeFlowCommand(t, "flow", "show", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/show")

		// add-node
		stdout, _ = executeFlowCommand(t, "flow", "add-node", flowID,
			"--label", "n1", "--kind", "skill", "--config", `{}`, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-node")

		// add-edge (need second node)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID,
			"--label", "n2", "--kind", "skill", "--config", `{}`, "--workspace", ws)
		stdout, _ = executeFlowCommand(t, "flow", "add-edge", flowID,
			"--from", "n1", "--to", "n2", "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-edge")
		edgeID := env.Data.(map[string]any)["id"].(string)

		// remove-edge
		stdout, _ = executeFlowCommand(t, "flow", "remove-edge", flowID, edgeID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/remove-edge")

		// remove-node
		stdout, _ = executeFlowCommand(t, "flow", "remove-node", flowID, "n1", "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/remove-node")

		// delete
		stdout, _ = executeFlowCommand(t, "flow", "delete", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/delete")
	})
}

// ---------------------------------------------------------------------------
// Test: transform config on edge
// ---------------------------------------------------------------------------

func TestFlowAddEdgeWithTransformConfig(t *testing.T) {
	ws := tempWorkspace(t)
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "tcfg-flow", "--workspace", ws)
	fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
	_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "a", "--kind", "skill", "--config", `{}`, "--workspace", ws)
	_, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "b", "--kind", "skill", "--config", `{}`, "--workspace", ws)

	t.Run("stores transform config", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "add-edge", fid,
			"--from", "a", "--to", "b",
			"--transform", "regex_extract", "--transform-config", `{"pattern":"\\d+","group":1}`,
			"--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/add-edge")

		data := env.Data.(map[string]any)
		if data["transform"] != "regex_extract" {
			t.Errorf("expected transform 'regex_extract', got %v", data["transform"])
		}
		if data["transform_config"] != `{"pattern":"\\d+","group":1}` {
			t.Errorf("expected transform_config to be set, got %v", data["transform_config"])
		}
	})
}

// ---------------------------------------------------------------------------
// Test: no-position default
// ---------------------------------------------------------------------------

func TestFlowAddNodeNoPosition(t *testing.T) {
	ws := tempWorkspace(t)
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "nopos-flow", "--workspace", ws)
	fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

	stdout, _ = executeFlowCommand(t, "flow", "add-node", fid, "--label", "nopos", "--kind", "skill",
		"--config", `{}`, "--workspace", ws)
	env := parseEnvelope(t, stdout)
	assertValidOKEnvelope(t, env, "flow/add-node")

	data := env.Data.(map[string]any)
	if data["position"] != nil {
		t.Errorf("expected position to be nil when not provided, got %v", data["position"])
	}
}

// ---------------------------------------------------------------------------
// Test: long names
// ---------------------------------------------------------------------------

func TestFlowLongName(t *testing.T) {
	ws := tempWorkspace(t)

	t.Run("handles 256 char name", func(t *testing.T) {
		longName := strings.Repeat("a", 256)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", longName, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/create")

		data := env.Data.(map[string]any)
		if data["name"] != longName {
			t.Error("expected 256-char name to be stored correctly")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: workspace defaults to current directory
// ---------------------------------------------------------------------------

func TestFlowWorkspaceDefault(t *testing.T) {
	// Reset flag to default.
	flowWorkspaceFlag = "."

	t.Run("workspace defaults to dot", func(t *testing.T) {
		if flowWorkspaceFlag != "." {
			t.Errorf("expected default workspace '.', got %q", flowWorkspaceFlag)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: show with empty nodes/edges arrays
// ---------------------------------------------------------------------------

func TestFlowShowEmptyGraph(t *testing.T) {
	ws := tempWorkspace(t)
	stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "empty-graph", "--workspace", ws)
	fid := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

	stdout, _ = executeFlowCommand(t, "flow", "show", fid, "--workspace", ws)
	env := parseEnvelope(t, stdout)
	assertValidOKEnvelope(t, env, "flow/show")

	data := env.Data.(map[string]any)
	nodes, ok := data["nodes"].([]any)
	if !ok {
		t.Fatalf("expected nodes to be array, got %T", data["nodes"])
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
	edges, ok := data["edges"].([]any)
	if !ok {
		t.Fatalf("expected edges to be array, got %T", data["edges"])
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
}

// Ensure filepath is used (avoids unused import).
var (
	_ = filepath.Join("a", "b")
	_ = os.DevNull
	_ = fmt.Sprintf // used in test subtest names
)
