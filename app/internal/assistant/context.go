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

// playerContextBlock runs the player-context query once, before any provider is called,
// and returns the header and a compact JSON summary. It returns "" when there is no game
// data, so the prompt then carries no such block.
func (a *Assistant) playerContextBlock() string {
	if a.g == nil {
		return ""
	}
	v, err := a.g.PlayerContext()
	if err != nil {
		return ""
	}
	room := contextLimit - utf8.RuneCountInString(contextHeader) - 1
	summary := compactContext(v, room)
	if summary == "" {
		return ""
	}
	return contextHeader + "\n" + summary
}

// compactContext keeps the parts of the player context that most questions need, in
// priority order, as compact JSON of at most limit runes. A part that does not fit is
// left out; for the tracked quest the version without its objectives is tried first.
func compactContext(v any, limit int) string {
	m := asMap(v)
	if m == nil || limit <= 0 {
		return ""
	}
	type part struct {
		key string
		val any
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
	parts := []part{
		{"ubicacion", m["ubicacion"]},
		{"personaje", m["personaje"]},
		{"misionRastreada", m["misionRastreada"]},
		{"ritmo", m["ritmo"]},
		{"datos", m["datos"]},
		{"avisos", m["avisos"]},
		{"dinero", m["dinero"]},
		{"objetivo", m["objetivo"]},
		{"profesiones", m["profesiones"]},
		{"entrenamiento", m["entrenamiento"]},
		{"rutaProfesiones", m["rutaProfesiones"]},
	}
	out := map[string]any{}
	for _, p := range parts {
		if p.val == nil {
			continue
		}
		with := copyFields(out)
		with[p.key] = p.val
		if fitsJSON(with, limit) {
			out = with
			continue
		}
		if p.key == "misionRastreada" && slimQuest != nil {
			with[p.key] = slimQuest
			if fitsJSON(with, limit) {
				out = with
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	data, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(data)
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
