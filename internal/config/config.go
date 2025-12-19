package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	WebDAVURL string `json:"webdav_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	StateFile string `json:"state_file"`
}

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		// Return default config if file doesn't exist
		// Default to data/state.json if /app/data exists (Docker environment)
		// Otherwise default to state.json (local development)
		defaultStateFile := "state.json"
		if _, err := os.Stat("/app/data"); err == nil {
			defaultStateFile = "data/state.json"
		}
		
		return &Config{
			WebDAVURL: "",
			Username:  "",
			Password:  "",
			StateFile: defaultStateFile,
		}, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.StateFile == "" {
		// Default to data/state.json if /app/data exists (Docker environment)
		// Otherwise default to state.json (local development)
		if _, err := os.Stat("/app/data"); err == nil {
			cfg.StateFile = "data/state.json"
		} else {
			cfg.StateFile = "state.json"
		}
	}

	return &cfg, nil
}

func Save(cfg *Config, filename string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}
