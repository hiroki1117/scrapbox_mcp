package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

type ListPagesTool struct {
	client *scrapbox.Client
}

func NewListPagesTool(client *scrapbox.Client) *ListPagesTool {
	return &ListPagesTool{client: client}
}

func (t *ListPagesTool) Name() string {
	return "list_pages"
}

func (t *ListPagesTool) Description() string {
	return "Lists all pages in the Scrapbox project. Supports pagination with limit and skip parameters."
}

func (t *ListPagesTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of pages to return (default: 100)",
			},
			"skip": map[string]interface{}{
				"type":        "number",
				"description": "Number of pages to skip for pagination (default: 0)",
			},
		},
		"required": []string{},
	}
}

func (t *ListPagesTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	limit := 100
	if limitArg, ok := arguments["limit"].(float64); ok {
		limit = int(limitArg)
	}

	skip := 0
	if skipArg, ok := arguments["skip"].(float64); ok {
		skip = int(skipArg)
	}

	pages, err := t.client.RESTClient.ListPages(project, limit, skip)
	if err != nil {
		return nil, err
	}

	// Format the response as JSON
	result, err := json.MarshalIndent(pages, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format pages: %v", err)
	}

	return string(result), nil
}
