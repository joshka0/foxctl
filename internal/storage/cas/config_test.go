package cas

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestConfigValidateAcceptsValidDriverSpecificRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "file",
			cfg:  Config{Driver: DriverFile, File: FileConfig{Path: "/tmp/cas"}},
		},
		{
			name: "sqlite",
			cfg: Config{Driver: DriverSQLite, SQLite: SQLiteConfig{
				DBPath:        "/tmp/cas.db",
				BlobThreshold: 0,
				BusyTimeout:   0,
			}},
		},
		{
			name: "turso",
			cfg: Config{Driver: DriverTurso, Turso: TursoConfig{
				URL:           "libsql://example.turso.io",
				AuthToken:     "token",
				BlobThreshold: 0,
			}},
		},
		{
			name: "s3",
			cfg:  Config{Driver: DriverS3, S3: S3Config{Bucket: "bucket"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestConfigValidateRejectsInvalidDriverSpecificFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing driver", cfg: Config{}},
		{name: "unsupported driver", cfg: Config{Driver: "memory"}},
		{name: "file blank path", cfg: Config{Driver: DriverFile, File: FileConfig{Path: " \t\n"}}},
		{name: "sqlite blank path", cfg: Config{Driver: DriverSQLite, SQLite: SQLiteConfig{DBPath: " \t\n"}}},
		{name: "sqlite negative threshold", cfg: Config{Driver: DriverSQLite, SQLite: SQLiteConfig{DBPath: "/tmp/cas.db", BlobThreshold: -1}}},
		{name: "sqlite negative timeout", cfg: Config{Driver: DriverSQLite, SQLite: SQLiteConfig{DBPath: "/tmp/cas.db", BusyTimeout: -1}}},
		{name: "turso blank url", cfg: Config{Driver: DriverTurso, Turso: TursoConfig{URL: " \t\n", AuthToken: "token"}}},
		{name: "turso blank token", cfg: Config{Driver: DriverTurso, Turso: TursoConfig{URL: "libsql://example.turso.io", AuthToken: " \t\n"}}},
		{name: "turso negative threshold", cfg: Config{Driver: DriverTurso, Turso: TursoConfig{URL: "libsql://example.turso.io", AuthToken: "token", BlobThreshold: -1}}},
		{name: "s3 blank bucket", cfg: Config{Driver: DriverS3, S3: S3Config{Bucket: " \t\n"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatalf("Validate() expected error")
			}
		})
	}
}

func TestConfigValidatePropertyRequiredFieldsMustBeNonBlank(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(raw string) bool {
		value := strings.TrimSpace(raw)
		if value == "" {
			return validateConfig(Config{Driver: DriverFile, File: FileConfig{Path: raw}}) != nil &&
				validateConfig(Config{Driver: DriverSQLite, SQLite: SQLiteConfig{DBPath: raw}}) != nil &&
				validateConfig(Config{Driver: DriverTurso, Turso: TursoConfig{URL: raw, AuthToken: "token"}}) != nil &&
				validateConfig(Config{Driver: DriverTurso, Turso: TursoConfig{URL: "libsql://example.turso.io", AuthToken: raw}}) != nil &&
				validateConfig(Config{Driver: DriverS3, S3: S3Config{Bucket: raw}}) != nil
		}

		return validateConfig(Config{Driver: DriverFile, File: FileConfig{Path: raw}}) == nil &&
			validateConfig(Config{Driver: DriverSQLite, SQLite: SQLiteConfig{DBPath: raw}}) == nil &&
			validateConfig(Config{Driver: DriverTurso, Turso: TursoConfig{URL: raw, AuthToken: raw}}) == nil &&
			validateConfig(Config{Driver: DriverS3, S3: S3Config{Bucket: raw}}) == nil
	}, cfg)
	if err != nil {
		t.Fatalf("required field property failed: %v", err)
	}
}

func validateConfig(cfg Config) error {
	return cfg.Validate()
}

func TestConfigValidatePropertyThresholdsAndTimeoutsRejectOnlyNegativeValues(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(sqliteThreshold, tursoThreshold int64, busyTimeout int) bool {
		sqlite := Config{Driver: DriverSQLite, SQLite: SQLiteConfig{
			DBPath:        "/tmp/cas.db",
			BlobThreshold: sqliteThreshold,
			BusyTimeout:   busyTimeout,
		}}
		turso := Config{Driver: DriverTurso, Turso: TursoConfig{
			URL:           "libsql://example.turso.io",
			AuthToken:     "token",
			BlobThreshold: tursoThreshold,
		}}

		sqliteWantErr := sqliteThreshold < 0 || busyTimeout < 0
		tursoWantErr := tursoThreshold < 0
		return (sqlite.Validate() != nil) == sqliteWantErr &&
			(turso.Validate() != nil) == tursoWantErr
	}, cfg)
	if err != nil {
		t.Fatalf("numeric config property failed: %v", err)
	}
}

func TestDefaultConfigsValidate(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]Config{
		"default": DefaultConfig("/tmp/foxctl"),
		"file":    DefaultFileConfig("/tmp/foxctl"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}
