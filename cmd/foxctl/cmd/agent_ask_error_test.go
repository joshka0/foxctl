package cmd

import (
	"errors"
	"testing"

	"github.com/joshka0/foxctl/internal/protocol"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
)

func TestAskServiceErrorCode_MapsV2EnvelopeCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want protocol.ErrorCode
	}{
		{
			name: "validation",
			err: &v2errors.V2Error{
				Kind: v2errors.ErrValidation,
			},
			want: protocol.ErrorCodeEARG,
		},
		{
			name: "policy",
			err: &v2errors.V2Error{
				Kind: v2errors.ErrPolicyViolation,
			},
			want: protocol.ErrorCodeEPolicy,
		},
		{
			name: "not_found",
			err: &v2errors.V2Error{
				Kind: v2errors.ErrNotFound,
			},
			want: protocol.ErrorCodeENotFound,
		},
		{
			name: "timeout",
			err: &v2errors.V2Error{
				Kind: v2errors.ErrTimeout,
			},
			want: protocol.ErrorCodeETimeout,
		},
		{
			name: "dependency",
			err: &v2errors.V2Error{
				Kind: v2errors.ErrDependency,
			},
			want: protocol.ErrorCodeERuntime,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := askServiceErrorCode(tc.err); got != tc.want {
				t.Fatalf("askServiceErrorCode()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestAskServiceErrorCode_GenericErrorDefaultsRuntime(t *testing.T) {
	t.Parallel()

	if got := askServiceErrorCode(errors.New("boom")); got != protocol.ErrorCodeERuntime {
		t.Fatalf("askServiceErrorCode()=%q want %q", got, protocol.ErrorCodeERuntime)
	}
}
