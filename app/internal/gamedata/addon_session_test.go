package gamedata

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/MTEKode/companeroWoW/app/internal/bridge"
)

// addonSessionLua is a SavedVariables file with one character whose session block has the
// shape of the on-demand contract. The session started an hour ago; the character needs 1000 XP
// for the level and has 600 XP of the session's 600 gained.
func addonSessionLua(started, updated int64) string {
	return fmt.Sprintf(`CompaneroWoWDB = {
	["characters"] = {
		["Thrall-Forever"] = {
			["updatedAt"] = %[3]d,
			["character"] = { ["name"] = "Thrall", ["level"] = 19, ["xp"] = 300, ["xpMax"] = 1000 },
			["session"] = {
				["startedAt"] = %[1]d, ["updatedAt"] = %[2]d, ["playedSec"] = 3600,
				["level"] = { ["start"] = 18, ["now"] = 19 },
				["xpGained"] = 600, ["xpPerHour"] = 600,
				["money"] = { ["start"] = 6000, ["now"] = 9000 },
				["questsCompleted"] = { 166, 167 }, ["questsAccepted"] = { 168 },
				["kills"] = 42, ["deaths"] = 1, ["zones"] = { 1433, 1436 },
				["timeSplit"] = { ["combatSec"] = 900, ["mountedSec"] = 600, ["restingSec"] = 120, ["otherSec"] = 1980 },
				["xpSamples"] = {
					{ ["t"] = %[1]d, ["xp"] = 0, ["level"] = 18 },
					{ ["t"] = %[1]d + 1800, ["xp"] = 300, ["level"] = 18 },
					{ ["t"] = %[1]d + 3600, ["xp"] = 300, ["level"] = 19 },
				},
				["previous"] = {
					{ ["startedAt"] = %[1]d - 7200, ["endedAt"] = %[1]d - 3600, ["playedSec"] = 3600, ["xpGained"] = 200,
					  ["levelStart"] = 18, ["levelEnd"] = 18, ["moneyDelta"] = 500, ["questsCompleted"] = 3, ["kills"] = 20, ["deaths"] = 0 },
				},
			},
		},
	},
}
`, started, updated, updated)
}

func TestAddonSessionFeedsSummaryFromSavedVariables(t *testing.T) {
	started := time.Now().Add(-time.Hour).Unix()
	g := svService(t, addonSessionLua(started, time.Now().Unix()))
	g.SetOnDemand(true)

	m := objOf(t, g.SessionSummary)
	if m["available"] != true {
		t.Fatalf("summary not available: %v", m)
	}
	if m["enCurso"] != true {
		t.Errorf("enCurso = %v, want true (the addon's session is the running one)", m["enCurso"])
	}
	if m["nivelInicio"] != 18 || m["nivelFin"] != 19 || m["nivelesSubidos"] != 1 {
		t.Errorf("levels = %v %v %v, want 18 19 1", m["nivelInicio"], m["nivelFin"], m["nivelesSubidos"])
	}
	if pct, _ := m["xpGanadoPct"].(float64); pct != 60 {
		t.Errorf("xpGanadoPct = %v, want 60 (600 XP of 1000)", m["xpGanadoPct"])
	}
	if m["dineroCambio"] != int64(3000) {
		t.Errorf("dineroCambio = %v, want 3000", m["dineroCambio"])
	}
	if m["muertes"] != 1 {
		t.Errorf("muertes = %v, want 1", m["muertes"])
	}
	if m["enemigosDerrotados"] != 42 {
		t.Errorf("enemigosDerrotados = %v, want the addon's 42 kills", m["enemigosDerrotados"])
	}
	if quests, _ := m["misionesCompletadas"].([]map[string]any); len(quests) != 2 {
		t.Errorf("misionesCompletadas = %v, want the two ids of the session", m["misionesCompletadas"])
	}
	if zones, _ := m["zonas"].([]string); len(zones) != 2 {
		t.Errorf("zonas = %v, want two zones", m["zonas"])
	}
	if txt, _ := m["texto"].(string); !strings.Contains(txt, "Sesión en curso") {
		t.Errorf("texto = %q, want the running-session line", txt)
	}
}

