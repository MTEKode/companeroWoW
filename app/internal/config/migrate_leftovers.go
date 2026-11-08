package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// leftoverNames are files and folders that earlier versions wrote under the data folder
// and that nothing reads any more: the WebView2 profile of the old window, and the agy
// session settings from before agy ran in its own workspace folder (agy\).
var leftoverNames = []string{"webview2", "antigravity-settings.json"}

// leftoverPrefixes match the renamed copies of the WebView2 profile.
var leftoverPrefixes = []string{"webview2.stale-"}

// CleanupLeftovers removes the leftovers of earlier versions from the data folder dir. It
// returns the names it removed. Missing items are not an error, and anything else in dir
// is left alone.
func CleanupLeftovers(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read data folder %s: %w", dir, err)
	}
	var removed []string
	var firstErr error
	for _, e := range entries {
		if !isLeftover(e.Name()) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(p); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("remove leftover %s: %w", p, err)
			}
			continue
		}
		removed = append(removed, e.Name())
	}
	return removed, firstErr
}

func isLeftover(name string) bool {
	for _, n := range leftoverNames {
		if strings.EqualFold(name, n) {
			return true
		}
	}
	for _, p := range leftoverPrefixes {
		if strings.HasPrefix(strings.ToLower(name), p) {
			return true
		}
	}
	return false
}
