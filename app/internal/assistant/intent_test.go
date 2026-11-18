package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MTEKode/companeroWoW/app/internal/ai"
	"github.com/MTEKode/companeroWoW/app/internal/eval"
	"github.com/MTEKode/companeroWoW/app/internal/skill"
	"github.com/MTEKode/companeroWoW/app/internal/store"
)

// intentCases are Spanish questions with the intent a player means by them. They include
// questions without accents, with the usual typos and with two intents in one sentence: the
// first intent named by the question wins only when the cues weigh more.
var intentCases = []struct {
	q    string
	want Intent
}{
	// Quests.
	{"¿Qué hago ahora?", IntentQuests},
	{"que hago ahora", IntentQuests},
	{"¿Dónde entrego mi misión?", IntentQuests},
	{"Donde entrego la mision", IntentQuests},
	{"¿Qué misiones puedo hacer ya?", IntentQuests},
	{"que mision hago", IntentQuests},
	{"¿Qué misiones me convienen en esta zona?", IntentQuests},
	{"que misiones hay en esta zona", IntentQuests},
	{"misoines disponibles", IntentQuests},
	{"¿Cómo subo de nivel?", IntentQuests},
	{"¿Qué misiones dan más XP por minuto?", IntentQuests},
	{"Mision rastreada, ¿qué sigue?", IntentQuests},
	{"¿Dónde está mi próxima misión?", IntentQuests},
	{"qué encargo tengo", IntentQuests},
	{"¿Ya terminé los objetivos de la misión?", IntentQuests},
	{"que misiones me conviene hacer", IntentQuests},
	// Gear.
	{"Analiza mi equipo", IntentGear},
	{"analiza mi equipo", IntentGear},
	{"¿Qué me conviene equiparme?", IntentGear},
	{"¿Qué mejoro?", IntentGear},
	{"¿Dónde consigo la Capucha de Hilo Sagrada?", IntentGear},
	{"como consigo la capusha", IntentGear},
	{"¿Me sirve este casco?", IntentGear},
	{"Mi pecho, ¿qué me pongo?", IntentGear},
	{"¿Qué armadura me conviene para el pecho?", IntentGear},
	{"¿Qué objeto mejora mi ranura de anillo?", IntentGear},
	// Builds, talents, rotation, bars and macros.
	{"Revisa mi rotación", IntentBuild},
	{"revisa mi rotaicon", IntentBuild},
	{"¿Qué talentos me conviene coger?", IntentBuild},
	{"Colócame las barras", IntentBuild},
	{"ordena las barras de acción", IntentBuild},
	{"¿Qué macros uso?", IntentBuild},
	{"¿Cuál es mi build?", IntentBuild},
	{"¿Cuánto dps hago?", IntentBuild},
	{"¿Qué hechizos tengo?", IntentBuild},
	{"Qué atajos pongo en la barra", IntentBuild},
	{"¿Qué estadísticas priorizo?", IntentBuild},
	{"consejo de rotacion para el guerrero", IntentBuild},
	{"quiero una macro para el pull", IntentBuild},
	{"revisa mis macros", IntentBuild},
	// Combat and deaths.
	{"¿Por qué he muerto?", IntentCombat},
	{"Me mataron otra vez, ¿qué hice mal?", IntentCombat},
	{"¿Qué estoy haciendo mal en los combates?", IntentCombat},
	{"combates", IntentCombat},
	{"¿Cuándo uso la poción?", IntentCombat},
	{"¿Qué defensivos tengo?", IntentCombat},
	{"Me muero mucho en las peleas", IntentCombat},
	{"¿Cuánta vida me queda?", IntentCombat},
	{"Pierdo mucha vida al pelear", IntentCombat},
	{"¿Qué combate fue el peor?", IntentCombat},
	// Professions and training.
	{"¿Cuándo entreno mi profesión?", IntentProfessions},
	{"¿Dónde está el instructor de Herboristería?", IntentProfessions},
	{"donde esta el intructor de herboristeria", IntentProfessions},
	{"Como subo sastrería", IntentProfessions},
	{"¿Qué hechizos entreno ahora?", IntentProfessions},
	{"entrenador de alquimia", IntentProfessions},
	{"Quiero aprender cocina", IntentProfessions},
	{"mineria: receta del siguiente rango", IntentProfessions},
	{"¿Qué profesion me conviene?", IntentProfessions},
	{"entrenar mi herreria", IntentProfessions},
	// Gold and trade.
	{"¿Qué hago con mi oro?", IntentTrade},
	{"¿En qué gasto el dinero?", IntentTrade},
	{"¿Cuánto oro tengo?", IntentTrade},
	{"¿Cuánto cuesta la subasta?", IntentTrade},
	{"precio del hierro en la tienda", IntentTrade},
	{"Hazme un bot que farmee oro solo", IntentTrade},
	// Dungeons.
	{"¿Qué mazmorra me conviene?", IntentDungeon},
	{"¿Qué saco de la mazmorra?", IntentDungeon},
	{"mazmorrra de Orgrimmar", IntentDungeon},
	{"¿Qué botín da el jefe de la instancia?", IntentDungeon},
	{"Raid de 10, ¿cuándo?", IntentDungeon},
	// Enemies.
	{"¿Puedo con este élite?", IntentEnemy},
	{"¿Puedo con dos a la vez?", IntentEnemy},
	{"Ese enemigo es muy duro", IntentEnemy},
	{"¿Qué tal es este mob?", IntentEnemy},
	{"enemgio de nivel 30", IntentEnemy},
	{"¿Me puedo enfrentar a este bicho?", IntentEnemy},
	// Pace and session.
	{"¿Cuánto me falta para subir de nivel?", IntentSession},
	{"¿Cuánto tardo en llegar al nivel 40?", IntentSession},
	{"¿Qué ritmo llevo?", IntentSession},
	{"cómo ha ido la sesión", IntentSession},
	{"Dame un resumen de la sesion", IntentSession},
	{"cuanto tiempo llevo jugando", IntentSession},
	{"cuanto me falta pa subir", IntentSession},
	// Travel and places.
	{"¿Dónde está la Ciudad de Lunargenta?", IntentTravel},
	{"¿Cómo llego a Elwynn?", IntentTravel},
	{"Donde estoy?", IntentTravel},
	{"¿Cuál es la ruta a Durotar?", IntentTravel},
	{"coordenadas del mapa", IntentTravel},
	{"¿Dónde está el NPC Tharynn?", IntentTravel},
	// Lore and knowledge of Forever.
	{"¿Quién es Garrosh Hellscream?", IntentLore},
	{"quien es garosh", IntentLore},
	{"Cuéntame la historia de Forever", IntentLore},
	{"¿Qué es el modo ventana?", IntentLore},
	{"Busca en la web cómo se consigue la montura", IntentLore},
	{"lore de los orcos", IntentLore},
	{"¿Quién fue Arthas?", IntentLore},
	{"trasfondo de la Horda", IntentLore},
	// General: the screen, greetings and anything else.
	{"¿Qué hay en mi pantalla?", IntentGeneral},
	{"Captura lo que ves", IntentGeneral},
	{"¿Qué ves?", IntentGeneral},
	{"Hola", IntentGeneral},
	{"gracias", IntentGeneral},
	{"", IntentGeneral},
	{"ayuda", IntentGeneral},
}

