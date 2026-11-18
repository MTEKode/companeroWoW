package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnswerCacheMinDefaultsToTenMinutes(t *testing.T) {
	if got := Defaults().AnswerCacheMin; got != 10 {
		t.Errorf("default answerCacheMin = %d, want 10", got)
	}
}

// A config file from before the setting keeps the default; an explicit 0 turns the cache off.
func TestAnswerCacheMinFromFile(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]int{
		"old.json":  DefaultAnswerCacheMin,
		"off.json":  0,
		"long.json": 30,
	}
	bodies := map[string]string{
		"old.json":  `{"verifyAnswers":true}`,
		"off.json":  `{"answerCacheMin":0}`,
		"long.json": `{"answerCacheMin":30}`,
	}
	for name, body := range bodies {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		c, _, err := LoadInfo(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if c.AnswerCacheMin != cases[name] {
			t.Errorf("%s: answerCacheMin = %d, want %d", name, c.AnswerCacheMin, cases[name])
		}
	}
}
