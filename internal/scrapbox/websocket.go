package scrapbox

import (
	"crypto/rand"
	"encoding/hex"
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

// userIDSuffix returns the last 6 characters of userID for line ID generation.
// This matches the Scrapbox line ID format.
func userIDSuffix(userID string) string {
	if len(userID) < 6 {
		return userID
	}
	return userID[len(userID)-6:]
}

// createLineId generates a new line ID in Scrapbox format.
// Format: 8-char timestamp (seconds, hex) + 6-char userID suffix + 4-char fixed + 8-char random
// Total: 26 characters
func createLineId(userID string) string {
	// 8 characters: current time in seconds (hex)
	timestamp := fmt.Sprintf("%08x", time.Now().Unix())

	// 6 characters: last 6 chars of userID
	userSuffix := userIDSuffix(userID)

	// 4 characters: fixed padding
	fixed := "0000"

	// 8 characters: random hex
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomHex := hex.EncodeToString(randomBytes)

	return timestamp + userSuffix + fixed + randomHex
}

// WebSocketClient handles WebSocket connections for write operations
type WebSocketClient struct {
	wsURL       string
	projectName string
	cookie      string
	conn        *websocket.Conn
	mu          sync.Mutex
	connected   bool
	ackID       int
	ackChan     chan []byte
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(wsURL, projectName, cookie string) *WebSocketClient {
	return &WebSocketClient{
		wsURL:       wsURL,
		projectName: projectName,
		cookie:      cookie,
		ackChan:     make(chan []byte, 1),
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
	wsc.ackID = 0

	// Handle Engine.IO handshake
	if err := wsc.handleHandshake(); err != nil {
		wsc.conn.Close()
		wsc.connected = false
		return err
	}

	// Start message handler
	go wsc.messageHandler()

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

	// Response should start with "40" (can be "40" or "40{...}")
	if len(response) < 2 || response[0] != '4' || response[1] != '0' {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, fmt.Sprintf("Invalid connect response: %s", string(response)), nil)
	}

	return nil
}

// messageHandler handles incoming messages
func (wsc *WebSocketClient) messageHandler() {
	for wsc.connected {
		_, message, err := wsc.conn.ReadMessage()
		if err != nil {
			wsc.connected = false
			return
		}

		if len(message) == 0 {
			continue
		}

		// Engine.IO ping packet (type 2)
		if message[0] == '2' {
			wsc.mu.Lock()
			wsc.conn.WriteMessage(websocket.TextMessage, []byte("3"))
			wsc.mu.Unlock()
			continue
		}

		// Socket.IO ACK packet (type 43)
		if len(message) >= 2 && message[0] == '4' && message[1] == '3' {
			select {
			case wsc.ackChan <- message:
			default:
			}
		}
	}
}

// diffToChanges computes the changes needed to transform oldLines into newLines.
// It generates _insert, _update, and _delete operations.
// The algorithm processes line by line, tracking positions in both old and new arrays.
func diffToChanges(oldLines []Line, newTexts []string, userID string) []map[string]interface{} {
	changes := make([]map[string]interface{}, 0)

	oldLen := len(oldLines)
	newLen := len(newTexts)

	// Track the last valid line ID for insertion chaining
	// This is used when inserting multiple consecutive lines
	var lastLineID string
	if oldLen > 0 {
		lastLineID = oldLines[oldLen-1].ID
	}

	// First pass: handle updates and track which old lines to keep
	// For simplicity, we use a position-based approach:
	// - Lines at same position with different text -> update
	// - Extra new lines -> insert
	// - Extra old lines -> delete

	minLen := oldLen
	if newLen < minLen {
		minLen = newLen
	}

	// Update existing lines where text differs
	for i := 0; i < minLen; i++ {
		if oldLines[i].Text != newTexts[i] {
			changes = append(changes, map[string]interface{}{
				"_update": oldLines[i].ID,
				"lines": map[string]interface{}{
					"text": newTexts[i],
				},
			})
		}
	}

	// Delete extra old lines (from end to avoid index issues)
	for i := oldLen - 1; i >= newLen; i-- {
		changes = append(changes, map[string]interface{}{
			"_delete": oldLines[i].ID,
			"lines":   -1,
		})
	}

	// Insert extra new lines
	if newLen > oldLen {
		// Determine insert position
		if oldLen > 0 {
			lastLineID = oldLines[oldLen-1].ID
		}

		for i := oldLen; i < newLen; i++ {
			newLineID := createLineId(userID)
			changes = append(changes, map[string]interface{}{
				"_insert": lastLineID,
				"lines": map[string]interface{}{
					"id":   newLineID,
					"text": newTexts[i],
				},
			})
			// Chain: next insert happens after this new line
			lastLineID = newLineID
		}
	}

	return changes
}

// PatchPage applies a patch to a page using diff-based changes.
// This is the core function that computes the diff between old and new content
// and generates the appropriate _insert, _update, _delete operations.
func (wsc *WebSocketClient) PatchPage(page *Page, projectID, userID string, newTexts []string) error {
	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
	}

	// Generate changes using diff
	changes := diffToChanges(page.Lines, newTexts, userID)

	if len(changes) == 0 {
		// No changes needed
		return nil
	}

	// Build commit data
	commitData := map[string]interface{}{
		"kind":      "page",
		"projectId": projectID,
		"pageId":    page.ID,
		"parentId":  page.CommitID,
		"userId":    userID,
		"changes":   changes,
		"cursor":    nil,
		"freeze":    true,
	}

	// Build socket.io-request payload
	payload := map[string]interface{}{
		"method": "commit",
		"data":   commitData,
	}

	reqBody := []interface{}{"socket.io-request", payload}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to marshal request", err)
	}

	// Socket.IO EVENT packet with ACK: 42<ackId>["socket.io-request", {...}]
	wsc.mu.Lock()
	wsc.ackID++
	ackID := wsc.ackID
	packet := fmt.Sprintf("42%d%s", ackID, string(reqJSON))
	err = wsc.conn.WriteMessage(websocket.TextMessage, []byte(packet))
	wsc.mu.Unlock()

	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to send commit", err)
	}

	// Wait for ACK response
	select {
	case ackMsg := <-wsc.ackChan:
		// Parse ACK response: 43<ackId>[{...}]
		if len(ackMsg) > 3 {
			// Find the JSON array start
			jsonStart := 2
			for jsonStart < len(ackMsg) && ackMsg[jsonStart] >= '0' && ackMsg[jsonStart] <= '9' {
				jsonStart++
			}
			if jsonStart < len(ackMsg) {
				var ackData []map[string]interface{}
				if err := json.Unmarshal(ackMsg[jsonStart:], &ackData); err == nil && len(ackData) > 0 {
					if errData, ok := ackData[0]["error"]; ok {
						errJSON, _ := json.Marshal(errData)
						return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, fmt.Sprintf("Commit error: %s", string(errJSON)), nil)
					}
				}
			}
		}
		return nil
	case <-time.After(30 * time.Second):
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Timeout waiting for commit response", nil)
	}
}

