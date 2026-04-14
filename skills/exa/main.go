package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const (
	command = "exa/search"
	baseURL = "https://api.exa.ai"
)

// Input defines the skill parameters for the Exa API.
type Input struct {
	Action string `json:"action"`

	// Core params
	Query      string   `json:"query"`
	URL        string   `json:"url"`
	URLs       []string `json:"urls"`
	NumResults int      `json:"num_results"`

	// Search params
	Type     string `json:"type"`
	Category string `json:"category"`

	// Filters
	IncludeDomains     []string `json:"include_domains"`
	ExcludeDomains     []string `json:"exclude_domains"`
	StartPublishedDate string   `json:"start_published_date"`
	EndPublishedDate   string   `json:"end_published_date"`
	IncludeText        []string `json:"include_text"`
	ExcludeText        []string `json:"exclude_text"`

	// Content options
	Text           bool   `json:"text"`
	TextMaxChars   int    `json:"text_max_chars"`
	Highlights     bool   `json:"highlights"`
	HighlightQuery string `json:"highlight_query"`
	Summary        bool   `json:"summary"`
	SummaryQuery   string `json:"summary_query"`
}

type Result struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	ID            string   `json:"id,omitempty"`
	Score         float64  `json:"score,omitempty"`
	PublishedDate string   `json:"publishedDate,omitempty"`
	Author        string   `json:"author,omitempty"`
	Text          string   `json:"text,omitempty"`
	Highlights    []string `json:"highlights,omitempty"`
	Summary       string   `json:"summary,omitempty"`
}

type Citation struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Author        string `json:"author,omitempty"`
	PublishedDate string `json:"publishedDate,omitempty"`
}

type CostDollars struct {
	Total float64 `json:"total"`
}

