package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
)

const (
	command         = "arxiv/summarize"
	defaultModel    = "google/gemini-3.1-flash-lite-preview"
	defaultEndpoint = "https://openrouter.ai/api/v1/chat/completions"
	defaultMaxPDF   = 80 << 20
)

var arxivIDPattern = regexp.MustCompile(`^([a-z-]+(\.[A-Z]{2})?/\d{7}|\d{4}\.\d{4,5})(v\d+)?$`)

type input struct {
	Paper       string  `json:"paper" validate:"required"`
	Mode        string  `json:"mode" validate:"omitempty,oneof=outline implementation query"`
	Query       string  `json:"query"`
	Force       bool    `json:"force"`
	Model       string  `json:"model"`
	Endpoint    string  `json:"endpoint"`
	APIKey      string  `json:"api_key"`
	Engine      string  `json:"engine" validate:"omitempty,oneof=native mistral-ocr pdf-text"`
	Prompt      string  `json:"prompt"`
	TimeoutSec  int     `json:"timeout_sec" validate:"gte=0,lte=600"`
	MaxTokens   int     `json:"max_tokens" validate:"gte=0,lte=32000"`
	Temperature float64 `json:"temperature" validate:"gte=0,lte=2"`
	MaxPDFBytes int64   `json:"max_pdf_bytes" validate:"gte=0"`
}

type pdfDocument struct {
	Data     []byte
	Filename string
	Source   string
}

