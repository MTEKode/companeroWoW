package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/config"
)

// keyServer answers chat completions and records the Authorization header of each call.
func keyServer(t *testing.T, hits *int32, auth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		*auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"respuesta"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenAICompatMissingKeyNamesVariableNotValue(t *testing.T) {
	const envName = "COMPANERO_TEST_NIM_MISSING"
	t.Setenv(envName, "")
	p := NewOpenAICompat(config.APIProvider{
		Name: "nvidia", BaseURL: "https://x.example/v1", APIKeyEnv: envName, Model: "m",
	}, nil, time.Second)
	err := p.Available()
	if err == nil {
		t.Fatal("missing key must not be available")
	}
	if !strings.Contains(err.Error(), envName) || !strings.Contains(err.Error(), "api:nvidia") {
		t.Fatalf("error should name the provider and variable: %v", err)
	}
}

func TestOpenAICompatKeyFromEnvVarReachesHeader(t *testing.T) {
	const envName = "COMPANERO_TEST_NIM_ENV"
	t.Setenv(envName, "env-secret-value")
	var hits int32
	var auth string
	srv := keyServer(t, &hits, &auth)
	p := NewOpenAICompat(config.APIProvider{
		Name: "nvidia", BaseURL: srv.URL + "/v1", APIKeyEnv: envName, Model: "m",
	}, nil, 5*time.Second)
	if err := p.Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	resp, err := p.Ask(context.Background(), Request{Prompt: "hola"})
	if err != nil || resp.Text != "respuesta" {
		t.Fatalf("Ask = %+v, %v", resp, err)
	}
	if auth != "Bearer env-secret-value" {
		t.Fatalf("Authorization = %q", auth)
	}
}

func TestOpenAICompatLiteralKeyBeatsEnvVar(t *testing.T) {
	const envName = "COMPANERO_TEST_NIM_PRECEDENCE"
	t.Setenv(envName, "from-env")
	var hits int32
	var auth string
	srv := keyServer(t, &hits, &auth)
	p := NewOpenAICompat(config.APIProvider{
		Name: "nvidia", BaseURL: srv.URL + "/v1", APIKey: "literal-key", APIKeyEnv: envName, Model: "m",
	}, nil, 5*time.Second)
	if _, err := p.Ask(context.Background(), Request{Prompt: "hola"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if auth != "Bearer literal-key" {
		t.Fatalf("Authorization = %q, want the literal key", auth)
	}
}

func TestOpenAICompatMissingKeySendsNoRequest(t *testing.T) {
	const envName = "COMPANERO_TEST_NIM_NOCALL"
	t.Setenv(envName, "")
	var hits int32
	var auth string
	srv := keyServer(t, &hits, &auth)
	p := NewOpenAICompat(config.APIProvider{
		Name: "nvidia", BaseURL: srv.URL + "/v1", APIKeyEnv: envName, Model: "m",
	}, nil, 5*time.Second)
	_, err := p.Ask(context.Background(), Request{Prompt: "hola"})
	if err == nil || !strings.Contains(err.Error(), envName) {
		t.Fatalf("Ask err = %v, want missing-key error naming %s", err, envName)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("server was called %d times without a key", n)
	}
}
