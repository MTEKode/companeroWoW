package assistant

import "github.com/MTEKode/companeroWoW/app/internal/skill"

// SystemPrompt is the companion's instruction set. It is derived from the companero skill
// (internal/skill/SKILL.md), the single source of truth: the API providers send it as the
// system prompt and the CLI providers read the same text from their own files.
var SystemPrompt = skill.SystemPrompt()

// proactiveInstruction is appended to the prompt of each proactive tip. Tips use the
// skill's answer format, shortened.
const proactiveInstruction = "Responde en castellano en como máximo 2 líneas cortas y, si importa un lugar, añade una línea «📍 /compa marca <x> <y> <texto>». Usa el contexto del jugador que trae la pregunta; si no hay datos, dilo en una línea."

// tipPrompts maps a LiveState event code to the question asked for a proactive tip.
var tipPrompts = map[uint8]string{
	1: "Acabo de subir de nivel. ¿Qué misión y en qué zona hago ahora para seguir subiendo?",
	2: "Acabo de completar los objetivos de una misión. ¿Dónde la entrego y cuál es el siguiente paso?",
	3: "Acabo de entregar una misión. ¿Qué misión me conviene hacer ahora según mi nivel y zona?",
	4: "Acabo de cambiar de zona. ¿Qué misiones me convienen aquí para mi nivel?",
	5: "Mi habilidad en una profesión acaba de cambiar. ¿Conviene entrenar ahora o esperar?",
	6: "Acabo de aceptar una misión. ¿Dónde está su primer objetivo?",
}

// tipTitles is the card title shown for each event code.
var tipTitles = map[uint8]string{
	1: "¡Subiste de nivel!",
	2: "Objetivos completados",
	3: "Misión entregada",
	4: "Nueva zona",
	5: "Profesión actualizada",
	6: "Misión aceptada",
}
