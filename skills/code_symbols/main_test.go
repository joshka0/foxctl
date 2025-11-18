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
			json: `{"path": "."}`,
			want: input{
				Path: ".",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := parseInput(bytes.NewBufferString(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseInput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && in.Path != tt.want.Path {
				t.Errorf("parseInput() = %v, want %v", in, tt.want)
			}
		})
	}
}