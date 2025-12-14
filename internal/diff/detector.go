package diff

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-nc-client/internal/webdav"
)

type Detector struct {
	client    *webdav.Client
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
	Files          map[string]FileState `json:"files"`           // key: directory+path
	DirectoryETags map[string]string    `json:"directory_etags"` // key: directory path, value: ETag
	LastUpdate     time.Time            `json:"last_update"`
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
	Directory string    `json:"directory"`
	Changes   []Change  `json:"changes"`
	Timestamp time.Time `json:"timestamp"`
}

func NewDetector(client *webdav.Client, stateFile string) *Detector {
	return &Detector{
		client:    client,
		stateFile: stateFile,
	}
}

func (d *Detector) DetectChanges(directories []string, includeHidden bool) ([]Changes, error) {
	absPath, _ := filepath.Abs(d.stateFile)
	log.Printf("Loading previous state from %s (absolute: %s)", d.stateFile, absPath)

	// Load previous state
	prevState, err := d.loadState()
	if err != nil {
		log.Printf("[DIFF] No previous state found or error loading: %v", err)
		prevState = &State{
			Files:          make(map[string]FileState),
			DirectoryETags: make(map[string]string),
			LastUpdate:     time.Time{},
		}
	} else {
		log.Printf("[DIFF] Loaded previous state: %d files tracked, last update: %v",
			len(prevState.Files), prevState.LastUpdate)
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

		dirInfo, err := d.client.Stat(dir)
		if err != nil {
			log.Printf("Error statting directory %s: %v", dir, err)
			return nil, fmt.Errorf("failed to stat directory %s: %w", dir, err)
		}

		prevDirETag := prevState.DirectoryETags[dir]
		currentDirETag := dirInfo.ETag
		directoryUnchanged := prevDirETag != "" && prevDirETag == currentDirETag

		if directoryUnchanged {
			log.Printf("Directory %s unchanged, reusing state", dir)
		}

		var files []webdav.FileInfo
		if directoryUnchanged {
			// Directory hasn't changed, reuse previous state
			dirKey := dir
			fileCount := 0
			for key, fileState := range prevState.Files {
				if strings.HasPrefix(key, dirKey+":") {
					// Filter hidden files if not including them
					if !includeHidden && isHidden(fileState.Path) {
						continue
					}
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
					fileCount++
				}
			}
		} else {
			// Directory has changed or first scan, do full recursive scan with ETag optimization
			scanStartTime := time.Now()

			// Create ETag checker callback for subdirectories
			dirKey := dir
			etagChecker := func(subdirPath string) (bool, string, []webdav.FileInfo, error) {
				// Normalize subdirectory path
				normalizedSubdir := subdirPath
				if !strings.HasPrefix(normalizedSubdir, "/") {
					normalizedSubdir = "/" + normalizedSubdir
				}

				// Extract files from previous state for this subdirectory
				var prevFiles []webdav.FileInfo
				var dirFileState *FileState
				subdirPrefix := normalizedSubdir + "/"
				for key, fileState := range prevState.Files {
					if strings.HasPrefix(key, dirKey+":") {
						filePath := fileState.Path
						// Check if this file belongs to the subdirectory
						if filePath == normalizedSubdir || strings.HasPrefix(filePath, subdirPrefix) {
							// If this is the directory itself, save its file state
							if filePath == normalizedSubdir && fileState.IsDir {
								dirFileState = &fileState
							}
							prevFiles = append(prevFiles, webdav.FileInfo{
								Path:         fileState.Path,
								IsDir:        fileState.IsDir,
								Size:         fileState.Size,
								ModifiedTime: fileState.ModifiedTime,
								ETag:         fileState.ETag,
							})
						}
					}
				}

				// Try to get ETag from DirectoryETags map first
				prevETag, hasETag := prevState.DirectoryETags[normalizedSubdir]
				if !hasETag {
					// Fallback: use the directory's file entry ETag if available
					// This handles cases where DirectoryETags wasn't populated in previous runs
					if dirFileState != nil && dirFileState.IsDir {
						prevETag = dirFileState.ETag
						hasETag = true
					}
				}

				if !hasETag {
					return false, "", nil, nil
				}

				return true, prevETag, prevFiles, nil
			}

			// Create ETag storer callback to store subdirectory ETags as we encounter them
			etagStorer := func(subdirPath string, etag string) {
				// Normalize subdirectory path
				normalizedSubdir := subdirPath
				if !strings.HasPrefix(normalizedSubdir, "/") {
					normalizedSubdir = "/" + normalizedSubdir
				}
				currentState.DirectoryETags[normalizedSubdir] = etag
			}

			files, err = d.client.ListFilesWithETagOptimization(dir, includeHidden, etagChecker, etagStorer)
			if err != nil {
				log.Printf("Error listing files in %s: %v", dir, err)
				return nil, fmt.Errorf("failed to list files in %s: %w", dir, err)
			}
			log.Printf("Scanned %d files in %s (%v)", len(files), dir, time.Since(scanStartTime))

			// Build current state for this directory
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

		changeCounts := make(map[string]int)
		for _, change := range changes {
			changeCounts[change.Type]++
		}
		if len(changes) > 0 {
			log.Printf("Detected %d changes in %s: %v", len(changes), dir, changeCounts)
		}

		allChanges = append(allChanges, Changes{
			Directory: dir,
			Changes:   changes,
			Timestamp: time.Now(),
		})

	}

	// Save new state
	if err := d.saveState(currentState); err != nil {
		log.Printf("Error saving state: %v", err)
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
	// Strategy:
	// 1. First try ETag matching (most reliable - same ETag = same file)
	// 2. Then try unique size matching with time constraint (within 1 minute)
	
	for delKey, delFile := range deletedFiles {
		for crKey, crFile := range createdFiles {
			if delFile.IsDir || crFile.IsDir {
				continue
			}
			if delFile.Size <= 0 || delFile.Size != crFile.Size {
				continue
			}

			// Priority 1: ETag matching (most reliable)
			// If ETags match, it's definitely the same file (moved)
			if delFile.ETag != "" && crFile.ETag != "" && delFile.ETag == crFile.ETag {
				// This is definitely a move - same ETag means same file
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

				delete(deletedFiles, delKey)
				delete(createdFiles, crKey)
				break
			}

			// Priority 2: Size matching with uniqueness check and time constraint
			// Check if this size is unique (only one deleted and one created file with this size)
			// AND the time difference between delete and create is within 1 minute
			sameSizeDeleted := 0
			sameSizeCreated := 0
			for _, df := range deletedFiles {
				if df.Size == delFile.Size && !df.IsDir {
					sameSizeDeleted++
				}
			}
			for _, cf := range createdFiles {
				if cf.Size == crFile.Size && !cf.IsDir {
					sameSizeCreated++
				}
			}

			// Check if size is unique AND times are within 1 minute
			timeDiff := crFile.ModifiedTime.Sub(delFile.ModifiedTime)
			if sameSizeDeleted == 1 && sameSizeCreated == 1 &&
				timeDiff < 1*time.Minute && timeDiff > -1*time.Minute {
				// Unique size match with close timestamps - very likely a move
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

				delete(deletedFiles, delKey)
				delete(createdFiles, crKey)
				break
			}
		}
	}

	return changes
}

// isHidden checks if a file or directory path contains hidden components
// Hidden files/directories are those starting with "."
func isHidden(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for _, part := range parts {
		if part != "" && strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
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
	// Resolve absolute path for logging and to ensure correct location
	absPath, err := filepath.Abs(d.stateFile)
	if err != nil {
		absPath = d.stateFile // Fallback to original if Abs fails
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(d.stateFile)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("Error creating state file directory %s: %v", dir, err)
			return err
		}
	}

	// Use Marshal instead of MarshalIndent for better performance with large files
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	if err := os.WriteFile(d.stateFile, data, 0644); err != nil {
		log.Printf("Error writing state file to %s (absolute: %s): %v", d.stateFile, absPath, err)
		return err
	}

	log.Printf("State saved to %s (absolute: %s)", d.stateFile, absPath)
	return nil
}
