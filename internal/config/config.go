package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Поддерживаемые значения APP_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

type Config struct {
	HTTP     HTTPConfig
	Log      LogConfig
	Postgres PostgresConfig
	Redis    RedisConfig
	Skinport SkinportConfig
}

type HTTPConfig struct {
	Port            int
	Env             string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

type LogConfig struct {
	Level  string
	Format string
}

type PostgresConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	QueryTimeout    time.Duration
}

func (p PostgresConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.Database, p.SSLMode,
	)
}

type RedisConfig struct {
	Addr             string
	Password         string
	DB               int
	DialTimeout      time.Duration
	OperationTimeout time.Duration
}

type SkinportConfig struct {
	BaseURL         string
	Timeout         time.Duration
	CacheTTL        time.Duration
	CacheStaleTTL   time.Duration
	DefaultAppID    int
	DefaultCurrency string
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTP: HTTPConfig{
			Port:            getEnvInt("APP_PORT", 8080),
			Env:             getEnv("APP_ENV", "development"),
			ReadTimeout:     time.Duration(getEnvInt("HTTP_READ_TIMEOUT_SECONDS", 10)) * time.Second,
			WriteTimeout:    time.Duration(getEnvInt("HTTP_WRITE_TIMEOUT_SECONDS", 10)) * time.Second,
			IdleTimeout:     time.Duration(getEnvInt("HTTP_IDLE_TIMEOUT_SECONDS", 60)) * time.Second,
			ShutdownTimeout: time.Duration(getEnvInt("HTTP_SHUTDOWN_TIMEOUT_SECONDS", 10)) * time.Second,
		},
		Log: LogConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		Postgres: PostgresConfig{
			Host:            getEnv("POSTGRES_HOST", "localhost"),
			Port:            getEnvInt("POSTGRES_PORT", 5432),
			User:            getEnv("POSTGRES_USER", "postgres"),
			Password:        getEnv("POSTGRES_PASSWORD", "postgres"),
			Database:        getEnv("POSTGRES_DB", "skinport_test"),
			SSLMode:         getEnv("POSTGRES_SSLMODE", "disable"),
			MaxConns:        int32(getEnvInt("POSTGRES_MAX_CONNS", 10)),
			MinConns:        int32(getEnvInt("POSTGRES_MIN_CONNS", 2)),
			MaxConnLifetime: time.Duration(getEnvInt("POSTGRES_MAX_CONN_LIFETIME_MINUTES", 30)) * time.Minute,
			QueryTimeout:    time.Duration(getEnvInt("POSTGRES_QUERY_TIMEOUT_SECONDS", 5)) * time.Second,
		},
		Redis: RedisConfig{
			Addr:             getEnv("REDIS_ADDR", "localhost:6379"),
			Password:         getEnv("REDIS_PASSWORD", ""),
			DB:               getEnvInt("REDIS_DB", 0),
			DialTimeout:      time.Duration(getEnvInt("REDIS_DIAL_TIMEOUT_SECONDS", 3)) * time.Second,
			OperationTimeout: time.Duration(getEnvInt("REDIS_OPERATION_TIMEOUT_SECONDS", 2)) * time.Second,
		},
		Skinport: SkinportConfig{
			BaseURL:         getEnv("SKINPORT_BASE_URL", "https://api.skinport.com"),
			Timeout:         time.Duration(getEnvInt("SKINPORT_TIMEOUT_SECONDS", 10)) * time.Second,
			CacheTTL:        time.Duration(getEnvInt("SKINPORT_CACHE_TTL_SECONDS", 60)) * time.Second,
			CacheStaleTTL:   time.Duration(getEnvInt("SKINPORT_CACHE_STALE_TTL_SECONDS", 600)) * time.Second,
			DefaultAppID:    getEnvInt("DEFAULT_APP_ID", 730),
			DefaultCurrency: getEnv("DEFAULT_CURRENCY", "USD"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// IsProduction возвращает true, если APP_ENV=production.
// Используется для гейтинга dev-only функциональности (Swagger UI и т.п.).
func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.HTTP.Env, EnvProduction)
}

func (c *Config) validate() error {
	if c.HTTP.Port <= 0 || c.HTTP.Port > 65535 {
		return errors.New("APP_PORT must be in (0, 65535]")
	}
	switch strings.ToLower(c.HTTP.Env) {
	case EnvDevelopment, EnvProduction:
		// ok
	default:
		return fmt.Errorf("APP_ENV must be one of [%s, %s], got %q",
			EnvDevelopment, EnvProduction, c.HTTP.Env)
	}
	if c.Postgres.Host == "" {
		return errors.New("POSTGRES_HOST is required")
	}
	if c.Postgres.Database == "" {
		return errors.New("POSTGRES_DB is required")
	}
	if c.Redis.Addr == "" {
		return errors.New("REDIS_ADDR is required")
	}
	if c.Skinport.BaseURL == "" {
		return errors.New("SKINPORT_BASE_URL is required")
	}
	if c.Skinport.CacheTTL <= 0 {
		return errors.New("SKINPORT_CACHE_TTL_SECONDS must be > 0")
	}
	return nil
}

// String возвращает безопасное представление конфига с маскированными секретами.
func (c *Config) String() string {
	return fmt.Sprintf(
		"Config{HTTP: %+v, Log: %+v, Postgres: %s, Redis: %s, Skinport: %+v}",
		c.HTTP, c.Log,
		fmt.Sprintf("Postgres{Host: %s, Port: %d, User: %s, Password: ***, Database: %s, SSLMode: %s}",
			c.Postgres.Host, c.Postgres.Port, c.Postgres.User, c.Postgres.Database, c.Postgres.SSLMode),
		fmt.Sprintf("Redis{Addr: %s, Password: ***, DB: %d}",
			c.Redis.Addr, c.Redis.DB),
		c.Skinport,
	)
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
