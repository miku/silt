// Package config loads silt's optional config file,
// <UserConfigDir>/silt/config.toml.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config holds the settings the config file may set. A nil field was not
// set; an empty string was set to "".
type Config struct {
	Model   *string `toml:"model"`
	APIBase *string `toml:"api_base"`
	APIKey  *string `toml:"api_key"`
}

// Path returns the location of the config file, or "" if the platform has no
// config directory.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "silt", "config.toml")
}

// Load reads the config file at [Path]. A missing file is an empty config;
// any other problem, including an unknown key, is an error that names the
// file.
func Load() (Config, error) {
	path := Path()
	if path == "" {
		return Config{}, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := Parse(b)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes a config file's contents. Unknown keys are an error, so a
// typo doesn't silently fall back to a default.
func Parse(b []byte) (Config, error) {
	var cfg Config
	err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&cfg)
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) && len(strict.Errors) > 0 {
		key := strings.Join(strict.Errors[0].Key(), ".")
		row, col := strict.Errors[0].Position()
		return Config{}, fmt.Errorf("unknown field `%s`, expected one of `model`, `api_base`, `api_key` at line %d, column %d", key, row, col)
	}
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}
