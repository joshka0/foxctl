package policy

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/skill"
)

func TestValidateNetworkAccess_AllowsEgressPatterns(t *testing.T) {
	cases := []struct {
		name     string
		manifest skill.Manifest
		host     string
		port     int
	}{
		{
			name:     "network egress with empty allowlist allows all",
			manifest: manifestWithNetwork("egress"),
			host:     "api.github.com",
			port:     443,
		},
		{
			name:     "exact match allows access",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "api.github.com",
			port:     443,
		},
		{
			name:     "wildcard domain match allows access",
			manifest: manifestWithNetwork("egress", "*.amazonaws.com:443"),
			host:     "s3.amazonaws.com",
			port:     443,
		},
		{
			name:     "wildcard domain match subdomain",
			manifest: manifestWithNetwork("egress", "*.amazonaws.com:443"),
			host:     "s3.us-east-1.amazonaws.com",
			port:     443,
		},
		{
			name:     "wildcard port allows any port",
			manifest: manifestWithNetwork("egress", "example.com:*"),
			host:     "example.com",
			port:     8080,
		},
		{
			name:     "CIDR match allows IP in range",
			manifest: manifestWithNetwork("egress", "10.0.0.0/8:*"),
			host:     "10.5.10.20",
			port:     443,
		},
		{
			name: "multiple patterns - first matches",
			manifest: manifestWithNetwork("egress",
				"api.github.com:443",
				"*.amazonaws.com:443",
			),
			host: "api.github.com",
			port: 443,
		},
		{
			name: "multiple patterns - second matches",
			manifest: manifestWithNetwork("egress",
				"api.github.com:443",
				"*.amazonaws.com:443",
			),
			host: "s3.amazonaws.com",
			port: 443,
		},
	}

	cases[0].manifest.Capabilities.EgressAllow = []string{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewValidator(tc.manifest)
			if err := v.ValidateNetworkAccess(tc.host, tc.port); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateNetworkAccess_BlocksDisallowedAccess(t *testing.T) {
	cases := []struct {
		name     string
		manifest skill.Manifest
		host     string
		port     int
	}{
		{
			name:     "network none blocks all access",
			manifest: manifestWithNetwork("none"),
			host:     "api.github.com",
			port:     443,
		},
		{
			name:     "exact match blocks different port",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "api.github.com",
			port:     80,
		},
		{
			name:     "CIDR blocks IP out of range",
			manifest: manifestWithNetwork("egress", "10.0.0.0/8:*"),
			host:     "192.168.1.1",
			port:     443,
		},
		{
			name: "multiple patterns - none match",
			manifest: manifestWithNetwork("egress",
				"api.github.com:443",
				"*.amazonaws.com:443",
			),
			host: "example.com",
			port: 80,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewValidator(tc.manifest)
			if err := v.ValidateNetworkAccess(tc.host, tc.port); err == nil {
				t.Fatalf("expected error but got none")
			}
		})
	}
}

func manifestWithNetwork(network string, allow ...string) skill.Manifest {
	return skill.Manifest{
		Capabilities: skill.Capabilities{
			Network:     network,
			EgressAllow: append([]string{}, allow...),
		},
	}
}

func manifestWithFilesystem(types ...string) skill.Manifest {
	accesses := make([]skill.FileAccess, 0, len(types))
	for _, typ := range types {
		accesses = append(accesses, skill.FileAccess{Type: typ})
	}
	return skill.Manifest{
		Capabilities: skill.Capabilities{
			Filesystem: accesses,
		},
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

func TestValidateFilesystemAccess_Allows(t *testing.T) {
	cases := []struct {
		name          string
		manifest      skill.Manifest
		requestedPath string
		workDir       string
	}{
		{
			name:          "workdir capability allows access within workdir",
			manifest:      manifestWithFilesystem("workdir"),
			requestedPath: "/tmp/work/file.txt",
			workDir:       "/tmp/work",
		},
		{
			name:          "relative path within workdir allowed",
			manifest:      manifestWithFilesystem("workdir"),
			requestedPath: "subdir/file.txt",
			workDir:       "/tmp/work",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewValidator(tc.manifest)
			if err := v.ValidateFilesystemAccess(tc.requestedPath, tc.workDir); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateFilesystemAccess_Blocks(t *testing.T) {
	cases := []struct {
		name          string
		manifest      skill.Manifest
		requestedPath string
		workDir       string
	}{
		{
			name:          "workdir capability blocks access outside workdir",
			manifest:      manifestWithFilesystem("workdir"),
			requestedPath: "/etc/passwd",
			workDir:       "/tmp/work",
		},
		{
			name:          "no filesystem capability blocks all access",
			manifest:      manifestWithFilesystem(),
			requestedPath: "/tmp/work/file.txt",
			workDir:       "/tmp/work",
		},
		{
			name:          "path traversal blocked",
			manifest:      manifestWithFilesystem("workdir"),
			requestedPath: "../../../etc/passwd",
			workDir:       "/tmp/work",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := NewValidator(tc.manifest)
			if err := v.ValidateFilesystemAccess(tc.requestedPath, tc.workDir); err == nil {
				t.Fatalf("expected error but got none")
			}
		})
	}
}

func TestValidateFilesystemCapabilities_Valid(t *testing.T) {
	cases := []struct {
		name     string
		manifest skill.Manifest
	}{
		{name: "workdir", manifest: manifestWithFilesystem("workdir")},
		{name: "home", manifest: manifestWithFilesystem("home")},
		{name: "tmp", manifest: manifestWithFilesystem("tmp")},
		{name: "empty", manifest: manifestWithFilesystem()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateFilesystemCapabilities(tc.manifest); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateFilesystemCapabilities_Invalid(t *testing.T) {
	manifest := manifestWithFilesystem("invalid")

	if err := ValidateFilesystemCapabilities(manifest); err == nil {
		t.Fatalf("expected error but got none")
	}
}

func TestIsWithinWorkdir(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		workDir  string
		expected bool
	}{
		{"file in workdir", "/tmp/work/file.txt", "/tmp/work", true},
		{"subdir in workdir", "/tmp/work/sub/file.txt", "/tmp/work", true},
		{"file outside workdir", "/etc/passwd", "/tmp/work", false},
		{"path traversal attempt", "/tmp/work/../../../etc/passwd", "/tmp/work", false},
		{"relative path within", "file.txt", "/tmp/work", true},
		{"relative subdir", "sub/file.txt", "/tmp/work", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWithinWorkdir(tt.path, tt.workDir)
			if result != tt.expected {
				t.Errorf("isWithinWorkdir(%q, %q) = %v, want %v",
					tt.path, tt.workDir, result, tt.expected)
			}
		})
	}
}
