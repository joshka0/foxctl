package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestParseInput(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    input
		wantErr bool
	}{
		{
			name: "valid input",
			json: `{"filter": ".foo", "raw_output": true}`,
			want: input{
				Filter:    ".foo",
				RawOutput: true,
			},
		},
		{
			name:    "invalid json",
			json:    `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := parseInput(bytes.NewBufferString(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && in != tt.want {
				t.Errorf("parseInput() = %v, want %v", in, tt.want)
			}
		})
	}
}

func TestBuildJQArgs(t *testing.T) {
	tests := []struct {
		name string
		in   input
		want []string
	}{
		{
			name: "basic filter",
			in: input{
				Filter: ".",
			},
			want: []string{".", "-"},
		},
		{
			name: "raw output",
			in: input{
				Filter:    ".",
				RawOutput: true,
			},
			want: []string{"-r", ".", "-"},
		},
		{
			name: "compact output",
			in: input{
				Filter:        ".",
				CompactOutput: true,
			},
			want: []string{"-c", ".", "-"},
		},
		{
			name: "slurp",
			in: input{
				Filter: ".",
				Slurp:  true,
			},
			want: []string{"-s", ".", "-"},
		},
		{
			name: "sort keys",
			in: input{
				Filter:   ".",
				SortKeys: true,
			},
			want: []string{"-S", ".", "-"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildJQArgs(tt.in)
			if len(got) != len(tt.want) {
				t.Errorf("buildJQArgs() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("buildJQArgs()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
