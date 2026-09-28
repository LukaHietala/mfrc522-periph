package main

import (
	"encoding/json"
	"errors"
	"os"
)

const configFile = "config.json"

type Config struct {
	ServerAddr string `json:"server_addr"`
	ReaderID   uint16 `json:"reader_id"`
	SecretKey  string `json:"secret_key"`
}

func LoadConfig() (*Config, error) {
	f, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(f, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func SaveConfig(c *Config) error {
	if c == nil {
		return errors.New("invalid config")
	}
	if c.ServerAddr == "" {
		return errors.New("server address is empty")
	}
	if c.ReaderID == 0 {
		return errors.New("reader id is empty")
	}
	if c.SecretKey == "" {
		return errors.New("secret key is missing")
	}

	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	// Contains secrets so 0600
	return os.WriteFile(configFile, b, 0600)
}

func ResetConfig() error {
	err := os.Remove(configFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
