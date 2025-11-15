package protocol

import "testing"

func TestErrorCodeString(t *testing.T) {
	if got := ErrorCode("EARG").String(); got != "EARG" {
		t.Fatalf("expected EARG, got %s", got)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		code      ErrorCode
		retryable bool
	}{
		{ErrorCodeERuntime, true},
		{ErrorCodeERateLimit, true},
		{ErrorCodeETimeout, true},
		{ErrorCodeERuntimeRestart, false},
		{ErrorCodeEARG, false},
	}

	for _, tt := range tests {
		if got := IsRetryable(tt.code); got != tt.retryable {
			t.Errorf("IsRetryable(%s) = %v, want %v", tt.code, got, tt.retryable)
		}
	}
}
