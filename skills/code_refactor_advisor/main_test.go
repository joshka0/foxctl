package main

import (
	"strings"
	"testing"
)

func TestParseAdvisorOutput(t *testing.T) {
	raw := "```json\n{\"summary\":\"ok\",\"prioritized\":[{\"path\":\"a.go\",\"symbol\":\"Foo\",\"priority\":1,\"why\":\"best\"}]}\n```"
	got, err := parseAdvisorOutput(raw, nil, 3)
	if err != nil {
		t.Fatalf("parseAdvisorOutput error = %v", err)
	}
	if got.Summary != "ok" {
		t.Fatalf("summary=%q", got.Summary)
	}
	if len(got.Prioritized) != 1 || got.Prioritized[0].Path != "a.go" {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
}

func TestClampAdvisorOutputFiltersUnknownCandidates(t *testing.T) {
	candidates := []candidateBrief{
		{File: "a.go", Symbol: "Foo"},
		{File: "b.go", Symbol: "Bar"},
	}
	in := advisorOutput{
		Prioritized: []advisorItem{
			{Path: "a.go", Symbol: "Foo", Priority: 1},
			{Path: "c.go", Symbol: "Baz", Priority: 2},
		},
		Defer: []advisorItem{
			{Path: "b.go", Symbol: "Bar"},
			{Path: "x.go", Symbol: "Nope"},
		},
	}
	got := clampAdvisorOutput(in, candidates, 3)
	if len(got.Prioritized) != 1 || got.Prioritized[0].Path != "a.go" {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
	if len(got.Defer) != 1 || got.Defer[0].Path != "b.go" {
		t.Fatalf("defer=%#v", got.Defer)
	}
}

func TestBuildAdvisorUserPrompt(t *testing.T) {
	prompt := buildAdvisorUserPrompt(input{Path: "./internal", Language: "go", Focus: "slop", RuleSet: "default", ShortlistSize: 3}, []candidateBrief{
		{File: "internal/foo.go", Symbol: "Foo", Score: 90, RuleID: "function_hotspot"},
	})
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "\"language\": \"go\"") {
		t.Fatalf("prompt missing language: %q", prompt)
	}
	if !strings.Contains(prompt, "internal/foo.go") {
		t.Fatalf("prompt missing candidate file: %q", prompt)
	}
	if !strings.Contains(prompt, "\"focus\": \"slop\"") {
		t.Fatalf("prompt missing focus: %q", prompt)
	}
}

func TestFallbackAdvisorOutput(t *testing.T) {
	candidates := []candidateBrief{
		{File: "a.go", Symbol: "Foo", Severity: "high", Detail: "foo detail"},
		{File: "b.go", Symbol: "Bar", Severity: "medium", Detail: "bar detail"},
	}
	raw := "Best starting points:\n1. Foo in a.go because it is overloaded\n2. Bar in b.go after that"
	got, ok := fallbackAdvisorOutput(raw, candidates, 2)
	if !ok {
		t.Fatal("expected fallback parse to succeed")
	}
	if len(got.Prioritized) != 2 {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
	if got.Prioritized[0].Path != "a.go" || got.Prioritized[1].Path != "b.go" {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
}

func TestFallbackAdvisorOutputMatchesMethodByShortName(t *testing.T) {
	candidates := []candidateBrief{
		{File: "internal/actor/agent_actor.go", Symbol: "*AgentActor.handleAsk", Severity: "high", Detail: "detail"},
	}
	raw := "The best starting point is handleAsk in agent_actor.go because it combines multiple structural smells."
	got, ok := fallbackAdvisorOutput(raw, candidates, 1)
	if !ok {
		t.Fatal("expected fallback parse to succeed")
	}
	if len(got.Prioritized) != 1 || got.Prioritized[0].Symbol != "*AgentActor.handleAsk" {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
}

func TestFallbackFromTopCandidates(t *testing.T) {
	candidates := []candidateBrief{
		{File: "a.go", Symbol: "Foo", Severity: "high", Detail: "foo detail", SuggestedRefactor: "split foo"},
		{File: "b.go", Symbol: "Bar", Severity: "medium", Detail: "bar detail", SuggestedRefactor: "split bar"},
		{File: "c.go", Symbol: "Baz", Severity: "low", Detail: "baz detail", SuggestedRefactor: "split baz"},
	}
	got := fallbackFromTopCandidates(candidates, 2, "fallback")
	if got.Summary != "fallback" {
		t.Fatalf("summary=%q", got.Summary)
	}
	if len(got.Prioritized) != 2 {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
	if got.Prioritized[0].Path != "a.go" || got.Prioritized[1].Path != "b.go" {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
	if len(got.Defer) != 1 || got.Defer[0].Path != "c.go" {
		t.Fatalf("defer=%#v", got.Defer)
	}
	if len(got.Sequence) != 2 || got.Sequence[0] != "a.go:Foo" {
		t.Fatalf("sequence=%#v", got.Sequence)
	}
}

func TestFallbackAdvisorOutputPrefersSymbolMentionsOverFileOnly(t *testing.T) {
	candidates := []candidateBrief{
		{File: "internal/a.go", Symbol: "HotFunc", Score: 100, Severity: "high", Detail: "hot"},
		{File: "internal/a.go", Symbol: "", Score: 86, Severity: "high", Detail: "file cluster"},
	}
	raw := "Best starting point: HotFunc in internal/a.go. The file internal/a.go is also a backlog cluster."
	got, ok := fallbackAdvisorOutput(raw, candidates, 2)
	if !ok {
		t.Fatal("expected fallback parse to succeed")
	}
	if len(got.Prioritized) != 1 {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
	if got.Prioritized[0].Symbol != "HotFunc" {
		t.Fatalf("prioritized=%#v", got.Prioritized)
	}
}
