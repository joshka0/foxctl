package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestState_Constants(t *testing.T) {
	// Verify state constants have expected values
	tests := []struct {
		state State
		want  string
	}{
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopped, "stopped"},
		{StateError, "error"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if string(tt.state) != tt.want {
				t.Errorf("State = %q, want %q", tt.state, tt.want)
			}
		})
	}
}

func TestValidateStateAllowsOnlyDocumentedLifecycleStates(t *testing.T) {
	validStates := []State{StateStarting, StateRunning, StateStopped, StateError}
	for _, state := range validStates {
		if err := ValidateState(state); err != nil {
			t.Fatalf("ValidateState(%q) = %v, want nil", state, err)
		}
	}

	err := ValidateState(State("paused"))
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ValidateState(paused) = %v, want ErrInvalidState", err)
	}
}

func TestValidateStatePropertyUnknownStatesFailClosed(t *testing.T) {
	validStates := map[State]bool{
		StateStarting: true,
		StateRunning:  true,
		StateStopped:  true,
		StateError:    true,
	}

	prop := func(raw string) bool {
		state := State(raw)
		err := ValidateState(state)
		if validStates[state] {
			return err == nil
		}
		return errors.Is(err, ErrInvalidState)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestMarshalPolicyJSON(t *testing.T) {
	tests := []struct {
		name    string
		policy  Policy
		wantErr bool
	}{
		{
			name:    "empty policy",
			policy:  Policy{},
			wantErr: false,
		},
		{
			name: "full policy",
			policy: Policy{
				CPU:         4,
				MemoryMB:    1024,
				Timeout:     "20m",
				Network:     "egress",
				EgressAllow: []string{"api.example.com"},
				MaxOutputKB: 512,
				EnvAllow:    []string{"HOME", "PATH"},
				Secrets:     []string{"API_KEY"},
				Filesystem: []FilesystemPolicy{
					{Type: "workdir", From: "/src", To: "/dst"},
				},
			},
			wantErr: false,
		},
		{
			name: "policy with only network settings",
			policy: Policy{
				Network:     "none",
				EgressAllow: []string{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalPolicyJSON(tt.policy)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalPolicyJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(data) == 0 {
				t.Error("MarshalPolicyJSON() returned empty data")
			}
		})
	}
}

func TestMarshalPolicyJSONRejectsInvalidPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
	}{
		{name: "negative cpu", policy: Policy{CPU: -1}},
		{name: "negative memory", policy: Policy{MemoryMB: -1}},
		{name: "negative max output", policy: Policy{MaxOutputKB: -1}},
		{name: "malformed timeout", policy: Policy{Timeout: "tomorrow"}},
		{name: "zero timeout", policy: Policy{Timeout: "0s"}},
		{name: "negative timeout", policy: Policy{Timeout: "-1s"}},
		{name: "unknown network", policy: Policy{Network: "internet"}},
		{name: "egress allow with network none", policy: Policy{Network: "none", EgressAllow: []string{"api.example.com"}}},
		{name: "unknown filesystem policy", policy: Policy{Filesystem: []FilesystemPolicy{{Type: "rw", From: "/src"}}}},
		{name: "workdir without source", policy: Policy{Filesystem: []FilesystemPolicy{{Type: "workdir"}}}},
		{name: "ro without source", policy: Policy{Filesystem: []FilesystemPolicy{{Type: "ro"}}}},
		{name: "workdir without target", policy: Policy{Filesystem: []FilesystemPolicy{{Type: "workdir", From: "/src"}}}},
		{name: "ro without target", policy: Policy{Filesystem: []FilesystemPolicy{{Type: "ro", From: "/src"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := MarshalPolicyJSON(tt.policy); err == nil {
				t.Fatalf("expected invalid policy to be rejected")
			}
		})
	}
}

func TestUnmarshalPolicyJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Policy
		wantErr bool
	}{
		{
			name:    "empty JSON object",
			input:   "{}",
			want:    Policy{},
			wantErr: false,
		},
		{
			name:    "full policy",
			input:   `{"cpu":4,"memMB":1024,"timeout":"20m","network":"egress"}`,
			want:    Policy{CPU: 4, MemoryMB: 1024, Timeout: "20m", Network: "egress"},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			want:    Policy{},
			wantErr: true,
		},
		{
			name:    "policy with egress allow",
			input:   `{"egressAllow":["api.example.com","cdn.example.com"]}`,
			want:    Policy{EgressAllow: []string{"api.example.com", "cdn.example.com"}},
			wantErr: false,
		},
		{
			name:  "policy with filesystem",
			input: `{"filesystem":[{"type":"workdir","from":"/src","to":"/dst"}]}`,
			want: Policy{
				Filesystem: []FilesystemPolicy{{Type: "workdir", From: "/src", To: "/dst"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalPolicyJSON([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalPolicyJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.CPU != tt.want.CPU || got.MemoryMB != tt.want.MemoryMB {
					t.Errorf("UnmarshalPolicyJSON() = %+v, want %+v", got, tt.want)
				}
			}
		})
	}
}

func TestUnmarshalPolicyJSONRejectsInvalidPolicy(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "negative cpu", input: `{"cpu":-1}`},
		{name: "negative memory", input: `{"memMB":-1}`},
		{name: "negative max output", input: `{"max_output_kb":-1}`},
		{name: "malformed timeout", input: `{"timeout":"tomorrow"}`},
		{name: "zero timeout", input: `{"timeout":"0s"}`},
		{name: "negative timeout", input: `{"timeout":"-1s"}`},
		{name: "unknown network", input: `{"network":"internet"}`},
		{name: "egress allow with network none", input: `{"network":"none","egressAllow":["api.example.com"]}`},
		{name: "unknown filesystem policy", input: `{"filesystem":[{"type":"rw","from":"/src"}]}`},
		{name: "workdir without source", input: `{"filesystem":[{"type":"workdir"}]}`},
		{name: "ro without source", input: `{"filesystem":[{"type":"ro"}]}`},
		{name: "workdir without target", input: `{"filesystem":[{"type":"workdir","from":"/src"}]}`},
		{name: "ro without target", input: `{"filesystem":[{"type":"ro","from":"/src"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := UnmarshalPolicyJSON([]byte(tt.input)); err == nil {
				t.Fatalf("expected invalid policy JSON to be rejected")
			}
		})
	}
}

func TestValidatePolicyAllowsPositiveTimeoutDurations(t *testing.T) {
	for _, timeout := range []string{"1ns", "30s", "20m", "1h30m"} {
		t.Run(timeout, func(t *testing.T) {
			if err := ValidatePolicy(Policy{Timeout: timeout}); err != nil {
				t.Fatalf("ValidatePolicy() error = %v, want nil", err)
			}
		})
	}
}

func TestValidatePolicyPropertyAcceptsPositiveTimeoutDurations(t *testing.T) {
	prop := func(raw uint32) bool {
		duration := time.Duration(raw%86_400+1) * time.Second
		return ValidatePolicy(Policy{Timeout: duration.String()}) == nil
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePolicyPropertyRejectsInvalidTimeoutDurations(t *testing.T) {
	malformed := func(raw string) string {
		timeout := strings.TrimSpace(raw)
		if timeout == "" {
			return "not-a-duration"
		}
		if duration, err := time.ParseDuration(timeout); err == nil && duration > 0 {
			return "not-a-duration:" + timeout
		}
		return timeout
	}

	if err := quick.Check(func(raw string) bool {
		return ValidatePolicy(Policy{Timeout: malformed(raw)}) != nil
	}, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}

	if err := quick.Check(func(raw uint8) bool {
		duration := time.Duration(raw%60+1) * time.Second
		return ValidatePolicy(Policy{Timeout: "-" + duration.String()}) != nil
	}, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func TestMarshalUnmarshalPolicyJSON_RoundTrip(t *testing.T) {
	original := Policy{
		CPU:         8,
		MemoryMB:    2048,
		Timeout:     "30m",
		Network:     "egress",
		EgressAllow: []string{"api.openai.com", "api.anthropic.com"},
		MaxOutputKB: 1024,
		EnvAllow:    []string{"HOME", "PATH", "GOPATH"},
		Secrets:     []string{"OPENAI_API_KEY"},
		Filesystem: []FilesystemPolicy{
			{Type: "workdir", From: "/app", To: "/workspace"},
			{Type: "ro", From: "/config", To: "/readonly"},
		},
	}

	// Marshal
	data, err := MarshalPolicyJSON(original)
	if err != nil {
		t.Fatalf("MarshalPolicyJSON() error = %v", err)
	}

	// Unmarshal
	got, err := UnmarshalPolicyJSON(data)
	if err != nil {
		t.Fatalf("UnmarshalPolicyJSON() error = %v", err)
	}

	// Compare
	if got.CPU != original.CPU {
		t.Errorf("CPU = %d, want %d", got.CPU, original.CPU)
	}
	if got.MemoryMB != original.MemoryMB {
		t.Errorf("MemoryMB = %d, want %d", got.MemoryMB, original.MemoryMB)
	}
	if got.Timeout != original.Timeout {
		t.Errorf("Timeout = %q, want %q", got.Timeout, original.Timeout)
	}
	if got.Network != original.Network {
		t.Errorf("Network = %q, want %q", got.Network, original.Network)
	}
	if len(got.Filesystem) != len(original.Filesystem) {
		t.Errorf("Filesystem length = %d, want %d", len(got.Filesystem), len(original.Filesystem))
	}
}

func TestAgent_JSONSerialization(t *testing.T) {
	agent := Agent{
		ID:          "agent-123",
		ParentID:    "parent-456",
		Namespace:   "ns:project",
		Role:        "coder",
		Prompt:      "You are a coding assistant",
		SkillsAllow: []string{"fs/read", "fs/write"},
		Policy: Policy{
			CPU:      2,
			MemoryMB: 512,
			Network:  "none",
		},
		ShareBB:     "scoped",
		State:       StateRunning,
		LLMProvider: "openai",
		LLMModel:    "gpt-4",
		TerminalBinding: TerminalBinding{
			Backend:             "tmux",
			Session:             "collab",
			ParticipantID:       "agent-a",
			ParentParticipantID: "parent-a",
			ParentAgentID:       "agent:parent-1",
			RoomAccess:          "none",
		},
	}

	// Marshal
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// Unmarshal
	var got Agent
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Verify key fields
	if got.ID != agent.ID {
		t.Errorf("ID = %q, want %q", got.ID, agent.ID)
	}
	if got.Namespace != agent.Namespace {
		t.Errorf("Namespace = %q, want %q", got.Namespace, agent.Namespace)
	}
	if got.State != agent.State {
		t.Errorf("State = %v, want %v", got.State, agent.State)
	}
	if got.LLMProvider != agent.LLMProvider {
		t.Errorf("LLMProvider = %q, want %q", got.LLMProvider, agent.LLMProvider)
	}
	if got.TerminalBinding.ParticipantID != agent.TerminalBinding.ParticipantID {
		t.Errorf("TerminalBinding.ParticipantID = %q, want %q", got.TerminalBinding.ParticipantID, agent.TerminalBinding.ParticipantID)
	}
}

func TestNormalizeTerminalBinding(t *testing.T) {
	t.Run("child defaults to room access none", func(t *testing.T) {
		got := NormalizeTerminalBinding(TerminalBinding{
			Backend:             "TMUX",
			Session:             " collab ",
			ParticipantID:       " child-a ",
			ParentParticipantID: " parent-a ",
			RoomID:              "room-alpha",
			RoomAccess:          "default",
		})
		if got.Backend != "tmux" {
			t.Fatalf("Backend = %q, want tmux", got.Backend)
		}
		if got.RoomAccess != "none" {
			t.Fatalf("RoomAccess = %q, want none", got.RoomAccess)
		}
		if got.RoomID != "" {
			t.Fatalf("RoomID = %q, want empty when room access is none", got.RoomID)
		}
		if got.ParentParticipantID != "parent-a" {
			t.Fatalf("ParentParticipantID = %q, want parent-a", got.ParentParticipantID)
		}
	})

	t.Run("top level defaults to direct", func(t *testing.T) {
		got := NormalizeTerminalBinding(TerminalBinding{
			Backend:       "zellij",
			Session:       "alpha",
			ParticipantID: "lead-a",
			RoomID:        "room-alpha",
		})
		if got.RoomAccess != "direct" {
			t.Fatalf("RoomAccess = %q, want direct", got.RoomAccess)
		}
		if got.RoomID != "room-alpha" {
			t.Fatalf("RoomID = %q, want room-alpha", got.RoomID)
		}
	})
}

func TestNormalizeTerminalBindingPropertyRoomAccessControlsRoomID(t *testing.T) {
	prop := func(rawAccess uint8, hasParent bool) bool {
		parent := ""
		if hasParent {
			parent = " parent-a "
		}
		got := NormalizeTerminalBinding(TerminalBinding{
			Backend:             " TMUX ",
			RoomID:              " room-alpha ",
			RoomAccess:          generatedRoomAccess(rawAccess),
			ParentParticipantID: parent,
		})

		wantAccess := expectedRoomAccess(generatedRoomAccess(rawAccess), hasParent)
		if got.Backend != "tmux" || got.RoomAccess != wantAccess {
			return false
		}
		if got.RoomAccess == "direct" {
			return got.RoomID == "room-alpha"
		}
		return got.RoomID == ""
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestQuotas_JSONSerialization(t *testing.T) {
	quotas := Quotas{
		Namespace:         "ns:project",
		MaxConcurrentJobs: 10,
		CPULimit:          8,
		MemMBLimit:        4096,
		LLMCallsPerMin:    60,
		EgressBytesPerMin: 1024 * 1024,
	}

	data, err := json.Marshal(quotas)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Quotas
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Namespace != quotas.Namespace {
		t.Errorf("Namespace = %q, want %q", got.Namespace, quotas.Namespace)
	}
	if got.MaxConcurrentJobs != quotas.MaxConcurrentJobs {
		t.Errorf("MaxConcurrentJobs = %d, want %d", got.MaxConcurrentJobs, quotas.MaxConcurrentJobs)
	}
}

func TestQuotaConsumption_JSONSerialization(t *testing.T) {
	consumption := QuotaConsumption{
		Namespace:       "ns:project",
		ActiveJobs:      3,
		CPUUsed:         4,
		MemMBUsed:       1024,
		LLMCalls1Min:    15,
		EgressBytes1Min: 512 * 1024,
		LastResetTS:     1703520000,
	}

	data, err := json.Marshal(consumption)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got QuotaConsumption
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.ActiveJobs != consumption.ActiveJobs {
		t.Errorf("ActiveJobs = %d, want %d", got.ActiveJobs, consumption.ActiveJobs)
	}
	if got.LastResetTS != consumption.LastResetTS {
		t.Errorf("LastResetTS = %d, want %d", got.LastResetTS, consumption.LastResetTS)
	}
}

func TestFilesystemPolicy_JSONSerialization(t *testing.T) {
	policies := []FilesystemPolicy{
		{Type: "workdir", From: "/src", To: "/dst"},
		{Type: "ro", From: "/config"},
	}

	data, err := json.Marshal(policies)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got []FilesystemPolicy
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(got) != len(policies) {
		t.Errorf("len = %d, want %d", len(got), len(policies))
	}
	if got[0].Type != policies[0].Type {
		t.Errorf("Type = %q, want %q", got[0].Type, policies[0].Type)
	}
}

// Tests from session lineage branch

func TestBlackboardRecordIsExpired(t *testing.T) {
	tests := []struct {
		name    string
		record  BlackboardRecord
		expired bool
	}{
		{"no TTL", BlackboardRecord{TS: time.Now().Unix(), TTLSec: 0}, false},
		{"not expired", BlackboardRecord{TS: time.Now().Unix(), TTLSec: 3600}, false},
		{"expired", BlackboardRecord{TS: time.Now().Add(-2 * time.Hour).Unix(), TTLSec: 60}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func TestBlackboardRecordIsLeased(t *testing.T) {
	tests := []struct {
		name   string
		record BlackboardRecord
		leased bool
	}{
		{"no lease", BlackboardRecord{}, false},
		{"active lease", BlackboardRecord{Lease: &Lease{Holder: "agent1", Until: time.Now().Add(time.Hour).Unix()}}, true},
		{"expired lease", BlackboardRecord{Lease: &Lease{Holder: "agent1", Until: time.Now().Add(-time.Hour).Unix()}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.IsLeased(); got != tt.leased {
				t.Errorf("IsLeased() = %v, want %v", got, tt.leased)
			}
		})
	}
}

func TestFileReservationIsExpired(t *testing.T) {
	tests := []struct {
		name    string
		res     FileReservation
		expired bool
	}{
		{"active", FileReservation{ExpiresAt: time.Now().Add(time.Hour)}, false},
		{"expired", FileReservation{ExpiresAt: time.Now().Add(-time.Hour)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.IsExpired(); got != tt.expired {
				t.Errorf("IsExpired() = %v, want %v", got, tt.expired)
			}
		})
	}
}

func generatedRoomAccess(raw uint8) string {
	switch raw % 5 {
	case 0:
		return ""
	case 1:
		return " default "
	case 2:
		return " DIRECT "
	case 3:
		return " none "
	default:
		return " custom "
	}
}

func expectedRoomAccess(raw string, hasParent bool) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "default":
		if hasParent {
			return "none"
		}
		return "direct"
	default:
		return normalized
	}
}
