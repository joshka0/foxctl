package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// Mock AgentSpawner
// ---------------------------------------------------------------------------

type mockAgentSpawner struct {
	mu sync.Mutex

	// Spawn behavior
	spawnResult *AgentSpawnResult
	spawnErr    error
	spawnCalls  []spawnCall

	// Ask behavior
	askResult *AgentAskResult
	askErr    error
	askCalls  []askCall

	// Info behavior
	infoResults map[string][]*AgentInfoResult // agentID -> sequence of results (for polling)
	infoErr     error

	// Kill behavior
	killErr   error
	killCalls []string
}

type spawnCall struct {
	Role   string
	Prompt string
	Opts   AgentSpawnOptions
}

type askCall struct {
	AgentID   string
	Message   string
	TimeoutMS int
}

func (m *mockAgentSpawner) Spawn(ctx context.Context, role, prompt string, opts AgentSpawnOptions) (*AgentSpawnResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawnCalls = append(m.spawnCalls, spawnCall{Role: role, Prompt: prompt, Opts: opts})
	if m.spawnErr != nil {
		return nil, m.spawnErr
	}
	return m.spawnResult, nil
}

func (m *mockAgentSpawner) Ask(ctx context.Context, agentID string, message string, timeoutMS int) (*AgentAskResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.askCalls = append(m.askCalls, askCall{AgentID: agentID, Message: message, TimeoutMS: timeoutMS})
	if m.askErr != nil {
		return nil, m.askErr
	}
	return m.askResult, nil
}

func (m *mockAgentSpawner) Info(ctx context.Context, agentID string) (*AgentInfoResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.infoErr != nil {
		return nil, m.infoErr
	}
	if results, ok := m.infoResults[agentID]; ok && len(results) > 0 {
		result := results[0]
		m.infoResults[agentID] = results[1:]
		return result, nil
	}
	// Default: agent still running
	return &AgentInfoResult{Status: "running"}, nil
}

func (m *mockAgentSpawner) Kill(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killCalls = append(m.killCalls, sessionID)
	return m.killErr
}

// ---------------------------------------------------------------------------
// Helper constructors
// ---------------------------------------------------------------------------

func makeAgentNode(cfg AgentConfig) FlowNode {
	cfgBytes, _ := json.Marshal(cfg)
	return FlowNode{
		ID:     "test-agent-node",
		FlowID: "test-flow",
		Kind:   NodeAgent,
		Label:  "test-agent",
		Config: cfgBytes,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAgentExecutor_ValidateConfig_MissingRole(t *testing.T) {
	spawner := &mockAgentSpawner{}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Prompt: "test prompt",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "EARG" {
		t.Errorf("expected EARG error code, got %q", output.Envelope.Error.Code)
	}
}

func TestAgentExecutor_ValidateConfig_InvalidInputMode(t *testing.T) {
	spawner := &mockAgentSpawner{}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:      "researcher",
		Prompt:    "test",
		InputMode: "invalid",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "EARG" {
		t.Errorf("expected EARG error code, got %q", output.Envelope.Error.Code)
	}
}

func TestAgentExecutor_ValidateConfig_InvalidOutputMode(t *testing.T) {
	spawner := &mockAgentSpawner{}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "test",
		OutputMode: "invalid",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "EARG" {
		t.Errorf("expected EARG error code, got %q", output.Envelope.Error.Code)
	}
}

func TestAgentExecutor_SessionSummaryMode_Success(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "running"},
				{Status: "running"},
				{Status: "completed", Summary: "Research complete. Found 3 relevant files."},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:   "researcher",
		Prompt: "Research the auth module",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != "ok" {
		t.Errorf("expected ok status, got %q; error: %v", output.Envelope.Status, output.Envelope.Error)
	}
	if output.NodeID != "test-agent-node" {
		t.Errorf("expected node_id test-agent-node, got %q", output.NodeID)
	}

	data, ok := output.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", output.Envelope.Data)
	}
	if data["output_mode"] != "session_summary" {
		t.Errorf("expected output_mode session_summary, got %v", data["output_mode"])
	}
	if data["summary"] != "Research complete. Found 3 relevant files." {
		t.Errorf("unexpected summary: %v", data["summary"])
	}
	if data["agent_id"] != "agent-123" {
		t.Errorf("expected agent_id agent-123, got %v", data["agent_id"])
	}

	// Verify spawn was called correctly
	if len(spawner.spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.spawnCalls))
	}
	if spawner.spawnCalls[0].Role != "researcher" {
		t.Errorf("expected role researcher, got %q", spawner.spawnCalls[0].Role)
	}
	if spawner.spawnCalls[0].Prompt != "Research the auth module" {
		t.Errorf("unexpected prompt: %q", spawner.spawnCalls[0].Prompt)
	}
}

