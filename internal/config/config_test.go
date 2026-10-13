package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"technical-test-be-gdc-multi-sarana/internal/config"
)

const validSecret = "0123456789abcdef0123456789abcdef"

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{"JWT_SECRET": validSecret}))
	require.NoError(t, err)

	assert.Equal(t, config.Config{
		AppEnv:                  config.EnvDevelopment,
		HTTPPort:                8080,
		LogLevel:                "info",
		DBURL:                   "postgres://postgres:postgres@localhost:5432/task_management?sslmode=disable",
		DBMaxConns:              10,
		DBConnectRetries:        10,
		DBAutoMigrate:           true,
		JWTSecret:               validSecret,
		JWTTTL:                  24 * time.Hour,
		AuthRateLimit:           60,
		IdempotencyTTL:          24 * time.Hour,
		NotifierSimulateFailure: false,
	}, cfg)
	assert.False(t, cfg.IsProduction())
}

func TestLoad_FromEnvironment(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"APP_ENV":                   " PRODUCTION ",
		"HTTP_PORT":                 "9090",
		"LOG_LEVEL":                 "debug",
		"DB_HOST":                   "db",
		"DB_PORT":                   "6543",
		"DB_USER":                   "app",
		"DB_PASSWORD":               "p@ss:w/rd",
		"DB_NAME":                   "tasks",
		"DB_SSLMODE":                "require",
		"DB_MAX_CONNS":              "20",
		"DB_CONNECT_RETRIES":        "3",
		"DB_AUTO_MIGRATE":           "false",
		"JWT_SECRET":                validSecret,
		"JWT_TTL":                   "2h",
		"AUTH_RATE_LIMIT":           "0",
		"IDEMPOTENCY_TTL":           "48h",
		"NOTIFIER_SIMULATE_FAILURE": "true",
	}))
	require.NoError(t, err)

	assert.True(t, cfg.IsProduction())
	assert.Equal(t, 9090, cfg.HTTPPort)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "postgres://app:p%40ss%3Aw%2Frd@db:6543/tasks?sslmode=require", cfg.DBURL, "credentials are escaped")
	assert.EqualValues(t, 20, cfg.DBMaxConns)
	assert.Equal(t, 3, cfg.DBConnectRetries)
	assert.False(t, cfg.DBAutoMigrate)
	assert.Equal(t, 2*time.Hour, cfg.JWTTTL)
	assert.Zero(t, cfg.AuthRateLimit)
	assert.Equal(t, 48*time.Hour, cfg.IdempotencyTTL)
	assert.True(t, cfg.NotifierSimulateFailure)
}

func TestLoad_DatabaseURLOverridesParts(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"JWT_SECRET":   validSecret,
		"DATABASE_URL": "postgres://u:p@h:1/d",
		"DB_HOST":      "ignored",
	}))
	require.NoError(t, err)
	assert.Equal(t, "postgres://u:p@h:1/d", cfg.DBURL)
}

func TestLoad_ParseErrorsAreReportedTogether(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"JWT_SECRET":      validSecret,
		"HTTP_PORT":       "eighty",
		"DB_AUTO_MIGRATE": "maybe",
		"JWT_TTL":         "forever",
	}))

	require.Error(t, err)
	for _, want := range []string{"HTTP_PORT must be an integer", "DB_AUTO_MIGRATE must be a boolean", "JWT_TTL must be a duration"} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := map[string]struct {
		vars map[string]string
		want string
	}{
		"missing secret":           {map[string]string{}, "JWT_SECRET must be at least 32 characters"},
		"short secret":             {map[string]string{"JWT_SECRET": "short"}, "JWT_SECRET must be at least 32 characters"},
		"example secret in prod":   {map[string]string{"APP_ENV": "production", "JWT_SECRET": "change-me-to-a-random-secret-of-at-least-32-chars"}, "example value"},
		"unknown environment":      {map[string]string{"APP_ENV": "staging"}, "APP_ENV must be"},
		"port out of range":        {map[string]string{"HTTP_PORT": "70000"}, "HTTP_PORT must be between"},
		"non-positive token ttl":   {map[string]string{"JWT_TTL": "-1h"}, "JWT_TTL must be positive"},
		"negative rate limit":      {map[string]string{"AUTH_RATE_LIMIT": "-1"}, "AUTH_RATE_LIMIT must be zero"},
		"non-positive idempotency": {map[string]string{"IDEMPOTENCY_TTL": "0s"}, "IDEMPOTENCY_TTL must be positive"},
		"pool without connections": {map[string]string{"DB_MAX_CONNS": "0"}, "DB_MAX_CONNS must be at least 1"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := tc.vars["JWT_SECRET"]; !ok && name != "missing secret" {
				tc.vars["JWT_SECRET"] = validSecret
			}
			_, err := config.Load(env(tc.vars))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestLoad_ExampleSecretAllowedInDevelopment(t *testing.T) {
	_, err := config.Load(env(map[string]string{"JWT_SECRET": "change-me-to-a-random-secret-of-at-least-32-chars"}))
	assert.NoError(t, err)
}
