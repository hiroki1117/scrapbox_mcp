package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

type GetPageTool struct {
	client *scrapbox.Client
}

func NewGetPageTool(client *scrapbox.Client) *GetPageTool {
	return &GetPageTool{client: client}
}

func (t *GetPageTool) Name() string {
	return "get_page"
}

func (t *GetPageTool) Description() string {
	return "Retrieves a Scrapbox page by title. Returns the page content including all lines, metadata, and links."
}

func (t *GetPageTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the page to retrieve",
			},
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
		},
		"required": []string{"title"},
	}
}

func (t *GetPageTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	title, ok := arguments["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required and must be a string")
	}

	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	page, err := t.client.RESTClient.GetPage(project, title)
	if err != nil {
		return nil, err
	}

	// Format the response as JSON
	result, err := json.MarshalIndent(page, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format page: %v", err)
	}

	return string(result), nil
}
