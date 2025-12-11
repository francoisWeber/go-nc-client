# Nextcloud WebDAV Client

A lightweight HTTP server that monitors remote WebDAV directories and detects changes.

## Features

- Lightweight HTTP server
- WebDAV client integration
- Directory change detection (created, updated, moved, deleted files)
- RESTful API for managing directories and checking changes

## Setup

1. Install dependencies:
```bash
go mod tidy
```

2. Copy the example configuration:
```bash
cp config.json.example config.json
```

3. Edit `config.json` with your WebDAV server details:
```json
{
  "webdav_url": "https://your-nextcloud-server.com/remote.php/dav",
  "username": "your-username",
  "password": "your-password",
  "directories": [
    "/Documents",
    "/Photos"
  ],
  "state_file": "state.json"
}
```

4. Run the server:
```bash
go run main.go
```

Or build and run:
```bash
go build -o go-nc-client
./go-nc-client
```

**Port Configuration:**
The server port can be specified in three ways (in order of priority):
1. Command-line flag: `./go-nc-client --port 3000`
2. Environment variable: `PORT=3000 ./go-nc-client`
3. Default: `8080`

Example:
```bash
./go-nc-client --port 3000
```

## API Endpoints

### GET /health
Health check endpoint.

**Response:**
```json
{
  "status": "ok"
}
```

### GET /directories
Get the list of directories being observed.

**Response:**
```json
[
  "/Documents",
  "/Photos"
]
```

### POST /directories
Update the list of directories to observe.

**Request Body:**
```json
[
  "/Documents",
  "/Photos",
  "/Videos"
]
```

**Response:**
```json
{
  "message": "directories updated"
}
```

### GET /ls
List files and directories in a specific path.

**Query Parameters:**
- `path` (optional): The directory path to list. Defaults to `/` (root).

**Example:**
```bash
curl "http://localhost:8080/ls?path=/Obsidian"
```

**Response:**
```json
{
  "path": "/Obsidian",
  "files": [
    {
      "path": "/Obsidian/file1.md",
      "is_dir": false,
      "size": 1024,
      "modified_time": "2024-01-15T10:30:00Z",
      "etag": "abc123"
    },
    {
      "path": "/Obsidian/subfolder",
      "is_dir": true,
      "size": 0,
      "modified_time": "2024-01-15T09:00:00Z",
      "etag": "def456"
    }
  ]
}
```

### POST /diff
Trigger change detection on all observed directories.

**Response:**
```json
[
  {
    "directory": "/Documents",
    "changes": [
      {
        "type": "created",
        "path": "/Documents/new-file.txt",
        "is_dir": false,
        "size": 1024,
        "modified": "2024-01-15T10:30:00Z"
      },
      {
        "type": "updated",
        "path": "/Documents/existing-file.txt",
        "is_dir": false,
        "size": 2048,
        "modified": "2024-01-15T11:00:00Z"
      },
      {
        "type": "moved",
        "path": "/Documents/new-location.txt",
        "old_path": "/Documents/old-location.txt",
        "is_dir": false,
        "size": 512,
        "modified": "2024-01-15T12:00:00Z"
      },
      {
        "type": "deleted",
        "path": "/Documents/deleted-file.txt",
        "is_dir": false,
        "size": 256,
        "modified": "2024-01-14T09:00:00Z"
      }
    ],
    "timestamp": "2024-01-15T12:30:00Z"
  }
]
```

Change types:
- `created`: New file or directory
- `updated`: File modified (size, content, or modification time changed)
- `moved`: File moved to a new location
- `deleted`: File or directory removed

## Example curl Commands

### Health Check
```bash
curl http://localhost:8080/health
```

Response:
```json
{"status":"ok"}
```

### Get Observed Directories
```bash
curl http://localhost:8080/directories
```

Response:
```json
["/Documents","/Photos"]
```

### Update Observed Directories
```bash
curl -X POST http://localhost:8080/directories \
  -H "Content-Type: application/json" \
  -d '["/Documents", "/Photos", "/Videos"]'
```

Response:
```json
{"message":"directories updated"}
```

### List Directory Contents
```bash
curl "http://localhost:8080/ls?path=/Obsidian"
```

Or list root directory:
```bash
curl http://localhost:8080/ls
```

Response:
```json
{
  "path": "/Obsidian",
  "files": [...]
}
```

### Check for Changes (Diff)
```bash
curl -X POST http://localhost:8080/diff
```

Response:
```json
[
  {
    "directory": "/Documents",
    "changes": [
      {
        "type": "created",
        "path": "/Documents/new-file.txt",
        "is_dir": false,
        "size": 1024,
        "modified": "2024-01-15T10:30:00Z"
      }
    ],
    "timestamp": "2024-01-15T12:30:00Z"
  }
]
```

### Pretty Print JSON Response
To format the JSON output for better readability:
```bash
curl -X POST http://localhost:8080/diff | jq
```

Or using Python:
```bash
curl -X POST http://localhost:8080/diff | python -m json.tool
```

## How It Works

1. The server maintains a state file (`state.json` by default) that stores the last known state of all files in the observed directories.

2. When you call `/diff`, the server:
   - Lists all files in each observed directory recursively
   - Compares the current state with the previous state
   - Detects changes (created, updated, moved, deleted)
   - Updates the state file with the new state
   - Returns the detected changes

3. Move detection works by matching deleted files with created files that have:
   - The same size
   - Similar modification times (within 5 minutes)
   - Non-zero size (to avoid false positives)

## Dependencies

- `golang.org/x/net/webdav` - WebDAV client library

