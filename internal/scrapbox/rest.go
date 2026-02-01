package scrapbox

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	mcperrors "github.com/hiroki/scrapbox_mcp/pkg/errors"
)

// RESTClient handles REST API calls to Scrapbox
type RESTClient struct {
	baseURL    string
	httpClient *http.Client
	auth       *Auth
}

// NewRESTClient creates a new REST client
func NewRESTClient(baseURL, sessionCookie string, timeout time.Duration) *RESTClient {
	return &RESTClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		auth: NewAuth(sessionCookie),
	}
}

// GetPage retrieves a page by title
func (c *RESTClient) GetPage(project, title string) (*Page, error) {
	endpoint := fmt.Sprintf("%s/pages/%s/%s", c.baseURL, project, url.PathEscape(title))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to create request", err)
	}

	c.auth.AddAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to fetch page", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNotFound, fmt.Sprintf("Page not found: %s", title), nil)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeAuthFailed, "Authentication failed", nil)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, fmt.Sprintf("Unexpected status code: %d", resp.StatusCode), nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to read response", err)
	}

	var page Page
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to parse response", err)
	}

	return &page, nil
}

// ListPages retrieves a list of pages
func (c *RESTClient) ListPages(project string, limit, skip int) (*PagesResponse, error) {
	endpoint := fmt.Sprintf("%s/pages/%s?limit=%d&skip=%d", c.baseURL, project, limit, skip)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to create request", err)
	}

	c.auth.AddAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to list pages", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeAuthFailed, "Authentication failed", nil)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, fmt.Sprintf("Unexpected status code: %d", resp.StatusCode), nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to read response", err)
	}

	var pagesResp PagesResponse
	if err := json.Unmarshal(body, &pagesResp); err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to parse response", err)
	}

	return &pagesResp, nil
}

// SearchPages searches for pages matching the query
func (c *RESTClient) SearchPages(project, query string, limit int) (*SearchResponse, error) {
	endpoint := fmt.Sprintf("%s/pages/%s/search/query?q=%s", c.baseURL, project, url.QueryEscape(query))
	if limit > 0 {
		endpoint += fmt.Sprintf("&limit=%d", limit)
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to create request", err)
	}

	c.auth.AddAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to search pages", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeAuthFailed, "Authentication failed", nil)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, fmt.Sprintf("Unexpected status code: %d", resp.StatusCode), nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to read response", err)
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to parse response", err)
	}

	return &searchResp, nil
}
