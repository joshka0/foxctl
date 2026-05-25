package runservice

import (
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/storage/cache"
)

func TestRunOptionsValidateDefaultsEmptyCacheModeToOff(t *testing.T) {
	t.Parallel()

	opts := RunOptions{SkillName: "test/run"}
	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if opts.CacheMode != cache.ModeOff {
		t.Fatalf("CacheMode = %q, want %q", opts.CacheMode, cache.ModeOff)
	}
}

func TestRunOptionsValidatePropertyRejectsEveryNonOffCacheMode(t *testing.T) {
	t.Parallel()

	prop := func(raw string) bool {
		mode := cacheModeExceptOff(raw)
		opts := RunOptions{
			SkillName: "test/run",
			CacheMode: mode,
		}

		err := opts.Validate()
		if err == nil {
			t.Logf("Validate accepted cache mode %q while cache is disabled", mode)
			return false
		}
		if !strings.Contains(err.Error(), "cache is disabled") {
			t.Logf("Validate(%q) error = %v, want cache-disabled error", mode, err)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("non-off cache mode property failed: %v", err)
	}
}

func TestRunOptionsValidateRejectsNilOptions(t *testing.T) {
	t.Parallel()

	var opts *RunOptions
	err := opts.Validate()
	if err == nil {
		t.Fatal("Validate accepted nil options")
	}
	if err.Error() != "options cannot be nil" {
		t.Fatalf("nil options error = %v", err)
	}
}

func cacheModeExceptOff(raw string) cache.Mode {
	mode := cache.Mode(strings.TrimSpace(raw))
	if mode == "" || mode == cache.ModeOff {
		return cache.ModeAuto
	}
	return mode
}
