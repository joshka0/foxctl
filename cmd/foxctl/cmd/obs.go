package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/spf13/cobra"
)

const symbolEmbeddingTextOperation = "symbol.embedding_text"

func init() {
	rootCmd.AddCommand(newObsCommand())
}

func newObsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obs",
		Short: "Inspect observability events and metrics",
		Long:  "Inspect foxctl observability events and derived metrics from the local foxcular event stream.",
	}
	cmd.AddCommand(newObsEventsCommand(), newObsSymbolMetricsCommand())
	return cmd
}

func newObsEventsCommand() *cobra.Command {
	var (
		limit         int
		since         string
		component     string
		operation     string
		workspaceFlag string
		obsDir        string
		errorsOnly    bool
		textQuery     string
	)

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Query observability events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}
			sinceTime, err := parseObsSince(since)
			if err != nil {
				return err
			}
			workspaceID, workspaceRoot, workspaceFilters := obsWorkspaceFilters(cfg, workspaceFlag)
			entries, err := observability.QueryEventRecords(cmd.Context(), observability.EventQueryOptions{
				ObsDir:          strings.TrimSpace(obsDir),
				Limit:           limit,
				Since:           sinceTime,
				Component:       strings.TrimSpace(component),
				OperationPrefix: strings.TrimSpace(operation),
				WorkspaceID:     workspaceID,
				WorkspaceIDs:    workspaceFilters,
				ErrorsOnly:      errorsOnly,
				TextQuery:       strings.TrimSpace(textQuery),
			})
			if err != nil {
				return err
			}
			payload := map[string]any{
				"entries": entries,
				"count":   len(entries),
				"summary": buildObsEventsSummary(entries),
				"filters": obsFiltersPayload(obsDir, component, operation, workspaceID, workspaceRoot, since, errorsOnly),
			}
			return writeOK(cmd, "foxctl.obs.events", payload, "local", profilesCoreAgent)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum entries to return")
	cmd.Flags().StringVar(&since, "since", "24h", "Only include events newer than this duration")
	cmd.Flags().StringVar(&component, "component", "", "Filter by component")
	cmd.Flags().StringVar(&operation, "operation", "", "Filter by operation prefix")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Filter by workspace path")
	cmd.Flags().StringVar(&obsDir, "obs-dir", "", "Override observability directory")
	cmd.Flags().BoolVar(&errorsOnly, "errors", false, "Only include error events")
	cmd.Flags().StringVar(&textQuery, "query", "", "Case-insensitive text search over operation, command, error, and data")
	return cmd
}

func newObsSymbolMetricsCommand() *cobra.Command {
	var (
		limit         int
		since         string
		workspaceFlag string
		obsDir        string
		top           int
		sortBy        string
		kind          string
	)

	cmd := &cobra.Command{
		Use:   "symbol-metrics",
		Short: "Summarize symbol embedding size metrics",
		Long:  "Summarize symbol.embedding_text observability events emitted by the symbol indexer.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, ok := config.FromContext(cmd.Context())
			if !ok {
				return fmt.Errorf("configuration not loaded")
			}
			sortBy = strings.TrimSpace(sortBy)
			if !validSymbolMetricSortKey(sortBy) {
				return fmt.Errorf("invalid --sort-by %q: use source_lines, source_chars, stripped_source_lines, stripped_source_chars, embedding_text_lines, or embedding_text_chars", sortBy)
			}
			sinceTime, err := parseObsSince(since)
			if err != nil {
				return err
			}
			workspaceID, workspaceRoot, workspaceFilters := obsWorkspaceFilters(cfg, workspaceFlag)
			entries, err := observability.QueryEventRecords(cmd.Context(), observability.EventQueryOptions{
				ObsDir:          strings.TrimSpace(obsDir),
				Limit:           limit,
				Since:           sinceTime,
				OperationPrefix: symbolEmbeddingTextOperation,
				WorkspaceID:     workspaceID,
				WorkspaceIDs:    workspaceFilters,
			})
			if err != nil {
				return err
			}

			records := symbolMetricRecords(entries, kind)
			largest := topSymbolMetricRecords(records, sortBy, top)
			payload := map[string]any{
				"count":         len(records),
				"event_count":   len(entries),
				"summary":       buildSymbolMetricsSummary(records),
				"largest":       largest,
				"sort_by":       sortBy,
				"operation":     symbolEmbeddingTextOperation,
				"sample_notice": "set FOXCTL_OBS_SAMPLE_RATE=1 while indexing when exact counts are needed",
				"filters":       obsFiltersPayload(obsDir, "", symbolEmbeddingTextOperation, workspaceID, workspaceRoot, since, false),
			}
			if strings.TrimSpace(kind) != "" {
				payload["kind"] = strings.TrimSpace(kind)
			}
			return writeOK(cmd, "foxctl.obs.symbol_metrics", payload, "local", profilesCoreAgent)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 50000, "Maximum symbol metric events to read")
	cmd.Flags().StringVar(&since, "since", "24h", "Only include events newer than this duration")
	cmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Filter by workspace path")
	cmd.Flags().StringVar(&obsDir, "obs-dir", "", "Override observability directory")
	cmd.Flags().IntVar(&top, "top", 50, "Number of largest symbols to include")
	cmd.Flags().StringVar(&sortBy, "sort-by", "source_lines", "Outlier metric: source_lines, source_chars, stripped_source_lines, stripped_source_chars, embedding_text_lines, embedding_text_chars")
	cmd.Flags().StringVar(&kind, "kind", "", "Optional symbol kind filter, e.g. function, method, class")
	return cmd
}

