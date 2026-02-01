package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

type Transport struct {
	handler        *MessageHandler
	sessionManager *SessionManager
	allowedOrigins []string
	enableCORS     bool
}

func NewTransport(handler *MessageHandler, sessionMgr *SessionManager, allowedOrigins []string, enableCORS bool) *Transport {
	return &Transport{
		handler:        handler,
		sessionManager: sessionMgr,
		allowedOrigins: allowedOrigins,
		enableCORS:     enableCORS,
	}
}

func (t *Transport) HandlePOST(w http.ResponseWriter, r *http.Request) {
	// CORS handling
	if t.enableCORS {
		t.setCORSHeaders(w, r)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Validate Origin header for security
	if !t.validateOrigin(r) {
		http.Error(w, "Invalid origin", http.StatusForbidden)
		return
	}

	// Get or create session
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID != "" {
		_, exists := t.sessionManager.Get(sessionID)
		if !exists {
			http.Error(w, "Session not found", http.StatusUnauthorized)
			return
		}
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse JSON-RPC request
	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.sendJSONResponse(w, &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &RPCError{
				Code:    -32700,
				Message: "Parse error",
			},
		})
		return
	}

	// Handle the request
	response := t.handler.HandleRequest(r.Context(), &req, sessionID)

	// For initialize method, create a new session
	if req.Method == "initialize" && response != nil && response.Error == nil {
		if initResult, ok := response.Result.(*InitializeResult); ok {
			newSession := t.sessionManager.Create(initResult)
			w.Header().Set("Mcp-Session-Id", newSession.ID)
		}
	}

	// Send response
	if response != nil {
		t.sendJSONResponse(w, response)
	} else {
		// For notifications, return 204 No Content
		w.WriteHeader(http.StatusNoContent)
	}
}

func (t *Transport) HandleGET(w http.ResponseWriter, r *http.Request) {
	// CORS handling
	if t.enableCORS {
		t.setCORSHeaders(w, r)
	}

	// Validate session
	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusUnauthorized)
		return
	}

	_, exists := t.sessionManager.Get(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusUnauthorized)
		return
	}

	// Set up SSE stream
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// For now, just keep the connection open
	// In a full implementation, this would stream server-initiated messages
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial comment to establish connection
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	// Keep connection alive (in a real implementation, listen for server events)
	<-r.Context().Done()
}

func (t *Transport) HandleDELETE(w http.ResponseWriter, r *http.Request) {
	// CORS handling
	if t.enableCORS {
		t.setCORSHeaders(w, r)
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	t.sessionManager.Delete(sessionID)
	w.WriteHeader(http.StatusNoContent)
}

func (t *Transport) sendJSONResponse(w http.ResponseWriter, response interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func (t *Transport) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && t.isOriginAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id")
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func (t *Transport) validateOrigin(r *http.Request) bool {
	// For localhost, always allow
	host := r.Host
	if strings.HasPrefix(host, "localhost:") || strings.HasPrefix(host, "127.0.0.1:") {
		return true
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		// No origin header in same-origin requests
		return true
	}

	return t.isOriginAllowed(origin)
}

func (t *Transport) isOriginAllowed(origin string) bool {
	if len(t.allowedOrigins) == 0 {
		return true
	}

	for _, allowed := range t.allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}
