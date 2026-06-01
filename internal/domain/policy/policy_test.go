package policy

import (
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/domain/skill"
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
			name:     "network egress with empty allowlist allows IPv4 host",
			manifest: manifestWithNetwork("egress"),
			host:     "10.5.10.20",
			port:     443,
		},
		{
			name:     "network egress with empty allowlist allows IPv6 host",
			manifest: manifestWithNetwork("egress"),
			host:     "2001:db8::1",
			port:     443,
		},
		{
			name:     "network egress with empty allowlist allows bracketed IPv6 host",
			manifest: manifestWithNetwork("egress"),
			host:     "[2001:db8::1]",
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
			name:     "IPv6 CIDR match allows IP in range",
			manifest: manifestWithNetwork("egress", "2001:db8::/32:*"),
			host:     "2001:db8::1",
			port:     443,
		},
		{
			name:     "IPv6 CIDR match allows bracketed IP host",
			manifest: manifestWithNetwork("egress", "2001:db8::/32:*"),
			host:     "[2001:db8::1]",
			port:     443,
		},
		{
			name:     "DNS host match is case insensitive",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "API.GITHUB.COM",
			port:     443,
		},
		{
			name: "multiple patterns - first matches",
			manifest: manifestWithNetwork(
				"egress",
				"api.github.com:443",
				"*.amazonaws.com:443",
			),
			host: "api.github.com",
			port: 443,
		},
		{
			name: "multiple patterns - second matches",
			manifest: manifestWithNetwork(
				"egress",
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
			name:     "unknown network capability blocks even with matching allowlist",
			manifest: manifestWithNetwork("proxy", "api.github.com:443"),
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
			manifest: manifestWithNetwork(
				"egress",
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

func TestValidateNetworkAccessPropertyOnlyExplicitEgressCanAllowNetwork(t *testing.T) {
	t.Parallel()

	err := quick.Check(func(rawNetwork string) bool {
		network := nonEgressNetwork(rawNetwork)
		v := NewValidator(manifestWithNetwork(network, "api.github.com:443"))
		if err := v.ValidateNetworkAccess("api.github.com", 443); err == nil {
			t.Logf("network capability %q allowed matching egress target", network)
			return false
		}
		return true
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatalf("non-egress network capability property failed: %v", err)
	}
}

func TestValidateNetworkAccessBlocksInvalidPorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest skill.Manifest
	}{
		{
			name:     "empty egress allowlist",
			manifest: manifestWithNetwork("egress"),
		},
		{
			name:     "wildcard egress port",
			manifest: manifestWithNetwork("egress", "api.github.com:*"),
		},
		{
			name:     "exact egress port",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
		},
	}
	invalidPorts := []int{-1, 0, 65536, 100000}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator(tt.manifest)
			for _, port := range invalidPorts {
				if err := v.ValidateNetworkAccess("api.github.com", port); err == nil {
					t.Fatalf("ValidateNetworkAccess allowed invalid port %d", port)
				}
			}
		})
	}
}

func TestValidateNetworkAccessBlocksInvalidHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest skill.Manifest
		host     string
	}{
		{
			name:     "empty allowlist still requires a host",
			manifest: manifestWithNetwork("egress"),
			host:     "",
		},
		{
			name:     "empty allowlist rejects whitespace host",
			manifest: manifestWithNetwork("egress"),
			host:     " \t\n",
		},
		{
			name:     "wildcard allowlist rejects whitespace host",
			manifest: manifestWithNetwork("egress", "*.example.com:*"),
			host:     " \t",
		},
		{
			name:     "exact allowlist rejects bracket-only host",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "[]",
		},
		{
			name:     "empty allowlist rejects host with embedded whitespace",
			manifest: manifestWithNetwork("egress"),
			host:     "api github com",
		},
		{
			name:     "empty allowlist rejects leading whitespace host",
			manifest: manifestWithNetwork("egress"),
			host:     " api.github.com",
		},
		{
			name:     "empty allowlist rejects trailing whitespace host",
			manifest: manifestWithNetwork("egress"),
			host:     "api.github.com ",
		},
		{
			name:     "exact allowlist rejects bracketed DNS host",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "[api.github.com]",
		},
		{
			name:     "exact allowlist rejects leading unbalanced bracket",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "[api.github.com",
		},
		{
			name:     "exact allowlist rejects trailing unbalanced bracket",
			manifest: manifestWithNetwork("egress", "api.github.com:443"),
			host:     "api.github.com]",
		},
		{
			name:     "CIDR allowlist rejects leading unbalanced IPv6 bracket",
			manifest: manifestWithNetwork("egress", "2001:db8::/32:*"),
			host:     "[2001:db8::1",
		},
		{
			name:     "CIDR allowlist rejects trailing unbalanced IPv6 bracket",
			manifest: manifestWithNetwork("egress", "2001:db8::/32:*"),
			host:     "2001:db8::1]",
		},
		{
			name:     "CIDR allowlist rejects bracketed IPv6 with trailing content",
			manifest: manifestWithNetwork("egress", "2001:db8::/32:*"),
			host:     "[2001:db8::1]x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator(tt.manifest)
			if err := v.ValidateNetworkAccess(tt.host, 443); err == nil {
				t.Fatalf("ValidateNetworkAccess allowed invalid host %q", tt.host)
			}
		})
	}
}

