package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/stretchr/testify/require"
)

func TestCreateSkillResolver_EnvPrecedence(t *testing.T) {
	t.Setenv("AGENTCTL_HOME", t.TempDir())

	cwd := t.TempDir()
	// Resolve symlinks to match os.Getwd() behavior (macOS /var -> /private/var)
	realCwd, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	env1 := filepath.Join(realCwd, "env1")
	env2 := filepath.Join(realCwd, "env2")
	cfgSkills := filepath.Join(realCwd, "installed")
	cfg := config.Config{
		Paths: config.Paths{
			Skills: cfgSkills,
		},
	}

	t.Setenv("AGENTCTL_SKILLS_PATH", strings.Join([]string{env1, env2}, string(os.PathListSeparator)))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(origWD))
	})

	resolver := createSkillResolver(cfg)
	paths := resolver.SearchPaths()

	expected := []string{
		filepath.Clean(env1),
		filepath.Clean(env2),
		filepath.Clean(cfgSkills),
		filepath.Join(realCwd, "dist", "skills"),
		filepath.Join(realCwd, "skills"),
	}
	require.Equal(t, expected, paths)
}
