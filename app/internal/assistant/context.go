package assistant

import (
	"encoding/json"
	"unicode/utf8"
)

// contextHeader introduces the player context that the assistant consults itself. The
// model is told not to ask for it again, which saves a tool round-trip on every question.
const contextHeader = "Contexto del jugador (ya consultado, no vuelvas a pedirlo salvo que necesites más detalle):"

// contextLimit caps the player context block, header included, in runes.
const contextLimit = 1200

// playerContext runs the player-context query once, before any provider is called. It returns
// nil when there is no game data.
func (a *Assistant) playerContext() any {
	if a.g == nil {
		return nil
	}
	v, err := a.g.PlayerContext()
	if err != nil {
		return nil
	}
	return v
}

// playerContextBlock returns the context block of the first version: every legacy part, up to
// contextLimit runes. It returns "" when there is no game data. The prompt itself uses the
// block of the question's intent (see systemPrompt).
func (a *Assistant) playerContextBlock() string {
	block, _ := contextBlock(a.playerContext(), legacyContextKeys, contextLimit)
	return block
}

// contextBlock is the header and the compact JSON of the given parts of the player context, at
// most limit runes in all, and the parts it kept. It returns "" when none of the parts is there.
func contextBlock(v any, keys []string, limit int) (string, []string) {
	room := limit - utf8.RuneCountInString(contextHeader) - 1
	summary, kept := compactParts(v, keys, room)
	if summary == "" {
		return "", nil
	}
	return contextHeader + "\n" + summary, kept
}

// compactContext keeps the parts of the player context that most questions need, in
// priority order, as compact JSON of at most limit runes. A part that does not fit is
// left out; for the tracked quest the version without its objectives is tried first.
func compactContext(v any, limit int) string {
	s, _ := compactParts(v, legacyContextKeys, limit)
	return s
}

// compactParts keeps the listed parts of the player context, in the order given, as compact
// JSON of at most limit runes. It returns the JSON and the parts it kept. A part that does not
// fit is left out; for the tracked quest the version without its objectives is tried first.
func compactParts(v any, keys []string, limit int) (string, []string) {
	m := asMap(v)
	if m == nil || limit <= 0 {
		return "", nil
	}
	var slimQuest map[string]any
	if mission := asMap(m["misionRastreada"]); mission != nil {
		slimQuest = make(map[string]any, len(mission))
		for k, val := range mission {
			if k != "objetivos" {
				slimQuest[k] = val
			}
		}
	}
	out := map[string]any{}
	var kept []string
	for _, key := range keys {
		val := m[key]
		if val == nil {
			continue
		}
		with := copyFields(out)
		with[key] = val
		if fitsJSON(with, limit) {
			out = with
			kept = append(kept, key)
			continue
		}
		if key == "misionRastreada" && slimQuest != nil {
			with[key] = slimQuest
			if fitsJSON(with, limit) {
				out = with
				kept = append(kept, key)
			}
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", nil
	}
	return string(data), kept
}

// presentKeys lists the keys of allContextKeys that the player context holds at all, whatever the
// limit: the data the game has for this question. The eval compares what a block carries with it.
func presentKeys(v any) []string {
	m := asMap(v)
	var out []string
	for _, k := range allContextKeys {
		if m[k] != nil {
			out = append(out, k)
		}
	}
	return out
}

// copyFields returns a shallow copy of m.
func copyFields(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// fitsJSON reports whether v encodes to at most limit runes.
func fitsJSON(v any, limit int) bool {
	data, err := json.Marshal(v)
	return err == nil && utf8.RuneCount(data) <= limit
}

// trainingHeader introduces the trainable spells added to a level-up tip.
const trainingHeader = "Hechizos que puede entrenar ahora (ya consultado con que_entrenar; úsalo si preguntan qué entrenar):"

// trainingLimit caps the training block, header included, in runes.
const trainingLimit = 700

// trainingBlock is the compact answer of que_entrenar for the level-up tip. It returns ""
// when the addon has no training data.
func (a *Assistant) trainingBlock() string {
	if a.g == nil {
		return ""
	}
	v, err := a.g.TrainingPlan()
	if err != nil {
		return ""
	}
	m := asMap(v)
	if !truthy(m["available"]) {
		return ""
	}
	slim := map[string]any{}
	for _, k := range []string{"clase", "ahora", "costeAhoraTexto", "proximoNivel", "dinero", "puedesPagarAhora", "nota"} {
		if val, ok := m[k]; ok {
			slim[k] = val
		}
	}
	if trs, ok := m["entrenadores"].([]map[string]any); ok && len(trs) > 0 {
		slim["entrenador"] = map[string]any{"nombre": trs[0]["nombre"], "coords": trs[0]["coords"], "marca": trs[0]["marca"]}
	}
	data, err := json.Marshal(slim)
	if err != nil {
		return ""
	}
	return truncateRunes(trainingHeader+"\n"+string(data), trainingLimit)
}
