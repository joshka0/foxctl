package cmd

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/runtime/runservice"
	"github.com/jkatigb/agentctl/internal/storage/cache"
)

func TestRunOptions_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    runservice.RunOptions
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid options",
			opts: runservice.RunOptions{
				SkillName: "test-skill",
				Input:     []byte(`{}`),
				CacheMode: cache.ModeOff,
			},
			wantErr: false,
		},
		{
			name: "cache mode auto rejected",
			opts: runservice.RunOptions{
				SkillName: "test-skill",
				Input:     []byte(`{}`),
				CacheMode: cache.ModeAuto,
			},
			wantErr: true,
			errMsg:  "cache is disabled (expected --cache=off)",
		},
		{
			name: "empty skill name",
			opts: runservice.RunOptions{
				SkillName: "",
				Input:     []byte(`{}`),
			},
			wantErr: true,
			errMsg:  "skill name cannot be empty",
		},
		{
			name: "async with cache-only",
			opts: runservice.RunOptions{
				SkillName: "test-skill",
				Input:     []byte(`{}`),
				Async:     true,
				CacheMode: cache.ModeOnly,
			},
			wantErr: true,
			errMsg:  "cache is disabled (expected --cache=off)",
		},
		{
			name: "async with remember",
			opts: runservice.RunOptions{
				SkillName:    "test-skill",
				Input:        []byte(`{}`),
				Async:        true,
				RememberName: "test-memory",
			},
			wantErr: true,
			errMsg:  "--remember cannot be used with --async",
		},
		{
			name: "valid async without cache-only",
			opts: runservice.RunOptions{
				SkillName: "test-skill",
				Input:     []byte(`{}`),
				Async:     true,
				CacheMode: cache.ModeOff,
			},
			wantErr: false,
		},
		{
			name: "valid with remember",
			opts: runservice.RunOptions{
				SkillName:    "test-skill",
				Input:        []byte(`{}`),
				Async:        false,
				RememberName: "test-memory",
			},
			wantErr: false,
		},
		{
			name: "valid cache modes",
			opts: runservice.RunOptions{
				SkillName: "test-skill",
				Input:     []byte(`{}`),
				CacheMode: cache.ModeOff,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && err.Error() != tt.errMsg {
				t.Errorf("Validate() error message = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}
