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

// checkResponseStatus handles common HTTP status code errors
func checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeAuthFailed, "Authentication failed", nil)
	}
	if resp.StatusCode != http.StatusOK {
		return mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, fmt.Sprintf("Unexpected status code: %d", resp.StatusCode), nil)
	}
	return nil
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
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
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

	if err := checkResponseStatus(resp); err != nil {
		return nil, err
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

	if err := checkResponseStatus(resp); err != nil {
		return nil, err
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

// ExportSmartContext retrieves the Smart Context ("Export for AI") text for a page.
// hops must be 1 or 2. The response is plain text formatted for LLMs.
func (c *RESTClient) ExportSmartContext(project, title string, hops int) (string, error) {
	if hops != 1 && hops != 2 {
		return "", mcperrors.NewScrapboxError(mcperrors.ErrCodeInvalidInput, fmt.Sprintf("hops must be 1 or 2, got %d", hops), nil)
	}
	endpoint := fmt.Sprintf("%s/smart-context/export-%dhop-links/%s.txt?title=%s",
		c.baseURL, hops, url.PathEscape(project), url.QueryEscape(title))

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to create request", err)
	}

	c.auth.AddAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to export smart context", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		return "", mcperrors.NewScrapboxError(mcperrors.ErrCodeInvalidInput, fmt.Sprintf("Invalid smart context request: %s", title), nil)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "", mcperrors.NewScrapboxError(mcperrors.ErrCodeNotFound, fmt.Sprintf("Project not found: %s", project), nil)
	}
	if err := checkResponseStatus(resp); err != nil {
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to read response", err)
	}

	return string(body), nil
}

// GetMe retrieves the current user information
func (c *RESTClient) GetMe() (*User, error) {
	endpoint := fmt.Sprintf("%s/users/me", c.baseURL)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to create request", err)
	}

	c.auth.AddAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to fetch user", err)
	}
	defer resp.Body.Close()

	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to read response", err)
	}

	var user User
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to parse response", err)
	}

	return &user, nil
}

// ProjectInfo represents project information
type ProjectInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetProject retrieves project information
func (c *RESTClient) GetProject(projectName string) (*ProjectInfo, error) {
	endpoint := fmt.Sprintf("%s/projects/%s", c.baseURL, projectName)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to create request", err)
	}

	c.auth.AddAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to fetch project", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNotFound, fmt.Sprintf("Project not found: %s", projectName), nil)
	}
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to read response", err)
	}

	var projectInfo ProjectInfo
	if err := json.Unmarshal(body, &projectInfo); err != nil {
		return nil, mcperrors.NewScrapboxError(mcperrors.ErrCodeNetworkError, "Failed to parse response", err)
	}

	return &projectInfo, nil
}
