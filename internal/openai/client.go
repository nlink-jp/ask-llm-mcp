// Package openai is a thin OpenAI-compatible chat-completions client
// used by ask-llm-mcp's MCP tool. It targets any endpoint that speaks
// the OpenAI /v1/chat/completions API — primarily a locally running
// LM Studio server — and exposes a minimal surface: Ask(prompt)
// returns text. This isolates the HTTP dependency so the tool layer
// can depend on an Asker interface instead of a concrete client.
//
// Retry: transient failures (429 / 5xx / transport) are retried with
// exponential backoff up to maxRetries. Non-retryable failures (4xx
// auth/schema, an exceeded per-request timeout) surface immediately so
// the MCP client sees the real cause.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/nlink-jp/nlk/backoff"

	"github.com/nlink-jp/ask-llm-mcp/internal/toolerr"
)

const (
	// maxRetries caps retry attempts on retryable failures. Beyond this
	// the last error is surfaced so the MCP client sees the real cause.
	maxRetries = 5
	// maxRespBytes caps how much of a response body we read, guarding
	// against a misbehaving upstream streaming an unbounded body.
	maxRespBytes = 16 << 20 // 16 MiB
)

// Options configures a Client. BaseURL and Model are required; the rest
// are optional and default to "unset" (omitted from the request).
type Options struct {
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	Temperature  *float64
	MaxTokens    *int
	TimeoutSec   int
}

// Client is the per-process OpenAI-compatible handle for ask-llm-mcp.
// One instance is shared across every ask_llm tool call.
type Client struct {
	http         *http.Client
	endpoint     string // baseURL + /chat/completions
	baseURL      string // retained for error hints
	apiKey       string
	model        string
	systemPrompt string
	temperature  *float64
	maxTokens    *int
	timeout      time.Duration
	logger       *slog.Logger

	// backoff schedule; overridable in tests to keep retries fast.
	backoffBase time.Duration
	backoffMax  time.Duration
}

// New creates a client. It validates that Model and BaseURL are set;
// everything else is optional.
func New(opt Options) (*Client, error) {
	if strings.TrimSpace(opt.Model) == "" {
		return nil, fmt.Errorf("openai client: model is required")
	}
	base := strings.TrimRight(strings.TrimSpace(opt.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("openai client: base_url is required")
	}
	c := &Client{
		http:         &http.Client{},
		endpoint:     base + "/chat/completions",
		baseURL:      base,
		apiKey:       opt.APIKey,
		model:        opt.Model,
		systemPrompt: opt.SystemPrompt,
		temperature:  opt.Temperature,
		maxTokens:    opt.MaxTokens,
		backoffBase:  2 * time.Second,
		backoffMax:   30 * time.Second,
	}
	if opt.TimeoutSec > 0 {
		c.timeout = time.Duration(opt.TimeoutSec) * time.Second
	}
	return c, nil
}

// SetLogger attaches a logger used for retry diagnostics. Optional: the
// zero-value Client logs via slog.Default().
func (c *Client) SetLogger(l *slog.Logger) {
	if l != nil {
		c.logger = l
	}
}

// Model returns the configured model name.
func (c *Client) Model() string { return c.model }

func (c *Client) log() *slog.Logger {
	if c.logger == nil {
		return slog.Default()
	}
	return c.logger
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			// Content holds the answer. Any reasoning delivered in a
			// separate reasoning_content field is intentionally not
			// decoded — we only surface the final answer.
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Ask sends prompt to the configured endpoint and returns the response
// text.
//
// Cancellation: the inbound ctx is the Cobra signal context, so
// SIGINT/SIGTERM aborts any in-flight call. On top of that, the
// per-call timeout from config is layered via context.WithTimeout so a
// single hung upstream call can never block the MCP client forever.
func (c *Client) Ask(ctx context.Context, prompt string) (string, error) {
	callCtx := ctx
	if c.timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	msgs := make([]chatMessage, 0, 2)
	if strings.TrimSpace(c.systemPrompt) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: c.systemPrompt})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: prompt})

	payload, err := json.Marshal(chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: c.temperature,
		MaxTokens:   c.maxTokens,
		Stream:      false,
	})
	if err != nil {
		return "", toolerr.New(toolerr.CodeInternalError, "marshal request: "+err.Error())
	}

	bo := backoff.New(
		backoff.WithBase(c.backoffBase),
		backoff.WithMax(c.backoffMax),
	)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		text, retryable, err := c.attempt(callCtx, payload)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !retryable || attempt == maxRetries {
			return "", err
		}
		wait := bo.Duration(attempt)
		c.log().Warn("openai call failed, retrying",
			"attempt", attempt+1, "max", maxRetries+1,
			"wait", wait.Round(time.Millisecond), "err", err)
		select {
		case <-time.After(wait):
		case <-callCtx.Done():
			return "", ctxToolErr(callCtx.Err())
		}
	}
	return "", lastErr
}

