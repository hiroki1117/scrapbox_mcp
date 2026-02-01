package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
)

type InsertLinesTool struct {
	client *scrapbox.Client
	wsURL  string
}

func NewInsertLinesTool(client *scrapbox.Client, wsURL string) *InsertLinesTool {
	return &InsertLinesTool{
		client: client,
		wsURL:  wsURL,
	}
}

func (t *InsertLinesTool) Name() string {
	return "insert_lines"
}

func (t *InsertLinesTool) Description() string {
	return "Inserts lines into a Scrapbox page after a specified target line. If the target line is not found, lines are appended to the end of the page."
}

func (t *InsertLinesTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type":        "string",
				"description": "The title of the page to insert lines into",
			},
			"target_line": map[string]interface{}{
				"type":        "string",
				"description": "The line after which to insert new lines (or empty to append at end)",
			},
			"new_lines": map[string]interface{}{
				"type":        "string",
				"description": "The lines to insert (can be a single line or multiple lines separated by newlines)",
			},
			"project": map[string]interface{}{
				"type":        "string",
				"description": "Optional project name (uses default if not specified)",
			},
		},
		"required": []string{"title", "new_lines"},
	}
}

func (t *InsertLinesTool) Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error) {
	title, ok := arguments["title"].(string)
	if !ok || title == "" {
		return nil, fmt.Errorf("title is required and must be a string")
	}

	newLinesStr, ok := arguments["new_lines"].(string)
	if !ok || newLinesStr == "" {
		return nil, fmt.Errorf("new_lines is required and must be a string")
	}

	targetLine := ""
	if targetLineArg, ok := arguments["target_line"].(string); ok {
		targetLine = targetLineArg
	}

	project := t.client.ProjectName
	if projectArg, ok := arguments["project"].(string); ok && projectArg != "" {
		project = projectArg
	}

	// Ensure WebSocket client is initialized
	t.client.EnsureWebSocket(t.wsURL)

	// Split new lines by newline
	newLines := strings.Split(newLinesStr, "\n")

	// Execute insert
	if err := t.client.InsertLines(title, targetLine, newLines); err != nil {
		return nil, fmt.Errorf("failed to insert lines: %v", err)
	}

	return fmt.Sprintf("Successfully inserted %d line(s) into page '%s' in project '%s'", len(newLines), title, project), nil
}
