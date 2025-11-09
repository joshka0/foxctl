package policy

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/skill"
)

func TestValidateNetworkAccess(t *testing.T) {
	tests := []struct {
		name        string
		manifest    skill.Manifest
		host        string
		port        int
		expectError bool
	}{
		{
			name: "network none blocks all access",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network: "none",
				},
			},
			host:        "api.github.com",
			port:        443,
			expectError: true,
		},
		{
			name: "network egress with empty allowlist allows all",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{},
				},
			},
			host:        "api.github.com",
			port:        443,
			expectError: false,
		},
		{
			name: "exact match allows access",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{"api.github.com:443"},
				},
			},
			host:        "api.github.com",
			port:        443,
			expectError: false,
		},
		{
			name: "exact match blocks different port",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{"api.github.com:443"},
				},
			},
			host:        "api.github.com",
			port:        80,
			expectError: true,
		},
		{
			name: "wildcard domain match allows access",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{"*.amazonaws.com:443"},
				},
			},
			host:        "s3.amazonaws.com",
			port:        443,
			expectError: false,
		},
		{
			name: "wildcard port allows any port",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{"example.com:*"},
				},
			},
			host:        "example.com",
			port:        8080,
			expectError: false,
		},
		{
			name: "CIDR match allows IP in range",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{"10.0.0.0/8:*"},
				},
			},
			host:        "10.5.10.20",
			port:        443,
			expectError: false,
		},
		{
			name: "CIDR blocks IP out of range",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network:     "egress",
					EgressAllow: []string{"10.0.0.0/8:*"},
				},
			},
			host:        "192.168.1.1",
			port:        443,
			expectError: true,
		},
		{
			name: "multiple patterns - first matches",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network: "egress",
					EgressAllow: []string{
						"api.github.com:443",
						"*.amazonaws.com:443",
					},
				},
			},
			host:        "api.github.com",
			port:        443,
			expectError: false,
		},
		{
			name: "multiple patterns - second matches",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network: "egress",
					EgressAllow: []string{
						"api.github.com:443",
						"*.amazonaws.com:443",
					},
				},
			},
			host:        "s3.amazonaws.com",
			port:        443,
			expectError: false,
		},
		{
			name: "multiple patterns - none match",
			manifest: skill.Manifest{
				Capabilities: skill.Capabilities{
					Network: "egress",
					EgressAllow: []string{
						"api.github.com:443",
						"*.amazonaws.com:443",
					},
				},
			},
			host:        "example.com",
			port:        80,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator(tt.manifest)
			err := v.ValidateNetworkAccess(tt.host, tt.port)
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateWASIPolicy(t *testing.T) {
	tests := []struct {
		name        string
		manifest    skill.Manifest
		expectError bool
	}{
		{
			name: "WASI with network none is valid",
			manifest: skill.Manifest{
				Distribution: skill.Distribution{Type: "wasi"},
				Capabilities: skill.Capabilities{Network: "none"},
			},
			expectError: false,
		},
		{
			name: "WASI with network egress is invalid",
			manifest: skill.Manifest{
				Distribution: skill.Distribution{Type: "wasi"},
				Capabilities: skill.Capabilities{Network: "egress"},
			},
			expectError: true,
		},
		{
			name: "exec with network egress is valid",
			manifest: skill.Manifest{
				Distribution: skill.Distribution{Type: "exec"},
				Capabilities: skill.Capabilities{Network: "egress"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWASIPolicy(tt.manifest)
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestMatchesEgressPattern(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		pattern  string
		expected bool
	}{
		{"exact match", "api.github.com", 443, "api.github.com:443", true},
		{"wrong port", "api.github.com", 80, "api.github.com:443", false},
		{"wrong host", "example.com", 443, "api.github.com:443", false},
		{"wildcard domain match", "s3.amazonaws.com", 443, "*.amazonaws.com:443", true},
		{"wildcard domain match subdomain", "s3.us-east-1.amazonaws.com", 443, "*.amazonaws.com:443", true},
		{"wildcard domain no match", "example.com", 443, "*.amazonaws.com:443", false},
		{"wildcard port", "example.com", 8080, "example.com:*", true},
		{"wildcard port different port", "example.com", 443, "example.com:*", true},
		{"CIDR match", "10.5.10.20", 443, "10.0.0.0/8:*", true},
		{"CIDR match any port", "10.5.10.20", 8080, "10.0.0.0/8:*", true},
		{"CIDR no match", "192.168.1.1", 443, "10.0.0.0/8:*", false},
		{"localhost CIDR", "127.0.0.1", 8080, "127.0.0.0/8:*", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesEgressPattern(tt.host, tt.port, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesEgressPattern(%q, %d, %q) = %v, want %v",
					tt.host, tt.port, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestMatchesCIDR(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		cidr     string
		expected bool
	}{
		{"IPv4 in range", "10.5.10.20", "10.0.0.0/8", true},
		{"IPv4 out of range", "192.168.1.1", "10.0.0.0/8", false},
		{"localhost", "127.0.0.1", "127.0.0.0/8", true},
		{"hostname not IP", "example.com", "10.0.0.0/8", false},
		{"IPv6 in range", "2001:db8::1", "2001:db8::/32", true},
		{"IPv6 out of range", "2001:db9::1", "2001:db8::/32", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesCIDR(tt.host, tt.cidr)
			if result != tt.expected {
				t.Errorf("matchesCIDR(%q, %q) = %v, want %v",
					tt.host, tt.cidr, result, tt.expected)
			}
		})
	}
}
