package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const mcpProtocolVersion = "2025-06-18"
const defaultMCPSourceContext = 3

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpTool struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type mcpToolResult struct {
	Content           []mcpTextContent `json:"content"`
	StructuredContent any              `json:"structuredContent,omitempty"`
	IsError           bool             `json:"isError,omitempty"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpServer struct {
	session *LensmSession
	loadErr error
	out     *bufio.Writer
}

type AppMCPServer struct {
	mu           sync.RWMutex
	httpServer   *http.Server
	url          string
	commentsPath string
	session      *LensmSession
	loadError    error
	generation   uint64
}

func StartAppMCPServer(commentsPath string) (*AppMCPServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:7077")
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return nil, err
	}

	server := &AppMCPServer{
		url:          "http://" + listener.Addr().String() + "/mcp",
		commentsPath: commentsPath,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleHTTP)
	httpServer := &http.Server{Handler: mux}
	server.httpServer = httpServer

	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "lensm MCP server stopped: %v\n", err)
		}
	}()
	return server, nil
}

func (server *AppMCPServer) URL() string {
	if server == nil {
		return ""
	}
	return server.url
}

func (server *AppMCPServer) SetPath(path string, comments *CommentStore) {
	if server == nil {
		return
	}
	path = cleanPath(path)

	server.mu.Lock()
	server.generation++
	generation := server.generation
	commentsPath := server.commentsPath
	old := server.session
	server.session = nil
	server.loadError = nil
	server.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	if path == "" {
		return
	}

	go func() {
		session, err := NewLensmSessionWithComments(path, commentsPath, comments)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to load MCP session for %q: %v\n", path, err)
			server.replaceSession(generation, nil, err)
			return
		}
		server.replaceSession(generation, session, nil)
	}()
}

func (server *AppMCPServer) Close() error {
	if server == nil {
		return nil
	}
	server.mu.Lock()
	server.generation++
	old := server.session
	server.session = nil
	server.loadError = nil
	server.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	if server.httpServer == nil {
		return nil
	}
	return server.httpServer.Close()
}

func (server *AppMCPServer) replaceSession(generation uint64, session *LensmSession, loadErr error) {
	server.mu.Lock()
	if generation != server.generation {
		server.mu.Unlock()
		if session != nil {
			_ = session.Close()
		}
		return
	}
	old := server.session
	server.session = session
	server.loadError = loadErr
	server.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func (server *AppMCPServer) handleHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" && req.URL.Path != "/mcp" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Mcp-Session-Id")

	switch req.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "lensm",
			"mcp":  server.url,
		})
	case http.MethodPost:
		server.handleHTTPPost(w, req)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *AppMCPServer) handleHTTPPost(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	data, err := io.ReadAll(io.LimitReader(req.Body, 64*1024*1024))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var msg rpcMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(rpcMessage{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}

	response, ok := server.handleHTTPMessage(msg)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (server *AppMCPServer) handleHTTPMessage(msg rpcMessage) (rpcMessage, bool) {
	server.mu.RLock()
	defer server.mu.RUnlock()
	return (&mcpServer{session: server.session, loadErr: server.loadError}).handle(msg)
}

func runMCPCommand(args []string) int {
	fs := flag.NewFlagSet("lensm mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	commentsPath := fs.String("comments", "", "comments sidecar path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: lensm mcp [-comments path] <exePath>")
		return 2
	}

	workInProgressWASM = os.Getenv("LENSM_EXPERIMENT_WASM") != ""

	session, err := NewLensmSession(fs.Arg(0), *commentsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer session.Close()

	server := &mcpServer{
		session: session,
		out:     bufio.NewWriter(os.Stdout),
	}
	if err := server.serve(os.Stdin); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func (server *mcpServer) serve(input io.Reader) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			_ = server.writeMessage(rpcMessage{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}
		response, ok := server.handle(msg)
		if ok {
			if err := server.writeMessage(response); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func (server *mcpServer) writeMessage(msg rpcMessage) error {
	msg.JSONRPC = "2.0"
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := server.out.Write(data); err != nil {
		return err
	}
	if err := server.out.WriteByte('\n'); err != nil {
		return err
	}
	return server.out.Flush()
}

func (server *mcpServer) handle(msg rpcMessage) (rpcMessage, bool) {
	if len(msg.ID) == 0 {
		return rpcMessage{}, false
	}
	response := rpcMessage{JSONRPC: "2.0", ID: msg.ID}
	result, err := server.handleRequest(msg.Method, msg.Params)
	if err != nil {
		response.Error = err
		return response, true
	}
	response.Result = result
	return response, true
}

func (server *mcpServer) handleRequest(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    "lensm",
				"title":   "lensm",
				"version": "dev",
			},
			"instructions": "Use tools to inspect Go source, Go assembly, native assembly, and comments for the loaded executable.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools()}, nil
	case "tools/call":
		return server.handleToolCall(params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func (server *mcpServer) handleToolCall(params json.RawMessage) (any, *rpcError) {
	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := decodeJSON(params, &req); err != nil {
		return nil, invalidParams(err)
	}
	if len(req.Arguments) == 0 {
		req.Arguments = []byte("{}")
	}
	if server.session == nil {
		if server.loadErr != nil {
			return toolError(server.loadErr), nil
		}
		return toolError(errors.New("no executable loaded")), nil
	}

	var (
		result any
		err    error
	)
	switch req.Name {
	case "list_functions":
		result, err = server.toolListFunctions(req.Arguments)
	case "get_function":
		result, err = server.toolGetFunction(req.Arguments)
	case "set_comment":
		result, err = server.toolSetComment(req.Arguments)
	case "get_comments":
		result, err = server.toolGetComments(req.Arguments)
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool: " + req.Name}
	}
	if err != nil {
		return toolError(err), nil
	}
	return toolSuccess(result), nil
}

func (server *mcpServer) toolListFunctions(args json.RawMessage) (any, error) {
	var req struct {
		Filter string `json:"filter"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := decodeJSON(args, &req); err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	var rx *regexp.Regexp
	if req.Filter != "" {
		var err error
		rx, err = regexp.Compile("(?i)" + req.Filter)
		if err != nil {
			return nil, err
		}
	}

	type functionInfo struct {
		Index int    `json:"index"`
		Name  string `json:"name"`
	}
	var all []functionInfo
	for i, fn := range server.session.Funcs() {
		name := fn.Name()
		if rx != nil && !rx.MatchString(name) {
			continue
		}
		all = append(all, functionInfo{Index: i, Name: name})
	}

	end := req.Offset + req.Limit
	if end > len(all) {
		end = len(all)
	}
	var page []functionInfo
	if req.Offset < len(all) {
		page = all[req.Offset:end]
	}
	return map[string]any{
		"binary":    server.session.Path,
		"functions": page,
		"total":     len(all),
		"offset":    req.Offset,
		"limit":     req.Limit,
	}, nil
}

