package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun_InvalidConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(func(string) string { return "" }, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), `"msg":"invalid configuration"`)
	assert.Contains(t, stderr.String(), "JWT_SECRET")
	assert.Empty(t, stdout.String())
}

func TestRun_DatabaseUnavailable(t *testing.T) {
	env := map[string]string{
		"JWT_SECRET":         "0123456789abcdef0123456789abcdef",
		"DB_HOST":            "127.0.0.1",
		"DB_PORT":            "1",
		"DB_CONNECT_RETRIES": "1",
	}
	var stdout, stderr bytes.Buffer

	code := run(func(key string) string { return env[key] }, &stdout, &stderr)

	assert.Equal(t, 1, code)
	assert.Contains(t, stdout.String(), `"msg":"application stopped with error"`)
	assert.Contains(t, stdout.String(), "connect database")
}