type cacheRecord struct {
	ArtifactDigest string
	ArtifactBytes  int64
	ResponseModel  string
	Usage          map[string]any
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Plugins     []plugin      `json:"plugins,omitempty"`
	Stream      bool          `json:"stream"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type string       `json:"type"`
	Text string       `json:"text,omitempty"`
	File *fileContent `json:"file,omitempty"`
}

type fileContent struct {
	Filename string `json:"filename"`
	FileData string `json:"file_data"`
}

type plugin struct {
	ID  string    `json:"id"`
	PDF pluginPDF `json:"pdf"`
}

type pluginPDF struct {
	Engine string `json:"engine"`
}

type chatResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []chatChoice   `json:"choices"`
	Usage   map[string]any `json:"usage,omitempty"`
	Error   *providerError `json:"error,omitempty"`
}

type chatChoice struct {
	Message responseMessage `json:"message"`
}

type responseMessage struct {
	Content any `json:"content"`
}

type providerError struct {
	Message string `json:"message"`
	Code    any    `json:"code,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	applyDefaults(rc, &in)

	apiKey := strings.TrimSpace(in.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(rc.Config.LLM.ResolveAPIKey("openrouter"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if apiKey == "" {
		return skillerr.Auth("OPENROUTER_API_KEY is required")
	}

	doc, err := loadPDF(ctx, rc, in)
	if err != nil {
		return err
	}
	pdfSHA := sha256Hex(doc.Data)
	pdfArtifact, err := skillmain.PersistBuffer(ctx, rc, bytes.NewBuffer(doc.Data), "application/pdf", "arxiv_pdf")
	if err != nil {
		return skillerr.WrapRuntime("persist PDF", err)
	}
	cache, err := openCache(ctx, rc)
	if err != nil {
		return err
	}
	defer func() {
		errs.Ignore(cache.Close(), "close arxiv cache")
	}()

	promptHash := sha256Hex([]byte(in.Prompt))
	cacheKey := resultCacheKey(pdfSHA, in.Mode, in.Query, promptHash, in.Model, in.Engine)
	if !in.Force {
		if record, ok, err := lookupCachedResult(ctx, cache, cacheKey); err != nil {
			return err
		} else if ok {
			outline, err := readCachedArtifact(ctx, rc, record.ArtifactDigest)
			if err != nil {
				rc.Logger.Warn().Err(err).Str("digest", record.ArtifactDigest).Msg("cached arxiv result artifact unavailable")
			} else {
				if err := touchCachedResult(ctx, cache, cacheKey); err != nil {
					return err
				}
				return skillout.Emit(rc, command, map[string]any{
					"outline":         outline,
					"result":          outline,
					"cached":          true,
					"cache_key":       cacheKey,
					"artifact_digest": record.ArtifactDigest,
					"artifact_bytes":  record.ArtifactBytes,
					"pdf_sha256":      pdfSHA,
					"pdf_digest":      pdfArtifact.Digest,
					"mode":            in.Mode,
					"query":           in.Query,
					"source":          doc.Source,
					"filename":        doc.Filename,
					"pdf_bytes":       len(doc.Data),
					"model":           in.Model,
					"response_model":  record.ResponseModel,
					"engine":          in.Engine,
					"usage":           record.Usage,
				})
			}
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSec)*time.Second)
	defer cancel()

	response, err := callOpenRouter(callCtx, in, apiKey, doc)
	if err != nil {
		return err
	}
	outline := strings.TrimSpace(extractContent(response))
	if outline == "" {
		return skillerr.Parse("OpenRouter response did not include text content")
	}
	resultArtifact, err := skillmain.PersistBuffer(ctx, rc, bytes.NewBufferString(outline), "text/markdown", "arxiv_summary")
	if err != nil {
		return skillerr.WrapRuntime("persist summary", err)
	}
	if err := upsertCache(ctx, cache, doc, pdfSHA, pdfArtifact, in, cacheKey, promptHash, response, resultArtifact); err != nil {
		return err
	}

	return skillout.Emit(rc, command, map[string]any{
		"outline":         outline,
		"result":          outline,
		"cached":          false,
		"cache_key":       cacheKey,
		"artifact_digest": resultArtifact.Digest,
		"artifact_bytes":  resultArtifact.Size,
		"pdf_sha256":      pdfSHA,
		"pdf_digest":      pdfArtifact.Digest,
		"mode":            in.Mode,
		"query":           in.Query,
		"source":          doc.Source,
		"filename":        doc.Filename,
		"pdf_bytes":       len(doc.Data),
		"model":           in.Model,
		"response_model":  response.Model,
		"engine":          in.Engine,
		"usage":           response.Usage,
	})
}

func applyDefaults(rc *skillmain.RunContext, in *input) {
	if strings.TrimSpace(in.Model) == "" {
		in.Model = defaultModel
	}
	if strings.TrimSpace(in.Endpoint) == "" {
		if base := strings.TrimSpace(rc.Config.LLM.ResolveBaseURL("openrouter")); base != "" {
			in.Endpoint = strings.TrimRight(base, "/") + "/chat/completions"
		} else {
			in.Endpoint = defaultEndpoint
		}
	}
	if strings.TrimSpace(in.Engine) == "" {
		in.Engine = "native"
	}
	if strings.TrimSpace(in.Mode) == "" {
		if strings.TrimSpace(in.Query) != "" {
			in.Mode = "query"
		} else {
			in.Mode = "outline"
		}
	}
	if strings.TrimSpace(in.Prompt) == "" {
		in.Prompt = buildPrompt(*in)
	}
	if in.TimeoutSec <= 0 {
		in.TimeoutSec = 180
	}
	if in.MaxTokens <= 0 {
		in.MaxTokens = 12000
	}
	if in.Temperature == 0 {
		in.Temperature = 0.1
	}
	if in.MaxPDFBytes <= 0 {
		in.MaxPDFBytes = defaultMaxPDF
	}
}

func loadPDF(ctx context.Context, rc *skillmain.RunContext, in input) (pdfDocument, error) {
	if localPath, ok := existingLocalPath(in.Paper); ok {
		validPath, err := skillmain.ValidatePath(rc, localPath)
		if err != nil {
			return pdfDocument{}, err
		}
		data, err := readLimitedFile(validPath, in.MaxPDFBytes)
		if err != nil {
			return pdfDocument{}, skillerr.WrapIO("read PDF", err)
		}
		if err := validatePDF(data, "application/pdf", validPath); err != nil {
			return pdfDocument{}, err
		}
		return pdfDocument{Data: data, Filename: filepath.Base(validPath), Source: validPath}, nil
	}

	pdfURL, err := resolvePDFURL(in.Paper)
	if err != nil {
		return pdfDocument{}, skillerr.Arg(err.Error())
	}
	data, contentType, err := downloadPDF(ctx, pdfURL, in.MaxPDFBytes)
	if err != nil {
		return pdfDocument{}, skillerr.WrapIO("download PDF", err)
	}
	if err := validatePDF(data, contentType, pdfURL); err != nil {
		return pdfDocument{}, err
	}
	return pdfDocument{Data: data, Filename: filenameFromURL(pdfURL, contentType), Source: pdfURL}, nil
}

func existingLocalPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if _, err := os.Stat(value); err == nil {
		return value, true
	}
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			expanded := filepath.Join(home, strings.TrimPrefix(value, "~/"))
			if _, err := os.Stat(expanded); err == nil {
				return expanded, true
			}
		}
	}
	return "", false
}