// attempt performs a single request and reports whether the failure (if
// any) is worth retrying.
func (c *Client) attempt(ctx context.Context, payload []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", false, toolerr.New(toolerr.CodeInternalError, "build request: "+err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// A cancelled/expired context is terminal, not retryable.
		if ctx.Err() != nil {
			return "", false, ctxToolErr(ctx.Err())
		}
		// Other transport errors (connection refused, reset, EOF) may be
		// transient — the local server could still be starting up.
		return "", true, toolerr.New(toolerr.CodeUpstreamError,
			fmt.Sprintf("request to %s failed: %v (is the server running and reachable?)", c.baseURL, err))
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxRespBytes))

	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", retryable, toolerr.New(toolerr.CodeUpstreamError, upstreamMessage(resp.StatusCode, data)).
			WithDetails(map[string]any{"status": resp.StatusCode})
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", false, toolerr.New(toolerr.CodeUpstreamError, "decode response: "+err.Error())
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", false, toolerr.New(toolerr.CodeUpstreamError, "upstream error: "+cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", false, toolerr.New(toolerr.CodeUpstreamError, "no choices in response")
	}
	text := strings.TrimSpace(stripReasoning(cr.Choices[0].Message.Content))
	if text == "" {
		return "", false, toolerr.New(toolerr.CodeUpstreamError, "empty response from model")
	}
	return text, false, nil
}

// ctxToolErr maps a context error to a structured upstream_timeout.
func ctxToolErr(err error) *toolerr.Error {
	if errors.Is(err, context.Canceled) {
		return toolerr.New(toolerr.CodeUpstreamTimeout, "LLM call cancelled").
			WithDetails(map[string]any{"cause": err.Error()})
	}
	return toolerr.New(toolerr.CodeUpstreamTimeout, "LLM call exceeded per-request timeout").
		WithDetails(map[string]any{"cause": err.Error()})
}

// upstreamMessage builds a human-readable message for a non-200
// response, surfacing the upstream error text when the body carries the
// OpenAI {error:{message}} shape.
func upstreamMessage(status int, body []byte) string {
	var er struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &er) == nil && er.Error.Message != "" {
		return fmt.Sprintf("upstream returned HTTP %d: %s", status, er.Error.Message)
	}
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	if snippet == "" {
		return fmt.Sprintf("upstream returned HTTP %d", status)
	}
	return fmt.Sprintf("upstream returned HTTP %d: %s", status, snippet)
}

var reasoningRe = regexp.MustCompile(`(?s)<think(?:ing)?>.*?</think(?:ing)?>`)

// stripReasoning removes <think>...</think> / <thinking>...</thinking>
// blocks that local reasoning models (DeepSeek-R1, Qwen QwQ, …) emit
// inline in the message content. Reasoning delivered out-of-band in a
// separate reasoning_content field is naturally excluded because we
// only read message.content.
func stripReasoning(s string) string {
	return reasoningRe.ReplaceAllString(s, "")
}
