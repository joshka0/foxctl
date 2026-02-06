package plans

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantTitle    string
		wantSections int
		checkSection func(t *testing.T, plan *PlanInfo)
	}{
		{
			name: "basic plan with title and sections",
			content: `# My Feature Plan

## Problem

The system has a bug.

## Solution

Fix the bug by updating the code.

## Implementation Plan

### Step 1: Analyze the issue

Look at the logs.

### Step 2: Fix the code

Update the handler.
`,
			wantTitle:    "My Feature Plan",
			wantSections: 3,
			checkSection: func(t *testing.T, plan *PlanInfo) {
				if plan.Sections[0].Title != "Problem" {
					t.Errorf("first section title = %q, want %q", plan.Sections[0].Title, "Problem")
				}
				if plan.Sections[2].Title != "Implementation Plan" {
					t.Errorf("third section title = %q, want %q", plan.Sections[2].Title, "Implementation Plan")
				}
				if len(plan.Sections[2].Children) != 2 {
					t.Errorf("implementation section children = %d, want 2", len(plan.Sections[2].Children))
				}
			},
		},
		{
			name: "plan without title heading",
			content: `## Overview

This is the overview.

## Details

More details here.
`,
			wantTitle:    "",
			wantSections: 2,
		},
		{
			name: "deeply nested sections",
			content: `# Deep Plan

## Phase 1

### Step 1.1

#### Substep 1.1.1

Details.

#### Substep 1.1.2

More details.

### Step 1.2

Another step.
`,
			wantTitle:    "Deep Plan",
			wantSections: 1,
			checkSection: func(t *testing.T, plan *PlanInfo) {
				phase := plan.Sections[0]
				if len(phase.Children) != 2 {
					t.Errorf("phase children = %d, want 2", len(phase.Children))
				}
				if len(phase.Children[0].Children) != 2 {
					t.Errorf("step 1.1 children = %d, want 2", len(phase.Children[0].Children))
				}
			},
		},
		{
			name: "plan with code blocks",
			content: `# Code Plan

## Implementation

Add this code:

` + "```go" + `
func Hello() {
    fmt.Println("Hello")
}
` + "```" + `

## Testing

Run the tests.
`,
			wantTitle:    "Code Plan",
			wantSections: 2,
			checkSection: func(t *testing.T, plan *PlanInfo) {
				// Code block should be part of section content
				if !strings.Contains(plan.Sections[0].Content, "func Hello()") {
					t.Error("code block not captured in section content")
				}
			},
		},
		{
			name:         "empty content",
			content:      "",
			wantTitle:    "",
			wantSections: 0,
		},
		{
			name: "agentctl viewer enhancement plan format",
			content: `# agentctl Viewer Enhancement Plan

## Overview

Comprehensive improvements to the viewer.

## Current State Analysis

### What Viewer Currently Exposes

| View | Data Source |
|------|-------------|
| Jobs | Filesystem |

### Gap Analysis

Missing features.

## Implementation Plan

### Phase 1: Core UX Improvements

#### 1.1 Unified Selection Model

Add cursor to all views.

#### 1.2 Actor Picker

Replace hardcoded actor.

### Phase 2: Action Support

#### 2.1 Job Actions

Add cancel and re-run.
`,
			wantTitle:    "agentctl Viewer Enhancement Plan",
			wantSections: 3,
			checkSection: func(t *testing.T, plan *PlanInfo) {
				// Check nested structure
				implPlan := plan.Sections[2]
				if implPlan.Title != "Implementation Plan" {
					t.Errorf("expected Implementation Plan section, got %q", implPlan.Title)
				}
				if len(implPlan.Children) != 2 {
					t.Errorf("impl plan phases = %d, want 2", len(implPlan.Children))
				}
				// Phase 1 should have 2 children (1.1 and 1.2)
				phase1 := implPlan.Children[0]
				if len(phase1.Children) != 2 {
					t.Errorf("phase 1 children = %d, want 2", len(phase1.Children))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(DefaultParseOptions())
			plan, err := parser.Parse(tt.content)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if plan.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", plan.Title, tt.wantTitle)
			}

			if len(plan.Sections) != tt.wantSections {
				t.Errorf("Sections = %d, want %d", len(plan.Sections), tt.wantSections)
			}

			if tt.checkSection != nil {
				tt.checkSection(t, plan)
			}
		})
	}
}

