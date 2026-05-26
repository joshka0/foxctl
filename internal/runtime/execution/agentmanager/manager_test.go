package agentmanager

import (
	"fmt"
	"testing"
	"testing/quick"

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
			name: "valid - child timeout equals parent",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Timeout:  "5m",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Timeout:  "5m",
			},
			wantError: false,
		},
		{
			name: "valid - child timeout narrower than parent",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Timeout:  "5m",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Timeout:  "30s",
			},
			wantError: false,
		},
		{
			name: "valid - child adds caps under uncapped parent",
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				MaxOutputKB: 64,
			},
			wantError: false,
		},
		{
			name: "valid - child max output equals parent",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				MaxOutputKB: 128,
			},
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				MaxOutputKB: 128,
			},
			wantError: false,
		},
		{
			name: "invalid - parent CPU negative",
			parent: agent.Policy{
				CPU:      -1,
				MemoryMB: 2048,
			},
			child: agent.Policy{
				CPU:      1,
				MemoryMB: 1024,
			},
			wantError: true,
		},
		{
			name: "invalid - child CPU negative",
			parent: agent.Policy{
				CPU:      2,
				MemoryMB: 2048,
			},
			child: agent.Policy{
				CPU:      -1,
				MemoryMB: 1024,
			},
			wantError: true,
		},
		{
			name: "invalid - child clears parent CPU cap",
			parent: agent.Policy{
				CPU:      2,
				MemoryMB: 2048,
			},
			child: agent.Policy{
				MemoryMB: 1024,
			},
			wantError: true,
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
			name: "invalid - parent memory negative",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: -1,
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
			},
			wantError: true,
		},
		{
			name: "invalid - child memory negative",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: -1,
			},
			wantError: true,
		},
		{
			name: "invalid - child clears parent memory cap",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
			},
			child: agent.Policy{
				CPU: 2,
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
			name: "invalid - parent max output negative",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				MaxOutputKB: -1,
			},
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				MaxOutputKB: 64,
			},
			wantError: true,
		},
		{
			name: "invalid - child max output negative",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				MaxOutputKB: 128,
			},
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				MaxOutputKB: -1,
			},
			wantError: true,
		},
		{
			name: "invalid - child clears parent max output cap",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				MaxOutputKB: 128,
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
			},
			wantError: true,
		},
		{
			name: "invalid - child max output exceeds parent",
			parent: agent.Policy{
				CPU:         4,
				MemoryMB:    2048,
				MaxOutputKB: 128,
			},
			child: agent.Policy{
				CPU:         2,
				MemoryMB:    1024,
				MaxOutputKB: 256,
			},
			wantError: true,
		},
		{
			name: "invalid - child timeout exceeds parent",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Timeout:  "30s",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Timeout:  "5m",
			},
			wantError: true,
		},
		{
			name: "invalid - child clears parent timeout",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Timeout:  "30s",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
			},
			wantError: true,
		},
		{
			name: "invalid - child timeout malformed",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Timeout:  "tomorrow",
			},
			wantError: true,
		},
		{
			name: "invalid - parent timeout malformed",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Timeout:  "later",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Timeout:  "1s",
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
			name: "invalid - child network unknown",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Network:  "none",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Network:  "direct",
			},
			wantError: true,
		},
		{
			name: "invalid - parent network unknown",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Network:  "direct",
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Network:  "none",
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
			name: "valid - child filesystem exact subset",
			parent: agent.Policy{
				CPU:      4,
				MemoryMB: 2048,
				Filesystem: []agent.FilesystemPolicy{
					{Type: "workdir", From: "/repo", To: "/workspace"},
					{Type: "ro", From: "/docs", To: "/docs"},
				},
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
				Filesystem: []agent.FilesystemPolicy{
					{Type: "workdir", From: "/repo", To: "/workspace"},
					{Type: "ro", From: "/docs", To: "/docs"},
				},
			},
			wantError: false,
		},
		{
			name: "valid - child filesystem can narrow workdir to read-only",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/workspace"}},
			},
			child: agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "ro", From: "/repo", To: "/workspace"}},
			},
			wantError: false,
		},
		{
			name: "invalid - child filesystem changes source behind same target",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/workspace"}},
			},
			child: agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/etc", To: "/workspace"}},
			},
			wantError: true,
		},
		{
			name: "invalid - child filesystem changes target for same source",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/workspace"}},
			},
			child: agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/other"}},
			},
			wantError: true,
		},
		{
			name: "invalid - child filesystem broadens read-only parent",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "ro", From: "/repo", To: "/workspace"}},
			},
			child: agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/workspace"}},
			},
			wantError: true,
		},
		{
			name: "invalid - child filesystem type unknown",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/workspace"}},
			},
			child: agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "rw", From: "/repo", To: "/workspace"}},
			},
			wantError: true,
		},
		{
			name: "invalid - parent filesystem missing target",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo"}},
			},
			child: agent.Policy{
				CPU:      2,
				MemoryMB: 1024,
			},
			wantError: true,
		},
		{
			name: "invalid - child filesystem missing target",
			parent: agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo", To: "/workspace"}},
			},
			child: agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: "/repo"}},
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