type obsEventsSummary struct {
	ByStatus    map[string]int `json:"by_status,omitempty"`
	ByComponent map[string]int `json:"by_component,omitempty"`
	ByOperation map[string]int `json:"by_operation,omitempty"`
}

func buildObsEventsSummary(entries []observability.EventRecord) obsEventsSummary {
	summary := obsEventsSummary{
		ByStatus:    map[string]int{},
		ByComponent: map[string]int{},
		ByOperation: map[string]int{},
	}
	for _, entry := range entries {
		if entry.Status != "" {
			summary.ByStatus[entry.Status]++
		}
		if entry.Component != "" {
			summary.ByComponent[entry.Component]++
		}
		if entry.Operation != "" {
			summary.ByOperation[entry.Operation]++
		}
	}
	if len(summary.ByStatus) == 0 {
		summary.ByStatus = nil
	}
	if len(summary.ByComponent) == 0 {
		summary.ByComponent = nil
	}
	if len(summary.ByOperation) == 0 {
		summary.ByOperation = nil
	}
	return summary
}

type symbolMetricRecord struct {
	Timestamp           string `json:"ts,omitempty"`
	FilePath            string `json:"file_path,omitempty"`
	SymbolID            string `json:"symbol_id,omitempty"`
	SymbolKind          string `json:"symbol_kind,omitempty"`
	SourceChars         int64  `json:"source_chars,omitempty"`
	SourceLines         int64  `json:"source_lines,omitempty"`
	StrippedSourceChars int64  `json:"stripped_source_chars,omitempty"`
	StrippedSourceLines int64  `json:"stripped_source_lines,omitempty"`
	EmbeddingTextChars  int64  `json:"embedding_text_chars,omitempty"`
	EmbeddingTextLines  int64  `json:"embedding_text_lines,omitempty"`
	FieldCount          int64  `json:"field_count,omitempty"`
	RelationshipHints   int64  `json:"relationship_hint_count,omitempty"`
	SemanticAnchors     int64  `json:"semantic_anchor_count,omitempty"`
}

type symbolMetricsSummary struct {
	Count               int                              `json:"count"`
	SourceLines         metricStats                      `json:"source_lines"`
	SourceChars         metricStats                      `json:"source_chars"`
	StrippedSourceLines intMetricReductionStats          `json:"stripped_source_lines"`
	EmbeddingTextLines  metricStats                      `json:"embedding_text_lines"`
	EmbeddingTextChars  metricStats                      `json:"embedding_text_chars"`
	ByKind              map[string]symbolMetricsKindStat `json:"by_kind,omitempty"`
}

type symbolMetricsKindStat struct {
	Count              int         `json:"count"`
	SourceLines        metricStats `json:"source_lines"`
	EmbeddingTextLines metricStats `json:"embedding_text_lines"`
}

