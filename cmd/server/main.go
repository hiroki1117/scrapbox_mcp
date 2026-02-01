package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hiroki/scrapbox_mcp/internal/config"
	"github.com/hiroki/scrapbox_mcp/internal/mcp"
	"github.com/hiroki/scrapbox_mcp/internal/scrapbox"
	"github.com/hiroki/scrapbox_mcp/internal/tools"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (optional, won't error if file doesn't exist)
	_ = godotenv.Load()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Starting Scrapbox MCP Server...")
	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Port: %s", cfg.Port)
	log.Printf("Project: %s", cfg.ProjectName)

	// Initialize Scrapbox client
	scrapboxClient := scrapbox.NewClient(
		cfg.ProjectName,
		cfg.SessionCookie,
		cfg.RestAPIBaseURL,
		cfg.RequestTimeout,
	)

	// Initialize tool registry
	registry := tools.NewRegistry()
	registry.Register(tools.NewGetPageTool(scrapboxClient))
	registry.Register(tools.NewListPagesTool(scrapboxClient))
	registry.Register(tools.NewSearchPagesTool(scrapboxClient))
	registry.Register(tools.NewInsertLinesTool(scrapboxClient, cfg.WebSocketURL))
	registry.Register(tools.NewCreatePageTool(scrapboxClient, cfg.WebSocketURL))

	// Initialize MCP components
	sessionMgr := mcp.NewSessionManager(cfg.SessionTTL)
	handler := mcp.NewMessageHandler(registry, sessionMgr)
	transport := mcp.NewTransport(handler, sessionMgr, cfg.AllowedOrigins, cfg.EnableCORS)

	// Setup HTTP server
	mux := http.NewServeMux()

	// MCP endpoint
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			transport.HandlePOST(w, r)
		case "GET":
			transport.HandleGET(w, r)
		case "DELETE":
			transport.HandleDELETE(w, r)
		case "OPTIONS":
			transport.HandlePOST(w, r) // CORS preflight
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy"}`)
	})

	// Create HTTP server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  600 * time.Second,
		WriteTimeout: 600 * time.Second,
		IdleTimeout:  600 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server listening on port %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
