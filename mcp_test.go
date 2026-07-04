package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppMCPServerSetPathClearsStaleSessionOnLoadFailure(t *testing.T) {
	server := &AppMCPServer{
		session: &Session{Path: "old"},
	}

	server.SetPath(filepath.Join(t.TempDir(), "missing-binary"), nil)

	server.mu.Lock()
	if server.session != nil {
		t.Fatal("old MCP session was not cleared immediately")
	}
	server.mu.Unlock()

	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		server.mu.Lock()
		loadErr := server.loadError
		session := server.session
		server.mu.Unlock()
		if loadErr != nil {
			if session != nil {
				t.Fatal("MCP session was set after load failure")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for MCP load failure")
		case <-tick.C:
		}
	}
}

func TestMCPHandleHTTPRejectsRebinding(t *testing.T) {
	server := &AppMCPServer{}

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
	server := &AppMCPServer{}
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

func TestMCPToolCallReportsLoadError(t *testing.T) {
	server := &mcpServer{loadErr: errors.New("load failed")}
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
	if len(toolResult.Content) != 1 || !strings.Contains(toolResult.Content[0].Text, "load failed") {
		t.Fatalf("tool error content = %#v", toolResult.Content)
	}
}