func TestParser_ExtractSteps(t *testing.T) {
	content := `# Implementation Plan

## Phase 1: Setup

### Step 1: Create directory

Make the directory structure.

### Step 2: Initialize project

Run init command.

## Phase 2: Implementation

### Step 3: Add feature

Implement the feature.

### Step 4: Add tests

Write tests.
`

	parser := NewParser(DefaultParseOptions())
	plan, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	steps := parser.ExtractSteps(plan)
	if len(steps) != 4 {
		t.Errorf("ExtractSteps() = %d steps, want 4", len(steps))
	}

	// Check step titles are cleaned
	expectedTitles := []string{
		"Create directory",
		"Initialize project",
		"Add feature",
		"Add tests",
	}
	for i, step := range steps {
		if step.Title != expectedTitles[i] {
			t.Errorf("step[%d].Title = %q, want %q", i, step.Title, expectedTitles[i])
		}
		if step.Order != i+1 {
			t.Errorf("step[%d].Order = %d, want %d", i, step.Order, i+1)
		}
	}

	// Check dependencies (steps in same phase should depend on previous)
	if len(steps[1].DependsOn) != 1 || steps[1].DependsOn[0] != "Create directory" {
		t.Errorf("step 2 should depend on step 1, got %v", steps[1].DependsOn)
	}
}

