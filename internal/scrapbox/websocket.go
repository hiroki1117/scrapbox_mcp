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

// userIDPrefix returns the first 5 characters of userID for line ID generation.
// Returns the full string if shorter than 5 characters.
func userIDPrefix(userID string) string {
	if len(userID) < 5 {
		return userID
	}
	return userID[:5]
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

// InsertLines inserts lines into a page using socket.io-request protocol
func (wsc *WebSocketClient) InsertLines(page *Page, projectID, userID, targetLine string, newLines []string) error {
	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
	}

	// Find target line - the line AFTER which we insert
	var insertAfterLineID string
	if targetLine == "" {
		// Append to end: use last line's ID
		if len(page.Lines) > 0 {
			insertAfterLineID = page.Lines[len(page.Lines)-1].ID
		}
	} else {
		// Find the target line
		for _, line := range page.Lines {
			if line.Text == targetLine {
				insertAfterLineID = line.ID
				break
			}
		}
		// If not found, append to end
		if insertAfterLineID == "" && len(page.Lines) > 0 {
			insertAfterLineID = page.Lines[len(page.Lines)-1].ID
		}
	}

	if insertAfterLineID == "" {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "No lines found in page", nil)
	}

	// Build changes for commit
	changes := make([]map[string]interface{}, 0)
	currentInsertAfter := insertAfterLineID
	for i, newLine := range newLines {
		// Generate new line ID: userID prefix + timestamp
		newLineID := fmt.Sprintf("%s%x", userIDPrefix(userID), time.Now().UnixNano()/1e6+int64(i))
		change := map[string]interface{}{
			"_insert": currentInsertAfter, // Insert AFTER this existing line
			"lines": map[string]interface{}{
				"id":   newLineID, // ID for the NEW line
				"text": newLine,
			},
		}
		changes = append(changes, change)
		// Next line should be inserted after this new line
		currentInsertAfter = newLineID
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

// CreatePage creates a new page with the given title and body lines
// pageID should be the ID obtained from Scrapbox's GetPage API (pre-generated by server)
func (wsc *WebSocketClient) CreatePage(pageID, projectID, userID, title string, bodyLines []string) error {
	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
	}

	// Build all lines for the new page
	// For new pages, we use a single "lines" change that sets all lines at once
	allLines := make([]map[string]interface{}, 0, 1+len(bodyLines))

	// Title line (first line)
	baseTime := time.Now().UnixNano() / 1e6
	titleLineID := fmt.Sprintf("%s%x", userIDPrefix(userID), baseTime)
	allLines = append(allLines, map[string]interface{}{
		"id":   titleLineID,
		"text": title,
	})

	// Body lines
	for i, line := range bodyLines {
		lineID := fmt.Sprintf("%s%x", userIDPrefix(userID), baseTime+int64(i+1))
		allLines = append(allLines, map[string]interface{}{
			"id":   lineID,
			"text": line,
		})
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
		"title":     title,
		"titleLc":   strings.ToLower(title),
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

// InsertLines is a convenience method on Client
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

	// Insert via WebSocket
	return c.WebSocketClient.InsertLines(page, projectInfo.ID, user.ID, targetLine, lines)
}

// CreatePage is a convenience method on Client to create a new page
func (c *Client) CreatePage(title string, bodyLines []string) error {
	// Get page info - Scrapbox returns page info even for non-existent pages
	// A truly new page has an empty commitId
	existingPage, err := c.RESTClient.GetPage(c.ProjectName, title)
	if err != nil {
		return err
	}

	if existingPage.CommitID != "" {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, fmt.Sprintf("Page already exists: %s", title), nil)
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

	// Parse bodyLines if it's a single string with newlines
	lines := bodyLines
	if len(bodyLines) == 1 && strings.Contains(bodyLines[0], "\n") {
		lines = strings.Split(bodyLines[0], "\n")
	}

	// Step 1: Create the page with title only
	if err := c.WebSocketClient.CreatePage(existingPage.ID, projectInfo.ID, user.ID, title, nil); err != nil {
		return err
	}

	// Step 2: If there are body lines, insert them after the title
	if len(lines) > 0 {
		// Wait briefly for the page to be committed
		time.Sleep(500 * time.Millisecond)

		// Get the newly created page to get updated page info
		createdPage, err := c.RESTClient.GetPage(c.ProjectName, title)
		if err != nil {
			return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Page created but failed to add body: "+err.Error(), nil)
		}

		// Insert body lines after the title line
		if err := c.WebSocketClient.InsertLines(createdPage, projectInfo.ID, user.ID, title, lines); err != nil {
			return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Page created but failed to add body: "+err.Error(), nil)
		}
	}

	return nil
}
