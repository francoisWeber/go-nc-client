package webdav

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

func NewClient(baseURL, username, password string) *Client {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		username:   username,
		password:   password,
		httpClient: httpClient,
	}
}

// FileInfo represents information about a file or directory
type FileInfo struct {
	Path         string
	IsDir        bool
	Size         int64
	ModifiedTime time.Time
	ETag         string
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

// ListFiles lists all files in a directory recursively
func (c *Client) ListFiles(dirPath string, includeHidden bool) ([]FileInfo, error) {
	// Construct Nextcloud WebDAV path: /files/username/directory
	webdavPath := c.buildWebDAVPath(dirPath)

	var files []FileInfo
	err := c.walkDir(webdavPath, dirPath, &files, includeHidden)
	return files, err
}

// ListDir lists only the immediate children of a directory (non-recursive)
func (c *Client) ListDir(dirPath string, includeHidden bool) ([]FileInfo, error) {
	// Construct Nextcloud WebDAV path: /files/username/directory
	webdavPath := c.buildWebDAVPath(dirPath)
	
	// Ensure path ends with / for directories
	if !strings.HasSuffix(webdavPath, "/") {
		webdavPath += "/"
	}

	req, err := http.NewRequest("PROPFIND", c.baseURL+webdavPath, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Depth", "1")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PROPFIND failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse WebDAV XML response
	items, err := parsePropfindResponse(body, c.baseURL)
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for _, item := range items {
		// Normalize the item path for comparison (remove Nextcloud prefixes)
		normalizedItemPath := c.normalizePathForComparison(item.Path)
		normalizedWebDAVPath := c.normalizePathForComparison(webdavPath)
		
		// Skip the directory itself
		if normalizedItemPath == normalizedWebDAVPath || 
		   normalizedItemPath == strings.TrimSuffix(normalizedWebDAVPath, "/") ||
		   strings.TrimSuffix(normalizedItemPath, "/") == normalizedWebDAVPath {
			continue
		}

		// Convert WebDAV path back to relative path
		relativePath := c.extractRelativePath(item.Path, dirPath)
		item.Path = relativePath
		
		// Filter hidden files if not including them
		if !includeHidden && isHidden(relativePath) {
			continue
		}
		
		files = append(files, item)
	}

	return files, nil
}

// buildWebDAVPath constructs the full WebDAV path for Nextcloud
// Input: "/Obsidian" -> Output: "/files/username/Obsidian"
func (c *Client) buildWebDAVPath(dirPath string) string {
	dirPath = strings.TrimPrefix(dirPath, "/")
	if dirPath == "" {
		return "/files/" + c.username + "/"
	}
	return "/files/" + c.username + "/" + dirPath
}

func (c *Client) walkDir(webdavPath string, originalPath string, files *[]FileInfo, includeHidden bool) error {
	// Ensure path ends with / for directories
	if !strings.HasSuffix(webdavPath, "/") {
		webdavPath += "/"
	}

	req, err := http.NewRequest("PROPFIND", c.baseURL+webdavPath, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Depth", "1")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PROPFIND failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse WebDAV XML response
	items, err := parsePropfindResponse(body, c.baseURL)
	if err != nil {
		return err
	}

	for _, item := range items {
		// Normalize paths for comparison
		normalizedItemPath := c.normalizePathForComparison(item.Path)
		normalizedWebDAVPath := c.normalizePathForComparison(webdavPath)
		
		// Skip the directory itself
		if normalizedItemPath == normalizedWebDAVPath || 
		   normalizedItemPath == strings.TrimSuffix(normalizedWebDAVPath, "/") ||
		   strings.TrimSuffix(normalizedItemPath, "/") == normalizedWebDAVPath {
			continue
		}

		// Store the full WebDAV path for recursion
		fullWebDAVPath := item.Path
		
		// Convert WebDAV path back to relative path for storage
		relativePath := c.extractRelativePath(item.Path, originalPath)
		item.Path = relativePath
		
		// Filter hidden files if not including them
		if !includeHidden && isHidden(relativePath) {
			// Still need to recurse into hidden directories if they exist
			// but skip adding them to the results
			if item.IsDir {
				if !strings.HasSuffix(fullWebDAVPath, "/") {
					fullWebDAVPath += "/"
				}
				if err := c.walkDir(fullWebDAVPath, relativePath, files, includeHidden); err != nil {
					return err
				}
			}
			continue
		}
		
		*files = append(*files, item)

		// Recursively walk subdirectories using the full WebDAV path
		if item.IsDir {
			// Ensure the path ends with / for directories
			if !strings.HasSuffix(fullWebDAVPath, "/") {
				fullWebDAVPath += "/"
			}
			if err := c.walkDir(fullWebDAVPath, relativePath, files, includeHidden); err != nil {
				return err
			}
		}
	}

	return nil
}

// normalizePathForComparison normalizes a path by removing Nextcloud prefixes
// Used for comparing paths regardless of their format
func (c *Client) normalizePathForComparison(path string) string {
	// Remove /remote.php/dav/files/username/ prefix if present
	nextcloudPrefix := "/remote.php/dav/files/" + c.username + "/"
	path = strings.TrimPrefix(path, nextcloudPrefix)
	
	// Also handle /files/username/ prefix
	filesPrefix := "/files/" + c.username + "/"
	path = strings.TrimPrefix(path, filesPrefix)
	
	// Ensure it starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	
	return path
}

// extractRelativePath extracts the relative path from a full WebDAV path
// Removes Nextcloud-specific prefixes like /remote.php/dav/files/username/
func (c *Client) extractRelativePath(webdavPath, baseDir string) string {
	// Remove /remote.php/dav/files/username/ prefix if present
	nextcloudPrefix := "/remote.php/dav/files/" + c.username + "/"
	webdavPath = strings.TrimPrefix(webdavPath, nextcloudPrefix)
	
	// Also handle /files/username/ prefix (without remote.php/dav)
	filesPrefix := "/files/" + c.username + "/"
	webdavPath = strings.TrimPrefix(webdavPath, filesPrefix)
	
	// Ensure it starts with /
	if !strings.HasPrefix(webdavPath, "/") {
		webdavPath = "/" + webdavPath
	}
	
	// Remove trailing slash for files (but keep / for root)
	webdavPath = strings.TrimSuffix(webdavPath, "/")
	if webdavPath == "" {
		webdavPath = "/"
	}
	
	return webdavPath
}

// Stat gets information about a specific file
func (c *Client) Stat(filePath string) (*FileInfo, error) {
	webdavPath := c.buildWebDAVPath(filePath)

	req, err := http.NewRequest("PROPFIND", c.baseURL+webdavPath, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Depth", "0")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PROPFIND failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	items, err := parsePropfindResponse(body, c.baseURL)
	if err != nil {
		return nil, err
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	result := items[0]
	result.Path = c.extractRelativePath(result.Path, filePath)
	return &result, nil
}