func TestAgentExecutor_InputModePrompt_InjectsUpstreamData(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "completed", Summary: "Done"},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:      "researcher",
		Prompt:    "Analyze this data",
		InputMode: "prompt",
	})

	// Upstream input
	upstreamData := map[string]any{"query": "auth", "results": []string{"file1.go", "file2.go"}}

	output, err := executor.Execute(context.Background(), node, upstreamData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != "ok" {
		t.Errorf("expected ok status, got %q", output.Envelope.Status)
	}

	// Verify the prompt includes upstream data
	if len(spawner.spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.spawnCalls))
	}
	prompt := spawner.spawnCalls[0].Prompt
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	// Should contain the original prompt
	if prompt[:len("Analyze this data")] != "Analyze this data" {
		t.Errorf("expected prompt to start with original prompt, got: %q", prompt[:50])
	}
	// Should contain upstream data marker
	if !contains(prompt, "--- Upstream Data ---") {
		t.Errorf("expected prompt to contain upstream data marker, got: %q", prompt)
	}
}

func TestAgentExecutor_InputModeAsk_SendsAsk(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		askResult: &AgentAskResult{
			Reply:  "Found 3 files matching the query",
			Status: "replied",
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "You are a researcher",
		InputMode:  "ask",
		OutputMode: "ask",
	})

	upstreamData := map[string]any{"query": "auth"}
	output, err := executor.Execute(context.Background(), node, upstreamData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != "ok" {
		t.Errorf("expected ok status, got %q", output.Envelope.Status)
	}

	data, ok := output.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", output.Envelope.Data)
	}
	if data["output_mode"] != "ask" {
		t.Errorf("expected output_mode ask, got %v", data["output_mode"])
	}
	if data["reply"] != "Found 3 files matching the query" {
		t.Errorf("unexpected reply: %v", data["reply"])
	}

	// Verify ask was called
	if len(spawner.askCalls) != 1 {
		t.Fatalf("expected 1 ask call, got %d", len(spawner.askCalls))
	}
	if spawner.askCalls[0].AgentID != "agent-123" {
		t.Errorf("expected agent_id agent-123, got %q", spawner.askCalls[0].AgentID)
	}
}

func TestAgentExecutor_TimeoutHandling(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		// Agent never completes
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:    "researcher",
		Prompt:  "Research something",
		Timeout: "100ms", // Very short timeout
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	output, err := executor.Execute(ctx, node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "ETIMEOUT" {
		t.Errorf("expected ETIMEOUT error code, got %q", output.Envelope.Error.Code)
	}

	// Verify kill was called
	if len(spawner.killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(spawner.killCalls))
	}
}

func TestAgentExecutor_SpawnError(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnErr: fmt.Errorf("no LLM providers configured"),
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:   "researcher",
		Prompt: "Research something",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "ESPAWN" {
		t.Errorf("expected ESPAWN error code, got %q", output.Envelope.Error.Code)
	}
}

func TestAgentExecutor_AgentError(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "running"},
				{Status: "error", Error: "LLM API rate limit exceeded"},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:   "researcher",
		Prompt: "Research something",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "EAGENT" {
		t.Errorf("expected EAGENT error code, got %q", output.Envelope.Error.Code)
	}
	if output.Envelope.Error.Message != "LLM API rate limit exceeded" {
		t.Errorf("unexpected error message: %q", output.Envelope.Error.Message)
	}
}

func TestAgentExecutor_AskTimeout(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		askErr: fmt.Errorf("timeout waiting for reply"),
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Research",
		InputMode:  "ask",
		OutputMode: "ask",
	})

	output, err := executor.Execute(context.Background(), node, map[string]any{"q": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "ETIMEOUT" {
		t.Errorf("expected ETIMEOUT error code, got %q", output.Envelope.Error.Code)
	}

	// Verify kill was called to clean up
	if len(spawner.killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(spawner.killCalls))
	}
}

