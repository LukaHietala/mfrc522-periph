package main

import (
	"errors"
	"os"

	"github.com/BurntSushi/toml"
)

// Change if needed
const configFile = "../config.toml"

type Config struct {
	ServerAddr string `toml:"server_address"`
	Reader     struct {
		SecretKey string `toml:"secret_key"`
	}
}

func LoadConfig() (*Config, error) {
	f, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg Config

	_, err = toml.Decode(string(f), &cfg)
	if err != nil {
		return nil, err
	}

	if cfg.ServerAddr == "" {
		return nil, errors.New("server_address is required")
	}
	if cfg.Reader.SecretKey == "" {
		return nil, errors.New("secret_key is required")
	}

	return &cfg, err
}