type metricStats struct {
	Count int     `json:"count"`
	Min   int64   `json:"min"`
	P50   int64   `json:"p50"`
	P90   int64   `json:"p90"`
	P95   int64   `json:"p95"`
	P99   int64   `json:"p99"`
	Max   int64   `json:"max"`
	Avg   float64 `json:"avg"`
}

type intMetricReductionStats struct {
	metricStats
	TotalDelta int64   `json:"total_delta"`
	AvgDelta   float64 `json:"avg_delta"`
}

func symbolMetricRecords(entries []observability.EventRecord, kindFilter string) []symbolMetricRecord {
	kindFilter = strings.ToLower(strings.TrimSpace(kindFilter))
	records := make([]symbolMetricRecord, 0, len(entries))
	for _, entry := range entries {
		record, ok := symbolMetricRecordFromEvent(entry)
		if !ok {
			continue
		}
		if kindFilter != "" && strings.ToLower(record.SymbolKind) != kindFilter {
			continue
		}
		records = append(records, record)
	}
	return records
}

func symbolMetricRecordFromEvent(entry observability.EventRecord) (symbolMetricRecord, bool) {
	if entry.Operation != symbolEmbeddingTextOperation || len(entry.Data) == 0 {
		return symbolMetricRecord{}, false
	}
	record := symbolMetricRecord{
		Timestamp:           entry.Timestamp,
		FilePath:            dataString(entry.Data, "file_path"),
		SymbolID:            dataString(entry.Data, "symbol_id"),
		SymbolKind:          dataString(entry.Data, "symbol_kind"),
		SourceChars:         dataInt64(entry.Data, "source_chars"),
		SourceLines:         dataInt64(entry.Data, "source_lines"),
		StrippedSourceChars: dataInt64(entry.Data, "stripped_source_chars"),
		StrippedSourceLines: dataInt64(entry.Data, "stripped_source_lines"),
		EmbeddingTextChars:  dataInt64(entry.Data, "embedding_text_chars"),
		EmbeddingTextLines:  dataInt64(entry.Data, "embedding_text_lines"),
		FieldCount:          dataInt64(entry.Data, "field_count"),
		RelationshipHints:   dataInt64(entry.Data, "relationship_hint_count"),
		SemanticAnchors:     dataInt64(entry.Data, "semantic_anchor_count"),
	}
	return record, record.FilePath != "" || record.SymbolID != ""
}

func buildSymbolMetricsSummary(records []symbolMetricRecord) symbolMetricsSummary {
	sourceLines := make([]int64, 0, len(records))
	sourceChars := make([]int64, 0, len(records))
	strippedLines := make([]int64, 0, len(records))
	embeddingLines := make([]int64, 0, len(records))
	embeddingChars := make([]int64, 0, len(records))
	byKindRecords := map[string][]symbolMetricRecord{}
	for _, record := range records {
		sourceLines = append(sourceLines, record.SourceLines)
		sourceChars = append(sourceChars, record.SourceChars)
		strippedLines = append(strippedLines, record.StrippedSourceLines)
		embeddingLines = append(embeddingLines, record.EmbeddingTextLines)
		embeddingChars = append(embeddingChars, record.EmbeddingTextChars)
		kind := strings.TrimSpace(record.SymbolKind)
		if kind == "" {
			kind = "unknown"
		}
		byKindRecords[kind] = append(byKindRecords[kind], record)
	}
	byKind := make(map[string]symbolMetricsKindStat, len(byKindRecords))
	for kind, items := range byKindRecords {
		kindSourceLines := make([]int64, 0, len(items))
		kindEmbeddingLines := make([]int64, 0, len(items))
		for _, item := range items {
			kindSourceLines = append(kindSourceLines, item.SourceLines)
			kindEmbeddingLines = append(kindEmbeddingLines, item.EmbeddingTextLines)
		}
		byKind[kind] = symbolMetricsKindStat{
			Count:              len(items),
			SourceLines:        buildMetricStats(kindSourceLines),
			EmbeddingTextLines: buildMetricStats(kindEmbeddingLines),
		}
	}
	if len(byKind) == 0 {
		byKind = nil
	}
	return symbolMetricsSummary{
		Count:               len(records),
		SourceLines:         buildMetricStats(sourceLines),
		SourceChars:         buildMetricStats(sourceChars),
		StrippedSourceLines: buildReductionStats(sourceLines, strippedLines),
		EmbeddingTextLines:  buildMetricStats(embeddingLines),
		EmbeddingTextChars:  buildMetricStats(embeddingChars),
		ByKind:              byKind,
	}
}