func TestParser_ParseFile(t *testing.T) {
	// Create temp file
	dir := t.TempDir()
	planPath := filepath.Join(dir, "test-plan.md")

	content := `# Test Plan

## Overview

This is a test.
`
	if err := os.WriteFile(planPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	parser := NewParser(DefaultParseOptions())
	plan, err := parser.ParseFile(planPath)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if plan.Title != "Test Plan" {
		t.Errorf("Title = %q, want %q", plan.Title, "Test Plan")
	}
	if plan.FilePath != planPath {
		t.Errorf("FilePath = %q, want %q", plan.FilePath, planPath)
	}
	if plan.FileName != "test-plan.md" {
		t.Errorf("FileName = %q, want %q", plan.FileName, "test-plan.md")
	}
	if !strings.HasPrefix(plan.ContentHash, "sha256:") {
		t.Errorf("ContentHash should start with sha256:, got %q", plan.ContentHash)
	}
	if plan.ModTime.IsZero() {
		t.Error("ModTime should not be zero")
	}
	if plan.Status != StatusActive {
		t.Errorf("Status = %q, want %q", plan.Status, StatusActive)
	}
}

func TestDetector_Detect(t *testing.T) {
	// Create temp directory structure
	dir := t.TempDir()
	plansDir := filepath.Join(dir, "plans")
	archivedDir := filepath.Join(plansDir, "archived")

	if err := os.MkdirAll(archivedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create test plans
	plan1 := filepath.Join(plansDir, "plan-one.md")
	plan2 := filepath.Join(plansDir, "plan-two.md")
	archived := filepath.Join(archivedDir, "old-plan.md")

	// Write all files first
	if err := os.WriteFile(plan1, []byte("# Plan One\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plan2, []byte("# Plan Two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archived, []byte("# Archived Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set explicit times to ensure consistent ordering across filesystems
	// plan1 = 2 hours ago, plan2 = 1 hour ago (plan2 is more recent)
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(plan1, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(plan2, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	t.Run("detect all non-archived", func(t *testing.T) {
		detector := NewDetector(plansDir)
		plans, err := detector.Detect(DetectOptions{})
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if len(plans) != 2 {
			t.Errorf("Detect() = %d plans, want 2", len(plans))
		}
		// Should be sorted by mod time (most recent first)
		if plans[0].Title != "Plan Two" {
			t.Errorf("first plan = %q, want Plan Two (most recent)", plans[0].Title)
		}
	})

	t.Run("detect with limit", func(t *testing.T) {
		detector := NewDetector(plansDir)
		plans, err := detector.Detect(DetectOptions{Limit: 1})
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if len(plans) != 1 {
			t.Errorf("Detect() = %d plans, want 1", len(plans))
		}
	})

	t.Run("include archived", func(t *testing.T) {
		detector := NewDetector(plansDir)
		plans, err := detector.Detect(DetectOptions{IncludeArchived: true})
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if len(plans) != 3 {
			t.Errorf("Detect() = %d plans, want 3", len(plans))
		}
		// Find archived plan and check status
		var foundArchived bool
		for _, p := range plans {
			if p.Title == "Archived Plan" {
				foundArchived = true
				if p.Status != StatusArchived {
					t.Errorf("archived plan status = %q, want %q", p.Status, StatusArchived)
				}
			}
		}
		if !foundArchived {
			t.Error("archived plan not found")
		}
	})

	t.Run("detect modified since", func(t *testing.T) {
		// Use a time between plan1 (2hr ago) and plan2 (1hr ago)
		since := time.Now().Add(-90 * time.Minute)

		detector := NewDetector(plansDir)
		plans, err := detector.Detect(DetectOptions{Since: since})
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		// Only plan2 should be returned (modified after since time)
		if len(plans) != 1 {
			t.Errorf("Detect() = %d plans, want 1", len(plans))
		}
		if len(plans) > 0 && plans[0].Title != "Plan Two" {
			t.Errorf("plan = %q, want Plan Two", plans[0].Title)
		}
	})

	t.Run("detect most recent", func(t *testing.T) {
		detector := NewDetector(plansDir)
		plan, err := detector.DetectMostRecent()
		if err != nil {
			t.Fatalf("DetectMostRecent() error = %v", err)
		}
			if plan == nil {
				t.Fatal("DetectMostRecent() = nil, want plan")
				return
			}
			if plan.Title != "Plan Two" {
				t.Errorf("DetectMostRecent() = %q, want Plan Two", plan.Title)
			}
	})

	t.Run("empty directory", func(t *testing.T) {
		emptyDir := filepath.Join(dir, "empty")
		if err := os.MkdirAll(emptyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		detector := NewDetector(emptyDir)
		plans, err := detector.Detect(DetectOptions{})
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if len(plans) != 0 {
			t.Errorf("Detect() = %d plans, want 0", len(plans))
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		detector := NewDetector(filepath.Join(dir, "nonexistent"))
		plans, err := detector.Detect(DetectOptions{})
		if err != nil {
			t.Fatalf("Detect() error = %v", err)
		}
		if len(plans) != 0 {
			t.Errorf("Detect() = %d plans, want 0", len(plans))
		}
	})
}

func TestCalculateProgress(t *testing.T) {
	tests := []struct {
		name     string
		statuses map[string]string
		want     PlanProgress
	}{
		{
			name: "all completed",
			statuses: map[string]string{
				"t1": "completed",
				"t2": "completed",
			},
			want: PlanProgress{
				Total:           2,
				Completed:       2,
				PercentComplete: 100,
			},
		},
		{
			name: "mixed statuses",
			statuses: map[string]string{
				"t1": "completed",
				"t2": "in_progress",
				"t3": "pending",
				"t4": "blocked",
			},
			want: PlanProgress{
				Total:           4,
				Completed:       1,
				InProgress:      1,
				Pending:         1,
				Blocked:         1,
				PercentComplete: 25,
			},
		},
		{
			name:     "empty",
			statuses: map[string]string{},
			want: PlanProgress{
				Total:           0,
				PercentComplete: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateProgress(tt.statuses)
			if got.Total != tt.want.Total {
				t.Errorf("Total = %d, want %d", got.Total, tt.want.Total)
			}
			if got.Completed != tt.want.Completed {
				t.Errorf("Completed = %d, want %d", got.Completed, tt.want.Completed)
			}
			if got.InProgress != tt.want.InProgress {
				t.Errorf("InProgress = %d, want %d", got.InProgress, tt.want.InProgress)
			}
			if got.Pending != tt.want.Pending {
				t.Errorf("Pending = %d, want %d", got.Pending, tt.want.Pending)
			}
			if got.Blocked != tt.want.Blocked {
				t.Errorf("Blocked = %d, want %d", got.Blocked, tt.want.Blocked)
			}
			if got.PercentComplete != tt.want.PercentComplete {
				t.Errorf("PercentComplete = %f, want %f", got.PercentComplete, tt.want.PercentComplete)
			}
		})
	}
}

func TestIsStepSection(t *testing.T) {
	tests := []struct {
		title string
		want  bool
	}{
		{"Step 1: Do something", true},
		{"Step 2", true},
		{"1. First task", true},
		{"1.1 Subtask", true},
		{"2) Another task", true},
		{"Implement the feature", true},
		{"Create new component", true},
		{"Add validation", true},
		{"Update handler", true},
		{"Fix the bug", true},
		{"Remove old code", true},
		{"Refactor module", true},
		{"Test the changes", true},
		{"Configure settings", true},
		{"Overview", false},
		{"Problem Statement", false},
		{"Solution", false},
		{"Random heading", false},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := isStepSection(tt.title); got != tt.want {
				t.Errorf("isStepSection(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestCleanStepTitle(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Step 1: Create file", "Create file"},
		{"Step 2: Update config", "Update config"},
		{"1. Initialize project", "Initialize project"},
		{"1.1 Add dependency", "Add dependency"},
		{"2) Run tests", "Run tests"},
		{"Regular title", "Regular title"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := cleanStepTitle(tt.title); got != tt.want {
				t.Errorf("cleanStepTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}
