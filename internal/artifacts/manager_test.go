package artifacts

import (
	"context"
	"errors"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage"
)

// mockCASStore is a test double for storage.CASStore
type mockCASStore struct {
	storage.CASStore // embed to satisfy interface
	pinned           map[string]bool
	pinErr           error
	unpinErr         error
	perUnpinErr      map[string]error
	unpinAttempts    []string
}

func (m *mockCASStore) Pin(_ context.Context, digest string) error {
	if m.pinErr != nil {
		return m.pinErr
	}
	if m.pinned == nil {
		m.pinned = make(map[string]bool)
	}
	m.pinned[digest] = true
	return nil
}

func (m *mockCASStore) Unpin(_ context.Context, digest string) error {
	m.unpinAttempts = append(m.unpinAttempts, digest)
	if m.pinned == nil {
		m.pinned = make(map[string]bool)
	}
	if err, ok := m.perUnpinErr[digest]; ok && err != nil {
		return err
	}
	if m.unpinErr != nil {
		return m.unpinErr
	}
	delete(m.pinned, digest)
	return nil
}

func (m *mockCASStore) Close() error {
	return nil
}

func TestManager_ExtractDigests(t *testing.T) {
	mockCAS := &mockCASStore{}
	mgr := NewManager(mockCAS)

	tests := []struct {
		name     string
		envelope []byte
		want     []string
	}{
		{
			name:     "valid envelope with artifacts",
			envelope: []byte(`{"data":{"artifact":"sha256:abc","artifacts":["sha256:def"]}}`),
			want:     []string{"sha256:abc", "sha256:def"},
		},
		{
			name:     "envelope with no artifacts",
			envelope: []byte(`{"data":{"foo":"bar"}}`),
			want:     nil,
		},
		{
			name:     "empty envelope",
			envelope: []byte(`{}`),
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mgr.ExtractDigests(tt.envelope)
			if err != nil {
				t.Fatalf("ExtractDigests() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractDigests() got %d digests, want %d", len(got), len(tt.want))
			}
			for i, d := range got {
				if d != tt.want[i] {
					t.Errorf("ExtractDigests()[%d] = %v, want %v", i, d, tt.want[i])
				}
			}
		})
	}
}

func TestManager_Pin(t *testing.T) {
	tests := []struct {
		name    string
		digests []string
		pinErr  error
		wantErr bool
	}{
		{
			name:    "pin single digest",
			digests: []string{"sha256:abc"},
			wantErr: false,
		},
		{
			name:    "pin multiple digests",
			digests: []string{"sha256:abc", "sha256:def"},
			wantErr: false,
		},
		{
			name:    "pin empty list",
			digests: []string{},
			wantErr: false,
		},
		{
			name:    "pin with error",
			digests: []string{"sha256:abc"},
			pinErr:  errors.New("pin failed"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCAS := &mockCASStore{
				pinned: make(map[string]bool),
				pinErr: tt.pinErr,
			}
			mgr := NewManager(mockCAS)

			err := mgr.Pin(context.Background(), tt.digests...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Pin() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for _, d := range tt.digests {
					if !mockCAS.pinned[d] {
						t.Errorf("Pin() digest %s not pinned", d)
					}
				}
			}
		})
	}
}

