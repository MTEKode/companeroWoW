package gamedata

import (
	"strings"
	"testing"
)

// The quest hints follow the data: missing data says "Haz /compa sync reload en el juego", and Questie is
// suggested only when the data says it is not installed (questieStatus.available false).
func TestQuestNotaAboutQuestieFollowsTheData(t *testing.T) {
	base := `["character"] = { ["class"] = "Priest", ["level"] = 19 },
		["questWindow"] = { ["available"] = true, ["quests"] = {} }`
	cases := []struct {
		name  string
		extra string
		want  string
		not   string
	}{
		{name: "no Questie data", extra: "", want: syncHint, not: "instal"},
		{name: "Questie not installed", extra: `, ["questieStatus"] = { ["available"] = false }`,
			want: questieMissing, not: "sync reload"},
		{name: "Questie installed", extra: `, ["questieStatus"] = { ["available"] = true, ["version"] = "12.10.0", ["quests"] = 27 }`,
			want: "No tengo misiones accesibles para ese filtro con estos datos.", not: "instal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := svService(t, charLua(base+c.extra))
			m := objOf(t, func() (any, error) { return svc.AvailableQuests("ahora", "") })
			nota, _ := m["nota"].(string)
			if !strings.Contains(nota, c.want) {
				t.Errorf("nota = %q, want it to say %q", nota, c.want)
			}
			if strings.Contains(nota, c.not) {
				t.Errorf("nota = %q, must not say %q", nota, c.not)
			}
		})
	}
}

// A missing quest window asks for a sync and never for Questie, even when Questie is known to be absent: the
// window does not need Questie.
func TestMissingQuestWindowAsksForSyncNotQuestie(t *testing.T) {
	svc := svService(t, charLua(`["character"] = { ["class"] = "Priest", ["level"] = 19 },
		["questWindow"] = { ["available"] = false, ["reason"] = "sin datos" },
		["questieStatus"] = { ["available"] = false }`))
	got := objOf(t, func() (any, error) { return svc.AvailableQuests("ahora", "") })
	if got["hint"] != hintQuestWindow || got["motivo"] != "sin datos" {
		t.Errorf("answer = %#v, want the quest window hint with its reason", got)
	}
	if !strings.Contains(hintQuestWindow, syncHint) {
		t.Errorf("hintQuestWindow = %q, want the sync hint", hintQuestWindow)
	}
	if strings.Contains(hintQuestWindow, "nstal") || strings.Contains(hintQuestWindow, "Questie") {
		t.Errorf("hintQuestWindow = %q, must not mention Questie", hintQuestWindow)
	}
}

// The state's questieStatus is read before the Questie export of the character (the summary is what the
// state keeps; the full export may not be there).
func TestQuestieStatusIsReadFromTheSummaryOrTheExport(t *testing.T) {
	cases := []struct {
		name      string
		fields    string
		installed bool
		known     bool
	}{
		{name: "summary installed", fields: `["questieStatus"] = { ["available"] = true }`, installed: true, known: true},
		{name: "summary missing", fields: `["questieStatus"] = { ["available"] = false }`, installed: false, known: true},
		{name: "export only", fields: `["questie"] = { ["available"] = true, ["version"] = "12.10.0" }`, installed: true, known: true},
		{name: "nothing", fields: ``, known: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := svService(t, charLua(`["character"] = { ["class"] = "Priest", ["level"] = 19 }`+sepIf(c.fields)+c.fields))
			ch, _, ok := svc.activeCharacter()
			if !ok {
				t.Fatal("no active character")
			}
			gd := gearData{}
			gd.readQuestieStatus(ch.data)
			if gd.QuestieKnown != c.known || gd.QuestieInstalled != c.installed {
				t.Errorf("known=%v installed=%v, want known=%v installed=%v", gd.QuestieKnown, gd.QuestieInstalled, c.known, c.installed)
			}
		})
	}
}

// sepIf puts the comma between two Lua table fields when there is a second one.
func sepIf(fields string) string {
	if fields == "" {
		return ""
	}
	return ","
}
