package errors

import "fmt"

// ScrapboxError represents errors from Scrapbox API
type ScrapboxError struct {
	Code    string
	Message string
	Cause   error
}

func (e *ScrapboxError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ScrapboxError) Unwrap() error {
	return e.Cause
}

// Common Scrapbox error codes
const (
	ErrCodeNotFound      = "SCRAPBOX_NOT_FOUND"
	ErrCodeAuthFailed    = "SCRAPBOX_AUTH_FAILED"
	ErrCodeNetworkError  = "SCRAPBOX_NETWORK_ERROR"
	ErrCodeInvalidInput  = "SCRAPBOX_INVALID_INPUT"
	ErrCodeRateLimit     = "SCRAPBOX_RATE_LIMIT"
	ErrCodeWebSocketFail = "SCRAPBOX_WEBSOCKET_FAILED"
)

// MCPError represents JSON-RPC errors
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e *MCPError) Error() string {
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// JSON-RPC error codes
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternalError  = -32603
)

// Application-specific error codes (-32000 to -32099)
const (
	ErrCodePageNotFound     = -32000
	ErrCodeUnauthorized     = -32001
	ErrCodeToolExecutionErr = -32002
	ErrCodeSessionNotFound  = -32003
)

// NewMCPError creates a new MCP error
func NewMCPError(code int, message string, data interface{}) *MCPError {
	return &MCPError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// NewScrapboxError creates a new Scrapbox error
func NewScrapboxError(code, message string, cause error) *ScrapboxError {
	return &ScrapboxError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
