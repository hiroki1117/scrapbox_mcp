# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/claude-code) when working with code in this repository.

## Project Overview

Scrapbox MCP Server is a Go-based Model Context Protocol (MCP) server for Scrapbox/Cosense. It provides LLM tools to read and write Scrapbox pages via a Streamable HTTP transport, designed for CloudRun deployment.

## Tech Stack

- **Language**: Go 1.23+
- **Protocol**: MCP 2024-11-05 (Streamable HTTP transport)
- **External APIs**: Scrapbox REST API and WebSocket (Socket.IO)
- **Deployment**: Docker / Google CloudRun

## Architecture

```
cmd/server/main.go              # Entry point, HTTP server setup
internal/
├── config/config.go            # Environment variable configuration
├── mcp/
│   ├── handler.go              # JSON-RPC message handler
│   ├── session.go              # Session management
│   ├── transport.go            # HTTP transport (POST/GET/DELETE)
│   └── types.go                # MCP protocol types
├── scrapbox/
│   ├── auth.go                 # Cookie-based authentication
│   ├── rest.go                 # REST API client
│   ├── types.go                # Scrapbox data types
│   └── websocket.go            # WebSocket client for writes
└── tools/
    ├── registry.go             # Tool registration interface
    ├── get_page.go             # Retrieve page content
    ├── list_pages.go           # List pages in project
    ├── search_pages.go         # Full-text search
    ├── insert_lines.go         # Insert lines (WebSocket)
    └── create_page.go          # Create new page (WebSocket)
pkg/errors/errors.go            # Custom error types
```

## Common Commands

```bash
# Run the server locally
go run cmd/server/main.go

# Build
go build -o server cmd/server/main.go

# Run with environment variables
COSENSE_PROJECT_NAME=your-project COSENSE_SID=your-cookie go run cmd/server/main.go

# Build Docker image
docker build -t scrapbox-mcp-server .
```

## Environment Variables

Required:
- `COSENSE_PROJECT_NAME` - Scrapbox project name
- `COSENSE_SID` - Session cookie (connect.sid)

Optional:
- `PORT` (default: 8080)
- `SESSION_TTL` (default: 1h)
- `SCRAPBOX_API_URL` (default: https://scrapbox.io/api)
- `SCRAPBOX_WS_URL` (default: wss://scrapbox.io/socket.io/)

## MCP Tools

| Tool | Description | Transport |
|------|-------------|-----------|
| `get_page` | Get page content by title | REST |
| `list_pages` | List all pages in project | REST |
| `search_pages` | Full-text search | REST |
| `insert_lines` | Insert lines into a page | WebSocket |
| `create_page` | Create a new page | WebSocket |

## Sub Agents

`.claude/agents/` 配下にサブエージェントを定義しています。

| Agent | File | Description |
|-------|------|-------------|
| `code-review` | `.claude/agents/code-review.md` | コードレビュー、Goベストプラクティス確認 |
| `scrapbox-spec` | `.claude/agents/scrapbox-spec.md` | Scrapbox/Cosense API仕様の調査・蓄積 |
| `test-server` | `.claude/agents/test-server.md` | MCPサーバーの動作確認テスト |

### 使い方

```
@code-review このPRをレビューして
@scrapbox-spec WebSocketのcommit操作の仕様を調べて
@test-server get_pageツールの動作確認して
```

### 仕様ドキュメント蓄積先

`scrapbox-spec` エージェントの調査結果は `docs/` 配下に蓄積:
- `docs/scrapbox-api.md` - REST API仕様
- `docs/scrapbox-websocket.md` - WebSocket仕様
- `docs/scrapbox-tips.md` - Tips・ハマりポイント
