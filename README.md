# Scrapbox MCP Server

A Model Context Protocol (MCP) server for Scrapbox/Cosense, implemented in Go and designed for CloudRun deployment.

## Features

- **MCP Streamable HTTP Transport**: Standards-compliant MCP server using the latest Streamable HTTP transport
- **4 Core Tools**:
  - `get_page` - Retrieve page content and metadata
  - `list_pages` - List all pages in a project
  - `search_pages` - Full-text search across pages
  - `insert_lines` - Insert lines into pages (via WebSocket)
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
- `LOG_LEVEL` - Logging level (default: info)
- `ALLOWED_ORIGINS` - CORS origins (comma-separated)

See [.env.example](.env.example) for a complete list.

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

```bash
# Tag for Google Container Registry
docker tag scrapbox-mcp-server asia-northeast1-docker.pkg.dev/YOUR-PROJECT/scrapbox-mcp-server/server:latest

# Push to GCR
docker push asia-northeast1-docker.pkg.dev/YOUR-PROJECT/scrapbox-mcp-server/server:latest


# Deploy to CloudRun
gcloud run deploy scrapbox-mcp-server \
  --image asia-northeast1-docker.pkg.dev/YOUR-PROJECT/scrapbox-mcp-server/server:latest \
  --platform managed \
  --region us-central1 \
  --service-account your-serviceaccount \
  --min-instances 0 \
  --max-instances 1 \
  --set-env-vars COSENSE_PROJECT_NAME=your-project \
  --set-secrets COSENSE_SID=cosense-sid:latest \
  --allow-unauthenticated
```

## MCP Client Integration

### Claude Desktop

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "scrapbox": {
      "url": "https://your-cloudrun-url.run.app/mcp"
    }
  }
}
```

### Other MCP Clients

Use the `/mcp` endpoint with Streamable HTTP transport. The server supports:
- POST requests for client-to-server messages
- GET requests for server-to-client SSE streams
- DELETE requests for session termination

## Project Structure

```
scrapbox_mcp/
├── cmd/server/main.go              # Application entry point
├── internal/
│   ├── mcp/                        # MCP protocol implementation
│   ├── scrapbox/                   # Scrapbox API client
│   ├── tools/                      # MCP tools (get_page, etc.)
│   └── config/                     # Configuration management
├── pkg/errors/                     # Error types
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
   ```

## License

MIT

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.
