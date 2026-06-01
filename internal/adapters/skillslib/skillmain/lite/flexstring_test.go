package lite

import (
	"encoding/json"
	"testing"
)

func TestFlexStringUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		wantErr bool
	}{
		{name: "string", payload: `{"value":"123"}`, want: "123"},
		{name: "number", payload: `{"value":123}`, want: "123"},
		{name: "invalid", payload: `{"value":true}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Value FlexString `json:"value"`
			}
			err := json.Unmarshal([]byte(tt.payload), &got)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Value.String() != tt.want {
				t.Fatalf("value = %q, want %q", got.Value.String(), tt.want)
			}
		})
	}
}