func buildMetricStats(values []int64) metricStats {
	if len(values) == 0 {
		return metricStats{}
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var total int64
	for _, value := range sorted {
		total += value
	}
	return metricStats{
		Count: len(sorted),
		Min:   sorted[0],
		P50:   percentile(sorted, 0.50),
		P90:   percentile(sorted, 0.90),
		P95:   percentile(sorted, 0.95),
		P99:   percentile(sorted, 0.99),
		Max:   sorted[len(sorted)-1],
		Avg:   float64(total) / float64(len(sorted)),
	}
}

func buildReductionStats(before, after []int64) intMetricReductionStats {
	stats := intMetricReductionStats{metricStats: buildMetricStats(after)}
	if len(before) == 0 || len(after) == 0 {
		return stats
	}
	n := len(before)
	if len(after) < n {
		n = len(after)
	}
	var total int64
	for i := 0; i < n; i++ {
		total += before[i] - after[i]
	}
	stats.TotalDelta = total
	stats.AvgDelta = float64(total) / float64(n)
	return stats
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func topSymbolMetricRecords(records []symbolMetricRecord, sortBy string, top int) []symbolMetricRecord {
	if top <= 0 || len(records) == 0 {
		return nil
	}
	out := append([]symbolMetricRecord(nil), records...)
	sort.SliceStable(out, func(i, j int) bool {
		left := symbolMetricSortValue(out[i], sortBy)
		right := symbolMetricSortValue(out[j], sortBy)
		if left == right {
			return out[i].SymbolID < out[j].SymbolID
		}
		return left > right
	})
	if len(out) > top {
		out = out[:top]
	}
	return out
}

func symbolMetricSortValue(record symbolMetricRecord, sortBy string) int64 {
	switch sortBy {
	case "source_chars":
		return record.SourceChars
	case "stripped_source_lines":
		return record.StrippedSourceLines
	case "stripped_source_chars":
		return record.StrippedSourceChars
	case "embedding_text_lines":
		return record.EmbeddingTextLines
	case "embedding_text_chars":
		return record.EmbeddingTextChars
	default:
		return record.SourceLines
	}
}

func validSymbolMetricSortKey(sortBy string) bool {
	switch sortBy {
	case "source_lines", "source_chars", "stripped_source_lines", "stripped_source_chars", "embedding_text_lines", "embedding_text_chars":
		return true
	default:
		return false
	}
}

func parseObsSince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse --since: %w", err)
	}
	return time.Now().Add(-d), nil
}

func obsWorkspaceFilters(cfg config.Config, workspaceFlag string) (workspaceID, workspaceRoot string, workspaceFilters []string) {
	if strings.TrimSpace(workspaceFlag) == "" {
		return "", "", nil
	}
	workspaceRoot = resolveWorkspace(cfg, workspaceFlag)
	if workspaceRoot != "" {
		if absRoot, absErr := filepath.Abs(workspaceRoot); absErr == nil {
			workspaceRoot = absRoot
		}
	}
	workspaceID = resolveWorkspaceID(cfg, workspaceFlag)
	workspaceFilters = appendWorkspaceFilters(workspaceFilters, workspaceID, workspaceRoot)
	return workspaceID, workspaceRoot, workspaceFilters
}

func obsFiltersPayload(obsDir, component, operation, workspaceID, workspaceRoot, since string, errorsOnly bool) map[string]any {
	return map[string]any{
		"errors_only":    errorsOnly,
		"component":      strings.TrimSpace(component),
		"operation":      strings.TrimSpace(operation),
		"workspace":      workspaceID,
		"workspace_root": workspaceRoot,
		"since":          strings.TrimSpace(since),
		"obs_dir":        firstNonEmptyValue(strings.TrimSpace(obsDir), observability.ResolveObsDir()),
	}
}

func dataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	switch value := data[key].(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func dataInt64(data map[string]any, key string) int64 {
	if data == nil {
		return 0
	}
	switch value := data[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	case float32:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}
