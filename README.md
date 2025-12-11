# Nextcloud WebDAV Client

A lightweight HTTP server that monitors remote WebDAV directories and detects changes.

## Overview

This Go application provides a RESTful API to monitor Nextcloud (or any WebDAV) directories and detect file changes. It uses ETag-based optimization to efficiently track changes without rescanning unchanged directories.

## Features

- Lightweight HTTP server
- WebDAV client integration
- Directory change detection (created, updated, moved, deleted files)
- RESTful API for managing directories and checking changes
- ETag-based optimization for fast change detection

## Setup

### Local Development

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

The server will start on port 8080 by default (or the port specified in the `PORT` environment variable or `--port` flag).

### Docker Deployment

1. Create `config.json` (copy from `config.json.example` and edit):
```bash
cp config.json.example config.json
# Edit config.json with your WebDAV credentials
```

2. Create data directory for state persistence:
```bash
mkdir -p data
```

3. Build and start with Docker Compose:
```bash
docker-compose up -d
```

4. View logs:
```bash
docker-compose logs -f
```

5. Stop the service:
```bash
docker-compose down
```

The service will:
- Run on port 8083 (mapped from container port 8083)
- Persist `state.json` in the `./data` directory
- Use `config.json` from the host (read-only mount)

**Note**: Make sure `config.json` exists before starting, or the container will use default empty configuration.

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
- `include-hidden` (optional): Boolean flag to include hidden files/directories (those starting with "."). Defaults to `false`.

**Example:**
```bash
# List without hidden files (default)
curl "http://localhost:8080/ls?path=/Obsidian"

# List including hidden files
curl "http://localhost:8080/ls?path=/Obsidian&include-hidden=true"
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
Trigger change detection on directories. Can use configured directories from `config.json` or specify custom paths via query parameter or request body.

**Query Parameters (optional):**
- `path`: Single directory path to scan (simpler for single paths)
- `include-hidden`: Boolean flag (`true`/`false`) to include hidden files/directories

**Request Body (optional):**
```json
{
  "include-hidden": false,
  "paths": ["/Obsidian", "/Documents"]
}
```

- `include-hidden` (optional): Boolean flag to include hidden files/directories in change detection. Defaults to `false`.
- `paths` (optional): Array of directory paths to scan. If not provided, uses directories from `config.json`.

**Priority order:** Query parameter `path` > Request body `paths` > Configured directories from `config.json`

**Examples:**
```bash
# Use configured directories from config.json (default behavior)
curl -X POST http://localhost:8080/diff

# Diff single path via query parameter (simplest)
curl -X POST "http://localhost:8080/diff?path=/Obsidian"

# Diff with include-hidden via query parameter
curl -X POST "http://localhost:8080/diff?path=/Obsidian&include-hidden=true"

# Diff multiple paths via request body
curl -X POST http://localhost:8080/diff \
  -H "Content-Type: application/json" \
  -d '{"paths": ["/Obsidian", "/Documents"]}'

# Include hidden files with custom paths
curl -X POST http://localhost:8080/diff \
  -H "Content-Type: application/json" \
  -d '{"paths": ["/Obsidian"], "include-hidden": true}'
```

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
# List without hidden files (default)
curl "http://localhost:8080/ls?path=/Obsidian"

# List including hidden files
curl "http://localhost:8080/ls?path=/Obsidian&include-hidden=true"

# List root directory
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
# Use configured directories (default)
curl -X POST http://localhost:8080/diff

# Diff single path via query parameter (simplest)
curl -X POST "http://localhost:8080/diff?path=/Obsidian"

# Diff with include-hidden via query parameter
curl -X POST "http://localhost:8080/diff?path=/Obsidian&include-hidden=true"

# Diff multiple paths via request body
curl -X POST http://localhost:8080/diff \
  -H "Content-Type: application/json" \
  -d '{"paths": ["/Obsidian", "/Documents"]}'

# Include hidden files
curl -X POST http://localhost:8080/diff \
  -H "Content-Type: application/json" \
  -d '{"include-hidden": true}'
```

Response:
```json
[
  {
    "directory": "/Documents",
    "changes": [...]
  }
]
```

## How It Works

1. The server maintains a state file (`state.json` by default) that stores the last known state of all files in the observed directories.

2. When you call `/diff`, the server:
   - Checks directory ETags to optimize scanning (skips unchanged directories)
   - Checks subdirectory ETags recursively (skips unchanged subdirectories)
   - Lists all files in each observed directory recursively
   - Compares the current state with the previous state
   - Detects changes (created, updated, moved, deleted)
   - Updates the state file with the new state
   - Returns the detected changes

3. Move detection works by matching deleted files with created files that have:
   - The same size
   - Similar modification times (within 5 minutes)
   - Non-zero size (to avoid false positives)

4. **ETag Optimization**: The server uses directory ETags to skip scanning unchanged directories and subdirectories, making subsequent diff operations much faster.

## Docker Usage

### Building the Image
```bash
docker-compose build
```

### Starting the Service
```bash
docker-compose up -d
```

### Viewing Logs
```bash
docker-compose logs -f go-nc-client
```

### Stopping the Service
```bash
docker-compose down
```

### Accessing the API
When running in Docker, the API is available on port 8083:
```bash
curl http://localhost:8083/health
curl -X POST "http://localhost:8083/diff?path=/Obsidian"
```

### Data Persistence
- State file (`state.json`) is persisted in `./data/state.json` on the host
- Config file (`config.json`) is mounted read-only from the host
- Make sure to set `state_file` in `config.json` to `data/state.json` if you want it in the data directory

## Dependencies

- Standard Go libraries only (no external dependencies required)
- Go 1.25.5 or later

## License

MIT License - see [LICENSE](LICENSE) file for details.

## License

MIT License - see [LICENSE](LICENSE) file for details.