func TestValidateNetworkAccessPropertyDNSPatternsAreCaseInsensitive(t *testing.T) {
	t.Parallel()

	v := NewValidator(manifestWithNetwork("egress", "api.github.com:443", "*.amazonaws.com:443"))
	err := quick.Check(func(seed uint64) bool {
		exactHost := mixedCaseASCII("api.github.com", seed)
		wildcardHost := mixedCaseASCII("s3.us-east-1.amazonaws.com", seed>>16)
		return v.ValidateNetworkAccess(exactHost, 443) == nil &&
			v.ValidateNetworkAccess(wildcardHost, 443) == nil
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatalf("DNS egress case-insensitivity property failed: %v", err)
	}
}

func TestValidateNetworkAccessPropertyWildcardDNSRequiresSubdomainBoundary(t *testing.T) {
	t.Parallel()

	err := quick.Check(func(baseSeed, subdomainSeed uint64) bool {
		base := dnsLabel("base", baseSeed) + "." + dnsLabel("zone", baseSeed>>16) + ".test"
		subdomain := dnsLabel("sub", subdomainSeed)
		pattern := "*." + base + ":443"
		v := NewValidator(manifestWithNetwork("egress", pattern))

		if err := v.ValidateNetworkAccess(subdomain+"."+base, 443); err != nil {
			t.Logf("wildcard pattern %q rejected valid subdomain: %v", pattern, err)
			return false
		}
		blockedHosts := []string{
			base,
			subdomain + base,
			subdomain + "-" + strings.ReplaceAll(base, ".", "-"),
			base + ".evil.test",
		}
		for _, host := range blockedHosts {
			if err := v.ValidateNetworkAccess(host, 443); err == nil {
				t.Logf("wildcard pattern %q allowed boundary-violating host %q", pattern, host)
				return false
			}
		}
		return true
	}, &quick.Config{MaxCount: 150})
	if err != nil {
		t.Fatalf("wildcard DNS boundary property failed: %v", err)
	}
}

func TestValidateNetworkAccessPropertyHostMustNameConcreteTarget(t *testing.T) {
	t.Parallel()

	manifests := []skill.Manifest{
		manifestWithNetwork("egress"),
		manifestWithNetwork("egress", "api.github.com:*"),
		manifestWithNetwork("egress", "*.example.com:443"),
		manifestWithNetwork("egress", "2001:db8::/32:*"),
	}

	err := quick.Check(func(hostSeed, manifestSeed uint8, portSeed uint16) bool {
		host := invalidNetworkHost(hostSeed)
		port := int(portSeed%65535) + 1
		v := NewValidator(manifests[int(manifestSeed)%len(manifests)])
		if err := v.ValidateNetworkAccess(host, port); err == nil {
			t.Logf("manifest allowed invalid host %q on port %d", host, port)
			return false
		}
		if matchesEgressPattern(host, port, "*.example.com:*") {
			t.Logf("egress matcher accepted invalid host %q", host)
			return false
		}
		return true
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatalf("concrete network host property failed: %v", err)
	}
}

func TestValidateNetworkAccessPropertyInvalidPortsAreAlwaysDenied(t *testing.T) {
	t.Parallel()

	manifests := []skill.Manifest{
		manifestWithNetwork("egress"),
		manifestWithNetwork("egress", "api.github.com:*"),
		manifestWithNetwork("egress", "api.github.com:443"),
	}

	err := quick.Check(func(rawPort int, manifestSeed uint8) bool {
		port := rawPort
		if port >= 1 && port <= 65535 {
			port = 65536 + port
		}
		v := NewValidator(manifests[int(manifestSeed)%len(manifests)])
		return v.ValidateNetworkAccess("api.github.com", port) != nil &&
			!matchesEgressPattern("api.github.com", port, "api.github.com:*")
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatalf("invalid network port property failed: %v", err)
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

func mixedCaseASCII(value string, seed uint64) string {
	out := []byte(value)
	for i, ch := range out {
		if ch < 'a' || ch > 'z' {
			continue
		}
		if seed&1 == 1 {
			out[i] = ch - 'a' + 'A'
		}
		seed >>= 1
	}
	return string(out)
}

func nonEgressNetwork(raw string) string {
	network := strings.TrimSpace(strings.ToLower(raw))
	if network == "" || network == "egress" {
		return "proxy"
	}
	return network
}

func dnsLabel(prefix string, seed uint64) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte('-')
	for i := 0; i < 8; i++ {
		b.WriteByte(alphabet[int(seed%uint64(len(alphabet)))])
		seed /= uint64(len(alphabet))
	}
	return b.String()
}

func invalidNetworkHost(seed uint8) string {
	hosts := []string{
		"",
		" \t\n",
		"api github com",
		" api.github.com",
		"api.github.com ",
		"[]",
		"[api.github.com]",
		"[api.github.com",
		"api.github.com]",
		"[2001:db8::1",
		"2001:db8::1]",
		"[2001:db8::1]x",
	}
	return hosts[int(seed)%len(hosts)]
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
		{"IPv6 CIDR match", "2001:db8::1", 443, "2001:db8::/32:*", true},
		{"IPv6 CIDR wrong port", "2001:db8::1", 80, "2001:db8::/32:443", false},
		{"localhost CIDR", "127.0.0.1", 8080, "127.0.0.0/8:*", true},
		{"case-insensitive exact DNS", "API.GITHUB.COM", 443, "api.github.com:443", true},
		{"case-insensitive wildcard DNS", "S3.AMAZONAWS.COM", 443, "*.amazonaws.com:443", true},
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
		{"dot-prefixed child path", "/tmp/work/..cache/file.txt", "/tmp/work", true},
		{"relative dot-prefixed child path", "..cache/file.txt", "/tmp/work", true},
		{"sibling with workdir prefix", "/tmp/work-evil/file.txt", "/tmp/work", false},
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

func TestIsWithinWorkdirPropertyAllowsAllDirectChildren(t *testing.T) {
	t.Parallel()

	workDir := filepath.Join(string(filepath.Separator), "tmp", "work")
	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw string) bool {
		child := validWorkdirChildSegment(raw)
		absoluteChild := filepath.Join(workDir, child, "file.txt")
		relativeChild := filepath.Join(child, "file.txt")
		return isWithinWorkdir(absoluteChild, workDir) &&
			isWithinWorkdir(relativeChild, workDir)
	}, cfg)
	if err != nil {
		t.Fatalf("workdir child property failed: %v", err)
	}
}

func TestIsWithinWorkdirPropertyBlocksSiblingPrefixes(t *testing.T) {
	t.Parallel()

	base := filepath.Join(string(filepath.Separator), "tmp")
	workDir := filepath.Join(base, "work")
	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw string) bool {
		suffix := validWorkdirChildSegment(raw)
		sibling := filepath.Join(base, "work-"+suffix, "file.txt")
		return !isWithinWorkdir(sibling, workDir)
	}, cfg)
	if err != nil {
		t.Fatalf("workdir sibling-prefix property failed: %v", err)
	}
}

func validWorkdirChildSegment(raw string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == filepath.Separator || r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, raw)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return "..cache"
	}
	base := strings.TrimLeft(cleaned, ".")
	if base == "" {
		return "..cache"
	}
	return ".." + base
}
