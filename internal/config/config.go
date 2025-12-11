package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	WebDAVURL   string   `json:"webdav_url"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Directories []string `json:"directories"`
	StateFile   string   `json:"state_file"`
}

func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		// Return default config if file doesn't exist
		return &Config{
			WebDAVURL:   "",
			Username:    "",
			Password:    "",
			Directories: []string{},
			StateFile:   "state.json",
		}, nil
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.StateFile == "" {
		cfg.StateFile = "state.json"
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