func TestAddonSessionFeedsLevelingPace(t *testing.T) {
	started := time.Now().Add(-time.Hour).Unix()
	g := svService(t, addonSessionLua(started, time.Now().Unix()))
	g.SetOnDemand(true)

	v, err := g.LevelingPace(20)
	if err != nil {
		t.Fatal(err)
	}
	m := asMap(v)
	if m["available"] != true {
		t.Fatalf("pace not available: %v", m)
	}
	cur := asMap(m["sesionActual"])
	if cur == nil {
		t.Fatalf("sesionActual missing: %v", m)
	}
	if pct, _ := cur["pct"].(float64); pct != 60 {
		t.Errorf("sesionActual.pct = %v, want 60", cur["pct"])
	}
	if _, ok := m["ultimasSesiones"]; !ok {
		t.Errorf("ultimasSesiones missing: the addon's previous session should be listed: %v", m)
	}
}

// Without a session block (the addon has not sent one yet) the summary says so instead of
// falling back to the app's own tracker, which the on-demand mode does not run.
func TestAddonSessionMissingIsReportedNotInvented(t *testing.T) {
	lua := "CompaneroWoWDB = {\n\t[\"characters\"] = {\n\t\t[\"Thrall-Forever\"] = {\n\t\t\t[\"updatedAt\"] = 1793880000,\n" +
		"\t\t\t[\"character\"] = { [\"level\"] = 19, [\"xpMax\"] = 1000 },\n\t\t},\n\t},\n}\n"
	g := svService(t, lua)
	g.SetOnDemand(true)
	m := objOf(t, g.SessionSummary)
	if m["available"] != false {
		t.Fatalf("summary without an addon session must be unavailable: %v", m)
	}
	if hint, _ := m["hint"].(string); !strings.Contains(hint, "/compa sync") && !strings.Contains(hint, "/reload") {
		t.Errorf("hint = %q, want the instruction to make the addon send the session", hint)
	}
}

// A session message that arrived after the saved one wins (the live channel is newer).
func TestAddonSessionLiveMessageIsNewerThanSaved(t *testing.T) {
	started := time.Now().Add(-time.Hour).Unix()
	savedAt := time.Now().Add(-30 * time.Minute)
	g := svService(t, addonSessionLua(started, savedAt.Unix()))
	g.SetOnDemand(true)

	r := bridge.NewReader(time.Second)
	live := `{"startedAt":` + fmt.Sprint(started) + `,"level":{"start":18,"now":19},"xpGained":800,"money":{"start":0,"now":0},"questsCompleted":[],"zones":[],"deaths":0}`
	if err := r.Channel().Data.Put(bridge.MsgSession, []byte(live), time.Now()); err != nil {
		t.Fatal(err)
	}
	g.reader = r

	m := objOf(t, g.SessionSummary)
	if pct, _ := m["xpGanadoPct"].(float64); pct != 80 {
		t.Errorf("xpGanadoPct = %v, want 80 from the live message (newer than the saved 600)", m["xpGanadoPct"])
	}
}

// The on-demand service pauses the SavedVariables checks; RefreshSavedVariables still reads the
// file when the app asks.
func TestOnDemandServiceDoesNotStartWatcher(t *testing.T) {
	g := svService(t, addonSessionLua(time.Now().Unix(), time.Now().Unix()))
	g.SetOnDemand(true)
	if !g.OnDemand() {
		t.Fatal("OnDemand() = false after SetOnDemand(true)")
	}
	if err := g.RefreshSavedVariables(); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}

func TestAddonSamplesAreCumulativeSessionXP(t *testing.T) {
	// The addon's xp is the session's cumulative xpGained, so it only grows, also across a level-up.
	list := []any{
		map[string]any{"t": float64(1000), "xp": float64(100), "level": float64(18)},
		map[string]any{"t": float64(1060), "xp": float64(150), "level": float64(18)},
		map[string]any{"t": float64(1120), "xp": float64(1170), "level": float64(19)},
	}
	got := addonSamples(list, 1000)
	if len(got) != 3 {
		t.Fatalf("samples = %d, want 3", len(got))
	}
	want := []float64{10, 15, 117}
	for i, w := range want {
		if math.Abs(got[i].CumPct-w) > 1e-9 {
			t.Errorf("sample %d cum = %v, want %v", i, got[i].CumPct, w)
		}
	}
	if got[2].Level != 19 {
		t.Errorf("level of the last sample = %d, want 19", got[2].Level)
	}
	if addonSamples(list, 0) != nil {
		t.Error("samples without a known XP maximum must be dropped")
	}
}
