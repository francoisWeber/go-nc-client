package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"go-nc-client/internal/config"
	"go-nc-client/internal/diff"
	"go-nc-client/internal/webdav"
)

type Handlers struct {
	config   *config.Config
	detector *diff.Detector
	client   *webdav.Client
}

func NewHandlers(cfg *config.Config, detector *diff.Detector, client *webdav.Client) *Handlers {
	return &Handlers{
		config:   cfg,
		detector: detector,
		client:   client,
	}
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *Handlers) Directories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(h.config.Directories)

	case http.MethodPost:
		var dirs []string
		if err := json.NewDecoder(r.Body).Decode(&dirs); err != nil {
			log.Printf("Error decoding directories request: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		h.config.Directories = dirs
		if err := config.Save(h.config, "config.json"); err != nil {
			log.Printf("Error saving config: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "directories updated"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type DiffRequest struct {
	IncludeHidden bool     `json:"include-hidden"`
	Paths         []string `json:"paths"`
}

func (h *Handlers) Diff(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := parseDiffRequest(r)
	if err != nil {
		log.Printf("Error parsing diff request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	directories, err := h.resolveDirectories(r, req)
	if err != nil {
		log.Printf("Error resolving directories: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	changes, err := h.detector.DetectChanges(directories, req.IncludeHidden)
	if err != nil {
		log.Printf("Error detecting changes: %v", err)
		http.Error(w, fmt.Sprintf("Failed to detect changes: %v", err), http.StatusInternalServerError)
		return
	}

	totalChanges := 0
	for _, change := range changes {
		totalChanges += len(change.Changes)
	}

	log.Printf("Diff completed: %d dirs, %d changes in %v", len(changes), totalChanges, time.Since(startTime))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(changes); err != nil {
		log.Printf("Error encoding response: %v", err)
		return
	}
}

func (h *Handlers) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	includeHidden := r.URL.Query().Get("include-hidden") == "true"

	files, err := h.client.ListDir(path, includeHidden)
	if err != nil {
		log.Printf("Error listing directory %s: %v", path, err)
		http.Error(w, fmt.Sprintf("Failed to list directory: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path":           path,
		"files":          files,
		"include_hidden": includeHidden,
	})
}

func parseDiffRequest(r *http.Request) (*DiffRequest, error) {
	req := &DiffRequest{IncludeHidden: false}

	// Try to decode JSON body, but don't fail if body is empty
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			req.IncludeHidden = false
		}
	}

	// Check for include-hidden in query parameter (overrides body)
	if r.URL.Query().Get("include-hidden") == "true" {
		req.IncludeHidden = true
	} else if r.URL.Query().Get("include-hidden") == "false" {
		req.IncludeHidden = false
	}

	return req, nil
}

func (h *Handlers) resolveDirectories(r *http.Request, req *DiffRequest) ([]string, error) {
	// Priority: query parameter > request body > config file
	if pathParam := r.URL.Query().Get("path"); pathParam != "" {
		return []string{pathParam}, nil
	}

	if len(req.Paths) > 0 {
		return req.Paths, nil
	}

	if len(h.config.Directories) > 0 {
		return h.config.Directories, nil
	}

	return nil, fmt.Errorf("no directories specified. Either provide 'path' query parameter, 'paths' in request body, or configure directories in config.json")
}