func (server *mcpServer) toolGetFunction(args json.RawMessage) (any, error) {
	var req struct {
		Name    string `json:"name"`
		Context *int   `json:"context"`
	}
	if err := decodeJSON(args, &req); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	context := defaultMCPSourceContext
	if req.Context != nil {
		context = *req.Context
	}
	if context < 0 {
		return nil, errors.New("context must be non-negative")
	}
	code, err := server.session.LoadCode(req.Name, context)
	if err != nil {
		return nil, err
	}
	return BuildFunctionCodeDTO(server.session.Path, code, server.session.Comments), nil
}

func (server *mcpServer) toolSetComment(args json.RawMessage) (any, error) {
	var req struct {
		Name string          `json:"name"`
		View CommentView     `json:"view"`
		File string          `json:"file"`
		Line int             `json:"line"`
		PC   json.RawMessage `json:"pc"`
		Text string          `json:"text"`
	}
	if err := decodeJSON(args, &req); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	code, err := server.session.LoadCode(req.Name, 0)
	if err != nil {
		return nil, err
	}

	var coord CommentCoord
	switch req.View {
	case CommentViewSource:
		if !sourceLineExists(code, req.File, req.Line) {
			return nil, fmt.Errorf("source line %s:%d is not present in function %q", req.File, req.Line, req.Name)
		}
		coord = CommentCoord{
			Function: req.Name,
			View:     CommentViewSource,
			File:     req.File,
			Line:     req.Line,
		}
	case CommentViewGoAsm, CommentViewNativeAsm:
		pc, ok, err := parsePC(req.PC)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("pc is required for asm comments")
		}
		if !asmPCExists(code, req.View, pc) {
			return nil, fmt.Errorf("pc %s is not present in %s for function %q", formatPC(pc), req.View, req.Name)
		}
		coord = CommentCoord{
			Function: req.Name,
			View:     req.View,
			PC:       pc,
		}
	default:
		return nil, fmt.Errorf("unsupported view %q", req.View)
	}

	if err := server.session.Comments.Set(coord, req.Text); err != nil {
		return nil, err
	}
	coord = server.session.Comments.normalize(coord)
	return map[string]any{
		"comment": coord,
		"deleted": strings.TrimSpace(req.Text) == "",
		"path":    server.session.Comments.Path(),
	}, nil
}