func TestClassifyIntentTable(t *testing.T) {
	if len(intentCases) < 60 {
		t.Fatalf("the table has %d questions, want at least 60", len(intentCases))
	}
	for _, c := range intentCases {
		if got := classifyIntent(c.q); got != c.want {
			t.Errorf("classifyIntent(%q) = %s, want %s", c.q, got, c.want)
		}
	}
}

func TestNormalizeQuestionFoldsAccentsAndPunctuation(t *testing.T) {
	cases := map[string]string{
		"¿Qué Hago?":            "que hago",
		"  Dónde   está, NPC! ": "donde esta npc",
		"Sesión 40%":            "sesion 40",
	}
	for in, want := range cases {
		if got := normalizeQuestion(in); got != want {
			t.Errorf("normalizeQuestion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEveryIntentHasAPlaybookExcerpt(t *testing.T) {
	playbookOnce.Do(func() { playbooks = parsePlaybooks(skill.Body()) })
	known := map[string]bool{}
	for _, parts := range playbookParts {
		for _, p := range parts {
			known[p.name] = true
		}
	}
	for name := range playbooks {
		if !known[name] {
			t.Errorf("SKILL.md marks playbook %q, which no intent uses", name)
		}
	}
	for _, in := range intentOrder {
		for _, p := range playbookParts[in] {
			if playbooks[p.name] == "" {
				t.Errorf("playbook part %s of %s is not marked in SKILL.md", p.name, in)
			}
		}
		pb := playbookFor(in, "")
		if pb == "" {
			t.Errorf("intent %s has no playbook excerpt in SKILL.md", in)
			continue
		}
		if !strings.HasPrefix(pb, "Playbook ("+string(in)+"):") {
			t.Errorf("playbook of %s starts %q", in, pb[:min(len(pb), 30)])
		}
		if n := utf8.RuneCountInString(pb); n > 2000 {
			t.Errorf("playbook of %s has %d runes; an excerpt should stay short", in, n)
		}
	}
	if pb := playbookFor(IntentQuests, "¿Qué hago ahora?"); !strings.Contains(pb, "guia_mision") || strings.Contains(pb, "misiones_disponibles") {
		t.Errorf("the quest-step question must get the quest-step playbook only: %s", pb)
	}
	if pb := playbookFor(IntentQuests, "¿Qué misiones puedo hacer ya?"); !strings.Contains(pb, "misiones_disponibles") || strings.Contains(pb, "guia_mision") {
		t.Errorf("the available-quests question must get its own part only: %s", pb)
	}
	if pb := playbookFor(IntentBuild, "¿Cuál es mi build?"); strings.Contains(pb, "consejo_barras") {
		t.Errorf("a talents question must not carry the bars playbook: %s", pb)
	}
}

// contextKeysOf returns the keys of the player context block, or nil when there is none.
func contextKeysOf(t *testing.T, block string) []string {
	t.Helper()
	i := strings.Index(block, contextHeader)
	if i < 0 {
		return nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(block[i:], contextHeader))
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		t.Fatalf("context block is not JSON: %v\n%s", err, payload)
	}
	var keys []string
	for _, k := range allContextKeys {
		if _, ok := doc[k]; ok {
			keys = append(keys, k)
		}
	}
	return keys
}

func TestContextSelectionPerIntent(t *testing.T) {
	a, _ := fixtureAssistant(t)
	questions := map[Intent]string{
		IntentQuests:      "¿Qué hago ahora?",
		IntentGear:        "Analiza mi equipo",
		IntentBuild:       "Revisa mi rotación",
		IntentCombat:      "¿Por qué he muerto?",
		IntentProfessions: "¿Cómo subo Sastrería?",
		IntentTravel:      "¿Dónde está el NPC Tharynn?",
		IntentTrade:       "¿Qué hago con mi oro?",
		IntentDungeon:     "¿Qué mazmorra me conviene?",
		IntentSession:     "¿Cuánto me falta para subir de nivel?",
		IntentEnemy:       "¿Puedo con este élite?",
		IntentGeneral:     "¿Qué hay en mi pantalla?",
	}
	for in, q := range questions {
		if got := classifyIntent(q); got != in {
			t.Fatalf("%q is %s, the test expects %s", q, got, in)
		}
		sys := a.systemPrompt(q)
		block := a.LastContext()
		keys := contextKeysOf(t, block)
		allowed := map[string]bool{}
		for _, k := range intentContextKeys[in] {
			allowed[k] = true
		}
		for _, k := range keys {
			if !allowed[k] {
				t.Errorf("%s: context carries %q, which the intent does not select (%v)", in, k, keys)
			}
		}
		if n := utf8.RuneCountInString(block); n > intentContextLimit {
			t.Errorf("%s: context block has %d runes, limit %d", in, n, intentContextLimit)
		}
		if !strings.HasPrefix(sys, "Playbook ("+string(in)+"):") {
			t.Errorf("%s: system prompt must open with its playbook, got %q", in, sys[:min(len(sys), 40)])
		}
		st := a.LastPromptStats()
		if st.Intent != string(in) || st.Chars != utf8.RuneCountInString(sys) {
			t.Errorf("%s: prompt stats %+v do not match the prompt", in, st)
		}
	}
}

func TestQuestWithTrackedQuestCarriesTheQuest(t *testing.T) {
	a, _ := fixtureAssistant(t)
	a.systemPrompt("¿Qué hago ahora?")
	block := a.LastContext()
	keys := contextKeysOf(t, block)
	for _, want := range []string{"ubicacion", "misionRastreada"} {
		if !hasStr(keys, want) {
			t.Errorf("quests context lacks %q: %v", want, keys)
		}
	}
}

func TestLoreCarriesNoGameData(t *testing.T) {
	a, _ := fixtureAssistant(t)
	sys := a.systemPrompt("¿Quién es Garrosh Hellscream en WoW: Forever?")
	for _, noise := range []string{contextHeader, "Estado actual", "Zona:"} {
		if strings.Contains(sys, noise) {
			t.Errorf("lore must carry nothing but the profile, found %q in %q", noise, sys)
		}
	}
	if a.LastContext() != "" {
		t.Errorf("lore context = %q, want none", a.LastContext())
	}
}

func TestStateBlockOnlyAsFallback(t *testing.T) {
	a, _ := fixtureAssistant(t)
	// Gear with data: the context block carries the state, so "Estado actual" is not repeated.
	if sys := a.systemPrompt("Analiza mi equipo"); strings.Contains(sys, "Estado actual") {
		t.Errorf("the state block repeats data the context block carries: %q", sys)
	}
	// No game data: the state block says so, as before.
	bare := newWithAsker(&fakeAsker{}, nil, openStore(t), nil)
	if sys := bare.systemPrompt("¿Qué hago ahora?"); !strings.Contains(sys, "Estado actual: sin datos del juego.") {
		t.Errorf("without game data the state block must say so: %q", sys)
	}
}

// TestIntentPromptsKeepTheNeededContext: the parts that the scripted questions quote reach their
// prompt, and each question gets the intent the eval expects. It runs without a provider.
func TestIntentPromptsKeepTheNeededContext(t *testing.T) {
	a, _ := fixtureAssistant(t)
	for _, q := range eval.Questions {
		a.systemPrompt(q.Text)
		for _, c := range eval.CheckPrompt(q, a.LastPromptStats()) {
			if !c.OK {
				t.Errorf("%s %q: %s — %s", q.ID, q.Text, c.Name, c.Detail)
			}
		}
	}
}

// TestPromptIsSmallerThanFirstVersion: the data part of each scripted question is no larger than the
// first version's block, and the fixture's totals show the saving.
func TestPromptIsSmallerThanFirstVersion(t *testing.T) {
	a, _ := fixtureAssistant(t)
	var before, after int
	for _, q := range eval.Questions {
		a.systemPrompt(q.Text)
		st := a.LastPromptStats()
		before += st.BaselineChars
		after += st.Chars
	}
	t.Logf("system prompts over the scripted questions: first version %d runes, now %d runes", before, after)
	if after >= before {
		t.Errorf("the intent selection saves nothing: before %d, after %d runes", before, after)
	}
}

func TestPruneHistoryDropsOldTurnsOfOtherIntents(t *testing.T) {
	turns := func(qs ...string) []store.ChatMsg {
		var out []store.ChatMsg
		for _, q := range qs {
			out = append(out,
				store.ChatMsg{Role: "user", Content: q},
				store.ChatMsg{Role: "assistant", Content: "respuesta a " + q, Provider: "codex"})
		}
		return out
	}
	msgs := turns(
		"¿Qué misiones hago?",      // quests, turn 4 back from the newest
		"¿Cuándo entreno?",         // professions, 3 back
		"¿Qué hago con mi oro?",    // trade, 2 back: kept although another intent
		"¿Por qué he muerto?",      // combat, newest
	)
	// The current question is about travel: the trade and combat turns are the two latest, kept;
	// the professions and quest turns are older and about other intents, dropped.
	got := pruneHistory(msgs, IntentTravel)
	if !sameQuestions(got, "¿Qué hago con mi oro?", "¿Por qué he muerto?") {
		t.Fatalf("travel history = %v", questionsOf(got))
	}
	// A question about the quests keeps its own older turn.
	got = pruneHistory(msgs, IntentQuests)
	if !sameQuestions(got, "¿Qué misiones hago?", "¿Qué hago con mi oro?", "¿Por qué he muerto?") {
		t.Fatalf("quests history = %v", questionsOf(got))
	}
}

func TestPruneHistoryKeepsSixTurnsAndNoTips(t *testing.T) {
	var msgs []store.ChatMsg
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			store.ChatMsg{Role: "user", Content: "¿Qué misiones hay? " + string(rune('a'+i))},
			store.ChatMsg{Role: "assistant", Content: "ok", Provider: "codex"})
	}
	msgs = append(msgs, store.ChatMsg{Role: "assistant", Content: "Consejo: entrena.", Provider: "proactivo:gemini"})
	got := pruneHistory(msgs, IntentQuests)
	if len(got) != 2*historyTurns {
		t.Fatalf("history has %d messages, want the last %d turns (%d)", len(got), historyTurns, 2*historyTurns)
	}
	for _, m := range got {
		if strings.Contains(m.Content, "Consejo") {
			t.Fatalf("a proactive tip went back as history: %+v", got)
		}
	}
	if !strings.HasSuffix(got[0].Content, string(rune('a'+8-historyTurns))) {
		t.Errorf("the oldest kept turn is %q, want the turn %d of 8", got[0].Content, 8-historyTurns+1)
	}
}

