package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nlink-jp/ask-llm-mcp/internal/toolerr"
)

type fakeAsker struct {
	gotPrompt string
	reply     string
	err       error
}

func (f *fakeAsker) Ask(ctx context.Context, prompt string) (string, error) {
	f.gotPrompt = prompt
	return f.reply, f.err
}

func TestAskLLMHandler_SuccessPassesPrompt(t *testing.T) {
	fa := &fakeAsker{reply: "hello back"}
	h := AskLLMHandler(fa)

	out, err := h(context.Background(), json.RawMessage(`{"prompt":"hi llm"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out != "hello back" {
		t.Errorf("output = %q, want hello back", out)
	}
	if fa.gotPrompt != "hi llm" {
		t.Errorf("prompt forwarded = %q, want %q", fa.gotPrompt, "hi llm")
	}
}

func TestAskLLMHandler_RejectsEmptyPrompt(t *testing.T) {
	cases := []string{
		`{"prompt":""}`,
		`{"prompt":"   "}`,
		`{"prompt":"\t\n"}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			h := AskLLMHandler(&fakeAsker{reply: "should not run"})
			_, err := h(context.Background(), json.RawMessage(raw))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, toolerr.New(toolerr.CodeInvalidArguments, "")) {
				t.Errorf("want CodeInvalidArguments, got %v", err)
			}
		})
	}
}

func TestAskLLMHandler_RejectsMissingPrompt(t *testing.T) {
	h := AskLLMHandler(&fakeAsker{})
	_, err := h(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, toolerr.New(toolerr.CodeInvalidArguments, "")) {
		t.Errorf("want CodeInvalidArguments, got %v", err)
	}
}

func TestAskLLMHandler_RejectsUnknownField(t *testing.T) {
	// Strict decoding catches misspelled keys like "promot" instead of
	// "prompt" so the user sees a real error rather than silently asking
	// the model an empty question.
	h := AskLLMHandler(&fakeAsker{})
	_, err := h(context.Background(), json.RawMessage(`{"prompt":"x","extra":"oops"}`))
	if err == nil {
		t.Fatal("expected unknown-field error, got nil")
	}
	if !errors.Is(err, toolerr.New(toolerr.CodeInvalidArguments, "")) {
		t.Errorf("want CodeInvalidArguments, got %v", err)
	}
}

func TestAskLLMHandler_PropagatesAskerError(t *testing.T) {
	want := toolerr.New(toolerr.CodeUpstreamError, "boom")
	h := AskLLMHandler(&fakeAsker{err: want})
	_, err := h(context.Background(), json.RawMessage(`{"prompt":"hi"}`))
	if !errors.Is(err, toolerr.New(toolerr.CodeUpstreamError, "")) {
		t.Errorf("want upstream_error, got %v", err)
	}
}

func TestAskLLMTool_Descriptor(t *testing.T) {
	tool := AskLLMTool()
	if tool.Name != "ask_llm" {
		t.Errorf("name = %q, want ask_llm", tool.Name)
	}
	if tool.InputSchema == nil {
		t.Error("InputSchema empty")
	}
	// Sanity-check the schema parses as JSON so a typo in the literal
	// fails the test instead of the runtime.
	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatalf("schema not JSON: %v", err)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
}
