package scrapbox

import "time"

// Page represents a Scrapbox page
type Page struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Image       string    `json:"image,omitempty"`
	Descriptions []string `json:"descriptions"`
	User        User      `json:"user"`
	Pin         int       `json:"pin"`
	Views       int       `json:"views"`
	Linked      int       `json:"linked"`
	CommitID    string    `json:"commitId"`
	Created     int64     `json:"created"`
	Updated     int64     `json:"updated"`
	Accessed    int64     `json:"accessed"`
	Lines       []Line    `json:"lines"`
}

// Line represents a line in a Scrapbox page
type Line struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	UserID  string `json:"userId,omitempty"`
	Created int64  `json:"created,omitempty"`
	Updated int64  `json:"updated,omitempty"`
}

// User represents a Scrapbox user
type User struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Photo       string `json:"photo"`
}

// PageInfo represents basic page information from list/search
type PageInfo struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Image       string   `json:"image,omitempty"`
	Descriptions []string `json:"descriptions,omitempty"`
	Pin         int      `json:"pin"`
	Views       int      `json:"views"`
	Linked      int      `json:"linked"`
	Created     int64    `json:"created"`
	Updated     int64    `json:"updated"`
	Accessed    int64    `json:"accessed"`
}

// PagesResponse represents the response from /api/pages/:project
type PagesResponse struct {
	ProjectName string     `json:"projectName"`
	Skip        int        `json:"skip"`
	Limit       int        `json:"limit"`
	Count       int        `json:"count"`
	Pages       []PageInfo `json:"pages"`
}

// SearchResponse represents the response from search endpoints
type SearchResponse struct {
	Query string     `json:"query"`
	Pages []PageInfo `json:"pages"`
	Count int        `json:"count"`
}

// Client represents the main Scrapbox client
type Client struct {
	ProjectName     string
	RESTClient      *RESTClient
	WebSocketClient *WebSocketClient
}

// NewClient creates a new Scrapbox client
func NewClient(projectName, sessionCookie, baseURL string, timeout time.Duration) *Client {
	return &Client{
		ProjectName: projectName,
		RESTClient:  NewRESTClient(baseURL, sessionCookie, timeout),
	}
}
