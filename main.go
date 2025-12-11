package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"go-nc-client/internal/config"
	"go-nc-client/internal/diff"
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
	detector := diff.NewDetector(client, cfg.StateFile)

	// Setup routes
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/directories", directoriesHandler(cfg))
	http.HandleFunc("/diff", diffHandler(detector))
	http.HandleFunc("/ls", lsHandler(client))

	// Determine port: command-line flag > environment variable > default
	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func directoriesHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(cfg.Directories)
		case http.MethodPost:
			var dirs []string
			if err := json.NewDecoder(r.Body).Decode(&dirs); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cfg.Directories = dirs
			if err := config.Save(cfg, "config.json"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"message": "directories updated"})
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func diffHandler(detector *diff.Detector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cfg, err := config.Load("config.json")
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
			return
		}

		changes, err := detector.DetectChanges(cfg.Directories)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to detect changes: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(changes)
	}
}

func lsHandler(client *webdav.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get path from query parameter, default to root
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}

		files, err := client.ListDir(path)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list directory: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"path":  path,
			"files": files,
		})
	}
}

