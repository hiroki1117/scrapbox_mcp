package tools

import (
	"context"
	"fmt"

	mcperrors "github.com/hiroki/scrapbox_mcp/pkg/errors"
)

// Tool represents a tool definition for MCP
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
}

// ToolCallResult represents the result of a tool execution
type ToolCallResult struct {
	Content []ContentBlock
	IsError bool
}

// ContentBlock represents a content block in a tool result
type ContentBlock struct {
	Type string
	Text string
}

// ToolHandler defines the interface for all MCP tools
type ToolHandler interface {
	Name() string
	Description() string
	InputSchema() map[string]interface{}
	Execute(ctx context.Context, arguments map[string]interface{}) (interface{}, error)
}

// Registry manages all available tools
type Registry struct {
	tools map[string]ToolHandler
}

// NewRegistry creates a new tool registry
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]ToolHandler),
	}
}

// Register adds a tool to the registry
func (r *Registry) Register(tool ToolHandler) {
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name
func (r *Registry) Get(name string) (ToolHandler, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool, nil
}

// List returns all registered tools
func (r *Registry) List() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, handler := range r.tools {
		tools = append(tools, Tool{
			Name:        handler.Name(),
			Description: handler.Description(),
			InputSchema: handler.InputSchema(),
		})
	}
	return tools
}

// Execute runs a tool with the given arguments
func (r *Registry) Execute(ctx context.Context, name string, arguments map[string]interface{}) (*ToolCallResult, error) {
	tool, err := r.Get(name)
	if err != nil {
		return &ToolCallResult{
			Content: []ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf("Tool not found: %s", name),
			}},
			IsError: true,
		}, mcperrors.NewMCPError(mcperrors.ErrCodeMethodNotFound, "Tool not found", map[string]string{"tool": name})
	}

	result, err := tool.Execute(ctx, arguments)
	if err != nil {
		return &ToolCallResult{
			Content: []ContentBlock{{
				Type: "text",
				Text: fmt.Sprintf("Tool execution failed: %v", err),
			}},
			IsError: true,
		}, mcperrors.NewMCPError(mcperrors.ErrCodeToolExecutionErr, "Tool execution failed", map[string]string{"error": err.Error()})
	}

	// Convert result to text content
	return &ToolCallResult{
		Content: []ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("%v", result),
		}},
		IsError: false,
	}, nil
}
