package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"testing/quick"

	"github.com/casbin/casbin/v2"
	"github.com/joshka0/foxctl/internal/domain/identity"
)

func TestEnforceAdminCanExecuteAllTools(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:admin, admin, tenant-a
p, admin, *, tool:*, execute, allow
`)

	principal := identity.Principal{Platform: "web", UserID: "admin", TenantID: "tenant-a"}
	resources := []string{"tool:fs.read", "tool:fs.write", "tool:exec.run", "tool:memory"}

	for _, resource := range resources {
		resource := resource
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			allowed, err := Enforce(enforcer, principal, resource, "execute")
			if err != nil {
				t.Fatalf("enforce returned error: %v", err)
			}
			if !allowed {
				t.Fatalf("expected allow for %q", resource)
			}
		})
	}
}

func TestEnforceViewerBlockedFromFSWriteAndExec(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:viewer, viewer, tenant-a
p, viewer, *, tool:search, execute, allow
p, viewer, *, tool:memory, execute, allow
p, viewer, *, tool:fs.write, execute, deny
p, viewer, *, tool:exec.*, execute, deny
`)

	principal := identity.Principal{Platform: "web", UserID: "viewer", TenantID: "tenant-a"}

	blockedResources := []string{"tool:fs.write", "tool:exec.run"}
	for _, resource := range blockedResources {
		allowed, err := Enforce(enforcer, principal, resource, "execute")
		if err != nil {
			t.Fatalf("enforce returned error for %q: %v", resource, err)
		}
		if allowed {
			t.Fatalf("expected deny for %q", resource)
		}
	}
}

func TestEnforceTenantIsolation(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:alice, viewer, tenant-a
p, viewer, tenant-a, tool:search, execute, allow
`)

	principalTenantA := identity.Principal{Platform: "web", UserID: "alice", TenantID: "tenant-a"}
	allowedA, err := Enforce(enforcer, principalTenantA, "tool:search", "execute")
	if err != nil {
		t.Fatalf("enforce returned error for tenant-a: %v", err)
	}
	if !allowedA {
		t.Fatal("expected tenant-a access to be allowed")
	}

	principalTenantB := identity.Principal{Platform: "web", UserID: "alice", TenantID: "tenant-b"}
	allowedB, err := Enforce(enforcer, principalTenantB, "tool:search", "execute")
	if err != nil {
		t.Fatalf("enforce returned error for tenant-b: %v", err)
	}
	if allowedB {
		t.Fatal("expected tenant-b access to be denied")
	}
}

func TestEnforceWildcardTenantPolicy(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:charlie, admin, *
p, admin, *, tool:*, execute, allow
`)

	principal := identity.Principal{Platform: "web", UserID: "charlie", TenantID: "tenant-z"}
	allowed, err := Enforce(enforcer, principal, "tool:fs.write", "execute")
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected wildcard tenant policy to allow access")
	}
}

func TestEnforceDenyOverridesAllow(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
g, user:web:bob, editor, tenant-a
p, editor, tenant-a, tool:*, execute, allow
p, editor, tenant-a, tool:fs.write, execute, deny
`)

	principal := identity.Principal{Platform: "web", UserID: "bob", TenantID: "tenant-a"}

	readAllowed, err := Enforce(enforcer, principal, "tool:fs.read", "execute")
	if err != nil {
		t.Fatalf("enforce returned error for read: %v", err)
	}
	if !readAllowed {
		t.Fatal("expected read to be allowed")
	}

	writeAllowed, err := Enforce(enforcer, principal, "tool:fs.write", "execute")
	if err != nil {
		t.Fatalf("enforce returned error for write: %v", err)
	}
	if writeAllowed {
		t.Fatal("expected write to be denied by explicit deny")
	}
}

func TestEnforceAnonymousPassesThrough(t *testing.T) {
	t.Parallel()

	enforcer := mustNewEnforcer(t, `
p, admin, *, tool:*, execute, allow
`)

	allowed, err := Enforce(enforcer, identity.Principal{}, "tool:fs.write", "execute")
	if err != nil {
		t.Fatalf("enforce returned error: %v", err)
	}
	if !allowed {
		t.Fatal("expected anonymous principal to pass through")
	}
}

func TestEnforcePropertyDenyOverridesWildcardAllow(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(rawResource string) bool {
		deniedResource := "tool:" + authPolicyToken(rawResource)
		allowedResource := deniedResource + ".allowed"
		enforcer := mustNewEnforcer(t, fmt.Sprintf(`
g, user:web:bob, editor, tenant-a
p, editor, tenant-a, tool:*, execute, allow
p, editor, tenant-a, %s, execute, deny
`, deniedResource))

		principal := identity.Principal{Platform: "web", UserID: "bob", TenantID: "tenant-a"}
		denied, err := Enforce(enforcer, principal, deniedResource, "execute")
		if err != nil || denied {
			return false
		}
		allowed, err := Enforce(enforcer, principal, allowedResource, "execute")
		return err == nil && allowed
	}, cfg)
	if err != nil {
		t.Fatalf("deny override property failed: %v", err)
	}
}

func TestEnforcePropertyTenantPoliciesDoNotCrossTenants(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(rawTenantA, rawTenantB, rawResource string) bool {
		tenantA := "tenant-" + authPolicyToken(rawTenantA)
		tenantB := "tenant-" + authPolicyToken(rawTenantB)
		if tenantA == tenantB {
			tenantB += "-other"
		}
		resource := "tool:" + authPolicyToken(rawResource)
		enforcer := mustNewEnforcer(t, fmt.Sprintf(`
g, user:web:alice, viewer, %s
p, viewer, %s, %s, execute, allow
`, tenantA, tenantA, resource))

		principalA := identity.Principal{Platform: "web", UserID: "alice", TenantID: tenantA}
		principalB := identity.Principal{Platform: "web", UserID: "alice", TenantID: tenantB}
		allowedA, err := Enforce(enforcer, principalA, resource, "execute")
		if err != nil || !allowedA {
			return false
		}
		allowedB, err := Enforce(enforcer, principalB, resource, "execute")
		return err == nil && !allowedB
	}, cfg)
	if err != nil {
		t.Fatalf("tenant isolation property failed: %v", err)
	}
}

func TestEnforcePropertyActionMustMatchPolicy(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(rawResource, rawAllowedAction, rawDeniedAction string) bool {
		resource := "tool:" + authPolicyToken(rawResource)
		allowedAction := "act-" + authPolicyToken(rawAllowedAction)
		deniedAction := "act-" + authPolicyToken(rawDeniedAction)
		if deniedAction == allowedAction {
			deniedAction += "-other"
		}
		enforcer := mustNewEnforcer(t, fmt.Sprintf(`
g, user:web:carol, operator, tenant-a
p, operator, tenant-a, %s, %s, allow
`, resource, allowedAction))

		principal := identity.Principal{Platform: "web", UserID: "carol", TenantID: "tenant-a"}
		allowed, err := Enforce(enforcer, principal, resource, allowedAction)
		if err != nil || !allowed {
			return false
		}
		denied, err := Enforce(enforcer, principal, resource, deniedAction)
		return err == nil && !denied
	}, cfg)
	if err != nil {
		t.Fatalf("action match property failed: %v", err)
	}
}

func mustNewEnforcer(t *testing.T, policy string) *casbin.Enforcer {
	t.Helper()

	enforcer, err := NewEnforcer(policy)
	if err != nil {
		t.Fatalf("NewEnforcer returned error: %v", err)
	}

	return enforcer
}

func authPolicyToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}
