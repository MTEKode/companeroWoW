package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDevRepoRootOnlyForDistWithAppModule(t *testing.T) {
	repo := t.TempDir()
	dist := filepath.Join(repo, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := devRepoRoot(dist); got != "" {
		t.Errorf("devRepoRoot without app/go.mod = %q, want empty", got)
	}
	if err := os.MkdirAll(filepath.Join(repo, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "app", "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := devRepoRoot(dist); got != repo {
		t.Errorf("devRepoRoot(dist) = %q, want %q", got, repo)
	}
	// Any other folder name is never a development layout, even with a go.mod beside it.
	bin := filepath.Join(repo, "bin")
	if got := devRepoRoot(bin); got != "" {
		t.Errorf("devRepoRoot(bin) = %q, want \"\"", got)
	}
}

func TestDotenvPathsDoNotSearchWorkingDirectory(t *testing.T) {
	wd := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	for _, p := range dotenvPaths() {
		if filepath.Dir(p) == wd || filepath.Clean(p) == filepath.Join(wd, ".env") {
			t.Errorf("dotenv path %s is in the working directory", p)
		}
	}
}