func (server *mcpServer) toolGetComments(args json.RawMessage) (any, error) {
	var req struct {
		Name string      `json:"name"`
		View CommentView `json:"view"`
	}
	if err := decodeJSON(args, &req); err != nil {
		return nil, err
	}
	if req.View != "" {
		switch req.View {
		case CommentViewSource, CommentViewGoAsm, CommentViewNativeAsm:
		default:
			return nil, fmt.Errorf("unsupported view %q", req.View)
		}
	}
	return map[string]any{
		"binary":   server.session.Path,
		"path":     server.session.Comments.Path(),
		"comments": server.session.Comments.Filter(req.Name, req.View),
	}, nil
}

func decodeJSON(data []byte, dst any) error {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return dec.Decode(dst)
}

func parsePC(data json.RawMessage) (uint64, bool, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return 0, false, nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		text = strings.TrimSpace(text)
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			base = 16
			text = text[2:]
		}
		pc, err := strconv.ParseUint(text, base, 64)
		if err != nil {
			return 0, true, fmt.Errorf("invalid pc %q", text)
		}
		return pc, true, nil
	}
	var num json.Number
	if err := decodeJSON(data, &num); err != nil {
		return 0, true, fmt.Errorf("invalid pc")
	}
	pc, err := strconv.ParseUint(num.String(), 10, 64)
	if err != nil {
		return 0, true, fmt.Errorf("invalid pc %q", num.String())
	}
	return pc, true, nil
}

func invalidParams(err error) *rpcError {
	return &rpcError{Code: -32602, Message: err.Error()}
}

func toolSuccess(value any) mcpToolResult {
	text, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		text = []byte(fmt.Sprint(value))
	}
	return mcpToolResult{
		Content: []mcpTextContent{{
			Type: "text",
			Text: string(text),
		}},
		StructuredContent: value,
	}
}

func toolError(err error) mcpToolResult {
	return mcpToolResult{
		Content: []mcpTextContent{{
			Type: "text",
			Text: err.Error(),
		}},
		IsError: true,
	}
}

func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "list_functions",
			Title:       "List Functions",
			Description: "List functions in the loaded executable. The optional filter is a case-insensitive Go regexp.",
			InputSchema: objectSchema(map[string]any{
				"filter": stringSchema("Optional case-insensitive regexp matched against function names."),
				"limit":  numberSchema("Maximum number of functions to return. Defaults to 100, capped at 1000."),
				"offset": numberSchema("Number of matching functions to skip."),
			}, nil),
		},
		{
			Name:        "get_function",
			Title:       "Get Function Code",
			Description: "Return Go source, Go assembly, native assembly, source-to-asm mappings, and comments for a function.",
			InputSchema: objectSchema(map[string]any{
				"name":    stringSchema("Exact function name."),
				"context": numberSchema("Number of extra source lines to include before and after referenced lines. Defaults to 3."),
			}, []string{"name"}),
		},
		{
			Name:        "set_comment",
			Title:       "Set Comment",
			Description: "Set or delete a comment for a source line, Go asm instruction, or native asm instruction. Empty text deletes the comment.",
			InputSchema: objectSchema(map[string]any{
				"name": stringSchema("Exact function name."),
				"view": enumSchema("Code view for the comment.", []string{
					string(CommentViewSource),
					string(CommentViewGoAsm),
					string(CommentViewNativeAsm),
				}),
				"file": stringSchema("Source file path. Required for source comments."),
				"line": numberSchema("Source line number. Required for source comments."),
				"pc":   pcSchema(),
				"text": stringSchema("Comment text. Empty string deletes the comment."),
			}, []string{"name", "view", "text"}),
		},
		{
			Name:        "get_comments",
			Title:       "Get Comments",
			Description: "Return comments, optionally filtered by function name and view.",
			InputSchema: objectSchema(map[string]any{
				"name": stringSchema("Optional exact function name."),
				"view": enumSchema("Optional code view filter.", []string{
					string(CommentViewSource),
					string(CommentViewGoAsm),
					string(CommentViewNativeAsm),
				}),
			}, nil),
		},
	}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func numberSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func enumSchema(description string, values []string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func pcSchema() map[string]any {
	return map[string]any{
		"description": "Instruction program counter for asm comments. Accepts an integer or hex string such as 0x1000.",
		"oneOf": []map[string]any{
			{"type": "integer"},
			{"type": "string"},
		},
	}
}
