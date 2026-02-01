package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hiroki/scrapbox_mcp/internal/tools"
	mcperrors "github.com/hiroki/scrapbox_mcp/pkg/errors"
)

type MessageHandler struct {
	toolRegistry   *tools.Registry
	sessionManager *SessionManager
}

func NewMessageHandler(registry *tools.Registry, sessionMgr *SessionManager) *MessageHandler {
	return &MessageHandler{
		toolRegistry:   registry,
		sessionManager: sessionMgr,
	}
}

func (h *MessageHandler) HandleRequest(ctx context.Context, req *JSONRPCRequest, sessionID string) *JSONRPCResponse {
	response := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		result, err := h.handleInitialize(ctx, req.Params, sessionID)
		if err != nil {
			response.Error = h.toRPCError(err)
		} else {
			response.Result = result
		}

	case "initialized":
		// Notification - no response needed
		return nil

	case "tools/list":
		result := h.handleToolsList()
		response.Result = result

	case "tools/call":
		result, err := h.handleToolsCall(ctx, req.Params)
		if err != nil {
			response.Error = h.toRPCError(err)
		} else {
			response.Result = result
		}

	case "ping":
		response.Result = PingResult{}

	default:
		response.Error = &RPCError{
			Code:    mcperrors.ErrCodeMethodNotFound,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
	}

	return response
}

func (h *MessageHandler) handleInitialize(ctx context.Context, params json.RawMessage, sessionID string) (*InitializeResult, error) {
	var initReq InitializeRequest
	if err := json.Unmarshal(params, &initReq); err != nil {
		return nil, mcperrors.NewMCPError(mcperrors.ErrCodeInvalidParams, "Invalid initialize params", err.Error())
	}

	result := &InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: ServerCapabilities{
			Tools: &ToolsCapability{
				ListChanged: false,
			},
		},
		ServerInfo: ServerInfo{
			Name:    "scrapbox-mcp-server",
			Version: "1.0.0",
		},
	}

	// Store session
	if sessionID != "" {
		session, exists := h.sessionManager.Get(sessionID)
		if exists {
			session.InitializeResult = result
		}
	}

	return result, nil
}

func (h *MessageHandler) handleToolsList() *ToolsListResult {
	toolsList := h.toolRegistry.List()
	mcpTools := make([]Tool, 0, len(toolsList))
	for _, t := range toolsList {
		mcpTools = append(mcpTools, Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return &ToolsListResult{
		Tools: mcpTools,
	}
}

func (h *MessageHandler) handleToolsCall(ctx context.Context, params json.RawMessage) (*ToolsCallResult, error) {
	var callReq ToolsCallRequest
	if err := json.Unmarshal(params, &callReq); err != nil {
		return nil, mcperrors.NewMCPError(mcperrors.ErrCodeInvalidParams, "Invalid tools/call params", err.Error())
	}

	result, err := h.toolRegistry.Execute(ctx, callReq.Name, callReq.Arguments)
	if err != nil {
		return nil, err
	}

	// Convert tools.ToolCallResult to mcp.ToolsCallResult
	mcpContent := make([]ContentBlock, 0, len(result.Content))
	for _, c := range result.Content {
		mcpContent = append(mcpContent, ContentBlock{
			Type: c.Type,
			Text: c.Text,
		})
	}

	return &ToolsCallResult{
		Content: mcpContent,
		IsError: result.IsError,
	}, nil
}

func (h *MessageHandler) toRPCError(err error) *RPCError {
	if mcpErr, ok := err.(*mcperrors.MCPError); ok {
		return &RPCError{
			Code:    mcpErr.Code,
			Message: mcpErr.Message,
			Data:    mcpErr.Data,
		}
	}

	return &RPCError{
		Code:    mcperrors.ErrCodeInternalError,
		Message: err.Error(),
	}
}