func readLimitedFile(filename string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if n > maxBytes {
		return nil, fmt.Errorf("PDF exceeds max_pdf_bytes (%d)", maxBytes)
	}
	return buf.Bytes(), nil
}

func resolvePDFURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("unsupported URL scheme %q", parsed.Scheme)
		}
		if strings.EqualFold(parsed.Hostname(), "arxiv.org") && strings.HasPrefix(parsed.Path, "/abs/") {
			paperID := strings.Trim(strings.TrimPrefix(parsed.Path, "/abs/"), "/")
			if paperID == "" {
				return "", errors.New("arXiv URL is missing a paper ID")
			}
			return "https://arxiv.org/pdf/" + paperID, nil
		}
		return value, nil
	}

	paperID := strings.TrimPrefix(value, "arXiv:")
	paperID = strings.TrimPrefix(paperID, "arxiv:")
	if !arxivIDPattern.MatchString(paperID) {
		return "", fmt.Errorf("not an arXiv ID, arXiv URL, PDF URL, or local PDF path: %s", value)
	}
	return "https://arxiv.org/pdf/" + paperID, nil
}

func downloadPDF(ctx context.Context, pdfURL string, maxBytes int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pdfURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "foxctl-arxiv-summarize/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, pdfURL)
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if n > maxBytes {
		return nil, "", fmt.Errorf("PDF exceeds max_pdf_bytes (%d)", maxBytes)
	}
	return buf.Bytes(), resp.Header.Get("Content-Type"), nil
}

func validatePDF(data []byte, contentType, source string) error {
	if bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF")) {
		return nil
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "application/pdf") {
		return nil
	}
	return skillerr.Validation(fmt.Sprintf("downloaded content does not look like a PDF: %s", source))
}

