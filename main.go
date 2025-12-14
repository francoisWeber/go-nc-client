package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"go-nc-client/internal/config"
	"go-nc-client/internal/diff"
	"go-nc-client/internal/handlers"
	"go-nc-client/internal/middleware"
	"go-nc-client/internal/webdav"
)

func main() {
	// Parse command-line flags
	portFlag := flag.String("port", "", "Port to run the server on (default: 8080 or PORT environment variable)")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize WebDAV client
	client := webdav.NewClient(cfg.WebDAVURL, cfg.Username, cfg.Password)

	// Initialize change detector
	absStateFile, _ := filepath.Abs(cfg.StateFile)
	log.Printf("State file configured as: %s (absolute: %s)", cfg.StateFile, absStateFile)
	detector := diff.NewDetector(client, cfg.StateFile)

	// Initialize handlers
	h := handlers.NewHandlers(detector, client)

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/diff", h.Diff)
	mux.HandleFunc("/ls", h.List)

	// Determine port: command-line flag > environment variable > default
	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, middleware.Logging(mux)))
}
