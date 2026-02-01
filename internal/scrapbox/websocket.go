package scrapbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	mcperrors "github.com/hiroki/scrapbox_mcp/pkg/errors"
)

// WebSocketClient handles WebSocket connections for write operations
type WebSocketClient struct {
	wsURL       string
	projectName string
	cookie      string
	conn        *websocket.Conn
	mu          sync.Mutex
	connected   bool
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(wsURL, projectName, cookie string) *WebSocketClient {
	return &WebSocketClient{
		wsURL:       wsURL,
		projectName: projectName,
		cookie:      cookie,
	}
}

// Connect establishes a WebSocket connection with Socket.IO protocol
func (wsc *WebSocketClient) Connect() error {
	wsc.mu.Lock()
	defer wsc.mu.Unlock()

	if wsc.connected && wsc.conn != nil {
		return nil
	}

	// Build WebSocket URL with Engine.IO parameters
	u, err := url.Parse(wsc.wsURL)
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Invalid WebSocket URL", err)
	}

	q := u.Query()
	q.Set("EIO", "4")
	q.Set("transport", "websocket")
	u.RawQuery = q.Encode()

	// Prepare headers with authentication cookie
	header := http.Header{}
	if wsc.cookie != "" {
		header.Set("Cookie", fmt.Sprintf("connect.sid=%s", wsc.cookie))
	}

	// Establish WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to connect to WebSocket", err)
	}

	wsc.conn = conn
	wsc.connected = true

	// Handle Engine.IO handshake
	if err := wsc.handleHandshake(); err != nil {
		wsc.conn.Close()
		wsc.connected = false
		return err
	}

	// Start heartbeat handler
	go wsc.heartbeatHandler()

	return nil
}

// handleHandshake processes the Engine.IO handshake
func (wsc *WebSocketClient) handleHandshake() error {
	// Read Engine.IO open packet (type 0)
	_, message, err := wsc.conn.ReadMessage()
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to read handshake", err)
	}

	// Message should start with "0{...}"
	if len(message) < 2 || message[0] != '0' {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Invalid handshake packet", nil)
	}

	// Send Socket.IO CONNECT packet (type 40)
	if err := wsc.conn.WriteMessage(websocket.TextMessage, []byte("40")); err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to send connect packet", err)
	}

	// Wait for Socket.IO CONNECT response
	_, response, err := wsc.conn.ReadMessage()
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to read connect response", err)
	}

	// Response should be "40"
	if string(response) != "40" {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Invalid connect response", nil)
	}

	return nil
}

// heartbeatHandler maintains the connection with ping/pong
func (wsc *WebSocketClient) heartbeatHandler() {
	for wsc.connected {
		_, message, err := wsc.conn.ReadMessage()
		if err != nil {
			wsc.connected = false
			return
		}

		// Engine.IO ping packet (type 2)
		if len(message) > 0 && message[0] == '2' {
			// Respond with pong (type 3)
			wsc.mu.Lock()
			wsc.conn.WriteMessage(websocket.TextMessage, []byte("3"))
			wsc.mu.Unlock()
		}
	}
}

// InsertLines inserts lines into a page
func (wsc *WebSocketClient) InsertLines(page *Page, targetLine string, newLines []string) error {
	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
	}

	// Find target line index
	lineIndex := -1
	for i, line := range page.Lines {
		if line.Text == targetLine {
			lineIndex = i
			break
		}
	}

	// If not found, append to end
	if lineIndex == -1 {
		lineIndex = len(page.Lines) - 1
	}

	// Build changes for Socket.IO commit event
	changes := make([]map[string]interface{}, 0)
	for i, newLine := range newLines {
		lineID := fmt.Sprintf("%x", time.Now().UnixNano()/1e6+int64(i))
		change := map[string]interface{}{
			"_insert": lineID,
			"lines": map[string]interface{}{
				"id":   lineID,
				"text": newLine,
			},
			"position": lineIndex + i + 1,
		}
		changes = append(changes, change)
	}

	// Build Socket.IO EVENT packet
	event := map[string]interface{}{
		"kind":      "page",
		"projectId": wsc.projectName,
		"pageId":    page.ID,
		"parentId":  page.CommitID,
		"changes":   changes,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to marshal event", err)
	}

	// Socket.IO EVENT packet format: 42["commit",{...}]
	packet := fmt.Sprintf(`42["commit",%s]`, string(eventJSON))

	// Send packet
	wsc.mu.Lock()
	err = wsc.conn.WriteMessage(websocket.TextMessage, []byte(packet))
	wsc.mu.Unlock()

	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to send commit event", err)
	}

	// Wait briefly for acknowledgment (simplified - a full implementation would parse the ack)
	time.Sleep(500 * time.Millisecond)

	return nil
}

// Close closes the WebSocket connection
func (wsc *WebSocketClient) Close() error {
	wsc.mu.Lock()
	defer wsc.mu.Unlock()

	if wsc.conn != nil {
		wsc.connected = false
		return wsc.conn.Close()
	}

	return nil
}

// Update the Client type to include WebSocket client
func (c *Client) EnsureWebSocket(wsURL string) {
	if c.WebSocketClient == nil {
		sessionCookie := ""
		if c.RESTClient != nil && c.RESTClient.auth != nil {
			sessionCookie = c.RESTClient.auth.sessionCookie
		}
		c.WebSocketClient = NewWebSocketClient(wsURL, c.ProjectName, sessionCookie)
	}
}

// InsertLines is a convenience method on Client
func (c *Client) InsertLines(pageTitle, targetLine string, newLines []string) error {
	// Get the current page
	page, err := c.RESTClient.GetPage(c.ProjectName, pageTitle)
	if err != nil {
		return err
	}

	// Parse newLines if it's a single string with newlines
	lines := newLines
	if len(newLines) == 1 && strings.Contains(newLines[0], "\n") {
		lines = strings.Split(newLines[0], "\n")
	}

	// Insert via WebSocket
	return c.WebSocketClient.InsertLines(page, targetLine, lines)
}
