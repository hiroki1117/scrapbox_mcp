package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

type EditPageTool struct {
	client *scrapbox.Client
	wsURL  string
}

func NewEditPageTool(client *scrapbox.Client, wsURL string) *EditPageTool {
	return &EditPageTool{
		client: client,
		wsURL:  wsURL,
	}
}

func (t *EditPageTool) Name() string {
	return "edit_page"
}

func (t *EditPageTool) Description() string {
	return "Replaces the entire content of a Scrapbox page with new text. Use get_page first to retrieve current content, then modify and pass the complete new content. The first line becomes the page title."
}

func (t *EditPageTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the page to edit",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The new content for the page (multiple lines separated by newlines). The first line should be the page title.",
			},
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
		},
		"required": []string{"title", "content"},
	}
}

func (t *EditPageTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	title, ok := arguments["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required and must be a string")
	}

	content, ok := arguments["content"].(string)
	if !ok || content == "" {
		return nil, fmt.Errorf("content is required and must be a string")
	}

	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	// Ensure WebSocket client is initialized
	t.client.EnsureWebSocket(t.wsURL)

	// Split content into lines
	newTexts := strings.Split(content, "\n")

	// Execute patch
	if err := t.client.PatchPage(title, newTexts); err != nil {
		return nil, fmt.Errorf("failed to edit page: %v", err)
	}

	return fmt.Sprintf("Successfully edited page '%s' in project '%s' (%d lines)", title, project, len(newTexts)), nil
}
