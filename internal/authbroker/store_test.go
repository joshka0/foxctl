package authbroker

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

func openTestSQLiteStore(t *testing.T) (*SQLiteStore, *sql.DB) {
	t.Helper()

	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := "file:" + name + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	store := NewSQLiteStore(db, fixedClock{now: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)})
	if err := store.Migrate(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("Migrate() error = %v", err)
	}
	return store, db
}

func TestStore_UpsertTokenAndGetToken(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	row := TokenRow{
		ID:           "tok-1",
		TenantID:     "tenant-1",
		Subject:      "user:cli:u1",
		Provider:     ProviderGitHub,
		ScopesHash:   ScopesHash([]string{"repo", "user:email"}, ""),
		TokenJSONEnc: []byte{0x01, 0x02, 0x03},
		CreatedAtMS:  1000,
		UpdatedAtMS:  1000,
		ExpiresAtMS:  2000,
	}
	if err := store.UpsertToken(ctx, row); err != nil {
		t.Fatalf("UpsertToken() error = %v", err)
	}

	got, err := store.GetToken(ctx, row.TenantID, row.Subject, row.Provider, row.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}
	if got == nil {
		t.Fatalf("GetToken() returned nil")
		return
	}
	if got.ID != row.ID || got.Provider != row.Provider || got.ExpiresAtMS != row.ExpiresAtMS {
		t.Fatalf("GetToken() unexpected row: %+v", *got)
	}
}

func TestStore_GetTokenUnknownReturnsNil(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	got, err := store.GetToken(ctx, "tenant-1", "user:cli:u1", ProviderGitHub, "missing")
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", *got)
	}
}

func TestStore_DeleteTokenRemovesSpecificToken(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	subject := "user:cli:u1"
	tenantID := "tenant-1"

	rowA := TokenRow{
		ID:           "tok-a",
		TenantID:     tenantID,
		Subject:      subject,
		Provider:     ProviderGitHub,
		ScopesHash:   ScopesHash([]string{"repo"}, ""),
		TokenJSONEnc: []byte{0x01},
		CreatedAtMS:  100,
		UpdatedAtMS:  100,
		ExpiresAtMS:  1000,
	}
	rowB := TokenRow{
		ID:           "tok-b",
		TenantID:     tenantID,
		Subject:      subject,
		Provider:     ProviderGoogle,
		ScopesHash:   ScopesHash([]string{"email"}, ""),
		TokenJSONEnc: []byte{0x02},
		CreatedAtMS:  100,
		UpdatedAtMS:  100,
		ExpiresAtMS:  1000,
	}
	if err := store.UpsertToken(ctx, rowA); err != nil {
		t.Fatalf("UpsertToken(rowA) error = %v", err)
	}
	if err := store.UpsertToken(ctx, rowB); err != nil {
		t.Fatalf("UpsertToken(rowB) error = %v", err)
	}

	if err := store.DeleteToken(ctx, tenantID, subject, ProviderGitHub); err != nil {
		t.Fatalf("DeleteToken() error = %v", err)
	}

	gotA, err := store.GetToken(ctx, tenantID, subject, ProviderGitHub, rowA.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken(github) error = %v", err)
	}
	if gotA != nil {
		t.Fatalf("expected github token deleted, got %+v", *gotA)
	}

	gotB, err := store.GetToken(ctx, tenantID, subject, ProviderGoogle, rowB.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken(google) error = %v", err)
	}
	if gotB == nil {
		t.Fatalf("expected google token to remain")
	}
}

