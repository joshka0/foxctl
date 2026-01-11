// Package main implements the session/recall skill for semantic session retrieval.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters.
type Input struct {
	Query             string  `json:"query" validate:"required"`
	Limit             int     `json:"limit,omitempty"`
	MinSimilarity     float64 `json:"min_similarity,omitempty"`
	Workspace         string  `json:"workspace,omitempty"`
	Project           string  `json:"project,omitempty"`
	WindowGranularity bool    `json:"window_granularity,omitempty"` // Search at context window level instead of session
}

// Output defines the skill output.
type Output struct {
	Query               string         `json:"query"`
	Matches             []SessionMatch `json:"matches,omitempty"`
	WindowMatches       []WindowMatch  `json:"window_matches,omitempty"` // Populated when window_granularity is true
	TotalWithEmbeddings int            `json:"total_with_embeddings"`
	Status              string         `json:"status"`
	Message             string         `json:"message"`
}

// SessionMatch represents a matched session with similarity score.
type SessionMatch struct {
	SessionID    string   `json:"session_id"`
	ProjectName  string   `json:"project_name"`
	GitBranch    string   `json:"git_branch,omitempty"`
	Summary      string   `json:"summary"`
	Accomplished []string `json:"accomplished,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Gotchas      []string `json:"gotchas,omitempty"`
	UserInsights []string `json:"user_insights,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	KeyFiles     []string `json:"key_files,omitempty"`
	Similarity   float64  `json:"similarity"`
	StartedAt    string   `json:"started_at,omitempty"`
}

// WindowMatch represents a matched context window with similarity score.
type WindowMatch struct {
	SessionID        string  `json:"session_id"`
	WindowIndex      int     `json:"window_index"`
	Trigger          string  `json:"trigger,omitempty"`
	PreCompactTokens int     `json:"pre_compact_tokens,omitempty"`
	Summary          string  `json:"summary,omitempty"`
	MessageCount     int     `json:"message_count"`
	StartedAt        string  `json:"started_at,omitempty"`
	EndedAt          string  `json:"ended_at,omitempty"`
	Similarity       float64 `json:"similarity"`
}

const (
	geminiModel   = "gemini-embedding-001"
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	defaultLimit  = 5
	defaultMinSim = 0.3
)

// geminiEmbedRequest is the request body for the Gemini embed API.
type geminiEmbedRequest struct {
	Model   string            `json:"model"`
	Content geminiContentPart `json:"content"`
}

type geminiContentPart struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

