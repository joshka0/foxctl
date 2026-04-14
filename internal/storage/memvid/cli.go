package memvid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CLI wraps the memvid-cli command-line tool.
// Install via: npm install -g memvid-cli
type CLI struct {
	// BinPath is the path to the memvid CLI binary.
	// If empty, uses "memvid" from PATH.
	BinPath string

	// Timeout for CLI commands (default: 5 minutes)
	Timeout time.Duration
}

// NewCLI creates a new CLI wrapper with default settings.
func NewCLI() *CLI {
	return &CLI{
		Timeout: 5 * time.Minute,
	}
}

// Available checks if the memvid CLI is installed and accessible.
func (c *CLI) Available(ctx context.Context) error {
	_, err := c.run(ctx, "version")
	if err != nil {
		return fmt.Errorf("memvid-cli not available: %w (install with: npm install -g memvid-cli)", err)
	}
	return nil
}

// Version returns the memvid CLI version.
func (c *CLI) Version(ctx context.Context) (string, error) {
	out, err := c.run(ctx, "version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Create initializes a new MV2 file at the given path.
func (c *CLI) Create(ctx context.Context, path string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	_, err := c.run(ctx, "create", path)
	return err
}

// Put adds content to an MV2 file.
func (c *CLI) Put(ctx context.Context, path string, frame Frame) error {
	// Build put command with options
	args := []string{"put", path}

	if frame.Title != "" {
		args = append(args, "--title", frame.Title)
	}

	if frame.URI != "" {
		args = append(args, "--uri", frame.URI)
	}

	// Pass tags as key=value pairs
	for k, v := range frame.Tags {
		args = append(args, "--tag", fmt.Sprintf("%s=%s", k, v))
	}

	// Content is passed via stdin
	_, err := c.runWithStdin(ctx, frame.Content, args...)
	return err
}

// PutBatch adds multiple frames to an MV2 file efficiently.
func (c *CLI) PutBatch(ctx context.Context, path string, frames []Frame) error {
	// Convert frames to JSONL format for batch ingestion
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, f := range frames {
		if err := enc.Encode(f); err != nil {
			return fmt.Errorf("failed to encode frame: %w", err)
		}
	}

	_, err := c.runWithStdin(ctx, buf.String(), "put", path, "--format", "jsonl")
	return err
}

// FindResponse is the JSON response from memvid find --json.
type FindResponse struct {
	Version  string `json:"version"`
	Query    string `json:"query"`
	Metadata struct {
		ElapsedMS int    `json:"elapsed_ms"`
		TotalHits int    `json:"total_hits"`
		Engine    string `json:"engine"`
	} `json:"metadata"`
	Hits []struct {
		Rank     int     `json:"rank"`
		Score    float64 `json:"score"`
		FrameID  int     `json:"frame_id"`
		URI      string  `json:"uri"`
		Title    string  `json:"title"`
		Text     string  `json:"text"`
		Metadata struct {
			Matches   int      `json:"matches"`
			Tags      []string `json:"tags"`
			Labels    []string `json:"labels"`
			CreatedAt string   `json:"created_at"`
		} `json:"metadata"`
	} `json:"hits"`
}

// Find searches an MV2 file and returns matching results.
func (c *CLI) Find(ctx context.Context, path string, opts SearchOptions) ([]SearchResult, error) {
	args := []string{"find", path, "--query", opts.Query, "--json"}

	if opts.Mode != "" {
		args = append(args, "--mode", string(opts.Mode))
	}

	if opts.TopK > 0 {
		args = append(args, "--top-k", strconv.Itoa(opts.TopK))
	}

	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var resp FindResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	results := make([]SearchResult, len(resp.Hits))
	for i, hit := range resp.Hits {
		results[i] = SearchResult{
			FrameID: uint64(hit.FrameID),
			URI:     hit.URI,
			Title:   hit.Title,
			Score:   hit.Score,
			Snippet: hit.Text,
		}
	}

	return results, nil
}

// Stats returns statistics about an MV2 file.
func (c *CLI) Stats(ctx context.Context, path string) (*Stats, error) {
	out, err := c.run(ctx, "stats", path, "--output", "json")
	if err != nil {
		return nil, err
	}

	var stats Stats
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		return nil, fmt.Errorf("failed to parse stats: %w", err)
	}

	return &stats, nil
}

// Verify checks the integrity of an MV2 file.
func (c *CLI) Verify(ctx context.Context, path string) error {
	_, err := c.run(ctx, "verify", path)
	return err
}

// run executes a memvid CLI command and returns stdout.
func (c *CLI) run(ctx context.Context, args ...string) (string, error) {
	return c.runWithStdin(ctx, "", args...)
}

// runWithStdin executes a command with optional stdin input.
func (c *CLI) runWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bin := c.BinPath
	if bin == "" {
		bin = "memvid"
	}

	cmd := exec.CommandContext(ctx, bin, args...)

	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out after %v", timeout)
		}
		return "", fmt.Errorf("memvid %s failed: %w\nstderr: %s", args[0], err, stderr.String())
	}

	return stdout.String(), nil
}
