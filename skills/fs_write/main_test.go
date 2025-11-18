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
			json: `{"path": "foo.txt", "content": "bar", "mode": 420}`, // 420 = 0644
			want: input{
				Path:    "foo.txt",
				Content: "bar",
				Mode:    0644,
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

func TestCheckWriteMode(t *testing.T) {
	// Create temp file
	f, err := os.CreateTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.Close()

	tests := []struct {
		name    string
		path    string
		in      input
		wantErr bool
	}{
		{
			name: "overwrite allowed",
			path: f.Name(),
			in:   input{Overwrite: true},
			wantErr: false,
		},
		{
			name: "overwrite denied",
			path: f.Name(),
			in:   input{Overwrite: false},
			wantErr: true,
		},
		{
			name: "append allowed",
			path: f.Name(),
			in:   input{Append: true},
			wantErr: false,
		},
		{
			name: "new file",
			path: f.Name() + ".new",
			in:   input{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkWriteMode(tt.path, tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkWriteMode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
