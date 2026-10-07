package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL       string
	ServiceKey        string
	LandAPIURL        string
	CharacteristicURL string
	PriceURL          string
	UsePlanURL        string
	Workers           int
	LandRPS           int
	CharacteristicRPS int
	PriceRPS          int
	UsePlanRPS        int
	HTTPTimeout       time.Duration
	MaxRetries        int
	BatchSize         int
	PNUFile           string
}

func Load() (Config, error) {
	defaultRPS := envInt("REQUESTS_PER_SECOND", 10)
	cfg := Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		ServiceKey:        os.Getenv("DATA_GO_KR_SERVICE_KEY"),
		LandAPIURL:        os.Getenv("LAND_API_URL"),
		CharacteristicURL: os.Getenv("CHARACTERISTIC_API_URL"),
		PriceURL:          os.Getenv("PRICE_API_URL"),
		UsePlanURL:        os.Getenv("USE_PLAN_API_URL"),
		Workers:           envInt("WORKERS", 16),
		LandRPS:           envInt("LAND_RPS", defaultRPS),
		CharacteristicRPS: envInt("CHARACTERISTIC_RPS", defaultRPS),
		PriceRPS:          envInt("PRICE_RPS", defaultRPS),
		UsePlanRPS:        envInt("USE_PLAN_RPS", defaultRPS),
		HTTPTimeout:       time.Duration(envInt("HTTP_TIMEOUT_SECONDS", 20)) * time.Second,
		MaxRetries:        envInt("MAX_RETRIES", 4),
		BatchSize:         envInt("BATCH_SIZE", 200),
		PNUFile:           envString("PNU_FILE", "/data/pnu.txt"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.ServiceKey == "" {
		return Config{}, fmt.Errorf("DATA_GO_KR_SERVICE_KEY is required")
	}
	if cfg.Workers < 1 || cfg.BatchSize < 1 || cfg.MaxRetries < 0 ||
		cfg.LandRPS < 1 || cfg.CharacteristicRPS < 1 || cfg.PriceRPS < 1 || cfg.UsePlanRPS < 1 {
		return Config{}, fmt.Errorf("invalid numeric configuration")
	}

	for name, endpoint := range map[string]string{
		"LAND_API_URL":           cfg.LandAPIURL,
		"CHARACTERISTIC_API_URL": cfg.CharacteristicURL,
		"PRICE_API_URL":          cfg.PriceURL,
		"USE_PLAN_API_URL":       cfg.UsePlanURL,
	} {
		if endpoint == "" {
			return Config{}, fmt.Errorf("%s is required", name)
		}
		if !strings.Contains(endpoint, "{serviceKey}") || !strings.Contains(endpoint, "{pnu}") {
			return Config{}, fmt.Errorf("%s must contain {serviceKey} and {pnu}", name)
		}
	}

	return cfg, nil
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
