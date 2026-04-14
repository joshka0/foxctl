package agentmanager

import (
	"testing"

	"github.com/joshka0/foxctl/internal/domain/agent"
)

func TestPolicyNarrowing(t *testing.T) {
	tests := []struct {
		name      string
		parent    agent.Policy
		child     agent.Policy
		wantError bool
	}{
		{
			name: "valid narrowing - all fields smaller",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				Network:     "egress",
				EgressAllow: []string{"api.github.com:443", "*.amazonaws.com:443"},
				Secrets:     []string{"secret1", "secret2"},
				EnvAllow:    []string{"ENV1", "ENV2"},
			},
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				Network:     "egress",
				EgressAllow: []string{"api.github.com:443"},
				Secrets:     []string{"secret1"},
				EnvAllow:    []string{"ENV1"},
			},
			wantError: false,
		},
		{
			name: "valid narrowing - network none",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Network:  "egress",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Network:  "none",
			},
			wantError: false,
		},
		{
			name: "invalid - child CPU exceeds parent",
			parent: agent.Policy{
				CPU:      2,
				MemoryMB: 2048,
				Network:  "none",
			},
			child: agent.Policy{
				CPU:      4,
				MemoryMB: 1024,
				Network:  "none",
			},
			wantError: true,
		},
		{
			name: "invalid - child memory exceeds parent",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 1024,
				Network:  "none",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 2048,
				Network:  "none",
			},
			wantError: true,
		},
		{
			name: "invalid - child network less restrictive",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Network:  "none",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Network:  "egress",
			},
			wantError: true,
		},
		{
			name: "invalid - child egress not subset",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				Network:     "egress",
				EgressAllow: []string{"api.github.com:443"},
			},
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				Network:     "egress",
				EgressAllow: []string{"api.github.com:443", "*.amazonaws.com:443"},
			},
			wantError: true,
		},
		{
			name: "invalid - child secrets not subset",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Secrets:  []string{"secret1"},
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Secrets:  []string{"secret1", "secret2"},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePolicyNarrowing(tt.parent, tt.child)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePolicyNarrowing() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSkillsAllowlistValidation(t *testing.T) {
	tests := []struct {
		name      string
		parent    []string
		child     []string
		wantError bool
	}{
		{
			name:      "valid subset",
			parent:    []string{"skill1", "skill2", "skill3"},
			child:     []string{"skill1", "skill2"},
			wantError: false,
		},
		{
			name:      "valid same",
			parent:    []string{"skill1", "skill2"},
			child:     []string{"skill1", "skill2"},
			wantError: false,
		},
		{
			name:      "valid empty child",
			parent:    []string{"skill1", "skill2"},
			child:     []string{},
			wantError: false,
		},
		{
			name:      "invalid - child has extra skill",
			parent:    []string{"skill1", "skill2"},
			child:     []string{"skill1", "skill2", "skill3"},
			wantError: true,
		},
		{
			name:      "invalid - completely different skills",
			parent:    []string{"skill1", "skill2"},
			child:     []string{"skill3", "skill4"},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSkillsAllowlist(tt.parent, tt.child)
			if (err != nil) != tt.wantError {
				t.Errorf("validateSkillsAllowlist() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestIsSubset(t *testing.T) {
	tests := []struct {
		name   string
		child  []string
		parent []string
		want   bool
	}{
		{
			name:   "empty child is subset",
			child:  []string{},
			parent: []string{"a", "b"},
			want:   true,
		},
		{
			name:   "same sets are subset",
			child:  []string{"a", "b"},
			parent: []string{"a", "b"},
			want:   true,
		},
		{
			name:   "proper subset",
			child:  []string{"a"},
			parent: []string{"a", "b", "c"},
			want:   true,
		},
		{
			name:   "not subset - extra element",
			child:  []string{"a", "d"},
			parent: []string{"a", "b", "c"},
			want:   false,
		},
		{
			name:   "not subset - disjoint",
			child:  []string{"d", "e"},
			parent: []string{"a", "b", "c"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSubset(tt.child, tt.parent); got != tt.want {
				t.Errorf("isSubset() = %v, want %v", got, tt.want)
			}
		})
	}
}
