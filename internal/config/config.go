package config

import (
	"fmt"
	"net/url"

	"rubix-fullnode-proxy/internal/constants"
)

type Config struct {
	FullnodeURL     string
	ProxyPort       string
	ProxyBindAddr   string
	ProxySecretKey  string
	RateLimitPerMin int
	RateLimitBurst  int
}

func Load() (*Config, error) {
	if err := loadEnvFile(constants.EnvFileName); err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	cfg := &Config{
		FullnodeURL:     getEnv(constants.EnvFullnodeURL, constants.DefaultFullnodeURL),
		ProxyPort:       getEnv(constants.EnvProxyPort, constants.DefaultProxyPort),
		ProxyBindAddr:   getEnv(constants.EnvProxyBindAddr, constants.DefaultBindAddr),
		ProxySecretKey:  getEnv(constants.EnvProxySecretKey, ""),
		RateLimitPerMin: getEnvInt(constants.EnvRateLimitPerMin, constants.DefaultRateLimitPerMin),
		RateLimitBurst:  getEnvInt(constants.EnvRateLimitBurst, constants.DefaultRateLimitBurst),
	}

	if cfg.ProxySecretKey == "" {
		return nil, fmt.Errorf("%s environment variable is required and cannot be empty", constants.EnvProxySecretKey)
	}

	parsedURL, err := url.ParseRequestURI(cfg.FullnodeURL)
	if err != nil {
		return nil, fmt.Errorf("invalid %s '%s': %w", constants.EnvFullnodeURL, cfg.FullnodeURL, err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("invalid scheme in %s: must be http or https", constants.EnvFullnodeURL)
	}

	return cfg, nil
}
