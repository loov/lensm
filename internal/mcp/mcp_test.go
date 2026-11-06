package mcp

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"loov.dev/lensm/internal/comments"
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
		{name: "loopback ip", host: "127.0.0.1:7077", status: 204},
		{name: "localhost", host: "localhost:7077", status: 204},
		{name: "ipv6 loopback", host: "[::1]:7077", status: 204},
		{name: "rebound host", host: "evil.example.com:7077", status: 403},
		{name: "loopback origin", host: "127.0.0.1:7077", origin: "http://localhost:8000", status: 403},
		{name: "cross-site origin", host: "127.0.0.1:7077", origin: "https://evil.example.com", status: 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("OPTIONS", "http://placeholder/", nil)
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

func TestMCPIgnoresResponses(t *testing.T) {
	server := &mcpServer{}
	if _, ok := server.handle(rpcMessage{ID: json.RawMessage("4"), Result: map[string]any{}}); ok {
		t.Fatal("replied to a response")
	}
	if _, ok := server.handle(rpcMessage{Method: "notifications/initialized"}); ok {
		t.Fatal("replied to a notification")
	}
	if _, ok := server.handle(rpcMessage{ID: json.RawMessage("5"), Method: "ping"}); !ok {
		t.Fatal("no reply to ping")
	}
}

func TestMCPRejectsUnknownArguments(t *testing.T) {
	server := &mcpServer{session: &Session{File: emptyFile{}}}
	result, rpcErr := server.handleToolCall(json.RawMessage(`{"name":"get_function","arguments":{"name":"f","contxt":5}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	if r := result.(mcpToolResult); !r.IsError || !strings.Contains(r.Content[0].Text, "contxt") {
		t.Fatalf("want an error naming contxt, got %+v", r)
	}
}

// codeFile is a disasm.File with one function f of three instructions.
type codeFile struct{ emptyFile }

type codeFunc struct{}

func (codeFile) Funcs() []disasm.Func { return []disasm.Func{codeFunc{}} }
func (codeFunc) Name() string         { return "f" }
func (codeFunc) Load(disasm.Options) (*disasm.Code, error) {
	return &disasm.Code{Name: "f", File: "f.go", Insts: []disasm.Inst{
		{PC: 0x10, Text: "MOVQ AX, BX", NativeText: "mov rbx, rax", File: "f.go", Line: 1},
		{},
		{PC: 0x14, Text: "RET", NativeText: "ret", File: "g.go", Line: 2},
	}}, nil
}

func TestMCPGetFunctionPages(t *testing.T) {
	server := &mcpServer{session: &Session{File: codeFile{}}}
	get := func(args string) FunctionCodeDTO {
		t.Helper()
		result, rpcErr := server.handleToolCall(json.RawMessage(`{"name":"get_function","arguments":` + args + `}`))
		if rpcErr != nil {
			t.Fatal(rpcErr.Message)
		}
		r := result.(mcpToolResult)
		if r.IsError {
			t.Fatal(r.Content[0].Text)
		}
		return r.StructuredContent.(FunctionCodeDTO)
	}

	all := get(`{"name":"f"}`)
	if all.AsmTotal != 3 || len(all.Asm) != 2 || all.Truncated {
		t.Fatalf("want 2 rows of 3 without the spacer, got %+v", all)
	}
	if all.Asm[0].File != "" || all.Asm[1].File != "g.go" {
		t.Errorf("file should only be set when it differs from f.go: %+v", all.Asm)
	}

	page := get(`{"name":"f","views":["native_asm"],"offset":2,"limit":1}`)
	if len(page.Asm) != 1 || page.Asm[0].Native != "ret" || page.Asm[0].Go != "" || page.Truncated {
		t.Fatalf("want only ret in native syntax, got %+v", page.Asm)
	}

	first := get(`{"name":"f","limit":1}`)
	if !first.Truncated || len(first.Asm) != 1 {
		t.Fatalf("want a truncated first page, got %+v", first)
	}
}

func TestParsePC(t *testing.T) {
	for _, tc := range []struct {
		in     string
		pc     uint64
		ok     bool
		hasErr bool
	}{
		{in: ``},
		{in: `null`},
		{in: `16`, pc: 16, ok: true},
		{in: `"0x10"`, pc: 16, ok: true},
		{in: `"16"`, pc: 16, ok: true},
		{in: `"-1"`, ok: true, hasErr: true},
		{in: `1.5`, ok: true, hasErr: true},
		{in: `"0x"`, ok: true, hasErr: true},
		{in: `-3`, ok: true, hasErr: true},
	} {
		pc, ok, err := parsePC(json.RawMessage(tc.in))
		if pc != tc.pc || ok != tc.ok || (err != nil) != tc.hasErr {
			t.Errorf("parsePC(%s) = %d, %v, %v", tc.in, pc, ok, err)
		}
	}
}

// TestMCPSetComment checks that set_comment only accepts positions the
// function has, and that an accepted comment comes back.
func TestMCPSetComment(t *testing.T) {
	store, err := comments.Open(filepath.Join(t.TempDir(), "c.json"), "bin")
	if err != nil {
		t.Fatal(err)
	}
	server := &mcpServer{session: &Session{File: codeFile{}, Comments: store}}
	call := func(args string) mcpToolResult {
		t.Helper()
		result, rpcErr := server.handleToolCall(json.RawMessage(`{"name":"set_comment","arguments":` + args + `}`))
		if rpcErr != nil {
			t.Fatal(rpcErr.Message)
		}
		return result.(mcpToolResult)
	}
	for _, bad := range []string{
		`{"name":"f","view":"go_asm","pc":"0x11","text":"x"}`,
		`{"name":"f","view":"go_asm","text":"x"}`,
		`{"name":"f","view":"source","file":"f.go","line":1,"text":"x"}`,
		`{"name":"missing","view":"go_asm","pc":16,"text":"x"}`,
	} {
		if r := call(bad); !r.IsError {
			t.Errorf("set_comment %s succeeded", bad)
		}
	}
	if r := call(`{"name":"f","view":"native_asm","pc":"0x14","text":"returns"}`); r.IsError {
		t.Fatal(r.Content[0].Text)
	}
	if got := store.ForAsm("f", comments.ViewNativeAsm, 0x14); got != "returns" {
		t.Fatalf("stored comment = %q", got)
	}
}

// TestMCPServeStdio runs the stdio loop: one reply per request, none for
// a notification, and a parse error with a null id.
func TestMCPServeStdio(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":3,"method":"nope"}`,
	}, "\n")
	var out strings.Builder
	server := &mcpServer{out: bufio.NewWriter(&out)}
	if err := server.serve(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	var replies []rpcMessage
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		replies = append(replies, msg)
	}
	if len(replies) != 4 {
		t.Fatalf("%d replies, want 4: %s", len(replies), out.String())
	}
	if string(replies[2].ID) != "null" || replies[2].Error == nil || replies[2].Error.Code != -32700 {
		t.Errorf("parse error reply = %+v", replies[2])
	}
	if replies[3].Error == nil || replies[3].Error.Code != -32601 {
		t.Errorf("unknown method reply = %+v", replies[3])
	}
}
