package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"
)

// OutputFormatter wraps output and applies formatting transformations.
type OutputFormatter struct {
	format string
	jq     string
	w      io.Writer
	buf    *bytes.Buffer
}

// NewOutputFormatter creates a formatter that intercepts output for transformation.
func NewOutputFormatter(w io.Writer, format, jq string) *OutputFormatter {
	return &OutputFormatter{
		format: format,
		jq:     jq,
		w:      w,
		buf:    &bytes.Buffer{},
	}
}

// Writer returns the writer to capture output.
func (f *OutputFormatter) Writer() io.Writer {
	if f.NeedsCapture() {
		return f.buf
	}
	return f.w
}

// NeedsCapture returns true if output needs to be captured for transformation.
func (f *OutputFormatter) NeedsCapture() bool {
	return f.format != "" && f.format != "json" || f.jq != ""
}

// Flush applies formatting and writes to the underlying writer.
func (f *OutputFormatter) Flush() error {
	if !f.NeedsCapture() {
		return nil
	}

	output := f.buf.Bytes()
	if len(output) == 0 {
		return nil
	}

	// Apply jq filter first if specified
	if f.jq != "" {
		filtered, err := applyJQ(output, f.jq)
		if err != nil {
			return fmt.Errorf("jq filter failed: %w", err)
		}
		output = filtered
	}

	// Apply format transformation
	switch f.format {
	case "table":
		return f.formatTable(output)
	case "compact":
		return f.formatCompact(output)
	case "", "json":
		_, err := f.w.Write(output)
		return err
	default:
		return fmt.Errorf("unknown format: %s (valid: json, table, compact)", f.format)
	}
}

// formatTable renders JSON as a table.
func (f *OutputFormatter) formatTable(data []byte) error {
	// Try to detect the data structure and format appropriately
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		// Not valid JSON, just output as-is
		_, werr := f.w.Write(data)
		return werr
	}

	// Look for common patterns in skill output
	dataField, hasData := envelope["data"].(map[string]any)
	if !hasData {
		// No data field, pretty print the whole thing
		return f.prettyPrint(data)
	}

	// Check for tasks array (todo/manage skill)
	if tasks, ok := dataField["tasks"].([]any); ok {
		return f.formatTasksTable(tasks, dataField)
	}

	// Check for items array (generic list)
	if items, ok := dataField["items"].([]any); ok {
		return f.formatGenericTable(items)
	}

	// Fall back to pretty print
	return f.prettyPrint(data)
}

// formatTasksTable renders tasks in a table format.
func (f *OutputFormatter) formatTasksTable(tasks []any, dataField map[string]any) error {
	tw := tabwriter.NewWriter(f.w, 0, 0, 2, ' ', 0)

	// Header
	fmt.Fprintln(tw, "STATUS\tTITLE\tCREATED\tID")
	fmt.Fprintln(tw, "------\t-----\t-------\t--")

	for _, t := range tasks {
		task, ok := t.(map[string]any)
		if !ok {
			continue
		}

		status := statusIcon(getStringField(task, "status"))
		title := truncate(getStringField(task, "title"), 50)
		created := formatDate(getStringField(task, "created_at"))
		id := truncate(getStringField(task, "id"), 12)

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", status, title, created, id)
	}

	if err := tw.Flush(); err != nil {
		return err
	}

	// Summary line
	if pending, ok := dataField["pending_tasks"].(float64); ok {
		fmt.Fprintf(f.w, "\n(%d pending tasks)\n", int(pending))
	}

	return nil
}

// formatGenericTable renders a generic array as a table.
func (f *OutputFormatter) formatGenericTable(items []any) error {
	if len(items) == 0 {
		fmt.Fprintln(f.w, "(no items)")
		return nil
	}

	// Get column names from first item
	first, ok := items[0].(map[string]any)
	if !ok {
		return f.prettyPrint(mustMarshal(items))
	}

	var cols []string
	for k := range first {
		cols = append(cols, k)
	}

	tw := tabwriter.NewWriter(f.w, 0, 0, 2, ' ', 0)

	// Header
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(cols, "\t")))

	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var vals []string
		for _, col := range cols {
			vals = append(vals, truncate(fmt.Sprintf("%v", m[col]), 40))
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}

	return tw.Flush()
}

// formatCompact renders JSON in a compact one-line-per-item format.
func (f *OutputFormatter) formatCompact(data []byte) error {
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		_, werr := f.w.Write(data)
		return werr
	}

	dataField, hasData := envelope["data"].(map[string]any)
	if !hasData {
		return f.prettyPrint(data)
	}

	// Check for tasks
	if tasks, ok := dataField["tasks"].([]any); ok {
		for _, t := range tasks {
			task, ok := t.(map[string]any)
			if !ok {
				continue
			}
			status := statusIcon(getStringField(task, "status"))
			title := getStringField(task, "title")
			id := getStringField(task, "id")
			fmt.Fprintf(f.w, "%s %s [%s]\n", status, title, truncate(id, 8))
		}
		return nil
	}

	// Check for items
	if items, ok := dataField["items"].([]any); ok {
		for _, item := range items {
			line, _ := json.Marshal(item)
			fmt.Fprintf(f.w, "%s\n", line)
		}
		return nil
	}

	return f.prettyPrint(data)
}

func (f *OutputFormatter) prettyPrint(data []byte) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		_, werr := f.w.Write(data)
		return werr
	}
	enc := json.NewEncoder(f.w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// applyJQ runs the jq command on the input data.
func applyJQ(data []byte, expr string) ([]byte, error) {
	cmd := exec.Command("jq", expr)
	cmd.Stdin = bytes.NewReader(data)
	return cmd.Output()
}

// Helper functions

func getStringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func statusIcon(status string) string {
	switch status {
	case "completed":
		return "✅"
	case "in_progress":
		return "🔄"
	case "pending":
		return "⏳"
	case "blocked":
		return "🚫"
	default:
		return "  "
	}
}

func formatDate(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02")
}

func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
