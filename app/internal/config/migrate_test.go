package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultsForAntigravity(t *testing.T) {
	d := Defaults()
	if want := []string{"antigravity", "codex", "api"}; !reflect.DeepEqual(d.ProviderOrder, want) {
		t.Errorf("ProviderOrder = %v, want %v", d.ProviderOrder, want)
	}
	if d.AntigravityCommand != "agy" {
		t.Errorf("AntigravityCommand = %q, want agy", d.AntigravityCommand)
	}
	if len(d.APIProviders) != 1 {
		t.Fatalf("APIProviders = %+v, want one NVIDIA entry", d.APIProviders)
	}
	got := d.APIProviders[0]
	want := APIProvider{
		Name:      "nvidia",
		BaseURL:   "https://integrate.api.nvidia.com/v1",
		APIKeyEnv: "NVIDIA_KEY",
		Model:     "meta/llama-4-maverick-17b-128e-instruct",
	}
	if got != want {
		t.Errorf("default provider = %+v, want %+v", got, want)
	}
	if got.APIKey != "" {
		t.Errorf("default provider must not carry a literal key")
	}
}

func TestLoadMigratesGeminiToAntigravityAndSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"providerOrder":["gemini","codex","api"],"geminiCommand":"gemini","apiProviders":[{"name":"x","baseUrl":"https://x.example/v1","apiKey":"literal","model":"m"}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"antigravity", "codex", "api"}; !reflect.DeepEqual(c.ProviderOrder, want) {
		t.Fatalf("ProviderOrder = %v, want %v", c.ProviderOrder, want)
	}
	if len(c.APIProviders) != 1 || c.APIProviders[0].APIKey != "literal" {
		t.Fatalf("existing API providers must be kept: %+v", c.APIProviders)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "geminiCommand") || strings.Contains(string(saved), `"gemini"`) {
		t.Fatalf("migrated file still mentions gemini:\n%s", saved)
	}
	if !strings.Contains(string(saved), `"antigravity"`) {
		t.Fatalf("migrated file lacks antigravity:\n%s", saved)
	}
}

func TestMigrateDedupesAntigravityAndFillsEmptyAPIs(t *testing.T) {
	c := Defaults()
	c.ProviderOrder = []string{"gemini", "antigravity", "codex"}
	c.APIProviders = []APIProvider{}
	if !c.migrate() {
		t.Fatal("migrate reported no change")
	}
	if want := []string{"antigravity", "codex"}; !reflect.DeepEqual(c.ProviderOrder, want) {
		t.Errorf("ProviderOrder = %v, want %v", c.ProviderOrder, want)
	}
	if len(c.APIProviders) != 1 || c.APIProviders[0].Name != "nvidia" {
		t.Errorf("APIProviders = %+v, want NVIDIA default", c.APIProviders)
	}
}

func TestMigrateNoChangeForCurrentConfig(t *testing.T) {
	c := Defaults()
	if c.migrate() {
		t.Fatal("migrate changed a current default config")
	}
}

func TestLoadKeepsExplicitEmptyAPIsAsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"apiProviders":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.APIProviders) != 1 || c.APIProviders[0].APIKeyEnv != "NVIDIA_KEY" {
		t.Fatalf("APIProviders = %+v", c.APIProviders)
	}
}
