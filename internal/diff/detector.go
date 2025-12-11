package diff

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-nc-client/internal/webdav"
)

type Detector struct {
	client   *webdav.Client
	stateFile string
}

type FileState struct {
	Path         string    `json:"path"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	ModifiedTime time.Time `json:"modified_time"`
	ETag         string    `json:"etag"`
}

type State struct {
	Files         map[string]FileState `json:"files"`          // key: directory+path
	DirectoryETags map[string]string     `json:"directory_etags"` // key: directory path, value: ETag
	LastUpdate    time.Time             `json:"last_update"`
}

type Change struct {
	Type     string    `json:"type"` // "created", "updated", "deleted", "moved"
	Path     string    `json:"path"`
	OldPath  string    `json:"old_path,omitempty"` // for moved files
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

type Changes struct {
	Directory string   `json:"directory"`
	Changes   []Change `json:"changes"`
	Timestamp time.Time `json:"timestamp"`
}

func NewDetector(client *webdav.Client, stateFile string) *Detector {
	return &Detector{
		client:    client,
		stateFile: stateFile,
	}
}

func (d *Detector) DetectChanges(directories []string) ([]Changes, error) {
	// Load previous state
	prevState, err := d.loadState()
	if err != nil {
		prevState = &State{
			Files:          make(map[string]FileState),
			DirectoryETags: make(map[string]string),
			LastUpdate:     time.Time{},
		}
	}

	// Ensure DirectoryETags map exists
	if prevState.DirectoryETags == nil {
		prevState.DirectoryETags = make(map[string]string)
	}

	// Get current state
	currentState := &State{
		Files:          make(map[string]FileState),
		DirectoryETags: make(map[string]string),
		LastUpdate:     time.Now(),
	}

	var allChanges []Changes

	for _, dir := range directories {
		dir = strings.TrimPrefix(dir, "/")
		if dir == "" {
			dir = "/"
		}
		if !strings.HasPrefix(dir, "/") {
			dir = "/" + dir
		}

		// Check directory ETag to optimize scanning
		dirInfo, err := d.client.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("failed to stat directory %s: %w", dir, err)
		}

		prevDirETag := prevState.DirectoryETags[dir]
		currentDirETag := dirInfo.ETag
		directoryUnchanged := prevDirETag != "" && prevDirETag == currentDirETag

		var files []webdav.FileInfo
		if directoryUnchanged {
			// Directory hasn't changed, reuse previous state
			dirKey := dir
			for key, fileState := range prevState.Files {
				if strings.HasPrefix(key, dirKey+":") {
					// Copy file from previous state
					currentState.Files[key] = fileState
					// Convert FileState back to FileInfo for consistency
					files = append(files, webdav.FileInfo{
						Path:         fileState.Path,
						IsDir:        fileState.IsDir,
						Size:         fileState.Size,
						ModifiedTime: fileState.ModifiedTime,
						ETag:         fileState.ETag,
					})
				}
			}
		} else {
			// Directory has changed or first scan, do full recursive scan
			files, err = d.client.ListFiles(dir)
			if err != nil {
				return nil, fmt.Errorf("failed to list files in %s: %w", dir, err)
			}

			// Build current state for this directory
			dirKey := dir
			for _, file := range files {
				key := dirKey + ":" + file.Path
				currentState.Files[key] = FileState{
					Path:         file.Path,
					IsDir:        file.IsDir,
					Size:         file.Size,
					ModifiedTime: file.ModifiedTime,
					ETag:         file.ETag,
				}
			}
		}

		// Store directory ETag
		currentState.DirectoryETags[dir] = currentDirETag

		// Detect changes
		changes := d.compareStates(dir, prevState, currentState)
		allChanges = append(allChanges, Changes{
			Directory: dir,
			Changes:   changes,
			Timestamp: time.Now(),
		})
	}

	// Save new state
	if err := d.saveState(currentState); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	return allChanges, nil
}

func (d *Detector) compareStates(directory string, prevState, currentState *State) []Change {
	var changes []Change
	dirKey := directory

	// Check for created and updated files
	for key, currentFile := range currentState.Files {
		if !strings.HasPrefix(key, dirKey+":") {
			continue
		}

		prevFile, exists := prevState.Files[key]
		if !exists {
			// New file
			changes = append(changes, Change{
				Type:     "created",
				Path:     currentFile.Path,
				IsDir:    currentFile.IsDir,
				Size:     currentFile.Size,
				Modified: currentFile.ModifiedTime,
			})
		} else {
			// Check if updated
			if currentFile.ETag != prevFile.ETag ||
				currentFile.Size != prevFile.Size ||
				!currentFile.ModifiedTime.Equal(prevFile.ModifiedTime) {
				changes = append(changes, Change{
					Type:     "updated",
					Path:     currentFile.Path,
					IsDir:    currentFile.IsDir,
					Size:     currentFile.Size,
					Modified: currentFile.ModifiedTime,
				})
			}
		}
	}

	// Check for deleted files
	for key, prevFile := range prevState.Files {
		if !strings.HasPrefix(key, dirKey+":") {
			continue
		}

		if _, exists := currentState.Files[key]; !exists {
			changes = append(changes, Change{
				Type:     "deleted",
				Path:     prevFile.Path,
				IsDir:    prevFile.IsDir,
				Size:     prevFile.Size,
				Modified: prevFile.ModifiedTime,
			})
		}
	}

	// Detect moved files (same size and similar timestamp, different path)
	changes = d.detectMoves(changes, directory, prevState, currentState)

	return changes
}

func (d *Detector) detectMoves(changes []Change, directory string, prevState, currentState *State) []Change {
	dirKey := directory

	// Find deleted files that might have been moved
	deletedFiles := make(map[string]FileState)
	for key, prevFile := range prevState.Files {
		if !strings.HasPrefix(key, dirKey+":") {
			continue
		}
		if _, exists := currentState.Files[key]; !exists {
			deletedFiles[key] = prevFile
		}
	}

	// Find created files that might be moves
	createdFiles := make(map[string]FileState)
	for key, currentFile := range currentState.Files {
		if !strings.HasPrefix(key, dirKey+":") {
			continue
		}
		if _, exists := prevState.Files[key]; !exists {
			createdFiles[key] = currentFile
		}
	}

	// Try to match deleted files with created files (potential moves)
	for delKey, delFile := range deletedFiles {
		for crKey, crFile := range createdFiles {
			// Check if they have the same size and similar modification time (within 1 minute)
			if delFile.Size == crFile.Size &&
				!delFile.IsDir &&
				!crFile.IsDir &&
				delFile.Size > 0 &&
				time.Since(delFile.ModifiedTime) < 24*time.Hour {
				// Check if modification times are close
				timeDiff := crFile.ModifiedTime.Sub(delFile.ModifiedTime)
				if timeDiff < 5*time.Minute && timeDiff > -5*time.Minute {
					// This looks like a move
					// Remove the created and deleted entries, add a moved entry
					changes = removeChange(changes, "created", crFile.Path)
					changes = removeChange(changes, "deleted", delFile.Path)
					
					changes = append(changes, Change{
						Type:     "moved",
						Path:     crFile.Path,
						OldPath:  delFile.Path,
						IsDir:    crFile.IsDir,
						Size:     crFile.Size,
						Modified: crFile.ModifiedTime,
					})

					// Remove from maps to avoid duplicate matches
					delete(deletedFiles, delKey)
					delete(createdFiles, crKey)
					break
				}
			}
		}
	}

	return changes
}

func removeChange(changes []Change, changeType, path string) []Change {
	var result []Change
	for _, c := range changes {
		if c.Type != changeType || c.Path != path {
			result = append(result, c)
		}
	}
	return result
}

func (d *Detector) loadState() (*State, error) {
	data, err := os.ReadFile(d.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{
				Files:          make(map[string]FileState),
				DirectoryETags: make(map[string]string),
				LastUpdate:     time.Time{},
			}, nil
		}
		return nil, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	if state.Files == nil {
		state.Files = make(map[string]FileState)
	}
	if state.DirectoryETags == nil {
		state.DirectoryETags = make(map[string]string)
	}

	return &state, nil
}

func (d *Detector) saveState(state *State) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(d.stateFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(d.stateFile, data, 0644)
}

