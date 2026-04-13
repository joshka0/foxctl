package worktree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePorcelain_NormalEntries(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123def456789012345678901234567890abcd
branch refs/heads/main

worktree /home/user/project-wt-feat
HEAD deadbeef123456789012345678901234567890ab
branch refs/heads/feat/x

`
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, "/home/user/project", entries[0].Path)
	assert.Equal(t, "abc123def456789012345678901234567890abcd", entries[0].Commit)
	assert.Equal(t, "main", entries[0].Branch)
	assert.Equal(t, StatusOK, entries[0].Status)
	assert.False(t, entries[0].Bare)

	assert.Equal(t, "/home/user/project-wt-feat", entries[1].Path)
	assert.Equal(t, "deadbeef123456789012345678901234567890ab", entries[1].Commit)
	assert.Equal(t, "feat/x", entries[1].Branch)
	assert.Equal(t, StatusOK, entries[1].Status)
}

func TestParsePorcelain_BareRepo(t *testing.T) {
	input := `worktree /path/to/repo.git
HEAD abc123def456789012345678901234567890abcd
bare

`
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "/path/to/repo.git", entries[0].Path)
	assert.Equal(t, "abc123def456789012345678901234567890abcd", entries[0].Commit)
	assert.True(t, entries[0].Bare)
	assert.Empty(t, entries[0].Branch)
}

func TestParsePorcelain_DetachedHead(t *testing.T) {
	input := `worktree /home/user/project-detached
HEAD cafebaad123456789012345678901234567890ab
detached

`
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "/home/user/project-detached", entries[0].Path)
	assert.Equal(t, "cafebaad123456789012345678901234567890ab", entries[0].Commit)
	assert.Empty(t, entries[0].Branch)
}

func TestParsePorcelain_LockedAnnotation(t *testing.T) {
	input := `worktree /home/user/project-locked
HEAD abc123def456789012345678901234567890abcd
branch refs/heads/feat/locked
locked

`
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, "/home/user/project-locked", entries[0].Path)
	assert.Equal(t, "feat/locked", entries[0].Branch)
	assert.Equal(t, StatusLocked, entries[0].Status)
}

func TestParsePorcelain_LockedWithReason(t *testing.T) {
	input := `worktree /home/user/project-locked
HEAD abc123def456789012345678901234567890abcd
branch refs/heads/feat/locked
locked reason: working on this

`
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, StatusLocked, entries[0].Status)
	assert.Equal(t, "working on this", entries[0].Reason)
}

func TestParsePorcelain_PrunableAnnotation(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		status WorktreeStatus
	}{
		{
			name: "prunable with reason",
			input: `worktree /home/user/project-gone
HEAD abc123def456789012345678901234567890abcd
branch refs/heads/feat/gone
prunable gitdir file points to non-existent location

`,
			status: StatusPrunable,
		},
		{
			name: "prunable without reason",
			input: `worktree /home/user/project-gone
HEAD abc123def456789012345678901234567890abcd
branch refs/heads/feat/gone
prunable

`,
			status: StatusPrunable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := ParsePorcelain(tc.input)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, tc.status, entries[0].Status)
			assert.Equal(t, "/home/user/project-gone", entries[0].Path)
			assert.Equal(t, "feat/gone", entries[0].Branch)
		})
	}
}

func TestParsePorcelain_EmptyInput(t *testing.T) {
	entries, err := ParsePorcelain("")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestParsePorcelain_MixedEntries(t *testing.T) {
	input := `worktree /home/user/project
HEAD abc123def456789012345678901234567890abcd
branch refs/heads/main

worktree /home/user/project-bare
HEAD deadbeef123456789012345678901234567890ab
bare

worktree /home/user/project-detached
HEAD cafebaad123456789012345678901234567890ab
detached

worktree /home/user/project-locked
HEAD 1111111111111111111111111111111111111111
branch refs/heads/feat/locked
locked reason: fixing bug

worktree /home/user/project-gone
HEAD 2222222222222222222222222222222222222222
branch refs/heads/feat/gone
prunable gitdir file points to non-existent location

`
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 5)

	assert.Equal(t, "main", entries[0].Branch)
	assert.Equal(t, StatusOK, entries[0].Status)
	assert.False(t, entries[0].Bare)

	assert.Empty(t, entries[1].Branch)
	assert.True(t, entries[1].Bare)

	assert.Empty(t, entries[2].Branch)
	assert.Equal(t, StatusOK, entries[2].Status)

	assert.Equal(t, "feat/locked", entries[3].Branch)
	assert.Equal(t, StatusLocked, entries[3].Status)
	assert.Equal(t, "fixing bug", entries[3].Reason)

	assert.Equal(t, "feat/gone", entries[4].Branch)
	assert.Equal(t, StatusPrunable, entries[4].Status)
}

func TestParsePorcelain_TrailingNewline(t *testing.T) {
	// git worktree list --porcelain always ends with a blank line
	input := "worktree /home/user/project\nHEAD abc123\ndetached\n\n"
	entries, err := ParsePorcelain(input)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "/home/user/project", entries[0].Path)
}

func TestBranchFromRef(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected string
	}{
		{name: "full ref", ref: "refs/heads/main", expected: "main"},
		{name: "feature branch", ref: "refs/heads/feat/x", expected: "feat/x"},
		{name: "nested branch", ref: "refs/heads/feat/x/y", expected: "feat/x/y"},
		{name: "empty ref", ref: "", expected: ""},
		{name: "tag ref", ref: "refs/tags/v1.0", expected: ""},
		{name: "partial ref", ref: "heads/main", expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, branchFromRef(tc.ref))
		})
	}
}
