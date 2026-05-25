package authbroker

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"testing/quick"
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

func TestStore_DeleteExpiredTokensIncludesCutoff(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	cutoff := int64(2000)
	expiredAtCutoff := TokenRow{
		ID:           "tok-cutoff",
		TenantID:     "tenant-1",
		Subject:      "user:cli:u1",
		Provider:     ProviderGitHub,
		ScopesHash:   ScopesHash([]string{"repo"}, ""),
		TokenJSONEnc: []byte{0x01},
		CreatedAtMS:  1000,
		UpdatedAtMS:  1000,
		ExpiresAtMS:  cutoff,
	}
	future := TokenRow{
		ID:           "tok-future",
		TenantID:     "tenant-1",
		Subject:      "user:cli:u1",
		Provider:     ProviderGoogle,
		ScopesHash:   ScopesHash([]string{"email"}, ""),
		TokenJSONEnc: []byte{0x02},
		CreatedAtMS:  1000,
		UpdatedAtMS:  1000,
		ExpiresAtMS:  cutoff + 1,
	}
	if err := store.UpsertToken(ctx, expiredAtCutoff); err != nil {
		t.Fatalf("UpsertToken(expiredAtCutoff) error = %v", err)
	}
	if err := store.UpsertToken(ctx, future); err != nil {
		t.Fatalf("UpsertToken(future) error = %v", err)
	}

	deleted, err := store.DeleteExpiredTokens(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteExpiredTokens() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 token deleted at cutoff, got %d", deleted)
	}

	gotExpired, err := store.GetToken(ctx, expiredAtCutoff.TenantID, expiredAtCutoff.Subject, expiredAtCutoff.Provider, expiredAtCutoff.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken(expiredAtCutoff) error = %v", err)
	}
	if gotExpired != nil {
		t.Fatalf("expected cutoff token deleted, got %+v", *gotExpired)
	}

	gotFuture, err := store.GetToken(ctx, future.TenantID, future.Subject, future.Provider, future.ScopesHash)
	if err != nil {
		t.Fatalf("GetToken(future) error = %v", err)
	}
	if gotFuture == nil {
		t.Fatal("expected future token to remain")
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

func TestStore_CreateAuthRequestRejectsInvalidInitialStates(t *testing.T) {
	ctx := context.Background()

	completedAt := int64(1500)
	tests := []struct {
		name string
		mut  func(*AuthRequestRow)
	}{
		{name: "blank id", mut: func(row *AuthRequestRow) { row.ID = " " }},
		{name: "blank tenant", mut: func(row *AuthRequestRow) { row.TenantID = " " }},
		{name: "blank subject", mut: func(row *AuthRequestRow) { row.Subject = " " }},
		{name: "unknown provider", mut: func(row *AuthRequestRow) { row.Provider = Provider("unknown") }},
		{name: "blank state nonce", mut: func(row *AuthRequestRow) { row.StateNonce = " " }},
		{name: "blank conversation id", mut: func(row *AuthRequestRow) { row.ConversationID = " " }},
		{name: "expires at creation", mut: func(row *AuthRequestRow) { row.ExpiresAtMS = row.CreatedAtMS }},
		{name: "expires before creation", mut: func(row *AuthRequestRow) { row.ExpiresAtMS = row.CreatedAtMS - 1 }},
		{name: "already completed", mut: func(row *AuthRequestRow) { row.CompletedAtMS = &completedAt }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, db := openTestSQLiteStore(t)
			defer func() { _ = db.Close() }()

			row := validAuthRequestRow("invalid-" + strings.ReplaceAll(tt.name, " ", "-"))
			tt.mut(&row)
			if err := store.CreateAuthRequest(ctx, row); err == nil {
				t.Fatalf("CreateAuthRequest(%s) succeeded for invalid row: %+v", tt.name, row)
			}
			if strings.TrimSpace(row.ID) == "" {
				return
			}
			got, err := store.GetAuthRequest(ctx, row.ID)
			if err != nil {
				t.Fatalf("GetAuthRequest(%q) error = %v", row.ID, err)
			}
			if got != nil {
				t.Fatalf("invalid auth request was persisted: %+v", *got)
			}
		})
	}
}

func TestStore_CreateAuthRequestPropertyRejectsNonPendingOrExpiredInitialState(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	var seq uint64
	err := quick.Check(func(validWindow uint16, completed bool) bool {
		seq++
		row := validAuthRequestRow("prop-invalid-" + safeAuthRequestToken(seq))
		row.CreatedAtMS = int64(1000 + seq*100000)
		row.ExpiresAtMS = row.CreatedAtMS - int64(validWindow)
		if completed {
			completedAt := row.CreatedAtMS
			row.CompletedAtMS = &completedAt
		}

		if err := store.CreateAuthRequest(ctx, row); err == nil {
			t.Logf("CreateAuthRequest accepted invalid initial state: %+v", row)
			return false
		}
		got, err := store.GetAuthRequest(ctx, row.ID)
		return err == nil && got == nil
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatalf("auth request invalid initial-state property failed: %v", err)
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

func TestStore_CompleteAuthRequestRejectsReplay(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	row := AuthRequestRow{
		ID:             "ar-replay",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo",
		StateNonce:     "nonce-replay",
		ConversationID: "conv-replay",
		CreatedAtMS:    1000,
		ExpiresAtMS:    5000,
	}
	if err := store.CreateAuthRequest(ctx, row); err != nil {
		t.Fatalf("CreateAuthRequest() error = %v", err)
	}

	completedAt := int64(3000)
	if err := store.CompleteAuthRequest(ctx, row.ID, completedAt); err != nil {
		t.Fatalf("CompleteAuthRequest(first) error = %v", err)
	}
	if err := store.CompleteAuthRequest(ctx, row.ID, 4000); err == nil {
		t.Fatal("expected replayed auth request completion to be rejected")
	}

	got, err := store.GetAuthRequest(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest() error = %v", err)
	}
	if got == nil || got.CompletedAtMS == nil || *got.CompletedAtMS != completedAt {
		t.Fatalf("completed_at changed after replay: %+v", got)
	}
}

func TestStore_CompleteAuthRequestRejectsExpiredRequest(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	row := AuthRequestRow{
		ID:             "ar-expired",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo",
		StateNonce:     "nonce-expired",
		ConversationID: "conv-expired",
		CreatedAtMS:    1000,
		ExpiresAtMS:    2000,
	}
	if err := store.CreateAuthRequest(ctx, row); err != nil {
		t.Fatalf("CreateAuthRequest() error = %v", err)
	}

	if err := store.CompleteAuthRequest(ctx, row.ID, 2000); err == nil {
		t.Fatal("expected expired auth request completion to be rejected")
	}
	got, err := store.GetAuthRequest(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest() error = %v", err)
	}
	if got == nil {
		t.Fatal("expected auth request to remain for audit")
	}
	if got.CompletedAtMS != nil {
		t.Fatalf("expired auth request was completed: %+v", got.CompletedAtMS)
	}
}

func TestStore_CompleteAuthRequestPropertyEnforcesExpiryAndSingleUse(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	var seq uint64
	prop := func(validWindow, lateBy uint16) bool {
		seq++
		createdAt := int64(1000 + seq*100000)
		expiresAt := createdAt + int64(validWindow) + 1
		validCompletion := expiresAt - 1
		expiredCompletion := expiresAt + int64(lateBy)

		valid := AuthRequestRow{
			ID:             "ar-prop-valid-" + safeAuthRequestToken(seq),
			TenantID:       "tenant-1",
			Subject:        "user:cli:u1",
			Provider:       ProviderGitHub,
			Scopes:         "repo",
			StateNonce:     "nonce-prop-valid-" + safeAuthRequestToken(seq),
			ConversationID: "conv-prop-valid",
			CreatedAtMS:    createdAt,
			ExpiresAtMS:    expiresAt,
		}
		if err := store.CreateAuthRequest(ctx, valid); err != nil {
			t.Logf("CreateAuthRequest(valid): %v", err)
			return false
		}
		if err := store.CompleteAuthRequest(ctx, valid.ID, validCompletion); err != nil {
			t.Logf("CompleteAuthRequest(valid at %d before %d): %v", validCompletion, expiresAt, err)
			return false
		}
		if err := store.CompleteAuthRequest(ctx, valid.ID, validCompletion); err == nil {
			t.Logf("CompleteAuthRequest accepted replay for %q", valid.ID)
			return false
		}

		expired := AuthRequestRow{
			ID:             "ar-prop-expired-" + safeAuthRequestToken(seq),
			TenantID:       "tenant-1",
			Subject:        "user:cli:u1",
			Provider:       ProviderGitHub,
			Scopes:         "repo",
			StateNonce:     "nonce-prop-expired-" + safeAuthRequestToken(seq),
			ConversationID: "conv-prop-expired",
			CreatedAtMS:    createdAt,
			ExpiresAtMS:    expiresAt,
		}
		if err := store.CreateAuthRequest(ctx, expired); err != nil {
			t.Logf("CreateAuthRequest(expired): %v", err)
			return false
		}
		if err := store.CompleteAuthRequest(ctx, expired.ID, expiredCompletion); err == nil {
			t.Logf("CompleteAuthRequest accepted completion at %d for expiry %d", expiredCompletion, expiresAt)
			return false
		}
		got, err := store.GetAuthRequest(ctx, expired.ID)
		return err == nil && got != nil && got.CompletedAtMS == nil
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("auth request lifecycle property failed: %v", err)
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

func TestStore_DeleteExpiredAuthRequestsIncludesCutoff(t *testing.T) {
	ctx := context.Background()
	store, db := openTestSQLiteStore(t)
	defer func() { _ = db.Close() }()

	cutoff := int64(2000)
	expiredAtCutoff := AuthRequestRow{
		ID:             "ar-cutoff",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo",
		StateNonce:     "nonce-cutoff",
		ConversationID: "conv-cutoff",
		CreatedAtMS:    1000,
		ExpiresAtMS:    cutoff,
	}
	future := AuthRequestRow{
		ID:             "ar-future",
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGoogle,
		Scopes:         "email",
		StateNonce:     "nonce-future",
		ConversationID: "conv-future",
		CreatedAtMS:    1000,
		ExpiresAtMS:    cutoff + 1,
	}
	if err := store.CreateAuthRequest(ctx, expiredAtCutoff); err != nil {
		t.Fatalf("CreateAuthRequest(expiredAtCutoff) error = %v", err)
	}
	if err := store.CreateAuthRequest(ctx, future); err != nil {
		t.Fatalf("CreateAuthRequest(future) error = %v", err)
	}

	deleted, err := store.DeleteExpiredAuthRequests(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteExpiredAuthRequests() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 auth request deleted at cutoff, got %d", deleted)
	}

	gotExpired, err := store.GetAuthRequest(ctx, expiredAtCutoff.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest(expiredAtCutoff) error = %v", err)
	}
	if gotExpired != nil {
		t.Fatalf("expected cutoff auth request deleted, got %+v", *gotExpired)
	}

	gotFuture, err := store.GetAuthRequest(ctx, future.ID)
	if err != nil {
		t.Fatalf("GetAuthRequest(future) error = %v", err)
	}
	if gotFuture == nil {
		t.Fatal("expected future auth request to remain")
	}
}

func safeAuthRequestToken(n uint64) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	if n == 0 {
		return "0"
	}
	var buf [13]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = alphabet[n%uint64(len(alphabet))]
		n /= uint64(len(alphabet))
	}
	return string(buf[i:])
}

func validAuthRequestRow(id string) AuthRequestRow {
	return AuthRequestRow{
		ID:             id,
		TenantID:       "tenant-1",
		Subject:        "user:cli:u1",
		Provider:       ProviderGitHub,
		Scopes:         "repo",
		StateNonce:     "nonce-" + id,
		ConversationID: "conv-" + id,
		CreatedAtMS:    1000,
		ExpiresAtMS:    5000,
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

func TestScopesHashPropertyOrderIndependentAndPure(t *testing.T) {
	t.Parallel()

	prop := func(rawScopes []string, rawAudience string) bool {
		scopes := generatedScopes(rawScopes)
		original := slices.Clone(scopes)
		reordered := slices.Clone(scopes)
		slices.Reverse(reordered)

		a := ScopesHash(scopes, rawAudience)
		b := ScopesHash(reordered, rawAudience)
		if a != b {
			t.Logf("ScopesHash(%q, %q)=%q but reversed hash=%q", scopes, rawAudience, a, b)
			return false
		}
		if !slices.Equal(scopes, original) {
			t.Logf("ScopesHash mutated caller slice: got %q want %q", scopes, original)
			return false
		}
		return len(a) == 16
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func generatedScopes(raw []string) []string {
	if len(raw) == 0 {
		return []string{"repo"}
	}
	if len(raw) > 12 {
		raw = raw[:12]
	}
	out := make([]string, 0, len(raw))
	for i, scope := range raw {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			scope = safeAuthRequestToken(uint64(i + 1))
		}
		if len(scope) > 48 {
			scope = scope[:48]
		}
		out = append(out, scope)
	}
	return out
}