// geminiEmbedResponse is the response from the Gemini embed API.
type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *geminiError `json:"error,omitempty"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

func main() {
	skillmain.Main("session/recall", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	normalizeInput(&in, rc)

	// Check for embedding API key - prefer Voyage, fall back to Gemini, then FTS
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	useFTSFallback := voyageKey == "" && geminiKey == ""

	// Get agentctl home
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		homeDir, _ := os.UserHomeDir()
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		return fmt.Errorf("open sessions store: %w", err)
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Generate embedding for the query - prefer Voyage, fall back to Gemini, then FTS
	var queryEmbedding []float32
	if !useFTSFallback {
		if voyageKey != "" {
			// Get model from scope-based recommendation (supports env var override)
			model, _ := semantic.ScopeModelRecommendation(semantic.ScopeSessions)
			vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
				APIKey:        voyageKey,
				Model:         model, // Must match session_summarize for compatible embeddings
				RateLimitWait: boolPtr(true),
			})
			if err != nil {
				return fmt.Errorf("voyage provider: %w", err)
			}
			queryEmbedding, err = vp.EmbedQuery(ctx, in.Query)
			if err != nil {
				return fmt.Errorf("generate query embedding: %w", err)
			}
		} else {
			queryEmbedding, err = generateGeminiEmbedding(ctx, geminiKey, in.Query)
			if err != nil {
				return fmt.Errorf("generate query embedding: %w", err)
			}
		}
	}

	var output Output
	output.Query = in.Query

	if in.WindowGranularity {
		// Search at context window level for more granular retrieval
		windowResults, err := sessionStore.SearchContextWindows(ctx, queryEmbedding, in.Limit*2)
		if err != nil {
			return fmt.Errorf("search context windows: %w", err)
		}

		windowMatches := make([]WindowMatch, 0)
		for _, r := range windowResults {
			if r.Similarity < in.MinSimilarity {
				continue
			}

			match := WindowMatch{
				SessionID:        r.Window.SessionID,
				WindowIndex:      r.Window.WindowIndex,
				Trigger:          r.Window.Trigger,
				PreCompactTokens: r.Window.PreCompactTokens,
				Summary:          r.Window.Summary,
				MessageCount:     r.Window.MessageCount,
				Similarity:       r.Similarity,
			}

			if !r.Window.StartedAt.IsZero() {
				match.StartedAt = r.Window.StartedAt.Format(time.RFC3339)
			}
			if !r.Window.EndedAt.IsZero() {
				match.EndedAt = r.Window.EndedAt.Format(time.RFC3339)
			}

			windowMatches = append(windowMatches, match)

			if len(windowMatches) >= in.Limit {
				break
			}
		}

		output.WindowMatches = windowMatches
		output.TotalWithEmbeddings = len(windowResults)
		output.Status = "ok"
		output.Message = fmt.Sprintf("Found %d relevant context windows for query", len(windowMatches))

		if len(windowMatches) == 0 {
			output.Status = "no_matches"
			output.Message = "No context windows matched the query above the similarity threshold"
		}
	} else {
		// Search at session level (default)
		matches := make([]SessionMatch, 0)
		var totalSearched int

		if useFTSFallback {
			// Full-text search fallback when no embedding API available
			ftsResults, err := sessionStore.Search(ctx, in.Query, in.Limit*2)
			if err != nil {
				return fmt.Errorf("text search: %w", err)
			}
			totalSearched = len(ftsResults)

			for _, s := range ftsResults {
				if in.Workspace != "" && s.WorkspacePath != in.Workspace {
					continue
				}
				if in.Project != "" && s.ProjectName != in.Project {
					continue
				}

				match := SessionMatch{
					SessionID:    s.ID,
					ProjectName:  s.ProjectName,
					GitBranch:    s.GitBranch,
					Summary:      s.Summary,
					Accomplished: s.Accomplished,
					Decisions:    s.Decisions,
					Gotchas:      s.Gotchas,
					UserInsights: s.UserInsights,
					Tags:         s.Tags,
					KeyFiles:     s.KeyFiles,
					Similarity:   1.0, // FTS doesn't have similarity scores
				}

				if !s.StartedAt.IsZero() {
					match.StartedAt = s.StartedAt.Format(time.RFC3339)
				}

				matches = append(matches, match)
				if len(matches) >= in.Limit {
					break
				}
			}

			output.Status = "ok"
			output.Message = fmt.Sprintf("Found %d sessions via full-text search (no API key for semantic)", len(matches))
		} else {
			// Semantic search with embeddings
			results, err := sessionStore.SearchSimilar(ctx, queryEmbedding, in.Limit*2)
			if err != nil {
				return fmt.Errorf("search similar sessions: %w", err)
			}
			totalSearched = len(results)

			for _, r := range results {
				if r.Similarity < in.MinSimilarity {
					continue
				}

				if in.Workspace != "" && r.Session.WorkspacePath != in.Workspace {
					continue
				}

				if in.Project != "" && r.Session.ProjectName != in.Project {
					continue
				}

				match := SessionMatch{
					SessionID:    r.Session.ID,
					ProjectName:  r.Session.ProjectName,
					GitBranch:    r.Session.GitBranch,
					Summary:      r.Session.Summary,
					Accomplished: r.Session.Accomplished,
					Decisions:    r.Session.Decisions,
					Gotchas:      r.Session.Gotchas,
					UserInsights: r.Session.UserInsights,
					Tags:         r.Session.Tags,
					KeyFiles:     r.Session.KeyFiles,
					Similarity:   r.Similarity,
				}

				if !r.Session.StartedAt.IsZero() {
					match.StartedAt = r.Session.StartedAt.Format(time.RFC3339)
				}

				matches = append(matches, match)

				if len(matches) >= in.Limit {
					break
				}
			}

			output.Status = "ok"
			output.Message = fmt.Sprintf("Found %d relevant sessions for query", len(matches))
		}

		output.Matches = matches
		output.TotalWithEmbeddings = totalSearched

		if len(matches) == 0 {
			output.Status = "no_matches"
			if useFTSFallback {
				output.Message = "No sessions matched the query via full-text search"
			} else {
				output.Message = "No sessions matched the query above the similarity threshold"
			}
		}
	}

	return skillout.Emit(rc, "session/recall", output)
}

func normalizeInput(in *Input, rc *skillmain.RunContext) {
	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}
	if in.MinSimilarity <= 0 {
		in.MinSimilarity = defaultMinSim
	}
	if in.Workspace == "" {
		in.Workspace = rc.Workspace
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// generateGeminiEmbedding calls the Gemini embedding API.
func generateGeminiEmbedding(ctx context.Context, apiKey, text string) ([]float32, error) {
	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", geminiBaseURL, geminiModel, apiKey)

	reqBody := geminiEmbedRequest{
		Model: fmt.Sprintf("models/%s", geminiModel),
		Content: geminiContentPart{
			Parts: []geminiTextPart{{Text: text}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("API error %d: %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp geminiEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embedResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return embedResp.Embedding.Values, nil
}
