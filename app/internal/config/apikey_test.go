package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDotenvCases(t *testing.T) {
	src := "\xEF\xBB\xBF" + // UTF-8 BOM
		"# comentario completo\r\n" +
		"\r\n" +
		"NVIDIA_KEY=nvapi-plain\r\n" +
		"export OTHER=exported\n" +
		"  SPACED   =   value with spaces  \n" +
		"DQ=\"double quoted # not a comment\"\n" +
		"SQ='single quoted'\n" +
		"INLINE=abc123 # comentario\n" +
		"TABBED=abc\t#comentario\n" +
		"HASHVALUE=abc#notcomment\n" +
		"EMPTY=\n" +
		"BADLINE\n" +
		"1BAD=x\n" +
		"DUP=first\n" +
		"DUP=last\n" +
		"export\tTABEXPORT=ok\n"
	got := parseDotenv([]byte(src))
	want := map[string]string{
		"NVIDIA_KEY": "nvapi-plain",
		"OTHER":      "exported",
		"SPACED":     "value with spaces",
		"DQ":         "double quoted # not a comment",
		"SQ":         "single quoted",
		"INLINE":     "abc123",
		"TABBED":     "abc",
		"HASHVALUE":  "abc#notcomment",
		"EMPTY":      "",
		"DUP":        "last",
		"TABEXPORT":  "ok",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDotenv =\n%v\nwant\n%v", got, want)
	}
}

func TestParseDotenvUnterminatedQuote(t *testing.T) {
	got := parseDotenv([]byte(`K="sin cerrar`))
	if got["K"] != "sin cerrar" {
		t.Fatalf("K = %q", got["K"])
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDotenvReloadsWhenFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "CACHE_TEST_KEY=first\n")
	v, err := loadDotenv(path)
	if err != nil || v["CACHE_TEST_KEY"] != "first" {
		t.Fatalf("first load = %v, %v", v, err)
	}
	writeFile(t, path, "CACHE_TEST_KEY=second-value\n")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	v, err = loadDotenv(path)
	if err != nil || v["CACHE_TEST_KEY"] != "second-value" {
		t.Fatalf("reload after change = %v, %v", v, err)
	}
}

func TestLoadDotenvMissingFileWrapsNotExist(t *testing.T) {
	_, err := loadDotenv(filepath.Join(t.TempDir(), "nope.env"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestResolveKeyPrecedence(t *testing.T) {
	const envName = "COMPANERO_TEST_PRECEDENCE_KEY"
	dir := t.TempDir()
	first := filepath.Join(dir, "a", ".env")
	second := filepath.Join(dir, "b", ".env")
	writeFile(t, first, envName+"=from-first-file\n")
	writeFile(t, second, envName+"=from-second-file\n")
	paths := []string{first, second}

	p := APIProvider{Name: "nim", APIKey: "literal-key", APIKeyEnv: envName}

	t.Setenv(envName, "from-os-env")
	if got, err := resolveKey(p, paths); err != nil || got != "literal-key" {
		t.Fatalf("literal should win: %q, %v", got, err)
	}
	p.APIKey = ""
	if got, err := resolveKey(p, paths); err != nil || got != "from-os-env" {
		t.Fatalf("env should beat .env: %q, %v", got, err)
	}

	t.Setenv(envName, "")
	if got, err := resolveKey(p, paths); err != nil || got != "from-first-file" {
		t.Fatalf("first .env should win over second: %q, %v", got, err)
	}
	if got, err := resolveKey(p, []string{filepath.Join(dir, "missing", ".env"), second}); err != nil || got != "from-second-file" {
		t.Fatalf("missing file should be skipped: %q, %v", got, err)
	}
}

func TestResolveKeyMissingNamesVariableNotValue(t *testing.T) {
	const envName = "COMPANERO_TEST_MISSING_KEY"
	t.Setenv(envName, "")
	path := filepath.Join(t.TempDir(), ".env")
	writeFile(t, path, "COMPANERO_TEST_MISSING_KEY=\nOTHER_SECRET=sekrit-value-123\n")
	_, err := resolveKey(APIProvider{Name: "nim", APIKeyEnv: envName}, []string{path})
	if err == nil {
		t.Fatal("expected missing-key error")
	}
	if !strings.Contains(err.Error(), "falta la clave: define "+envName+" en el entorno o en .env") {
		t.Fatalf("error = %q", err)
	}
	if strings.Contains(err.Error(), "sekrit-value-123") {
		t.Fatalf("error leaks a value: %q", err)
	}
}

func TestResolveKeyWithoutVariableName(t *testing.T) {
	_, err := resolveKey(APIProvider{Name: "solo"}, nil)
	if err == nil || !strings.Contains(err.Error(), "apiKeyEnv") {
		t.Fatalf("err = %v", err)
	}
}

func TestDotenvPathsDeduplicated(t *testing.T) {
	got := uniquePaths([]string{`C:\a\.env`, `c:\A\.env`, `C:\b\.env`})
	if len(got) != 2 {
		t.Fatalf("uniquePaths = %v", got)
	}
}

func TestDotenvPathsIncludeConfigDirFirst(t *testing.T) {
	paths := dotenvPaths()
	if len(paths) == 0 {
		t.Fatal("no .env search paths")
	}
	dir, err := DefaultDir()
	if err != nil {
		t.Skipf("no user config dir: %v", err)
	}
	if paths[0] != filepath.Join(dir, ".env") {
		t.Fatalf("first path = %q, want %q", paths[0], filepath.Join(dir, ".env"))
	}
}
