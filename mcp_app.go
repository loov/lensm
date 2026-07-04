package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

func (ui *FileUI) startMCP() {
	if ui.MCP != nil {
		return
	}
	server, err := StartAppMCPServer(ui.Config.CommentsPath)
	if err != nil {
		ui.LoadError = fmt.Errorf("unable to start MCP server: %w", err)
		ui.invalidateMain()
		return
	}
	ui.MCP = server
	if ui.File != nil {
		ui.MCP.SetPath(ui.Config.Path, ui.Comments)
	}
	fmt.Fprintf(os.Stderr, "lensm MCP server listening at %s\n", server.URL())
	ui.invalidateMain()
}

func (ui *FileUI) stopMCP() {
	if ui.MCP == nil {
		return
	}
	if err := ui.MCP.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "unable to stop MCP server: %v\n", err)
	}
	ui.MCP = nil
	ui.invalidateMain()
}

type AppMCPServer struct {
	httpServer   *http.Server
	url          string
	commentsPath string

	// mu guards the fields below.
	mu         sync.Mutex
	session    *Session
	loadError  error
	generation uint64
	// active counts in-flight requests using session; a replaced
	// session is closed only once they have finished.
	active *sync.WaitGroup
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
		active:       &sync.WaitGroup{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleHTTP)
	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
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
	old, oldActive := server.session, server.active
	server.session = nil
	server.loadError = nil
	server.active = &sync.WaitGroup{}
	server.mu.Unlock()
	// SetPath runs on the UI event loop: don't block on in-flight requests.
	closeSessionWhenIdle(old, oldActive)

	if path == "" {
		return
	}

	go func() {
		session, err := NewSessionWithComments(path, commentsPath, comments)
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
	old, oldActive := server.session, server.active
	server.session = nil
	server.loadError = nil
	server.active = &sync.WaitGroup{}
	server.mu.Unlock()
	if old != nil {
		if oldActive != nil {
			oldActive.Wait()
		}
		_ = old.Close()
	}
	if server.httpServer == nil {
		return nil
	}
	return server.httpServer.Close()
}

func (server *AppMCPServer) replaceSession(generation uint64, session *Session, loadErr error) {
	server.mu.Lock()
	if generation != server.generation {
		server.mu.Unlock()
		if session != nil {
			_ = session.Close()
		}
		return
	}
	old, oldActive := server.session, server.active
	server.session = session
	server.loadError = loadErr
	server.active = &sync.WaitGroup{}
	server.mu.Unlock()
	closeSessionWhenIdle(old, oldActive)
}

// closeSessionWhenIdle closes session once in-flight requests holding it
// have finished, without blocking the caller.
func closeSessionWhenIdle(session *Session, active *sync.WaitGroup) {
	if session == nil {
		return
	}
	go func() {
		if active != nil {
			active.Wait()
		}
		_ = session.Close()
	}()
}

func (server *AppMCPServer) handleHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/" && req.URL.Path != "/mcp" {
		http.NotFound(w, req)
		return
	}
	// The MCP streamable-HTTP spec requires validating Origin (and, for a
	// loopback server, Host) to block DNS-rebinding pages from driving the
	// server; the CORS headers below are advisory only.
	if !isLoopbackHost(req.Host) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if origin := req.Header.Get("Origin"); origin != "" && !isLoopbackOrigin(origin) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
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
			// JSON-RPC 2.0 requires "id": null when the request id is
			// unknowable, otherwise strict clients cannot correlate it.
			ID:    json.RawMessage("null"),
			Error: &rpcError{Code: -32700, Message: "parse error"},
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
	session, loadErr, release := server.acquireSession()
	defer release()
	return (&mcpServer{session: session, loadErr: loadErr}).handle(msg)
}

// acquireSession snapshots the current session and keeps it open until
// release is called. The server lock is not held while a request is
// handled, so concurrent requests and SetPath never wait on a handler.
func (server *AppMCPServer) acquireSession() (session *Session, loadErr error, release func()) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.active == nil {
		server.active = &sync.WaitGroup{}
	}
	active := server.active
	active.Add(1)
	return server.session, server.loadError, active.Done
}

// isLoopbackHost reports whether a Host header (optionally host:port)
// refers to this machine.
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackOrigin reports whether an Origin header value refers to a
// page served from this machine.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return isLoopbackHost(u.Host)
}