// InsertLines inserts lines into a page after a target line.
// If targetLine is empty, lines are appended to the end.
// This uses the diff-based approach to properly handle line changes.
func (wsc *WebSocketClient) InsertLines(page *Page, projectID, userID, targetLine string, newLines []string) error {
	// Build the new content by inserting lines at the appropriate position
	var newTexts []string

	if targetLine == "" {
		// Append to end: keep all existing lines and add new ones
		for _, line := range page.Lines {
			newTexts = append(newTexts, line.Text)
		}
		newTexts = append(newTexts, newLines...)
	} else {
		// Find target line and insert after it
		inserted := false
		for _, line := range page.Lines {
			newTexts = append(newTexts, line.Text)
			if line.Text == targetLine && !inserted {
				newTexts = append(newTexts, newLines...)
				inserted = true
			}
		}
		// If target not found, append to end
		if !inserted {
			newTexts = append(newTexts, newLines...)
		}
	}

	return wsc.PatchPage(page, projectID, userID, newTexts)
}

// CreatePage creates a new page with the given title and body lines.
// pageID should be the ID obtained from Scrapbox's GetPage API (pre-generated by server).
// This uses the correct line ID format for Scrapbox compatibility.
func (wsc *WebSocketClient) CreatePage(pageID, projectID, userID, title string, bodyLines []string) error {
	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
	}

	// Build all lines for the new page
	allLines := make([]map[string]interface{}, 0, 1+len(bodyLines))

	// Title line (first line) - use correct line ID format
	titleLineID := createLineId(userID)
	allLines = append(allLines, map[string]interface{}{
		"id":   titleLineID,
		"text": title,
	})

	// Body lines - each with unique line ID
	for _, line := range bodyLines {
		lineID := createLineId(userID)
		allLines = append(allLines, map[string]interface{}{
			"id":   lineID,
			"text": line,
		})
		// Small delay to ensure unique timestamps
		time.Sleep(time.Millisecond)
	}

	// For new pages, use a single change that sets all lines
	changes := []map[string]interface{}{
		{
			"title": title,
		},
		{
			"lines": allLines,
		},
	}

	// Build commit data for new page
	commitData := map[string]interface{}{
		"kind":      "page",
		"projectId": projectID,
		"pageId":    pageID,
		"parentId":  nil, // null for new page
		"userId":    userID,
		"changes":   changes,
		"cursor":    nil,
		"freeze":    true,
	}

	// Build socket.io-request payload
	payload := map[string]interface{}{
		"method": "commit",
		"data":   commitData,
	}

	reqBody := []interface{}{"socket.io-request", payload}
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to marshal request", err)
	}

	// Socket.IO EVENT packet with ACK
	wsc.mu.Lock()
	wsc.ackID++
	ackID := wsc.ackID
	packet := fmt.Sprintf("42%d%s", ackID, string(reqJSON))
	err = wsc.conn.WriteMessage(websocket.TextMessage, []byte(packet))
	wsc.mu.Unlock()

	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to send commit", err)
	}

	// Wait for ACK response
	select {
	case ackMsg := <-wsc.ackChan:
		if len(ackMsg) > 3 {
			jsonStart := 2
			for jsonStart < len(ackMsg) && ackMsg[jsonStart] >= '0' && ackMsg[jsonStart] <= '9' {
				jsonStart++
			}
			if jsonStart < len(ackMsg) {
				var ackData []map[string]interface{}
				if err := json.Unmarshal(ackMsg[jsonStart:], &ackData); err == nil && len(ackData) > 0 {
					if errData, ok := ackData[0]["error"]; ok {
						errJSON, _ := json.Marshal(errData)
						return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, fmt.Sprintf("Commit error: %s", string(errJSON)), nil)
					}
				}
			}
		}
		return nil
	case <-time.After(30 * time.Second):
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Timeout waiting for commit response", nil)
	}
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

