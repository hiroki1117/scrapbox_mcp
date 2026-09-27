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

// diffToChanges computes the changes needed to transform oldLines into newTexts.
// It runs a line diff so that only the lines that actually changed are touched,
// keeping line IDs (and their created/updated metadata) of unchanged lines intact.
//
// Within each block of changed lines, deleted and inserted lines are paired as
// _update (keeping the old line ID); the rest become _delete or _insert.
// `_insert: X` inserts BEFORE line X, so all inserts of a block use the next
// unchanged line (or "_end") as the anchor, which keeps them in order.
func diffToChanges(oldLines []Line, newTexts []string, userID string) []map[string]interface{} {
	oldTexts := make([]string, len(oldLines))
	for i, line := range oldLines {
		oldTexts[i] = line.Text
	}

	changes := make([]map[string]interface{}, 0)
	var deleted []int  // old line indexes removed in the current block
	var inserted []int // new text indexes added in the current block

	flush := func(anchor string) {
		paired := len(deleted)
		if len(inserted) < paired {
			paired = len(inserted)
		}
		for i := 0; i < paired; i++ {
			oldLine, newText := oldLines[deleted[i]], newTexts[inserted[i]]
			if oldLine.Text != newText {
				changes = append(changes, updateChange(oldLine.ID, newText))
			}
		}
		for _, idx := range deleted[paired:] {
			changes = append(changes, map[string]interface{}{
				"_delete": oldLines[idx].ID,
				"lines":   -1,
			})
		}
		for _, idx := range inserted[paired:] {
			changes = append(changes, insertChange(anchor, newTexts[idx], userID))
		}
		deleted, inserted = deleted[:0], inserted[:0]
	}

	for _, e := range diffLines(oldTexts, newTexts) {
		switch e.op {
		case opDelete:
			deleted = append(deleted, e.oldIdx)
		case opInsert:
			inserted = append(inserted, e.newIdx)
		case opEqual:
			flush(oldLines[e.oldIdx].ID)
		}
	}
	flush("_end")

	return changes
}

// insertLinesChanges builds _insert changes that add newLines right after the
// first line whose text equals targetLine. If targetLine is empty or not found,
// the lines are appended to the end of the page.
func insertLinesChanges(oldLines []Line, targetLine string, newLines []string, userID string) []map[string]interface{} {
	anchor := "_end"
	if targetLine != "" {
		for i, line := range oldLines {
			if line.Text == targetLine {
				if i+1 < len(oldLines) {
					anchor = oldLines[i+1].ID
				}
				break
			}
		}
	}

	changes := make([]map[string]interface{}, 0, len(newLines))
	for _, text := range newLines {
		changes = append(changes, insertChange(anchor, text, userID))
	}
	return changes
}

func updateChange(lineID, text string) map[string]interface{} {
	return map[string]interface{}{
		"_update": lineID,
		"lines": map[string]interface{}{
			"text": text,
		},
	}
}

func insertChange(anchor, text, userID string) map[string]interface{} {
	return map[string]interface{}{
		"_insert": anchor,
		"lines": map[string]interface{}{
			"id":   createLineId(userID),
			"text": text,
		},
	}
}

// PatchPage applies a patch to a page using diff-based changes.
// This is the core function that computes the diff between old and new content
// and generates the appropriate _insert, _update, _delete operations.
func (wsc *WebSocketClient) PatchPage(page *Page, projectID, userID string, newTexts []string) error {
	return wsc.commitChanges(page, projectID, userID, diffToChanges(page.Lines, newTexts, userID))
}

// InsertLines inserts lines into a page after a target line.
// If targetLine is empty or not found, lines are appended to the end.
// Only _insert changes are sent, so existing lines are left untouched.
func (wsc *WebSocketClient) InsertLines(page *Page, projectID, userID, targetLine string, newLines []string) error {
	return wsc.commitChanges(page, projectID, userID, insertLinesChanges(page.Lines, targetLine, newLines, userID))
}

// commitChanges sends the given changes to the page as a single commit
func (wsc *WebSocketClient) commitChanges(page *Page, projectID, userID string, changes []map[string]interface{}) error {
	if len(changes) == 0 {
		// No changes needed
		return nil
	}

	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
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

	return wsc.sendCommitAndWaitACK(reqJSON)
}

// CreatePage creates a new page with the given title and body lines.
// pageID should be the ID obtained from Scrapbox's GetPage API (pre-generated by server).
// This uses the correct line ID format for Scrapbox compatibility.
func (wsc *WebSocketClient) CreatePage(pageID, projectID, userID, title string, bodyLines []string) error {
	// Ensure connection
	if err := wsc.Connect(); err != nil {
		return err
	}

	// Build changes using _insert operations
	changes := make([]map[string]interface{}, 0, 1+len(bodyLines))

	// Set the title - Scrapbox automatically creates the title line
	changes = append(changes, map[string]interface{}{
		"title": title,
	})

	// Body lines - build insert changes in reverse order
	// Scrapbox processes changes in reverse, so we build them backwards
	bodyChanges := make([]map[string]interface{}, 0, len(bodyLines))
	var lastLineID string
	for i := len(bodyLines) - 1; i >= 0; i-- {
		lineID := createLineId(userID)
		insertPos := "_end"
		if lastLineID != "" {
			insertPos = lastLineID
		}
		bodyChanges = append(bodyChanges, map[string]interface{}{
			"_insert": insertPos,
			"lines": map[string]interface{}{
				"id":   lineID,
				"text": bodyLines[i],
			},
		})
		lastLineID = lineID
		// Small delay to ensure unique timestamps
		time.Sleep(time.Millisecond)
	}
	changes = append(changes, bodyChanges...)

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

	return wsc.sendCommitAndWaitACK(reqJSON)
}

// sendCommitAndWaitACK sends a commit request and waits for ACK response
func (wsc *WebSocketClient) sendCommitAndWaitACK(reqJSON []byte) error {
	// Socket.IO EVENT packet with ACK: 42<ackId>["socket.io-request", {...}]
	wsc.mu.Lock()
	wsc.ackID++
	packet := fmt.Sprintf("42%d%s", wsc.ackID, string(reqJSON))
	err := wsc.conn.WriteMessage(websocket.TextMessage, []byte(packet))
	wsc.mu.Unlock()

	if err != nil {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Failed to send commit", err)
	}

	// Wait for ACK response
	select {
	case ackMsg := <-wsc.ackChan:
		return parseACKError(ackMsg)
	case <-time.After(30 * time.Second):
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Timeout waiting for commit response", nil)
	}
}

// parseACKError parses an ACK message and returns an error if it contains one
func parseACKError(ackMsg []byte) error {
	if len(ackMsg) <= 3 {
		return nil
	}

	// Find the JSON array start (skip "43" and ackId digits)
	jsonStart := 2
	for jsonStart < len(ackMsg) && ackMsg[jsonStart] >= '0' && ackMsg[jsonStart] <= '9' {
		jsonStart++
	}

	if jsonStart >= len(ackMsg) {
		return nil
	}

	var ackData []map[string]interface{}
	if err := json.Unmarshal(ackMsg[jsonStart:], &ackData); err != nil || len(ackData) == 0 {
		return nil
	}

	if errData, ok := ackData[0]["error"]; ok {
		errJSON, err := json.Marshal(errData)
		if err != nil {
			return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, "Commit error (failed to marshal)", nil)
		}
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeWebSocketFail, fmt.Sprintf("Commit error: %s", string(errJSON)), nil)
	}

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