func TestManager_Unpin(t *testing.T) {
	tests := []struct {
		name         string
		digests      []string
		unpinErr     error
		perUnpinErr  map[string]error
		wantErr      bool
		expectPinned map[string]bool
	}{
		{
			name:    "unpin single digest",
			digests: []string{"sha256:abc"},
			wantErr: false,
		},
		{
			name:    "unpin multiple digests",
			digests: []string{"sha256:abc", "sha256:def"},
			wantErr: false,
		},
		{
			name:    "unpin empty list",
			digests: []string{},
			wantErr: false,
		},
		{
			name:     "unpin with error",
			digests:  []string{"sha256:abc"},
			unpinErr: errors.New("unpin failed"),
			wantErr:  true,
		},
		{
			name:    "continues after individual error",
			digests: []string{"sha256:bad", "sha256:ok"},
			perUnpinErr: map[string]error{
				"sha256:bad": errors.New("boom"),
			},
			wantErr: true,
			expectPinned: map[string]bool{
				"sha256:bad": true,
				"sha256:ok":  false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCAS := &mockCASStore{
				pinned:      make(map[string]bool),
				unpinErr:    tt.unpinErr,
				perUnpinErr: tt.perUnpinErr,
			}
			// Pre-pin digests
			for _, d := range tt.digests {
				mockCAS.pinned[d] = true
			}

			mgr := NewManager(mockCAS)

			err := mgr.Unpin(context.Background(), tt.digests...)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unpin() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for _, d := range tt.digests {
				expectedPinned := false
				switch {
				case tt.expectPinned != nil:
					expectedPinned = tt.expectPinned[d]
				case tt.wantErr:
					expectedPinned = true
				default:
					expectedPinned = false
				}
				if mockCAS.pinned[d] != expectedPinned {
					t.Errorf("Unpin() digest %s pinned=%v, want %v", d, mockCAS.pinned[d], expectedPinned)
				}
			}

			if tt.name == "continues after individual error" && len(mockCAS.unpinAttempts) != len(tt.digests) {
				t.Fatalf("expected attempts for all digests, got %v", mockCAS.unpinAttempts)
			}
		})
	}
}

func TestManager_PinFromEnvelope(t *testing.T) {
	mockCAS := &mockCASStore{
		pinned: make(map[string]bool),
	}
	mgr := NewManager(mockCAS)

	envelope := []byte(`{"data":{"artifact":"sha256:abc123","artifacts":["sha256:def456"]}}`)

	digests, err := mgr.PinFromEnvelope(context.Background(), envelope)
	if err != nil {
		t.Fatalf("PinFromEnvelope() error = %v", err)
	}

	if len(digests) != 2 {
		t.Fatalf("PinFromEnvelope() returned %d digests, want 2", len(digests))
	}

	expectedDigests := []string{"sha256:abc123", "sha256:def456"}
	for i, expected := range expectedDigests {
		if digests[i] != expected {
			t.Errorf("PinFromEnvelope() digests[%d] = %v, want %v", i, digests[i], expected)
		}
		if !mockCAS.pinned[expected] {
			t.Errorf("PinFromEnvelope() digest %s not pinned", expected)
		}
	}
}

func TestManager_UnpinFromEnvelope(t *testing.T) {
	mockCAS := &mockCASStore{
		pinned: map[string]bool{
			"sha256:abc123": true,
			"sha256:def456": true,
		},
	}
	mgr := NewManager(mockCAS)

	envelope := []byte(`{"data":{"artifact":"sha256:abc123","artifacts":["sha256:def456"]}}`)

	digests, err := mgr.UnpinFromEnvelope(context.Background(), envelope)
	if err != nil {
		t.Fatalf("UnpinFromEnvelope() error = %v", err)
	}

	if len(digests) != 2 {
		t.Fatalf("UnpinFromEnvelope() returned %d digests, want 2", len(digests))
	}

	expectedDigests := []string{"sha256:abc123", "sha256:def456"}
	for i, expected := range expectedDigests {
		if digests[i] != expected {
			t.Errorf("UnpinFromEnvelope() digests[%d] = %v, want %v", i, digests[i], expected)
		}
		if mockCAS.pinned[expected] {
			t.Errorf("UnpinFromEnvelope() digest %s still pinned", expected)
		}
	}
}

func TestBatchOperation(t *testing.T) {
	mockCAS := &mockCASStore{
		pinned: make(map[string]bool),
	}
	mgr := NewManager(mockCAS)

	batch := NewBatch(mgr).
		Pin("sha256:abc", "sha256:def").
		Unpin("sha256:old1", "sha256:old2")

	if err := batch.Execute(context.Background()); err != nil {
		t.Fatalf("BatchOperation.Execute() error = %v", err)
	}

	// Check pins
	if !mockCAS.pinned["sha256:abc"] {
		t.Error("BatchOperation did not pin sha256:abc")
	}
	if !mockCAS.pinned["sha256:def"] {
		t.Error("BatchOperation did not pin sha256:def")
	}

	// Check unpins (they should not be in pinned map)
	if mockCAS.pinned["sha256:old1"] {
		t.Error("BatchOperation did not unpin sha256:old1")
	}
	if mockCAS.pinned["sha256:old2"] {
		t.Error("BatchOperation did not unpin sha256:old2")
	}
}
