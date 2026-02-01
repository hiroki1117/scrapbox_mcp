package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

type CreatePageTool struct {
	client *scrapbox.Client
	wsURL  string
}

func NewCreatePageTool(client *scrapbox.Client, wsURL string) *CreatePageTool {
	return &CreatePageTool{
		client: client,
		wsURL:  wsURL,
	}
}

func (t *CreatePageTool) Name() string {
	return "create_page"
}

func (t *CreatePageTool) Description() string {
	return "Creates a new Scrapbox page with the specified title and body content. Returns an error if the page already exists."
}

func (t *CreatePageTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the new page",
			},
			"body": map[string]interface{}{
				"type":        "string",
				"description": "The body content of the page (can be multiple lines separated by newlines)",
			},
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
		},
		"required": []string{"title"},
	}
}

func (t *CreatePageTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	title, ok := arguments["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required and must be a string")
	}

	body := ""
	if bodyArg, ok := arguments["body"].(string); ok {
		body = bodyArg
	}

	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	// Ensure WebSocket client is initialized
	t.client.EnsureWebSocket(t.wsURL)

	// Split body by newline
	var bodyLines []string
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}

	// Execute create
	if err := t.client.CreatePage(title, bodyLines); err != nil {
		return nil, fmt.Errorf("failed to create page: %v", err)
	}

	pageURL := fmt.Sprintf("https://scrapbox.io/%s/%s", project, title)
	return fmt.Sprintf("Successfully created page '%s' in project '%s'\nURL: %s", title, project, pageURL), nil
}
