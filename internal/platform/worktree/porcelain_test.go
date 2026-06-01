package worktree

import (
	"fmt"
	"strings"
	"testing"
	"testing/quick"

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

func TestParsePorcelain_RejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "missing worktree",
			input: `HEAD abc123def456789012345678901234567890abcd
branch refs/heads/main
`,
		},
		{
			name: "missing HEAD",
			input: `worktree /home/user/project
branch refs/heads/main
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := ParsePorcelain(tc.input)
			require.Error(t, err)
			assert.Empty(t, entries)
		})
	}
}

func TestParsePorcelain_RoundTripGeneratedRecords(t *testing.T) {
	property := func(raw []porcelainRecordCase) bool {
		var input strings.Builder
		want := make([]WorktreeEntry, 0, len(raw))

		for i, item := range raw {
			record, entry := generatedPorcelainRecord(i, item)
			input.WriteString(record)
			input.WriteString("\n\n")
			want = append(want, entry)
		}

		got, err := ParsePorcelain(input.String())
		if err != nil {
			return false
		}
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 128}); err != nil {
		t.Fatalf("generated porcelain round-trip failed: %v", err)
	}
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

type porcelainRecordCase struct {
	ID     uint8
	Kind   uint8
	Reason uint8
}

func generatedPorcelainRecord(index int, item porcelainRecordCase) (string, WorktreeEntry) {
	path := fmt.Sprintf("/tmp/foxctl-worktree-%03d-%03d", index, item.ID)
	commit := fmt.Sprintf("%040x", (index+1)*1000+int(item.ID))
	branch := generatedBranchName(index, item.ID)
	entry := WorktreeEntry{
		Path:   path,
		Commit: commit,
		Status: StatusOK,
	}

	var b strings.Builder
	fmt.Fprintf(&b, "worktree %s\nHEAD %s\n", path, commit)

	switch item.Kind % 5 {
	case 0:
		fmt.Fprintf(&b, "branch refs/heads/%s\n", branch)
		entry.Branch = branch
	case 1:
		fmt.Fprintf(&b, "branch refs/heads/%s\nlocked reason: reason-%03d\n", branch, item.Reason)
		entry.Branch = branch
		entry.Status = StatusLocked
		entry.Reason = fmt.Sprintf("reason-%03d", item.Reason)
	case 2:
		fmt.Fprintf(&b, "branch refs/heads/%s\nprunable gitdir file points to non-existent location\n", branch)
		entry.Branch = branch
		entry.Status = StatusPrunable
	case 3:
		b.WriteString("detached\n")
	default:
		b.WriteString("bare\n")
		entry.Bare = true
	}

	return b.String(), entry
}

func generatedBranchName(index int, id uint8) string {
	switch id % 3 {
	case 0:
		return fmt.Sprintf("main-%03d", index)
	case 1:
		return fmt.Sprintf("feat/%03d/%03d", index, id)
	default:
		return fmt.Sprintf("bugfix-%03d-%03d", index, id)
	}
}
