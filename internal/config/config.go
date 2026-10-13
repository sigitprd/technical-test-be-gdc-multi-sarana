// Package config loads the application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Environments.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

const minJWTSecretLength = 32

// exampleJWTSecretPrefix starts the placeholder secret in .env.example.
const exampleJWTSecretPrefix = "change-me"

// Config is the application configuration.
type Config struct {
	AppEnv   string
	HTTPPort int
	LogLevel string

	DBURL            string
	DBMaxConns       int32
	DBConnectRetries int
	DBAutoMigrate    bool

	JWTSecret     string
	JWTTTL        time.Duration
	AuthRateLimit int

	IdempotencyTTL          time.Duration
	NotifierSimulateFailure bool
}

// IsProduction reports whether the app runs in production mode.
func (c Config) IsProduction() bool { return c.AppEnv == EnvProduction }

// Load reads the configuration using getenv (usually os.Getenv), so tests
// can supply their own environment.
func Load(getenv func(string) string) (Config, error) {
	r := reader{getenv: getenv}

	cfg := Config{
		AppEnv:   strings.ToLower(r.str("APP_ENV", EnvDevelopment)),
		HTTPPort: r.int("HTTP_PORT", 8080),
		LogLevel: r.str("LOG_LEVEL", "info"),

		DBMaxConns:       int32(r.int("DB_MAX_CONNS", 10)),
		DBConnectRetries: r.int("DB_CONNECT_RETRIES", 10),
		DBAutoMigrate:    r.bool("DB_AUTO_MIGRATE", true),

		JWTSecret: r.str("JWT_SECRET", ""),
		JWTTTL:    r.duration("JWT_TTL", 24*time.Hour),
		// Requests per minute per client IP on /auth endpoints; 0 disables.
		AuthRateLimit: r.int("AUTH_RATE_LIMIT", 60),

		IdempotencyTTL:          r.duration("IDEMPOTENCY_TTL", 24*time.Hour),
		NotifierSimulateFailure: r.bool("NOTIFIER_SIMULATE_FAILURE", false),
	}

	cfg.DBURL = getenv("DATABASE_URL")
	if cfg.DBURL == "" {
		cfg.DBURL = buildDSN(
			r.str("DB_HOST", "localhost"),
			r.str("DB_PORT", "5432"),
			r.str("DB_USER", "postgres"),
			r.str("DB_PASSWORD", "postgres"),
			r.str("DB_NAME", "task_management"),
			r.str("DB_SSLMODE", "disable"),
		)
	}

	if len(r.errs) > 0 {
		return Config{}, errors.Join(r.errs...)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	var errs []error
	if c.AppEnv != EnvDevelopment && c.AppEnv != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q", EnvDevelopment, EnvProduction))
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		errs = append(errs, errors.New("HTTP_PORT must be between 1 and 65535"))
	}
	if len(c.JWTSecret) < minJWTSecretLength {
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d characters", minJWTSecretLength))
	}
	if c.IsProduction() && strings.HasPrefix(c.JWTSecret, exampleJWTSecretPrefix) {
		errs = append(errs, errors.New("JWT_SECRET still holds the example value from .env.example; generate one with `openssl rand -hex 32`"))
	}
	if c.JWTTTL <= 0 {
		errs = append(errs, errors.New("JWT_TTL must be positive"))
	}
	if c.AuthRateLimit < 0 {
		errs = append(errs, errors.New("AUTH_RATE_LIMIT must be zero (disabled) or positive"))
	}
	if c.IdempotencyTTL <= 0 {
		errs = append(errs, errors.New("IDEMPOTENCY_TTL must be positive"))
	}
	if c.DBMaxConns < 1 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be at least 1"))
	}
	return errors.Join(errs...)
}

// buildDSN builds a postgres URL, escaping credentials.
func buildDSN(host, port, user, password, name, sslmode string) string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     host + ":" + port,
		Path:     "/" + name,
		RawQuery: url.Values{"sslmode": []string{sslmode}}.Encode(),
	}
	return u.String()
}

// reader collects parse errors so all of them are reported at once.
type reader struct {
	getenv func(string) string
	errs   []error
}

func (r *reader) str(key, def string) string {
	if v := strings.TrimSpace(r.getenv(key)); v != "" {
		return v
	}
	return def
}

func (r *reader) int(key string, def int) int {
	v := strings.TrimSpace(r.getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be an integer", key))
		return def
	}
	return n
}

func (r *reader) bool(key string, def bool) bool {
	v := strings.TrimSpace(r.getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be a boolean", key))
		return def
	}
	return b
}

func (r *reader) duration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(r.getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s must be a duration such as 30s or 24h", key))
		return def
	}
	return d
}