type Output struct {
	Results   []Result     `json:"results,omitempty"`
	Answer    string       `json:"answer,omitempty"`
	Citations []Citation   `json:"citations,omitempty"`
	Action    string       `json:"action"`
	Query     string       `json:"query,omitempty"`
	URL       string       `json:"url,omitempty"`
	Cost      *CostDollars `json:"cost,omitempty"`
	Artifact  string       `json:"artifact,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	apiKey := rc.Config.Search.ExaAPIKey
	if apiKey == "" {
		return skillerr.Arg(
			"EXA_API_KEY not set",
			skillerr.WithHint("Set EXA_API_KEY in ~/.foxctl/.env"),
		)
	}

	if in.Action == "" {
		in.Action = "search"
	}
	if in.NumResults <= 0 {
		in.NumResults = 10
	}
	if in.NumResults > 100 {
		in.NumResults = 100
	}

	var output Output
	var err error

	switch in.Action {
	case "search":
		err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var e error
			output, e = doSearch(ctx, in, apiKey)
			return e
		})
	case "find_similar":
		err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var e error
			output, e = doFindSimilar(ctx, in, apiKey)
			return e
		})
	case "contents":
		err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var e error
			output, e = doContents(ctx, in, apiKey)
			return e
		})
	case "answer":
		err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
			var e error
			output, e = doAnswer(ctx, in, apiKey)
			return e
		})
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown action: %s", in.Action),
			skillerr.WithHint("Use: search, find_similar, contents, answer"),
		)
	}

	if err != nil {
		return err
	}

	return skillout.EmitWithCAS(ctx, rc, command, output)
}

// buildContentsRequest builds the Exa contents request object from input options.
func buildContentsRequest(in Input) map[string]any {
	contents := map[string]any{}

	if in.Text || (!in.Highlights && !in.Summary) {
		// Default to text if nothing else requested
		if in.TextMaxChars > 0 {
			contents["text"] = map[string]any{"maxCharacters": in.TextMaxChars}
		} else {
			contents["text"] = true
		}
	}

	if in.Highlights {
		h := map[string]any{}
		if in.HighlightQuery != "" {
			h["query"] = in.HighlightQuery
		}
		contents["highlights"] = h
	}

	if in.Summary {
		s := map[string]any{}
		if in.SummaryQuery != "" {
			s["query"] = in.SummaryQuery
		}
		contents["summary"] = s
	}

	return contents
}

// addCommonFilters adds shared filter params to a request body.
func addCommonFilters(body map[string]any, in Input) {
	if len(in.IncludeDomains) > 0 {
		body["includeDomains"] = in.IncludeDomains
	}
	if len(in.ExcludeDomains) > 0 {
		body["excludeDomains"] = in.ExcludeDomains
	}
	if in.StartPublishedDate != "" {
		body["startPublishedDate"] = in.StartPublishedDate
	}
	if in.EndPublishedDate != "" {
		body["endPublishedDate"] = in.EndPublishedDate
	}
	if len(in.IncludeText) > 0 {
		body["includeText"] = in.IncludeText
	}
	if len(in.ExcludeText) > 0 {
		body["excludeText"] = in.ExcludeText
	}
}

func doSearch(ctx context.Context, in Input, apiKey string) (Output, error) {
	if strings.TrimSpace(in.Query) == "" {
		return Output{}, skillerr.Arg("query is required for search action")
	}

	reqBody := map[string]any{
		"query":      in.Query,
		"numResults": in.NumResults,
		"contents":   buildContentsRequest(in),
	}

	if in.Type != "" {
		reqBody["type"] = in.Type
	}
	if in.Category != "" {
		reqBody["category"] = in.Category
	}

	addCommonFilters(reqBody, in)

	resp, err := doRequest(ctx, "/search", reqBody, apiKey)
	if err != nil {
		return Output{}, err
	}

	return Output{
		Results: resp.Results,
		Action:  "search",
		Query:   in.Query,
		Cost:    resp.CostDollars,
	}, nil
}

func doFindSimilar(ctx context.Context, in Input, apiKey string) (Output, error) {
	if strings.TrimSpace(in.URL) == "" {
		return Output{}, skillerr.Arg("url is required for find_similar action")
	}

	reqBody := map[string]any{
		"url":        in.URL,
		"numResults": in.NumResults,
		"contents":   buildContentsRequest(in),
	}

	addCommonFilters(reqBody, in)

	resp, err := doRequest(ctx, "/findSimilar", reqBody, apiKey)
	if err != nil {
		return Output{}, err
	}

	return Output{
		Results: resp.Results,
		Action:  "find_similar",
		URL:     in.URL,
		Cost:    resp.CostDollars,
	}, nil
}

func doContents(ctx context.Context, in Input, apiKey string) (Output, error) {
	if len(in.URLs) == 0 {
		return Output{}, skillerr.Arg("urls is required for contents action")
	}

	reqBody := map[string]any{
		"urls": in.URLs,
	}

	if in.Text || (!in.Highlights && !in.Summary) {
		if in.TextMaxChars > 0 {
			reqBody["text"] = map[string]any{"maxCharacters": in.TextMaxChars}
		} else {
			reqBody["text"] = true
		}
	}
	if in.Highlights {
		h := map[string]any{}
		if in.HighlightQuery != "" {
			h["query"] = in.HighlightQuery
		}
		reqBody["highlights"] = h
	}
	if in.Summary {
		s := map[string]any{}
		if in.SummaryQuery != "" {
			s["query"] = in.SummaryQuery
		}
		reqBody["summary"] = s
	}

	resp, err := doRequest(ctx, "/contents", reqBody, apiKey)
	if err != nil {
		return Output{}, err
	}

	return Output{
		Results: resp.Results,
		Action:  "contents",
		Cost:    resp.CostDollars,
	}, nil
}

func doAnswer(ctx context.Context, in Input, apiKey string) (Output, error) {
	if strings.TrimSpace(in.Query) == "" {
		return Output{}, skillerr.Arg("query is required for answer action")
	}

	reqBody := map[string]any{
		"query": in.Query,
	}

	if in.Text {
		reqBody["text"] = true
	}

	resp, err := doAnswerRequest(ctx, reqBody, apiKey)
	if err != nil {
		return Output{}, err
	}

	return resp, nil
}

// exaResponse represents the common Exa API response.
type exaResponse struct {
	RequestID   string       `json:"requestId"`
	Results     []Result     `json:"results"`
	CostDollars *CostDollars `json:"costDollars"`
}

// exaAnswerResponse represents the Exa answer API response.
type exaAnswerResponse struct {
	RequestID   string       `json:"requestId"`
	Answer      string       `json:"answer"`
	Citations   []Citation   `json:"citations"`
	Results     []Result     `json:"results"`
	CostDollars *CostDollars `json:"costDollars"`
}

func doRequest(ctx context.Context, path string, reqBody map[string]any, apiKey string) (exaResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return exaResponse{}, skillerr.WrapRuntime("marshal exa request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+path, bytes.NewReader(body))
	if err != nil {
		return exaResponse{}, skillerr.WrapRuntime("create exa request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return exaResponse{}, skillerr.WrapIO("exa request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return exaResponse{}, skillerr.Runtime(
			fmt.Sprintf("exa %s returned %d: %s", path, resp.StatusCode, string(respBody)),
		)
	}

	var exaResp exaResponse
	if err := json.NewDecoder(resp.Body).Decode(&exaResp); err != nil {
		return exaResponse{}, skillerr.WrapRuntime("decode exa response", err)
	}

	return exaResp, nil
}

func doAnswerRequest(ctx context.Context, reqBody map[string]any, apiKey string) (Output, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return Output{}, skillerr.WrapRuntime("marshal exa answer request", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/answer", bytes.NewReader(body))
	if err != nil {
		return Output{}, skillerr.WrapRuntime("create exa answer request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Output{}, skillerr.WrapIO("exa answer request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return Output{}, skillerr.Runtime(
			fmt.Sprintf("exa /answer returned %d: %s", resp.StatusCode, string(respBody)),
		)
	}

	var answerResp exaAnswerResponse
	if err := json.NewDecoder(resp.Body).Decode(&answerResp); err != nil {
		return Output{}, skillerr.WrapRuntime("decode exa answer response", err)
	}

	return Output{
		Answer:    answerResp.Answer,
		Citations: answerResp.Citations,
		Results:   answerResp.Results,
		Action:    "answer",
		Query:     fmt.Sprintf("%v", reqBody["query"]),
		Cost:      answerResp.CostDollars,
	}, nil
}