func TestAgentExecutor_WorkspacePropagation(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "completed", Summary: "Done"},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/default"}

	node := makeAgentNode(AgentConfig{
		Role:      "researcher",
		Prompt:    "Research",
		Workspace: "/custom/workspace",
	})

	_, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify workspace was propagated to spawn
	if len(spawner.spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.spawnCalls))
	}
	if spawner.spawnCalls[0].Opts.Workspace != "/custom/workspace" {
		t.Errorf("expected workspace /custom/workspace, got %q", spawner.spawnCalls[0].Opts.Workspace)
	}
}

func TestAgentExecutor_DefaultWorkspace(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "completed", Summary: "Done"},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/default/workspace"}

	node := makeAgentNode(AgentConfig{
		Role:   "researcher",
		Prompt: "Research",
		// No workspace specified — should use executor's default
	})

	_, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spawner.spawnCalls[0].Opts.Workspace != "/default/workspace" {
		t.Errorf("expected workspace /default/workspace, got %q", spawner.spawnCalls[0].Opts.Workspace)
	}
}

func TestAgentExecutor_ExecModePropagation(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "completed", Summary: "Done"},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:          "researcher",
		Prompt:        "Research",
		ExecMode:      "autonomous",
		MaxIterations: 20,
		MaxAutoTurns:  3,
		LLMProvider:   "openrouter",
		LLMModel:      "openrouter/aurora-alpha",
		SkillsAllow:   []string{"code_search", "fs_read_file"},
	})

	_, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	opts := spawner.spawnCalls[0].Opts
	if opts.ExecMode != "autonomous" {
		t.Errorf("expected exec_mode autonomous, got %q", opts.ExecMode)
	}
	if opts.MaxIterations != 20 {
		t.Errorf("expected max_iterations 20, got %d", opts.MaxIterations)
	}
	if opts.MaxAutoTurns != 3 {
		t.Errorf("expected max_auto_turns 3, got %d", opts.MaxAutoTurns)
	}
	if opts.LLMProvider != "openrouter" {
		t.Errorf("expected llm_provider openrouter, got %q", opts.LLMProvider)
	}
	if opts.LLMModel != "openrouter/aurora-alpha" {
		t.Errorf("expected llm_model openrouter/aurora-alpha, got %q", opts.LLMModel)
	}
	if len(opts.SkillsAllow) != 2 || opts.SkillsAllow[0] != "code_search" || opts.SkillsAllow[1] != "fs_read_file" {
		t.Errorf("expected skills_allow [code_search, fs_read_file], got %v", opts.SkillsAllow)
	}
}

func TestAgentExecutor_CompletedWithSummaryAndError(t *testing.T) {
	// Test that when an agent completes but has an error in its status,
	// the output envelope correctly reports the error while still including summary.
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-123",
			SessionID: "session-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-123": {
				{Status: "completed", Summary: "Partial results", Error: "ran out of iterations"},
			},
		},
	}
	executor := &AgentExecutor{Spawner: spawner, Workspace: "/tmp"}

	node := makeAgentNode(AgentConfig{
		Role:   "researcher",
		Prompt: "Research something complex",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	data, ok := output.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", output.Envelope.Data)
	}
	if data["summary"] != "Partial results" {
		t.Errorf("expected summary 'Partial results', got %v", data["summary"])
	}
	if data["error"] != "ran out of iterations" {
		t.Errorf("expected error 'ran out of iterations', got %v", data["error"])
	}
}

func TestAgentConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AgentConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid minimal config",
			cfg:     AgentConfig{Role: "researcher", Prompt: "test"},
			wantErr: false,
		},
		{
			name:    "missing role",
			cfg:     AgentConfig{Prompt: "test"},
			wantErr: true,
			errMsg:  "role is required",
		},
		{
			name:    "missing prompt with default input_mode",
			cfg:     AgentConfig{Role: "researcher"},
			wantErr: true,
			errMsg:  "prompt is required",
		},
		{
			name:    "missing prompt with ask input_mode is ok",
			cfg:     AgentConfig{Role: "researcher", InputMode: "ask"},
			wantErr: false,
		},
		{
			name:    "invalid input_mode",
			cfg:     AgentConfig{Role: "researcher", Prompt: "test", InputMode: "bad"},
			wantErr: true,
			errMsg:  "invalid input_mode",
		},
		{
			name:    "invalid output_mode",
			cfg:     AgentConfig{Role: "researcher", Prompt: "test", OutputMode: "bad"},
			wantErr: true,
			errMsg:  "invalid output_mode",
		},
		{
			name:    "valid with all fields",
			cfg:     AgentConfig{Role: "coder", Prompt: "code", ExecMode: "autonomous", MaxIterations: 10, MaxAutoTurns: 5, Timeout: "5m", LLMProvider: "openrouter", LLMModel: "test", SkillsAllow: []string{"s1"}, InputMode: "prompt", OutputMode: "session_summary", AskTimeoutMS: 10000, Workspace: "/tmp"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errMsg)
				}
			}
		})
	}
}

func TestNodeAgentKind_IsValid(t *testing.T) {
	if !NodeAgent.IsValid() {
		t.Error("NodeAgent should be a valid NodeKind")
	}
}

func TestNodeAgentKind_InValidNodeKinds(t *testing.T) {
	found := false
	for _, k := range ValidNodeKinds {
		if k == NodeAgent {
			found = true
			break
		}
	}
	if !found {
		t.Error("NodeAgent should be in ValidNodeKinds")
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Push Output Mode Tests
// ---------------------------------------------------------------------------

func TestAgentExecutor_PushMode_WaitsForPushedOutput(t *testing.T) {
	// Simulate push mode: the executor spawns an agent, subscribes to the
	// OutputBus, and waits for externally pushed output.
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
	}

	// Create a channel to simulate the pushed output.
	pushCh := make(chan NodeOutput, 1)

	// Push a result after a short delay.
	expectedData := map[string]any{"result": "research complete", "files": []string{"a.go", "b.go"}}
	expectedOutput := NodeOutput{
		Envelope: envelope.OK("flow/output", expectedData),
		NodeID:   "test-agent-node",
	}
	pushCh <- expectedOutput

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		SubscribeOutput: func(flowID, nodeID string) <-chan NodeOutput {
			if flowID != "test-flow" || nodeID != "test-agent-node" {
				t.Errorf("unexpected SubscribeOutput args: flowID=%q nodeID=%q", flowID, nodeID)
			}
			return pushCh
		},
		GetRunID: func(flowID string) string {
			return "run-abc-123"
		},
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Research the auth module",
		OutputMode: "push",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := executor.Execute(ctx, node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != "ok" {
		t.Errorf("expected ok status, got %q; error: %v", output.Envelope.Status, output.Envelope.Error)
	}
	if output.NodeID != "test-agent-node" {
		t.Errorf("expected node_id test-agent-node, got %q", output.NodeID)
	}

	// Verify the pushed data was returned.
	data, ok := output.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", output.Envelope.Data)
	}
	if data["result"] != "research complete" {
		t.Errorf("expected result 'research complete', got %v", data["result"])
	}
}

func TestAgentExecutor_PushMode_IncludesRunIDInPrompt(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
	}

	pushCh := make(chan NodeOutput, 1)
	pushCh <- NodeOutput{
		Envelope: envelope.OK("flow/output", map[string]any{"done": true}),
		NodeID:   "test-agent-node",
	}

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		SubscribeOutput: func(flowID, nodeID string) <-chan NodeOutput {
			return pushCh
		},
		GetRunID: func(flowID string) string {
			return "run-xyz-789"
		},
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Do research",
		OutputMode: "push",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := executor.Execute(ctx, node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the prompt includes the run_id.
	if len(spawner.spawnCalls) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(spawner.spawnCalls))
	}
	prompt := spawner.spawnCalls[0].Prompt
	if !contains(prompt, "run-xyz-789") {
		t.Errorf("expected prompt to contain run_id 'run-xyz-789', got: %q", prompt)
	}
	if !contains(prompt, "test-agent-node") {
		t.Errorf("expected prompt to contain node_id 'test-agent-node', got: %q", prompt)
	}
	if !contains(prompt, "foxctl flow output") {
		t.Errorf("expected prompt to contain 'foxctl flow output', got: %q", prompt)
	}
}