func TestStore_DeleteExpiredTokens(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	old := TokenRow{
		ID:           "tok-old",
		TenantID:     "tenant-1",
		Subject:      "user:cli:u1",
		Provider:     ProviderGitHub,
		ScopesHash:   ScopesHash([]string{"repo"}, ""),
		TokenJSONEnc: []byte{0x01},
		CreatedAtMS:  100,
		UpdatedAtMS:  100,
		ExpiresAtMS:  1000,
	}
	fresh := TokenRow{
		ID:           "tok-fresh",
		TenantID:     "tenant-1",
		Subject:      "user:cli:u1",
		Provider:     ProviderGoogle,
		ScopesHash:   ScopesHash([]string{"email"}, ""),
		TokenJSONEnc: []byte{0x02},
		CreatedAtMS:  100,
		UpdatedAtMS:  100,
		ExpiresAtMS:  3000,
	}
	if err := store.UpsertToken(ctx, old); err != nil {
		t.Fatalf("UpsertToken(old) error = %v", err)
	}
	if err := store.UpsertToken(ctx, fresh); err != nil {
		t.Fatalf("UpsertToken(fresh) error = %v", err)
	}

	deleted, err := store.DeleteExpiredTokens(ctx, 2000)
	if err != nil {
		t.Fatalf("DeleteExpiredTokens() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}

	gotOld, err := store.GetToken(ctx, old.TenantID, old.Subject, old.Provider, old.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken(old) error = %v", err)
	}
	if gotOld != nil {
		t.Fatalf("expected old token deleted")
	}

	gotFresh, err := store.GetToken(ctx, fresh.TenantID, fresh.Subject, fresh.Provider, fresh.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken(fresh) error = %v", err)
	}
	if gotFresh == nil {
		t.Fatalf("expected fresh token to remain")
	}
}

func TestStore_CreateAndGetAuthRequest(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	row := AuthRequestRow{
		ID:             "ar-1",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo user:email",
		StateNonce:     "nonce-1",
		ConversationID: "conv-1",
		ReplyContext:   `{"channel":"cli"}`,
		CreatedAtMS:    1000,
		ExpiresAtMS:    5000,
	}
	if err := store.CreateAuthRequest(ctx, row); err != nil {
		t.Fatalf("CreateAuthRequest() error = %v", err)
	}

	got, err := store.GetAuthRequest(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest() error = %v", err)
	}
	if got == nil {
		t.Fatalf("GetAuthRequest() returned nil")
		return
	}
	if got.StateNonce != row.StateNonce || got.ReplyContext != row.ReplyContext || got.Provider != row.Provider {
		t.Fatalf("GetAuthRequest() unexpected row: %+v", *got)
	}
}

func TestStore_GetAuthRequestByNonce(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	row := AuthRequestRow{
		ID:             "ar-2",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGoogle,
		Scopes:         "email profile",
		StateNonce:     "nonce-2",
		ConversationID: "conv-2",
		CreatedAtMS:    1000,
		ExpiresAtMS:    5000,
	}
	if err := store.CreateAuthRequest(ctx, row); err != nil {
		t.Fatalf("CreateAuthRequest() error = %v", err)
	}

	got, err := store.GetAuthRequestByNonce(ctx, row.StateNonce)
	if err != nil {
		t.Fatalf("GetAuthRequestByNonce() error = %v", err)
	}
	if got == nil {
		t.Fatalf("GetAuthRequestByNonce() returned nil")
		return
	}
	if got.ID != row.ID {
		t.Fatalf("expected ID %q, got %q", row.ID, got.ID)
	}
}

func TestStore_CompleteAuthRequestSetsCompletedAt(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	row := AuthRequestRow{
		ID:             "ar-3",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderMicrosoftGraph,
		Scopes:         "User.Read",
		StateNonce:     "nonce-3",
		ConversationID: "conv-3",
		CreatedAtMS:    1000,
		ExpiresAtMS:    5000,
	}
	if err := store.CreateAuthRequest(ctx, row); err != nil {
		t.Fatalf("CreateAuthRequest() error = %v", err)
	}

	completedAt := int64(3000)
	if err := store.CompleteAuthRequest(ctx, row.ID, completedAt); err != nil {
		t.Fatalf("CompleteAuthRequest() error = %v", err)
	}

	got, err := store.GetAuthRequest(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest() error = %v", err)
	}
	if got == nil {
		t.Fatalf("GetAuthRequest() returned nil")
		return
	}
	if got.CompletedAtMS == nil || *got.CompletedAtMS != completedAt {
		t.Fatalf("expected CompletedAtMS=%d, got %+v", completedAt, got.CompletedAtMS)
	}
}

func TestStore_DeleteExpiredAuthRequests(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	old := AuthRequestRow{
		ID:             "ar-old",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo",
		StateNonce:     "nonce-old",
		ConversationID: "conv-old",
		CreatedAtMS:    1000,
		ExpiresAtMS:    1500,
	}
	newer := AuthRequestRow{
		ID:             "ar-new",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo",
		StateNonce:     "nonce-new",
		ConversationID: "conv-new",
		CreatedAtMS:    2000,
		ExpiresAtMS:    4500,
	}
	if err := store.CreateAuthRequest(ctx, old); err != nil {
		t.Fatalf("CreateAuthRequest(old) error = %v", err)
	}
	if err := store.CreateAuthRequest(ctx, newer); err != nil {
		t.Fatalf("CreateAuthRequest(newer) error = %v", err)
	}

	deleted, err := store.DeleteExpiredAuthRequests(ctx, 3000)
	if err != nil {
		t.Fatalf("DeleteExpiredAuthRequests() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted row, got %d", deleted)
	}

	gotOld, err := store.GetAuthRequest(ctx, old.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest(old) error = %v", err)
	}
	if gotOld != nil {
		t.Fatalf("expected old auth request deleted")
	}

	gotNew, err := store.GetAuthRequest(ctx, newer.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest(new) error = %v", err)
	}
	if gotNew == nil {
		t.Fatalf("expected new auth request to remain")
	}
}

func TestScopesHash_Stability(t *testing.T) {
	a := ScopesHash([]string{"repo", "user:email"}, "aud-1")
	b := ScopesHash([]string{"repo", "user:email"}, "aud-1")
	if a != b {
		t.Fatalf("expected equal hashes, got %q and %q", a, b)
	}
}

func TestScopesHash_OrderIndependence(t *testing.T) {
	a := ScopesHash([]string{"repo", "user:email", "read:org"}, "")
	b := ScopesHash([]string{"read:org", "repo", "user:email"}, "")
	if a != b {
		t.Fatalf("expected equal hashes for different order, got %q and %q", a, b)
	}
}
