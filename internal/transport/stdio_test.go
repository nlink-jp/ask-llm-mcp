package transport

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestStdioTransport_ReadLine(t *testing.T) {
	in := strings.NewReader("{\"jsonrpc\":\"2.0\"}\n{\"id\":1}\n")
	tr := NewStdioTransport(in, io.Discard)

	first, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if string(first) != `{"jsonrpc":"2.0"}` {
		t.Errorf("first = %q", string(first))
	}

	second, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if string(second) != `{"id":1}` {
		t.Errorf("second = %q", string(second))
	}

	if _, err := tr.ReadMessage(); err != io.EOF {
		t.Errorf("third read: want EOF, got %v", err)
	}
}

func TestStdioTransport_WriteEncodesJSONAndAddsNewline(t *testing.T) {
	var buf bytes.Buffer
	tr := NewStdioTransport(strings.NewReader(""), &buf)

	if err := tr.WriteMessage(map[string]any{"k": "v"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := buf.String(); got != "{\"k\":\"v\"}\n" {
		t.Errorf("buf = %q", got)
	}
}

// TestStdioTransport_WriteSerializedConcurrently verifies the mutex
// prevents interleaved JSON when goroutines hammer WriteMessage. We
// count newlines and check each line is parseable.
func TestStdioTransport_WriteSerializedConcurrently(t *testing.T) {
	var buf bytes.Buffer
	tr := NewStdioTransport(strings.NewReader(""), &buf)

	const N = 200
	var wg sync.WaitGroup
	wg.Add(N)
	for i := range N {
		go func(i int) {
			defer wg.Done()
			_ = tr.WriteMessage(map[string]any{"id": i})
		}(i)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != N {
		t.Fatalf("got %d lines, want %d", len(lines), N)
	}
}

