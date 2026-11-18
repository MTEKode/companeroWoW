package bridge

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// Two changed-only deltas of the world block must build one document: the first delta's parts
// stay, the second adds and replaces its own parts.
func TestWorldDeltasAccumulateIntoOneDocument(t *testing.T) {
	d := NewLiveData()
	now := time.Unix(5000, 0)
	first := []byte(`{"flight":{"Alliance":{"costs":{"1>2":{"copper":1200,"seconds":95}}}}}`)
	second := []byte(`{"flight":{"Alliance":{"costs":{"2>1":{"copper":1200,"seconds":100}}}},"vendors":{"3001":{"name":"Hermano Rodrigo"}}}`)
	if err := d.Put(MsgWorld, first, now); err != nil {
		t.Fatal(err)
	}
	if err := d.Put(MsgWorld, second, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	it, ok := d.Get(MsgWorld)
	if !ok {
		t.Fatal("no world item")
	}
	var want any
	_ = json.Unmarshal([]byte(`{"flight":{"Alliance":{"costs":{"1>2":{"copper":1200,"seconds":95},"2>1":{"copper":1200,"seconds":100}}}},"vendors":{"3001":{"name":"Hermano Rodrigo"}}}`), &want)
	if !reflect.DeepEqual(it.Value, want) {
		t.Fatalf("merged world = %v, want both deltas in one document", it.Value)
	}
	if it.Count != 2 || !it.At.Equal(now.Add(time.Minute)) {
		t.Errorf("count %d at %v: want 2 and the second arrival time", it.Count, it.At)
	}
}

// A later delta that changes a leaf replaces only that leaf.
func TestWorldDeltaReplacesOnlyTheChangedLeaf(t *testing.T) {
	d := NewLiveData()
	now := time.Unix(5000, 0)
	_ = d.Put(MsgWorld, []byte(`{"flight":{"Alliance":{"costs":{"1>2":{"copper":1200,"seconds":95}}}}}`), now)
	_ = d.Put(MsgWorld, []byte(`{"flight":{"Alliance":{"costs":{"1>2":{"copper":1300,"seconds":90}}}}}`), now)
	it, _ := d.Get(MsgWorld)
	edge := it.Value.(map[string]any)["flight"].(map[string]any)["Alliance"].(map[string]any)["costs"].(map[string]any)["1>2"].(map[string]any)
	if edge["seconds"] != 90.0 || edge["copper"] != 1300.0 {
		t.Errorf("edge = %v, want the newer values", edge)
	}
}

// Other types are still replaced, not merged.
func TestOtherTypesStillReplaceTheirMessage(t *testing.T) {
	d := NewLiveData()
	now := time.Unix(5000, 0)
	_ = d.Put(MsgCharacter, []byte(`{"money":1,"level":2}`), now)
	_ = d.Put(MsgCharacter, []byte(`{"money":5}`), now)
	it, _ := d.Get(MsgCharacter)
	if !reflect.DeepEqual(it.Value, map[string]any{"money": 5.0}) {
		t.Errorf("character = %v, want only the latest message", it.Value)
	}
}

// The merged world document is what the livecache keeps and gives back after a restart.
func TestWorldDocumentSurvivesExportImport(t *testing.T) {
	d := NewLiveData()
	now := time.Unix(5000, 0)
	_ = d.Put(MsgWorld, []byte(`{"vendors":{"3001":{"name":"A"}}}`), now)
	_ = d.Put(MsgWorld, []byte(`{"trainers":{"2001":{"name":"B"}}}`), now.Add(time.Second))
	items := d.Export()
	found := false
	for _, c := range items {
		if c.Type == MsgWorld {
			found = true
		}
	}
	if !found {
		t.Fatal("world is not exported to the cache")
	}
	back := NewLiveData()
	if n := back.Import(items); n == 0 {
		t.Fatal("nothing imported")
	}
	it, ok := back.Get(MsgWorld)
	if !ok {
		t.Fatal("world not restored")
	}
	m := it.Value.(map[string]any)
	if _, ok := m["vendors"]; !ok {
		t.Error("vendors lost in the cache")
	}
	if _, ok := m["trainers"]; !ok {
		t.Error("trainers lost in the cache")
	}
}
