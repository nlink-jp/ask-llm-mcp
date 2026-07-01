package toolerr

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_Error_FormatsCodeAndMessage(t *testing.T) {
	cases := []struct {
		in   *Error
		want string
	}{
		{New("c", "m"), "c: m"},
		{New("c", ""), "c"},
	}
	for _, tc := range cases {
		if got := tc.in.Error(); got != tc.want {
			t.Errorf("Error() = %q, want %q", got, tc.want)
		}
	}
}

func TestError_Is_MatchesByCodeIgnoringMessage(t *testing.T) {
	sentinel := New(CodeInvalidArguments, "")
	concrete := New(CodeInvalidArguments, "prompt must not be empty").
		WithDetails(map[string]any{"prompt": ""})

	if !errors.Is(concrete, sentinel) {
		t.Fatalf("errors.Is(concrete, sentinel) = false, want true")
	}

	other := New(CodeUpstreamError, "")
	if errors.Is(concrete, other) {
		t.Fatalf("errors.Is(concrete, other) = true, want false")
	}
}

func TestError_Is_WrappedByFmtErrorf(t *testing.T) {
	concrete := New(CodeUpstreamTimeout, "deadline exceeded")
	wrapped := fmt.Errorf("call gemini: %w", concrete)

	if !errors.Is(wrapped, New(CodeUpstreamTimeout, "")) {
		t.Fatalf("errors.Is unwrap chain failed for %T", wrapped)
	}
}

func TestNewf_FormatsMessage(t *testing.T) {
	e := Newf(CodeInvalidArguments, "field %q empty", "prompt")
	if e.Message != `field "prompt" empty` {
		t.Errorf("message = %q, want %q", e.Message, `field "prompt" empty`)
	}
	if e.Code != CodeInvalidArguments {
		t.Errorf("code = %q, want %q", e.Code, CodeInvalidArguments)
	}
}

func TestWithDetails_DoesNotMutateOriginal(t *testing.T) {
	base := New(CodeUpstreamError, "x")
	withD := base.WithDetails(map[string]any{"reason": "content_filter"})

	if base.Details != nil {
		t.Errorf("base mutated: %v", base.Details)
	}
	if got := withD.Details["reason"]; got != "content_filter" {
		t.Errorf("withD reason = %v, want content_filter", got)
	}
}
