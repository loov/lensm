package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppMCPServerSetPathClearsStaleSessionOnLoadFailure(t *testing.T) {
	server := &AppMCPServer{
		session: &LensmSession{Path: "old"},
	}

	server.SetPath(filepath.Join(t.TempDir(), "missing-binary"), nil)

	server.mu.RLock()
	if server.session != nil {
		t.Fatal("old MCP session was not cleared immediately")
	}
	server.mu.RUnlock()

	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		server.mu.RLock()
		loadErr := server.loadError
		session := server.session
		server.mu.RUnlock()
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
