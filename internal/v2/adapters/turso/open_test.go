package turso

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestOpenStoreDBValidatesSpecBeforeOpening(t *testing.T) {
	complete := StoreOpenSpec{
		Name:             "v2 demo",
		EnvPrefix:        "V2_DEMO",
		LocalFilename:    "demo.turso",
		OverrideFilename: "demo.db",
	}

	tests := []struct {
		name        string
		storageRoot string
		spec        StoreOpenSpec
		wantErr     string
	}{
		{
			name:        "storage root required",
			storageRoot: "",
			spec:        complete,
			wantErr:     "v2 demo open: storageRoot is required",
		},
		{
			name:        "local filename required",
			storageRoot: t.TempDir(),
			spec: StoreOpenSpec{
				Name:             complete.Name,
				EnvPrefix:        complete.EnvPrefix,
				OverrideFilename: complete.OverrideFilename,
			},
			wantErr: "v2 demo open: local filename is required",
		},
		{
			name:        "override filename required",
			storageRoot: t.TempDir(),
			spec: StoreOpenSpec{
				Name:          complete.Name,
				EnvPrefix:     complete.EnvPrefix,
				LocalFilename: complete.LocalFilename,
			},
			wantErr: "v2 demo open: override filename is required",
		},
		{
			name:        "env prefix required",
			storageRoot: t.TempDir(),
			spec: StoreOpenSpec{
				Name:             complete.Name,
				LocalFilename:    complete.LocalFilename,
				OverrideFilename: complete.OverrideFilename,
			},
			wantErr: "v2 demo open: env prefix is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := OpenStoreDB(context.Background(), tt.storageRoot, tt.spec, noopMigrate)
			if err == nil {
				t.Fatal("OpenStoreDB() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("OpenStoreDB() error=%q want %q", err, tt.wantErr)
			}
		})
	}
}

func TestHasDriverOverrideChecksStoreAndGlobalEnv(t *testing.T) {
	t.Run("store-specific", func(t *testing.T) {
		t.Setenv("FOXCTL_DB_DRIVER", "")
		t.Setenv("FOXCTL_V2_DEMO_DB_DRIVER", "sqlite")
		if !hasDriverOverride("V2_DEMO") {
			t.Fatal("hasDriverOverride() = false, want true")
		}
	})

	t.Run("global", func(t *testing.T) {
		t.Setenv("FOXCTL_V2_DEMO_DB_DRIVER", "")
		t.Setenv("FOXCTL_DB_DRIVER", "sqlite")
		if !hasDriverOverride("V2_DEMO") {
			t.Fatal("hasDriverOverride() = false, want true")
		}
	})

	t.Run("none", func(t *testing.T) {
		t.Setenv("FOXCTL_V2_DEMO_DB_DRIVER", "")
		t.Setenv("FOXCTL_DB_DRIVER", "")
		if hasDriverOverride("V2_DEMO") {
			t.Fatal("hasDriverOverride() = true, want false")
		}
	})
}

func noopMigrate(context.Context, *sql.DB) error {
	return nil
}