func TestAgentExecutor_PushMode_FallsBackWithoutSubscribeOutput(t *testing.T) {
	// When SubscribeOutput is nil, push mode falls back to session_summary.
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-push-123": {
				{Status: "completed", Summary: "Fallback summary"},
			},
		},
	}

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		// SubscribeOutput is nil — should fall back
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Do research",
		OutputMode: "push",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != "ok" {
		t.Errorf("expected ok status, got %q", output.Envelope.Status)
	}

	data, ok := output.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", output.Envelope.Data)
	}
	// Should have session_summary output_mode since it fell back
	if data["output_mode"] != "session_summary" {
		t.Errorf("expected output_mode session_summary (fallback), got %v", data["output_mode"])
	}
}

func TestAgentExecutor_PushMode_FallsBackWhenSubscriptionNil(t *testing.T) {
	// When SubscribeOutput returns nil (flow not running), falls back to session_summary.
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
		infoResults: map[string][]*AgentInfoResult{
			"agent-push-123": {
				{Status: "completed", Summary: "Fallback summary"},
			},
		},
	}

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		SubscribeOutput: func(flowID, nodeID string) <-chan NodeOutput {
			return nil // Simulate flow not running
		},
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Do research",
		OutputMode: "push",
	})

	output, err := executor.Execute(context.Background(), node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != "ok" {
		t.Errorf("expected ok status, got %q", output.Envelope.Status)
	}

	data, ok := output.Envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", output.Envelope.Data)
	}
	if data["output_mode"] != "session_summary" {
		t.Errorf("expected output_mode session_summary (fallback), got %v", data["output_mode"])
	}
}

func TestAgentExecutor_PushMode_Timeout(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
	}

	// Channel that never receives — simulates no pushed output.
	pushCh := make(chan NodeOutput)

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		SubscribeOutput: func(flowID, nodeID string) <-chan NodeOutput {
			return pushCh
		},
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Do research",
		OutputMode: "push",
		Timeout:    "100ms",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	output, err := executor.Execute(ctx, node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "ETIMEOUT" {
		t.Errorf("expected ETIMEOUT error code, got %q", output.Envelope.Error.Code)
	}

	// Verify kill was called
	if len(spawner.killCalls) != 1 {
		t.Errorf("expected 1 kill call, got %d", len(spawner.killCalls))
	}
}

func TestAgentExecutor_PushMode_BusClosed(t *testing.T) {
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
	}

	// Channel that is immediately closed — simulates flow stopped.
	pushCh := make(chan NodeOutput)
	close(pushCh)

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		SubscribeOutput: func(flowID, nodeID string) <-chan NodeOutput {
			return pushCh
		},
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Do research",
		OutputMode: "push",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	output, err := executor.Execute(ctx, node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Envelope.Status != envelope.StatusError {
		t.Errorf("expected error status, got %q", output.Envelope.Status)
	}
	if output.Envelope.Error.Code != "ECANCELED" {
		t.Errorf("expected ECANCELED error code, got %q", output.Envelope.Error.Code)
	}
}

func TestAgentExecutor_PushMode_GetRunIDUnknown(t *testing.T) {
	// When GetRunID returns empty string, the prompt should say "unknown".
	spawner := &mockAgentSpawner{
		spawnResult: &AgentSpawnResult{
			AgentID:   "agent-push-123",
			SessionID: "session-push-456",
		},
	}

	pushCh := make(chan NodeOutput, 1)
	pushCh <- NodeOutput{
		Envelope: envelope.OK("flow/output", map[string]any{"done": true}),
		NodeID:   "test-agent-node",
	}

	executor := &AgentExecutor{
		Spawner:   spawner,
		Workspace: "/tmp",
		SubscribeOutput: func(flowID, nodeID string) <-chan NodeOutput {
			return pushCh
		},
		GetRunID: func(flowID string) string {
			return "" // Simulate unknown run ID
		},
	}

	node := makeAgentNode(AgentConfig{
		Role:       "researcher",
		Prompt:     "Do research",
		OutputMode: "push",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := executor.Execute(ctx, node, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := spawner.spawnCalls[0].Prompt
	if !contains(prompt, "unknown") {
		t.Errorf("expected prompt to contain 'unknown' run_id, got: %q", prompt)
	}
}
