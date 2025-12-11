package webdav

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type propfindResponse struct {
	XMLName xml.Name `xml:"multistatus"`
	Responses []response `xml:"response"`
}

type response struct {
	Href      string   `xml:"href"`
	PropStat  propStat `xml:"propstat"`
}

type propStat struct {
	Status string `xml:"status"`
	Prop   prop   `xml:"prop"`
}

type prop struct {
	DisplayName     string `xml:"displayname"`
	ResourceType    resType `xml:"resourcetype"`
	ContentLength   string `xml:"getcontentlength"`
	ContentType     string `xml:"getcontenttype"`
	LastModified    string `xml:"getlastmodified"`
	ETag            string `xml:"getetag"`
}

type resType struct {
	Collection *struct{} `xml:"collection"`
}

func parsePropfindResponse(body []byte, baseURL string) ([]FileInfo, error) {
	var resp propfindResponse
	if err := xml.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	var files []FileInfo
	for _, r := range resp.Responses {
		// Handle both absolute URLs and relative paths
		path := r.Href
		
		// If it's an absolute URL, extract the path part
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			parsedURL, err := url.Parse(path)
			if err == nil {
				path = parsedURL.Path
			}
		}
		
		// Normalize path - remove baseURL path prefix if present
		// baseURL might be "https://domain.com/remote.php/dav", so extract just the path part
		if parsedBaseURL, err := url.Parse(baseURL); err == nil {
			basePath := parsedBaseURL.Path
			path = strings.TrimPrefix(path, basePath)
		} else {
			// Fallback: try direct string prefix removal
			path = strings.TrimPrefix(path, baseURL)
		}
		
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}

		info := FileInfo{
			Path:  path,
			IsDir: r.PropStat.Prop.ResourceType.Collection != nil,
		}

		// Parse size
		if r.PropStat.Prop.ContentLength != "" {
			var size int64
			fmt.Sscanf(r.PropStat.Prop.ContentLength, "%d", &size)
			info.Size = size
		}

		// Parse modified time
		if r.PropStat.Prop.LastModified != "" {
			// WebDAV uses RFC1123 format
			if t, err := time.Parse(time.RFC1123, r.PropStat.Prop.LastModified); err == nil {
				info.ModifiedTime = t
			} else if t, err := time.Parse(time.RFC1123Z, r.PropStat.Prop.LastModified); err == nil {
				info.ModifiedTime = t
			}
		}

		// Parse ETag
		info.ETag = strings.Trim(r.PropStat.Prop.ETag, "\"")

		files = append(files, info)
	}

	return files, nil
}