func TestPolicyNarrowingPropertyFilesystemMountIdentityNeverBroadens(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw uint8) bool {
		parentFrom := fmt.Sprintf("/workspace/allowed-%d", raw%17)
		parentTo := fmt.Sprintf("/mnt/work-%d", raw%13)
		cases := []struct {
			childFrom string
			childTo   string
			wantAllow bool
		}{
			{childFrom: parentFrom, childTo: parentTo, wantAllow: true},
			{childFrom: parentFrom + "-sibling", childTo: parentTo, wantAllow: false},
			{childFrom: "/outside/" + parentFrom[1:], childTo: parentTo, wantAllow: false},
			{childFrom: parentFrom, childTo: parentTo + "-sibling", wantAllow: false},
		}

		for _, tc := range cases {
			err := validatePolicyNarrowing(agent.Policy{
				CPU:        4,
				MemoryMB:   2048,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: parentFrom, To: parentTo}},
			}, agent.Policy{
				CPU:        2,
				MemoryMB:   1024,
				Filesystem: []agent.FilesystemPolicy{{Type: "workdir", From: tc.childFrom, To: tc.childTo}},
			})
			if (err == nil) != tc.wantAllow {
				return false
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("filesystem mount identity property failed: %v", err)
	}
}

func TestPolicyNarrowingPropertyFilesystemModeNeverBroadens(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(parentAllowsWorkdir, childRequestsWorkdir bool) bool {
		parentType := "ro"
		if parentAllowsWorkdir {
			parentType = "workdir"
		}
		childType := "ro"
		if childRequestsWorkdir {
			childType = "workdir"
		}

		err := validatePolicyNarrowing(agent.Policy{
			CPU:        4,
			MemoryMB:   2048,
			Filesystem: []agent.FilesystemPolicy{{Type: parentType, From: "/repo", To: "/workspace"}},
		}, agent.Policy{
			CPU:        2,
			MemoryMB:   1024,
			Filesystem: []agent.FilesystemPolicy{{Type: childType, From: "/repo", To: "/workspace"}},
		})
		shouldAllow := !childRequestsWorkdir || parentAllowsWorkdir
		return (err == nil) == shouldAllow
	}, cfg)
	if err != nil {
		t.Fatalf("filesystem mode narrowing property failed: %v", err)
	}
}

func TestPolicyNarrowingPropertyResourceLimitsNeverBroaden(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(parentRaw, childRaw uint8) bool {
		parentCPU := int(parentRaw%32) + 1
		childCPU := int(childRaw % 34)

		err := validatePolicyNarrowing(agent.Policy{
			CPU:         parentCPU,
			MemoryMB:    2048,
			MaxOutputKB: 128,
		}, agent.Policy{
			CPU:         childCPU,
			MemoryMB:    1024,
			MaxOutputKB: 64,
		})
		shouldAllow := childCPU > 0 && childCPU <= parentCPU
		return (err == nil) == shouldAllow
	}, cfg)
	if err != nil {
		t.Fatalf("CPU resource narrowing property failed: %v", err)
	}

	err = quick.Check(func(parentRaw, childRaw uint8) bool {
		parentMemory := (int(parentRaw%64) + 1) * 128
		childMemory := int(childRaw%66) * 128

		err := validatePolicyNarrowing(agent.Policy{
			CPU:         4,
			MemoryMB:    parentMemory,
			MaxOutputKB: 128,
		}, agent.Policy{
			CPU:         2,
			MemoryMB:    childMemory,
			MaxOutputKB: 64,
		})
		shouldAllow := childMemory > 0 && childMemory <= parentMemory
		return (err == nil) == shouldAllow
	}, cfg)
	if err != nil {
		t.Fatalf("memory resource narrowing property failed: %v", err)
	}

	err = quick.Check(func(parentRaw, childRaw uint8) bool {
		parentOutputKB := int(parentRaw%128) + 1
		childOutputKB := int(childRaw % 130)

		err := validatePolicyNarrowing(agent.Policy{
			CPU:         4,
			MemoryMB:    2048,
			MaxOutputKB: parentOutputKB,
		}, agent.Policy{
			CPU:         2,
			MemoryMB:    1024,
			MaxOutputKB: childOutputKB,
		})
		shouldAllow := childOutputKB > 0 && childOutputKB <= parentOutputKB
		return (err == nil) == shouldAllow
	}, cfg)
	if err != nil {
		t.Fatalf("max output resource narrowing property failed: %v", err)
	}
}

