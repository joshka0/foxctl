package identity

import "testing"

func TestPrincipal_IsAnonymous(t *testing.T) {
	if !(Principal{}).IsAnonymous() {
		t.Fatalf("expected zero principal to be anonymous")
	}
	if (Principal{UserID: "u1"}).IsAnonymous() {
		t.Fatalf("expected user principal to not be anonymous")
	}
	if (Principal{ActorID: "a1"}).IsAnonymous() {
		t.Fatalf("expected actor principal to not be anonymous")
	}
}

func TestPrincipal_Subject(t *testing.T) {
	if got := (Principal{}).Subject(); got != "" {
		t.Fatalf("expected empty subject for anonymous principal, got %q", got)
	}

	pUser := Principal{Platform: "discord", UserID: "123"}
	if got := pUser.Subject(); got != "user:discord:123" {
		t.Fatalf("expected user subject, got %q", got)
	}

	pActor := Principal{ActorID: "actor:agent:coder-1"}
	if got := pActor.Subject(); got != "actor:agent:coder-1" {
		t.Fatalf("expected actor subject, got %q", got)
	}

	pActorRaw := Principal{ActorID: "coder-1"}
	if got := pActorRaw.Subject(); got != "actor:coder-1" {
		t.Fatalf("expected raw actor subject prefixing, got %q", got)
	}

	// User identity takes precedence if both are present.
	pBoth := Principal{Platform: "teams", UserID: "u1", ActorID: "a1"}
	if got := pBoth.Subject(); got != "user:teams:u1" {
		t.Fatalf("expected user subject precedence, got %q", got)
	}
}

func TestPrincipal_ConversationKey(t *testing.T) {
	pNoTenant := Principal{Platform: "teams"}
	if got := pNoTenant.ConversationKey("conv1"); got != "teams::conv1" {
		t.Fatalf("expected no-tenant conversation key, got %q", got)
	}

	pTenant := Principal{Platform: "teams", TenantID: "tenant-1"}
	if got := pTenant.ConversationKey("conv1"); got != "teams:tenant-1:conv1" {
		t.Fatalf("expected tenant-scoped conversation key, got %q", got)
	}
}
