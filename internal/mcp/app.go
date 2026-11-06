package mcp

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

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

type AppServer struct {
	httpServer *http.Server
	url        string

	// mu guards the fields below.
	mu      sync.Mutex
	session *Session
	// active counts in-flight requests using session; a replaced
	// session is closed only once they have finished.
	active *sync.WaitGroup
}

func StartAppServer() (*AppServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:7077")
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return nil, err
	}

	server := &AppServer{
		url:    "http://" + listener.Addr().String() + "/mcp",
		active: &sync.WaitGroup{},
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

func (server *AppServer) URL() string {
	if server == nil {
		return ""
	}
	return server.url
}

// SetFile serves file, the binary at path, with comments in store. The
// server takes over the caller's reference to file and closes it once
// it is replaced and no request uses it. A nil file stops serving.
// SetFile doesn't block on requests in flight.
func (server *AppServer) SetFile(path string, file disasm.File, store *comments.Store) {
	if server == nil {
		if file != nil {
			_ = file.Close()
		}
		return
	}
	var session *Session
	if file != nil {
		session = &Session{Path: comments.CleanPath(path), File: file, Comments: store}
	}
	server.mu.Lock()
	old, oldActive := server.swapSessionLocked(session)
	server.mu.Unlock()
	closeSessionWhenIdle(old, oldActive)
}

func (server *AppServer) Close() error {
	if server == nil {
		return nil
	}
	server.mu.Lock()
	old, oldActive := server.swapSessionLocked(nil)
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

// swapSessionLocked installs session and returns the
// previous session with the WaitGroup of requests still using it, for
// the caller to close once idle. Caller holds server.mu.
func (server *AppServer) swapSessionLocked(session *Session) (old *Session, oldActive *sync.WaitGroup) {
	old, oldActive = server.session, server.active
	server.session = session
	server.active = &sync.WaitGroup{}
	return old, oldActive
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

func (server *AppServer) handleHTTP(w http.ResponseWriter, req *http.Request) {
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

func (server *AppServer) handleHTTPPost(w http.ResponseWriter, req *http.Request) {
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

func (server *AppServer) handleHTTPMessage(msg rpcMessage) (rpcMessage, bool) {
	session, release := server.acquireSession()
	defer release()
	return (&mcpServer{session: session}).handle(msg)
}

// acquireSession snapshots the current session and keeps it open until
// release is called. The server lock is not held while a request is
// handled, so concurrent requests and SetFile never wait on a handler.
func (server *AppServer) acquireSession() (session *Session, release func()) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.active == nil {
		server.active = &sync.WaitGroup{}
	}
	active := server.active
	active.Add(1)
	return server.session, active.Done
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
