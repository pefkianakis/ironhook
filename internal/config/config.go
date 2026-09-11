package config

import "os"

type Config struct {
	HTTPAddr    string
	DatabaseURL string
}

func Load() Config {
	return Config{
		HTTPAddr:    envOrDefault("IRONHOOK_HTTP_ADDR", ":8080"),
		DatabaseURL: os.Getenv("IRONHOOK_DATABASE_URL"),
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
