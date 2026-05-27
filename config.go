package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds the configuration values for the proxy service
type Config struct {
	FullnodeURL     string
	ProxyPort       string
	ProxyBindAddr   string
	ProxySecretKey  string
	RateLimitPerMin int
	RateLimitBurst  int
}

// LoadConfig initializes the configuration by reading .env and environment variables
func LoadConfig() (*Config, error) {
	// Try loading from .env file in the current directory
	if err := loadEnvFile(".env"); err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	cfg := &Config{
		FullnodeURL:     getEnv("FULLNODE_URL", "http://localhost:20000"),
		ProxyPort:       getEnv("PROXY_PORT", "8080"),
		ProxyBindAddr:   getEnv("PROXY_BIND_ADDR", "127.0.0.1"),
		ProxySecretKey:  getEnv("PROXY_SECRET_KEY", ""),
		RateLimitPerMin: getEnvInt("RATE_LIMIT_PER_MIN", 60),
		RateLimitBurst:  getEnvInt("RATE_LIMIT_BURST", 10),
	}

	// Validate config
	if cfg.ProxySecretKey == "" {
		return nil, fmt.Errorf("PROXY_SECRET_KEY environment variable is required and cannot be empty")
	}

	// Validate FullnodeURL format
	parsedURL, err := url.ParseRequestURI(cfg.FullnodeURL)
	if err != nil {
		return nil, fmt.Errorf("invalid FULLNODE_URL '%s': %w", cfg.FullnodeURL, err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid scheme in FULLNODE_URL: must be http or https")
	}

	return cfg, nil
}

// getEnv retrieves environment variables with a fallback default value
func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return strings.TrimSpace(value)
	}
	return defaultValue
}

// getEnvInt retrieves an integer environment variable with a fallback default
func getEnvInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return v
		}
	}
	return defaultValue
}

// loadEnvFile reads a file line by line and populates environment variables
func loadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			// It is okay if .env doesn't exist, environment variables could be set in the environment
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines or comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Strip quotes if they surround the value
		if len(value) >= 2 && (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") ||
			strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}

		// Only set if not already set in the environment to allow overriding
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}

	return scanner.Err()
}
