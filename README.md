# Scrapbox MCP Server

A Model Context Protocol (MCP) server for Scrapbox/Cosense, implemented in Go and designed for CloudRun deployment.

## Features

- **MCP Streamable HTTP Transport**: Standards-compliant MCP server using the latest Streamable HTTP transport
- **7 Tools**:
  - `get_page` - Retrieve page content and metadata
  - `list_pages` - List all pages in a project
  - `search_pages` - Full-text search across pages
  - `get_smart_context` - Export a page and its related pages (1/2 hop links) as AI-ready text (Scrapbox "Export for AI" / Smart Context)
  - `insert_lines` - Insert lines into pages (via WebSocket)
  - `create_page` - Create a new page (via WebSocket)
  - `edit_page` - Replace the whole content of a page; only changed lines are sent (via WebSocket)
- **Multi-project**: Every tool accepts an optional `project` argument (defaults to `COSENSE_PROJECT_NAME`)
- **CloudRun Ready**: Containerized with Docker, ready for Google CloudRun deployment
- **Extensible Architecture**: Easy to add new tools following the registry pattern

## Architecture

- **Transport**: Streamable HTTP (MCP 2024-11-05 standard)
- **Read Operations**: REST API (`/api/pages/:project/:title`, etc.)
- **Write Operations**: WebSocket with Socket.IO protocol
- **Session Management**: Stateful HTTP sessions with automatic cleanup

## Requirements

- Go 1.23+
- Scrapbox/Cosense account with session cookie
- Docker (for CloudRun deployment)

## Configuration

All configuration is done via environment variables:

### Required
- `COSENSE_PROJECT_NAME` - Your Scrapbox project name
- `COSENSE_SID` - Session cookie value (connect.sid)

### Optional
- `PORT` - HTTP server port (default: 8080)
- `SESSION_TTL` - Session expiration (default: 1h)
- `SCRAPBOX_API_URL` - REST API base URL (default: https://scrapbox.io/api)
- `SCRAPBOX_WS_URL` - WebSocket URL (default: wss://scrapbox.io/socket.io/)
- `REQUEST_TIMEOUT` - REST API request timeout (default: 30s)
- `ALLOWED_ORIGINS` - Allowed CORS / Origin values (comma-separated, empty allows all)
- `ENABLE_CORS` - Send CORS headers (default: true)
- `ENVIRONMENT` - Only printed in the startup log (default: production)

`LOG_LEVEL`, `ENABLE_SSE` and `MAX_RETRIES` are parsed but not used yet.
See [.env.example](.env.example) for a template.

## Development

### Setup

```bash
# Clone the repository
git clone <repository-url>
cd scrapbox_mcp

# Install dependencies
go mod download

# Copy environment template
cp .env.example .env

# Edit .env with your Scrapbox credentials
```

### Running Locally

```bash
# Set environment variables
export COSENSE_PROJECT_NAME=your-project
export COSENSE_SID=your-session-cookie

# Run the server
go run cmd/server/main.go
```

### Testing

```bash
# Unit tests
go test ./...
```

Manual check against a running server:

```bash
# Health check
curl http://localhost:8080/health

# Initialize MCP session
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}'

# List available tools
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: <session-id-from-initialize>" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":2}'

# Get a page
curl -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: <session-id>" \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_page","arguments":{"title":"YourPageTitle"}},"id":3}'
```

## CloudRun Deployment

### Build Docker Image

```bash
docker build -t scrapbox-mcp-server .
```

### Deploy to CloudRun

Pushing to `main` builds the image and deploys it automatically via
[.github/workflows/deploy.yml](.github/workflows/deploy.yml)
(Workload Identity Federation; configure the `GCP_PROJECT_ID`, `GCP_REGION`, `ARTIFACT_REPOSITORY`,
`IMAGE_NAME`, `CLOUDRUN_SERVICE_NAME`, `WIF_PROVIDER`, `WIF_SERVICE_ACCOUNT` and `COSENSE_PROJECT_NAME`
repository secrets, and a `cosense-sid` secret in Secret Manager).

To deploy manually:

```bash
# Tag for Artifact Registry
docker tag scrapbox-mcp-server asia-northeast1-docker.pkg.dev/YOUR-PROJECT/scrapbox-mcp-server/server:latest

# Push to Artifact Registry
docker push asia-northeast1-docker.pkg.dev/YOUR-PROJECT/scrapbox-mcp-server/server:latest


# Deploy to CloudRun
gcloud run deploy scrapbox-mcp-server \
  --image asia-northeast1-docker.pkg.dev/YOUR-PROJECT/scrapbox-mcp-server/server:latest \
  --platform managed \
  --region asia-northeast1 \
  --service-account your-serviceaccount \
  --min-instances 0 \
  --max-instances 1 \
  --timeout 600s \
  --set-env-vars COSENSE_PROJECT_NAME=your-project \
  --set-secrets COSENSE_SID=cosense-sid:latest \
  --allow-unauthenticated
```

## MCP Client Integration

### Claude Code

```bash
claude mcp add --transport http scrapbox https://your-cloudrun-url.run.app/mcp
```

### Claude Desktop / claude.ai

Add `https://your-cloudrun-url.run.app/mcp` as a custom connector (Settings → Connectors).
`claude_desktop_config.json` only supports local (stdio) servers, so a remote URL cannot be set there directly.

### Other MCP Clients

Use the `/mcp` endpoint with Streamable HTTP transport. The server supports:
- POST requests for client-to-server messages
- GET requests open an SSE stream (kept open, but the server does not push messages yet)
- DELETE requests for session termination

## Project Structure

```
scrapbox_mcp/
├── cmd/server/main.go              # Application entry point
├── internal/
│   ├── mcp/                        # MCP protocol implementation
│   ├── scrapbox/                   # Scrapbox API client (REST, WebSocket, line diff)
│   ├── tools/                      # MCP tools (get_page, etc.)
│   └── config/                     # Configuration management
├── pkg/errors/                     # Error types
├── docs/                           # Scrapbox API / WebSocket notes and design docs
├── .github/workflows/deploy.yml    # CI deploy to CloudRun
├── Dockerfile                      # CloudRun deployment
└── .env.example                    # Configuration template
```

## Adding New Tools

1. Create a new file in `internal/tools/your_tool.go`
2. Implement the `ToolHandler` interface:
   ```go
   type YourTool struct {
       client *scrapbox.Client
   }

   func (t *YourTool) Name() string { ... }
   func (t *YourTool) Description() string { ... }
   func (t *YourTool) InputSchema() map[string]interface{} { ... }
   func (t *YourTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) { ... }
   ```
3. Register in `cmd/server/main.go`:
   ```go
   registry.Register(tools.NewYourTool(scrapboxClient))
   // Write tools also take the WebSocket URL:
   // registry.Register(tools.NewYourTool(scrapboxClient, cfg.WebSocketURL))
   ```

## License

MIT

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
