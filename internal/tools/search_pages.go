package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

type SearchPagesTool struct {
	client *scrapbox.Client
}

func NewSearchPagesTool(client *scrapbox.Client) *SearchPagesTool {
	return &SearchPagesTool{client: client}
}

func (t *SearchPagesTool) Name() string {
	return "search_pages"
}

func (t *SearchPagesTool) Description() string {
	return "Searches for pages containing the specified query string. Returns matching pages with their metadata."
}

func (t *SearchPagesTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query string",
			},
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of results to return",
			},
		},
		"required": []string{"query"},
	}
}

func (t *SearchPagesTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	query, ok := arguments["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required and must be a string")
	}

	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	limit := 0
	if limitArg, ok := arguments["limit"].(float64); ok {
		limit = int(limitArg)
	}

	searchResult, err := t.client.RESTClient.SearchPages(project, query, limit)
	if err != nil {
		return nil, err
	}

	// Format the response as JSON
	result, err := json.MarshalIndent(searchResult, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format search results: %v", err)
	}

	return string(result), nil
}
