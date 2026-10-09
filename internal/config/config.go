// Package config provides strongly-typed, validated application configuration.
// All environment access is centralized here; services must not call os.Getenv.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string
	Port        int
	BaseURL     string
	DatabaseURL string
	DirectURL   string
	JWT         JWTConfig
	CORS        CORSConfig
	MPesa       MPesaConfig
	Stripe      StripeConfig
	RedisURL    string
	LogLevel    string
	HTTP        HTTPConfig
	// Observability (all optional — zero config = disabled/no-op)
	SentryDSN         string
	SentryEnvironment string
	SentryRelease     string
	MetricsEnabled    bool
	MetricsToken      string // if set, /metrics requires Authorization: Bearer <token>
}

type JWTConfig struct {
	Secret    string
	ExpiresIn time.Duration
}

type CORSConfig struct {
	Origins []string // empty or ["*"] means reflect
}

type MPesaConfig struct {
	Env            string
	ConsumerKey    string
	ConsumerSecret string
	Shortcode      string
	Passkey        string
	CallbackURL    string
	CallbackSecret string
}

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
}

type HTTPConfig struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	MaxBodyBytes int64
}

func Load() (*Config, error) {
	env := getEnv("NODE_ENV", getEnv("APP_ENV", "development"))
	isProd := env == "production"

	jwtSecret := os.Getenv("JWT_SECRET")
	// Background worker does not issue JWTs
	if isProd && jwtSecret == "" && os.Getenv("RESTFLOW_ROLE") != "worker" {
		return nil, fmt.Errorf("JWT_SECRET must be set in production")
	}
	if jwtSecret == "" {
		jwtSecret = "dev-only-change-me"
	}

	expiresIn := 7 * 24 * time.Hour
	if v := os.Getenv("JWT_EXPIRES_IN"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			expiresIn = d
		} else if strings.HasSuffix(v, "d") {
			days, _ := strconv.Atoi(strings.TrimSuffix(v, "d"))
			if days > 0 {
				expiresIn = time.Duration(days) * 24 * time.Hour
			}
		}
	}

	corsRaw := getEnv("CORS_ORIGIN", "*")
	var origins []string
	if corsRaw != "" && corsRaw != "*" {
		for _, o := range strings.Split(corsRaw, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				origins = append(origins, o)
			}
		}
	}

	cfg := &Config{
		Env:         env,
		Port:        getEnvInt("PORT", 3000),
		BaseURL:     os.Getenv("BASE_URL"),
		DatabaseURL: getEnv("DATABASE_URL", "postgresql://postgres:postgres@localhost:5432/restflow?sslmode=disable"),
		DirectURL:   os.Getenv("DIRECT_URL"),
		JWT: JWTConfig{
			Secret:    jwtSecret,
			ExpiresIn: expiresIn,
		},
		CORS: CORSConfig{Origins: origins},
		MPesa: MPesaConfig{
			Env:            getEnv("MPESA_ENV", "sandbox"),
			ConsumerKey:    os.Getenv("MPESA_CONSUMER_KEY"),
			ConsumerSecret: os.Getenv("MPESA_CONSUMER_SECRET"),
			Shortcode:      os.Getenv("MPESA_SHORTCODE"),
			Passkey:        os.Getenv("MPESA_PASSKEY"),
			CallbackURL:    os.Getenv("MPESA_CALLBACK_URL"),
			CallbackSecret: os.Getenv("MPESA_CALLBACK_SECRET"),
		},
		Stripe: StripeConfig{
			SecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
			WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		},
		RedisURL: os.Getenv("REDIS_URL"),
		LogLevel: getEnv("LOG_LEVEL", "info"),
		HTTP: HTTPConfig{
			ReadTimeout:  getEnvDuration("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getEnvDuration("HTTP_WRITE_TIMEOUT", 60*time.Second),
			IdleTimeout:  getEnvDuration("HTTP_IDLE_TIMEOUT", 120*time.Second),
			MaxBodyBytes: int64(getEnvInt("HTTP_MAX_BODY_BYTES", 1<<20)), // 1 MiB
		},
		SentryDSN:         os.Getenv("SENTRY_DSN"),
		SentryEnvironment: getEnv("SENTRY_ENVIRONMENT", env),
		SentryRelease:     getEnv("SENTRY_RELEASE", getEnv("RENDER_GIT_COMMIT", "")),
		MetricsEnabled:    getEnv("METRICS_ENABLED", "true") != "false",
		MetricsToken:      os.Getenv("METRICS_TOKEN"),
	}

	if isProd {
		if cfg.DatabaseURL == "" {
			return nil, fmt.Errorf("DATABASE_URL is required in production")
		}
	}

	return cfg, nil
}

func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
