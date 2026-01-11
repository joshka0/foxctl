package skillerr

import (
	"errors"
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "simple error",
			err:  Arg("path is required"),
			want: "EARG: path is required",
		},
		{
			name: "error with cause",
			err:  Runtime("failed to execute", WithCause(errors.New("underlying error"))),
			want: "ERUNTIME: failed to execute: underlying error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	err := Runtime("wrapped", WithCause(cause))

	if !errors.Is(err, cause) {
		t.Error("errors.Is should find the cause")
	}
}

func TestError_Is(t *testing.T) {
	err1 := Arg("first")
	err2 := Arg("second")
	err3 := Runtime("different code")

	if !errors.Is(err1, err2) {
		t.Error("errors with same code should match")
	}

	if errors.Is(err1, err3) {
		t.Error("errors with different codes should not match")
	}
}

func TestConstructors(t *testing.T) {
	tests := []struct {
		name     string
		err      *Error
		wantCode string
	}{
		{"Arg", Arg("msg"), CodeArg},
		{"Argf", Argf("msg %d", 1), CodeArg},
		{"Runtime", Runtime("msg"), CodeRuntime},
		{"Runtimef", Runtimef("msg %d", 1), CodeRuntime},
		{"Parse", Parse("msg"), CodeParse},
		{"Parsef", Parsef("msg %d", 1), CodeParse},
		{"IO", IO("msg"), CodeIO},
		{"IOf", IOf("msg %d", 1), CodeIO},
		{"Auth", Auth("msg"), CodeAuth},
		{"Authf", Authf("msg %d", 1), CodeAuth},
		{"Validation", Validation("msg"), CodeValidation},
		{"Validationf", Validationf("msg %d", 1), CodeValidation},
		{"NotFound", NotFound("msg"), CodeNotFound},
		{"NotFoundf", NotFoundf("msg %d", 1), CodeNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.wantCode)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	t.Run("WithHint", func(t *testing.T) {
		err := Arg("invalid", WithHint("try using a valid path"))
		if err.Hint != "try using a valid path" {
			t.Errorf("Hint = %q, want %q", err.Hint, "try using a valid path")
		}
	})

	t.Run("WithData", func(t *testing.T) {
		err := Arg("invalid", WithData("field", "path"), WithData("value", "/bad"))
		if err.Data["field"] != "path" {
			t.Errorf("Data[field] = %v, want %q", err.Data["field"], "path")
		}
		if err.Data["value"] != "/bad" {
			t.Errorf("Data[value] = %v, want %q", err.Data["value"], "/bad")
		}
	})

	t.Run("WithCause", func(t *testing.T) {
		cause := errors.New("root")
		err := Runtime("failed", WithCause(cause))
		if err.Cause != cause {
			t.Error("Cause should be set")
		}
	})
}

func TestWrap(t *testing.T) {
	cause := errors.New("network failure")

	t.Run("Wrap", func(t *testing.T) {
		err := Wrap(CodeIO, "read failed", cause)
		if err.Code != CodeIO {
			t.Errorf("Code = %q, want %q", err.Code, CodeIO)
		}
		if !errors.Is(err, cause) {
			t.Error("should wrap cause")
		}
	})

	t.Run("WrapRuntime", func(t *testing.T) {
		err := WrapRuntime("execution failed", cause)
		if err.Code != CodeRuntime {
			t.Errorf("Code = %q, want %q", err.Code, CodeRuntime)
		}
	})

	t.Run("WrapIO", func(t *testing.T) {
		err := WrapIO("write failed", cause)
		if err.Code != CodeIO {
			t.Errorf("Code = %q, want %q", err.Code, CodeIO)
		}
	})

	t.Run("WrapParse", func(t *testing.T) {
		err := WrapParse("json decode failed", cause)
		if err.Code != CodeParse {
			t.Errorf("Code = %q, want %q", err.Code, CodeParse)
		}
	})
}

func TestIsCode(t *testing.T) {
	err := Arg("test")

	if !IsCode(err, CodeArg) {
		t.Error("IsCode should return true for matching code")
	}

	if IsCode(err, CodeRuntime) {
		t.Error("IsCode should return false for non-matching code")
	}

	if IsCode(errors.New("plain error"), CodeArg) {
		t.Error("IsCode should return false for non-skill errors")
	}
}

func TestGetCode(t *testing.T) {
	if code := GetCode(Arg("test")); code != CodeArg {
		t.Errorf("GetCode = %q, want %q", code, CodeArg)
	}

	if code := GetCode(errors.New("plain")); code != "" {
		t.Errorf("GetCode = %q, want empty string", code)
	}
}

func TestToEnvelopeData(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		err := Arg("test")
		if data := err.ToEnvelopeData(); data != nil {
			t.Errorf("ToEnvelopeData = %v, want nil", data)
		}
	})

	t.Run("with hint", func(t *testing.T) {
		err := Arg("test", WithHint("try this"))
		data := err.ToEnvelopeData()
		if data["hint"] != "try this" {
			t.Errorf("data[hint] = %v, want %q", data["hint"], "try this")
		}
	})

	t.Run("with data", func(t *testing.T) {
		err := Arg("test", WithData("key", "value"))
		data := err.ToEnvelopeData()
		if data["key"] != "value" {
			t.Errorf("data[key] = %v, want %q", data["key"], "value")
		}
	})
}

func TestCodeDescription(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{CodeArg, "Invalid argument or input"},
		{CodeRuntime, "Runtime execution error"},
		{CodeParse, "Parsing error"},
		{CodeIO, "I/O error"},
		{CodeAuth, "Authentication or authorization error"},
		{CodeValidation, "Validation error"},
		{CodeNotFound, "Resource not found"},
		{CodeTimeout, "Operation timed out"},
		{CodeConflict, "Conflict error"},
		{CodeInternal, "Internal error"},
		{"UNKNOWN", "Unknown error"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if got := CodeDescription(tt.code); got != tt.want {
				t.Errorf("CodeDescription(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}