func callOpenRouter(ctx context.Context, in input, apiKey string, doc pdfDocument) (chatResponse, error) {
	payload := chatRequest{
		Model: in.Model,
		Messages: []chatMessage{{
			Role: "user",
			Content: []contentPart{
				{Type: "text", Text: in.Prompt},
				{
					Type: "file",
					File: &fileContent{
						Filename: doc.Filename,
						FileData: "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(doc.Data),
					},
				},
			},
		}},
		Plugins: []plugin{{
			ID:  "file-parser",
			PDF: pluginPDF{Engine: in.Engine},
		}},
		Stream:      false,
		MaxTokens:   in.MaxTokens,
		Temperature: in.Temperature,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, skillerr.WrapRuntime("marshal request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, in.Endpoint, bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, skillerr.WrapRuntime("build request", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/joshka0/foxctl")
	req.Header.Set("X-Title", "foxctl arXiv summarizer")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return chatResponse{}, skillerr.WrapIO("OpenRouter request", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return chatResponse{}, skillerr.WrapIO("read OpenRouter response", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chatResponse{}, skillerr.Runtime(fmt.Sprintf("OpenRouter request failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw))))
	}

	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return chatResponse{}, skillerr.WrapParse("decode OpenRouter response", err)
	}
	if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
		return chatResponse{}, skillerr.Runtime("OpenRouter error: " + decoded.Error.Message)
	}
	return decoded, nil
}

func extractContent(response chatResponse) string {
	if len(response.Choices) == 0 {
		return ""
	}
	content := response.Choices[0].Message.Content
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, item := range value {
			object, ok := item.(map[string]any)
			if !ok || object["type"] != "text" {
				continue
			}
			if text, ok := object["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func filenameFromURL(rawURL, contentType string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "paper.pdf"
	}
	name := path.Base(parsed.Path)
	if name == "." || name == "/" || name == "" {
		name = "paper"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

func buildPrompt(in input) string {
	switch in.Mode {
	case "implementation":
		return implementationPrompt()
	case "query":
		return queryPrompt(in.Query)
	default:
		return outlinePrompt()
	}
}

func outlinePrompt() string {
	return `Create a full outline of this arXiv paper.

Interpret the entire PDF, including the abstract, main text, appendices, figures,
tables, diagrams, screenshots, plots, algorithms, equations, captions, and any
other visual material.

Leave citations out:
- Do not include a references or bibliography section.
- Do not list cited works.
- Remove inline citation markers such as [1], [12, 13], or author-year callouts
  unless needed to identify this paper itself.

Use this structure:
1. Title and thesis
2. Problem and motivation
3. Key contributions
4. Method or system design
5. Figures, tables, and visual evidence
6. Experiments or evaluation
7. Results and interpretation
8. Limitations and assumptions
9. Practical implications
10. Reproducibility notes
11. Open questions`
}

func implementationPrompt() string {
	return `Summarize this arXiv paper for the purpose of implementing the method in code.

Interpret the entire PDF, including algorithms, equations, pseudocode, figures,
tables, diagrams, plots, captions, appendices, and experiment setup details.

Leave citations out:
- Do not include a references or bibliography section.
- Do not list cited works.
- Remove inline citation markers such as [1], [12, 13], or author-year callouts
  unless needed to identify this paper itself.

Focus only on implementation-relevant details. Omit broad literature review,
historical framing, and citation context unless it changes what must be built.

Use this structure:
1. Implementation goal
2. Core abstractions and data structures
3. Inputs, outputs, and expected formats
4. Algorithmic steps
5. Model calls, prompts, policies, or learned components
6. Equations translated into implementation notes
7. Visual artifacts interpreted for implementation
8. Hyperparameters, constants, thresholds, and defaults
9. Evaluation harness and metrics to reproduce
10. Minimal viable implementation plan
11. Edge cases and failure modes
12. Tests or fixtures to write
13. Open implementation questions`
}

func queryPrompt(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		query = "Answer the user's question about the paper with the most relevant details."
	}
	return fmt.Sprintf(`Answer this targeted question about the arXiv paper:

%s

Use the entire PDF as context, including figures, tables, diagrams, plots,
algorithms, equations, captions, and appendices. If visual material is relevant
to the answer, interpret it directly.

Leave citations out:
- Do not include a references or bibliography section.
- Do not list cited works.
- Remove inline citation markers such as [1], [12, 13], or author-year callouts
  unless needed to identify this paper itself.

Answer structure:
1. Direct answer
2. Paper evidence
3. Implementation implications, if any
4. Caveats or uncertainty
5. Follow-up questions worth asking`, query)
}

func openCache(ctx context.Context, rc *skillmain.RunContext) (*sql.DB, error) {
	root := strings.TrimSpace(rc.Config.Storage.Root)
	if root == "" {
		root = strings.TrimSpace(rc.Config.Paths.Cache)
	}
	if root == "" {
		return nil, skillerr.Runtime("storage root is not configured")
	}
	db, err := sqliteutil.OpenDB(ctx, filepath.Join(root, "arxiv_summarize.sqlite"), migrateCache)
	if err != nil {
		return nil, skillerr.WrapIO("open arxiv cache", err)
	}
	return db, nil
}

func migrateCache(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS arxiv_papers (
			pdf_sha256 TEXT PRIMARY KEY,
			paper_input TEXT NOT NULL,
			source_url TEXT NOT NULL,
			filename TEXT NOT NULL,
			pdf_digest TEXT NOT NULL,
			pdf_bytes INTEGER NOT NULL,
			first_seen TEXT NOT NULL,
			last_fetched TEXT NOT NULL,
			last_summarized TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS arxiv_results (
			cache_key TEXT PRIMARY KEY,
			pdf_sha256 TEXT NOT NULL,
			mode TEXT NOT NULL,
			query TEXT NOT NULL,
			query_hash TEXT NOT NULL,
			prompt_hash TEXT NOT NULL,
			model TEXT NOT NULL,
			engine TEXT NOT NULL,
			response_model TEXT NOT NULL,
			artifact_digest TEXT NOT NULL,
			artifact_bytes INTEGER NOT NULL,
			usage_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_used TEXT NOT NULL,
			hit_count INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (pdf_sha256) REFERENCES arxiv_papers(pdf_sha256)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_arxiv_results_pdf_sha256 ON arxiv_results(pdf_sha256)`,
		`CREATE INDEX IF NOT EXISTS idx_arxiv_results_updated_at ON arxiv_results(updated_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate arxiv cache: %w", err)
		}
	}
	return nil
}

func lookupCachedResult(ctx context.Context, db *sql.DB, cacheKey string) (cacheRecord, bool, error) {
	var record cacheRecord
	var usageJSON string
	err := db.QueryRowContext(ctx, `
		SELECT artifact_digest, artifact_bytes, response_model, usage_json
		FROM arxiv_results
		WHERE cache_key = ?
	`, cacheKey).Scan(&record.ArtifactDigest, &record.ArtifactBytes, &record.ResponseModel, &usageJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return cacheRecord{}, false, nil
	}
	if err != nil {
		return cacheRecord{}, false, skillerr.WrapIO("lookup arxiv cache", err)
	}
	if strings.TrimSpace(usageJSON) != "" {
		if err := json.Unmarshal([]byte(usageJSON), &record.Usage); err != nil {
			return cacheRecord{}, false, skillerr.WrapParse("decode cached usage", err)
		}
	}
	return record, true, nil
}

func readCachedArtifact(ctx context.Context, rc *skillmain.RunContext, digest string) (string, error) {
	reader, _, err := rc.CASStore.Get(ctx, digest)
	if err != nil {
		return "", err
	}
	defer func() {
		errs.Ignore(reader.Close(), "close cached arxiv artifact")
	}()
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func touchCachedResult(ctx context.Context, db *sql.DB, cacheKey string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `
		UPDATE arxiv_results
		SET last_used = ?, hit_count = hit_count + 1
		WHERE cache_key = ?
	`, now, cacheKey); err != nil {
		return skillerr.WrapIO("touch arxiv cache", err)
	}
	return nil
}

func upsertCache(
	ctx context.Context,
	db *sql.DB,
	doc pdfDocument,
	pdfSHA string,
	pdfArtifact skillmain.Artifact,
	in input,
	cacheKey string,
	promptHash string,
	response chatResponse,
	resultArtifact skillmain.Artifact,
) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	usageJSON, err := json.Marshal(response.Usage)
	if err != nil {
		return skillerr.WrapRuntime("marshal usage", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return skillerr.WrapIO("begin arxiv cache tx", err)
	}
	defer func() {
		if err != nil {
			errs.Ignore(tx.Rollback(), "rollback arxiv cache tx")
		}
	}()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO arxiv_papers (
			pdf_sha256, paper_input, source_url, filename, pdf_digest, pdf_bytes,
			first_seen, last_fetched, last_summarized
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(pdf_sha256) DO UPDATE SET
			paper_input = excluded.paper_input,
			source_url = excluded.source_url,
			filename = excluded.filename,
			pdf_digest = excluded.pdf_digest,
			pdf_bytes = excluded.pdf_bytes,
			last_fetched = excluded.last_fetched,
			last_summarized = excluded.last_summarized
	`, pdfSHA, in.Paper, doc.Source, doc.Filename, pdfArtifact.Digest, len(doc.Data), now, now, now); err != nil {
		return skillerr.WrapIO("upsert arxiv paper", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO arxiv_results (
			cache_key, pdf_sha256, mode, query, query_hash, prompt_hash, model, engine,
			response_model, artifact_digest, artifact_bytes, usage_json,
			created_at, updated_at, last_used, hit_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(cache_key) DO UPDATE SET
			response_model = excluded.response_model,
			artifact_digest = excluded.artifact_digest,
			artifact_bytes = excluded.artifact_bytes,
			usage_json = excluded.usage_json,
			updated_at = excluded.updated_at,
			last_used = excluded.last_used
	`, cacheKey, pdfSHA, in.Mode, in.Query, sha256Hex([]byte(normalizeQuery(in.Query))), promptHash, in.Model, in.Engine,
		response.Model, resultArtifact.Digest, resultArtifact.Size, string(usageJSON), now, now, now); err != nil {
		return skillerr.WrapIO("upsert arxiv result", err)
	}
	if err = tx.Commit(); err != nil {
		return skillerr.WrapIO("commit arxiv cache tx", err)
	}
	return nil
}

func resultCacheKey(pdfSHA, mode, query, promptHash, model, engine string) string {
	parts := []string{
		pdfSHA,
		strings.TrimSpace(mode),
		normalizeQuery(query),
		strings.TrimSpace(promptHash),
		strings.TrimSpace(model),
		strings.TrimSpace(engine),
	}
	return sha256Hex([]byte(strings.Join(parts, "\x00")))
}

func normalizeQuery(query string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(query)), " ")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
