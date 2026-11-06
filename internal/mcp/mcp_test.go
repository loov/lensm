package mcp

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"loov.dev/lensm/internal/disasm"
)

// TestAppServerSetFileClosesReplacedFile checks that SetFile serves the
// new file and closes the old one once no request holds it.
func TestAppServerSetFileClosesReplacedFile(t *testing.T) {
	server := &AppServer{}
	old := &closeCounter{}
	server.SetFile("old", old, nil)

	session, release := server.acquireSession()
	server.SetFile("new", emptyFile{}, nil)
	if session.File != old {
		t.Fatal("request lost its session")
	}
	time.Sleep(10 * time.Millisecond)
	if old.closed.Load() != 0 {
		t.Fatal("old file closed while a request used it")
	}
	release()
	deadline := time.Now().Add(2 * time.Second)
	for old.closed.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("old file never closed")
		}
		time.Sleep(time.Millisecond)
	}

	server.SetFile("", nil, nil)
	if session, release := server.acquireSession(); session != nil {
		t.Fatal("SetFile(nil) kept a session")
	} else {
		release()
	}
}

func TestMCPHandleHTTPRejectsRebinding(t *testing.T) {
	server := &AppServer{}

	cases := []struct {
		name   string
		host   string
		origin string
		status int
	}{
		{name: "loopback ip", host: "127.0.0.1:7077", status: 200},
		{name: "localhost", host: "localhost:7077", status: 200},
		{name: "ipv6 loopback", host: "[::1]:7077", status: 200},
		{name: "rebound host", host: "evil.example.com:7077", status: 403},
		{name: "loopback origin", host: "127.0.0.1:7077", origin: "http://localhost:8000", status: 200},
		{name: "cross-site origin", host: "127.0.0.1:7077", origin: "https://evil.example.com", status: 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://placeholder/", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			server.handleHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestMCPHandleHTTPParseErrorHasNullID(t *testing.T) {
	server := &AppServer{}
	req := httptest.NewRequest("POST", "http://127.0.0.1:7077/mcp", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	server.handleHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"id":null`) {
		t.Fatalf("parse error response missing null id: %s", rec.Body.String())
	}
}

func TestMCPToolCallWithoutFile(t *testing.T) {
	server := &mcpServer{}
	result, rpcErr := server.handleToolCall(json.RawMessage(`{"name":"list_functions"}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	toolResult, ok := result.(mcpToolResult)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if !toolResult.IsError {
		t.Fatal("tool result is not marked as error")
	}
}

// emptyFile is a disasm.File with no functions, for tests that need a
// stand-in session without building a binary.
type emptyFile struct{}

func (emptyFile) Close() error         { return nil }
func (emptyFile) Funcs() []disasm.Func { return nil }

type closeCounter struct {
	emptyFile
	closed atomic.Int32
}

func (f *closeCounter) Close() error {
	f.closed.Add(1)
	return nil
}