func TestPruneHistoryCutsLongAnswersAndDanglingAnswers(t *testing.T) {
	long := strings.Repeat("palabra ", 200)
	msgs := []store.ChatMsg{
		{Role: "assistant", Content: "respuesta sin pregunta", Provider: "codex"},
		{Role: "user", Content: "¿Qué hago ahora?"},
		{Role: "assistant", Content: long, Provider: "codex"},
	}
	got := pruneHistory(msgs, IntentQuests)
	if len(got) != 2 {
		t.Fatalf("history = %+v, want the question and its answer only", got)
	}
	if n := utf8.RuneCountInString(got[1].Content); n > historyAnswerRunes+1 {
		t.Errorf("answer has %d runes, want at most %d", n, historyAnswerRunes+1)
	}
}

func sameQuestions(got []ai.Message, want ...string) bool {
	qs := questionsOf(got)
	if len(qs) != len(want) {
		return false
	}
	for i := range want {
		if qs[i] != want[i] {
			return false
		}
	}
	return true
}

// questionsOf returns the user messages of a history, in order.
func questionsOf(msgs []ai.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Role == "user" {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestOsaDistanceCountsSwapsAsOneEdit(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"misoin", "mision", 1},
		{"rotaicon", "rotacion", 1},
		{"enemgi", "enemig", 1},
		{"mision", "mision", 0},
		{"mison", "mision", 1},
		{"abc", "xyz", 3},
	}
	for _, c := range cases {
		if got := osaDistance([]rune(c.a), []rune(c.b)); got != c.want {
			t.Errorf("osaDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParsePlaybooksReadsMarkedBlocks(t *testing.T) {
	body := "intro\n<!-- pb:a -->\n- uno\n<!-- /pb -->\nmedio\n<!-- pb:b -->\n- dos\n<!-- /pb -->\n<!-- pb:a -->\n- tres\n<!-- /pb -->\n<!-- pb:c -->\n<!-- /pb -->"
	got := parsePlaybooks(body)
	if got["a"] != "- uno\n- tres" {
		t.Errorf("a = %q", got["a"])
	}
	if got["b"] != "- dos" {
		t.Errorf("b = %q", got["b"])
	}
	if _, ok := got["c"]; ok {
		t.Errorf("an empty block must not be kept: %v", got)
	}
}

func hasStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