func TestPolicyNarrowingRejectsNegativeResourceLimits(t *testing.T) {
	t.Parallel()

	neg := -1
	for name, tc := range map[string]struct {
		parent agent.Policy
		child  agent.Policy
	}{
		"parent_cpu_negative": {
			parent: agent.Policy{CPU: neg, MemoryMB: 2048, MaxOutputKB: 128},
			child:  agent.Policy{CPU: 2, MemoryMB: 1024, MaxOutputKB: 64},
		},
		"child_cpu_negative": {
			parent: agent.Policy{CPU: 4, MemoryMB: 2048, MaxOutputKB: 128},
			child:  agent.Policy{CPU: neg, MemoryMB: 1024, MaxOutputKB: 64},
		},
		"parent_memory_negative": {
			parent: agent.Policy{CPU: 4, MemoryMB: neg, MaxOutputKB: 128},
			child:  agent.Policy{CPU: 2, MemoryMB: 1024, MaxOutputKB: 64},
		},
		"child_memory_negative": {
			parent: agent.Policy{CPU: 4, MemoryMB: 2048, MaxOutputKB: 128},
			child:  agent.Policy{CPU: 2, MemoryMB: neg, MaxOutputKB: 64},
		},
		"parent_output_negative": {
			parent: agent.Policy{CPU: 4, MemoryMB: 2048, MaxOutputKB: neg},
			child:  agent.Policy{CPU: 2, MemoryMB: 1024, MaxOutputKB: 64},
		},
		"child_output_negative": {
			parent: agent.Policy{CPU: 4, MemoryMB: 2048, MaxOutputKB: 128},
			child:  agent.Policy{CPU: 2, MemoryMB: 1024, MaxOutputKB: neg},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePolicyNarrowing(tc.parent, tc.child); err == nil {
				t.Fatal("expected rejection for negative resource limit")
			}
		})
	}
}

func TestPolicyNarrowingPropertyTimeoutNeverBroadens(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(parentRaw, childRaw uint8) bool {
		parentSeconds := int(parentRaw%120) + 1
		childSeconds := int(childRaw%120) + 1

		err := validatePolicyNarrowing(agent.Policy{
			CPU:      4,
			MemoryMB: 2048,
			Timeout:  fmt.Sprintf("%ds", parentSeconds),
		}, agent.Policy{
			CPU:      2,
			MemoryMB: 1024,
			Timeout:  fmt.Sprintf("%ds", childSeconds),
		})
		shouldAllow := childSeconds <= parentSeconds
		return (err == nil) == shouldAllow
	}, cfg)
	if err != nil {
		t.Fatalf("timeout narrowing property failed: %v", err)
	}
}

func TestPolicyNarrowingPropertyRejectsMalformedTimeouts(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw string) bool {
		timeout := raw
		if _, _, err := parsePolicyTimeout(timeout); err == nil {
			timeout = "invalid:" + raw
		}
		err := validatePolicyNarrowing(agent.Policy{
			CPU:      4,
			MemoryMB: 2048,
		}, agent.Policy{
			CPU:      2,
			MemoryMB: 1024,
			Timeout:  timeout,
		})
		return err != nil
	}, cfg)
	if err != nil {
		t.Fatalf("malformed timeout property failed: %v", err)
	}
}

func TestPolicyNarrowingPropertyNetworkNeverBroadens(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(parentAllowsEgress, childRequestsEgress bool) bool {
		parentNetwork := "none"
		if parentAllowsEgress {
			parentNetwork = "egress"
		}
		childNetwork := "none"
		if childRequestsEgress {
			childNetwork = "egress"
		}

		err := validatePolicyNarrowing(agent.Policy{
			CPU:      4,
			MemoryMB: 2048,
			Network:  parentNetwork,
		}, agent.Policy{
			CPU:      2,
			MemoryMB: 1024,
			Network:  childNetwork,
		})
		shouldAllow := !childRequestsEgress || parentAllowsEgress
		return (err == nil) == shouldAllow
	}, cfg)
	if err != nil {
		t.Fatalf("network narrowing property failed: %v", err)
	}
}

func TestPolicyNarrowingPropertyRejectsUnknownNetworkValues(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw string) bool {
		network := raw
		switch normalizeNetworkPolicy(network) {
		case "none", "egress":
			network = "direct:" + raw
		}
		err := validatePolicyNarrowing(agent.Policy{
			CPU:      4,
			MemoryMB: 2048,
			Network:  "egress",
		}, agent.Policy{
			CPU:      2,
			MemoryMB: 1024,
			Network:  network,
		})
		return err != nil
	}, cfg)
	if err != nil {
		t.Fatalf("unknown child network property failed: %v", err)
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
