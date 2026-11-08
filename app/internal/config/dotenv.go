package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dotenvEntry is a parsed .env file, valid while the file keeps its size and mtime.
type dotenvEntry struct {
	mod  time.Time
	size int64
	vars map[string]string
}

var (
	dotenvMu    sync.Mutex
	dotenvCache = map[string]dotenvEntry{}
)

// dotenvPaths lists the .env files searched for API keys, in order: %APPDATA%\CompaneroWoW,
// the folder of the executable, the working directory and the parent of the executable's
// folder (dist\..\.env is the repository root during development).
// DotenvPaths lists the .env files that may hold API keys. The Antigravity sessions deny
// reading them.
func DotenvPaths() []string { return dotenvPaths() }

func dotenvPaths() []string {
	var paths []string
	if dir, err := DefaultDir(); err == nil {
		paths = append(paths, filepath.Join(dir, ".env"))
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		exeDir := filepath.Dir(exe)
		paths = append(paths, filepath.Join(exeDir, ".env"))
		if wd, err := os.Getwd(); err == nil {
			paths = append(paths, filepath.Join(wd, ".env"))
		}
		paths = append(paths, filepath.Join(filepath.Dir(exeDir), ".env"))
	} else if wd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(wd, ".env"))
	}
	return uniquePaths(paths)
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// loadDotenv returns the variables of the .env file at path. The parsed result is cached
// per path and reparsed when the file's size or modification time changes. A missing file
// is an error that wraps fs.ErrNotExist.
func loadDotenv(path string) (map[string]string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", path, err)
	}
	dotenvMu.Lock()
	defer dotenvMu.Unlock()
	if e, ok := dotenvCache[path]; ok && e.mod.Equal(fi.ModTime()) && e.size == fi.Size() {
		return e.vars, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("leer %s: %w", path, err)
	}
	vars := parseDotenv(data)
	dotenvCache[path] = dotenvEntry{mod: fi.ModTime(), size: fi.Size(), vars: vars}
	return vars, nil
}

// parseDotenv reads KEY=VALUE lines. It accepts an optional "export " prefix, blank lines,
// "#" comments (whole-line and after an unquoted value), single or double quoted values,
// CRLF line endings and a UTF-8 byte order mark. Malformed lines are skipped. A repeated key
// keeps its last value. Values are never logged by the caller.
func parseDotenv(data []byte) map[string]string {
	vars := map[string]string{}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = trimExportPrefix(line)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !validDotenvKey(key) {
			continue
		}
		vars[key] = parseDotenvValue(strings.TrimSpace(value))
	}
	return vars
}

func trimExportPrefix(line string) string {
	if len(line) > len("export") && strings.HasPrefix(line, "export") && (line[6] == ' ' || line[6] == '\t') {
		return strings.TrimSpace(line[7:])
	}
	return line
}

func validDotenvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// parseDotenvValue unquotes a value. A quoted value ends at its closing quote; anything after
// it is ignored. An unquoted value ends at an inline comment (" #" or a tab before "#").
func parseDotenvValue(v string) string {
	if v == "" {
		return ""
	}
	if q := v[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(v[1:], q); end >= 0 {
			return v[1 : 1+end]
		}
		return v[1:] // unterminated quote: keep the rest
	}
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			return strings.TrimSpace(v[:i-1])
		}
	}
	return strings.TrimSpace(v)
}