// InsertLines is a convenience method on Client.
// It inserts lines into a page after a specified target line.
// If targetLine is empty, lines are appended to the end.
func (c *Client) InsertLines(pageTitle, targetLine string, newLines []string) error {
	// Get the current page
	page, err := c.RESTClient.GetPage(c.ProjectName, pageTitle)
	if err != nil {
		return err
	}

	// Get user ID
	user, err := c.RESTClient.GetMe()
	if err != nil {
		return err
	}

	// Get project ID
	projectInfo, err := c.RESTClient.GetProject(c.ProjectName)
	if err != nil {
		return err
	}

	// Parse newLines if it's a single string with newlines
	lines := newLines
	if len(newLines) == 1 && strings.Contains(newLines[0], "\n") {
		lines = strings.Split(newLines[0], "\n")
	}

	// Insert via WebSocket using diff-based approach
	return c.WebSocketClient.InsertLines(page, projectInfo.ID, user.ID, targetLine, lines)
}

// PatchPage is a convenience method on Client.
// It replaces the entire page content with new lines.
// The first line in newTexts becomes the page title.
func (c *Client) PatchPage(pageTitle string, newTexts []string) error {
	// Get the current page
	page, err := c.RESTClient.GetPage(c.ProjectName, pageTitle)
	if err != nil {
		return err
	}

	// Get user ID
	user, err := c.RESTClient.GetMe()
	if err != nil {
		return err
	}

	// Get project ID
	projectInfo, err := c.RESTClient.GetProject(c.ProjectName)
	if err != nil {
		return err
	}

	// Patch via WebSocket using diff-based approach
	return c.WebSocketClient.PatchPage(page, projectInfo.ID, user.ID, newTexts)
}

// CreatePage is a convenience method on Client to create a new page.
// If the page already exists, it updates the page content instead.
func (c *Client) CreatePage(title string, bodyLines []string) error {
	// Get page info - Scrapbox returns page info even for non-existent pages
	existingPage, err := c.RESTClient.GetPage(c.ProjectName, title)
	if err != nil {
		return err
	}

	// Parse bodyLines if it's a single string with newlines
	lines := bodyLines
	if len(bodyLines) == 1 && strings.Contains(bodyLines[0], "\n") {
		lines = strings.Split(bodyLines[0], "\n")
	}

	// Get user ID
	user, err := c.RESTClient.GetMe()
	if err != nil {
		return err
	}

	// Get project ID
	projectInfo, err := c.RESTClient.GetProject(c.ProjectName)
	if err != nil {
		return err
	}

	// If page exists (has commitId), update it using PatchPage
	if existingPage.CommitID != "" {
		// Build new content: title + body lines
		newTexts := []string{title}
		newTexts = append(newTexts, lines...)
		return c.WebSocketClient.PatchPage(existingPage, projectInfo.ID, user.ID, newTexts)
	}

	// New page: create with all lines at once
	return c.WebSocketClient.CreatePage(existingPage.ID, projectInfo.ID, user.ID, title, lines)
}
