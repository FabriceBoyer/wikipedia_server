package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type config struct {
	DumpPath string // directory containing the dump files (required)
	Addr     string // listen address
	WebDir   string // directory of the built web UI; served at /
}

// loadConfig reads settings from the environment, with an optional .env file
// filling in variables that are not already set.
func loadConfig() (config, error) {
	loadDotEnv(".env")

	cfg := config{
		DumpPath: os.Getenv("DUMP_PATH"),
		Addr:     ":" + envOr("PORT", "9095"),
		WebDir:   envOr("WEB_DIR", "web/dist"),
	}
	if cfg.DumpPath == "" {
		return cfg, fmt.Errorf("DUMP_PATH is not set (environment or .env file)")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv parses simple KEY=VALUE lines; existing environment wins.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
