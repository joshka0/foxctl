// Package main implements the html/edit skill.
// It provides precise DOM-aware HTML editing using CSS selectors.
package main

import (
	"context"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/diffutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/htmledit"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "html/edit"

// input defines the skill input parameters for DOM-aware HTML editing with CSS selectors.
type input struct {
	Path         string      `json:"path"`
	Operations   []operation `json:"operations"`
	DryRun       bool        `json:"dry_run"`
	FormatOutput *bool       `json:"format_output"` // nil = preserve original, true = pretty print, false = minify
	ContextLines int         `json:"context_lines"`
}

// operation represents an HTML editing operation using CSS selectors and actions.
type operation = htmledit.Operation

// main is the skill entry point for html/edit.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates DOM-aware HTML editing using CSS selectors with diff generation and dry-run support.
//
// Index:
// - Purpose: Edit HTML files using CSS selectors with precise DOM manipulation and diff generation
// - Flow: validate input → read file → parse HTML → apply operations → render modified HTML → generate diff → write file
// - SideEffects: reads and writes HTML files; modifies DOM structure; generates unified diffs; preserves formatting
// - FailureModes: file access errors, HTML parsing errors, invalid CSS selectors, write permission errors
// - Observability: emits operation results, element counts, diff output, and editing statistics
// - Related: htmledit.ApplyOperations, htmledit.RenderDocument, diffutil.UnifiedDiff
// - Keywords: html/edit, dom_manipulation, css_selectors, html_parsing, diff_generation, dry_run
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate
	if strings.TrimSpace(in.Path) == "" {
		return skillerr.Arg(
			"path is required",
			skillerr.WithHint("Provide a path to the HTML file to edit."),
		)
	}
	if len(in.Operations) == 0 {
		return skillerr.Arg(
			"at least one operation is required",
			skillerr.WithHint("Provide at least one edit operation."),
		)
	}
	// Apply defaults
	if in.ContextLines <= 0 {
		in.ContextLines = 3
	}

	// Validate and resolve path
	absPath, err := skillmain.ValidatePath(rc, in.Path)
	if err != nil {
		return err
	}

	// Read original file
	originalBytes, err := os.ReadFile(absPath)
	if err != nil {
		return skillerr.WrapIO("read file", err)
	}
	original := string(originalBytes)

	// Parse HTML into a document
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(original))
	if err != nil {
		return skillerr.WrapParse("parse HTML", err)
	}

	// Track results
	results, totalAffected, opsApplied, allReadOnly := htmledit.ApplyOperations(doc, in.Operations)

	var modified string
	var diff string
	edited := false

	// Only render and diff if there are modifying operations
	if !allReadOnly {
		// Determine formatting mode
		formatOutput := false // Default: preserve original structure
		if in.FormatOutput != nil {
			formatOutput = *in.FormatOutput
		}

		// Render modified HTML
		var err error
		modified, err = htmledit.RenderDocument(doc, formatOutput)
		if err != nil {
			return skillerr.WrapRuntime("render HTML", err)
		}

		// Generate unified diff
		diff, err = diffutil.UnifiedDiff(absPath, original, modified, in.ContextLines)
		if err != nil {
			return skillerr.WrapRuntime("generate diff", err)
		}

		// Write file unless dry_run
		if !in.DryRun && original != modified {
			if err := os.WriteFile(absPath, []byte(modified), 0o644); err != nil {
				return skillerr.WrapIO("write file", err)
			}
			edited = true
		}
	}

	// Prepare response
	relPath := pathutil.RelTo(rc.PathValidator.Workspace(), absPath)

	data := map[string]any{
		"path":               relPath,
		"edited":             edited,
		"operations_applied": opsApplied,
		"elements_affected":  totalAffected,
		"dry_run":            in.DryRun,
		"results":            results,
	}

	if diff != "" {
		data["diff"] = diff
	}

	if diff == "" && totalAffected == 0 {
		data["message"] = "no changes made"
	}

	return skillout.Emit(rc, command, data)
}
