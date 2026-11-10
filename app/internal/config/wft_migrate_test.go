package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOldConfigGetsWowForeverTalentsFirstOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"webSources":["warcraft.wiki.gg","forevermeta.me"],"hideInCombat":true}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.WebSources) == 0 || c.WebSources[0] != WFTHost {
		t.Fatalf("WebSources = %v, want %s first", c.WebSources, WFTHost)
	}
	if !c.WFTAdded {
		t.Error("the migration must record that it ran")
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), `"wftAdded": true`) {
		t.Errorf("the migrated file must keep the flag:\n%s", saved)
	}

	// The user takes the site out: a later load must not put it back.
	c.WebSources = slices.DeleteFunc(c.WebSources, func(s string) bool { return s == WFTHost })
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(again.WebSources, WFTHost) {
		t.Errorf("removed site came back: %v", again.WebSources)
	}
}

func TestMigrationDoesNotDuplicateAListedSite(t *testing.T) {
	c := Defaults()
	c.WFTAdded = false
	c.WebSources = []string{"https://www.wowforevertalents.com/", "warcraft.wiki.gg"}
	if !c.migrate() {
		t.Fatal("the migration should report a change")
	}
	count := 0
	for _, s := range c.WebSources {
		if strings.Contains(s, WFTHost) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("site listed %d times: %v", count, c.WebSources)
	}
}

func TestDefaultsListTheSiteFirst(t *testing.T) {
	d := Defaults()
	if d.WebSources[0] != WFTHost || !d.WFTAdded {
		t.Errorf("defaults = %v (added %v)", d.WebSources, d.WFTAdded)
	}
}
