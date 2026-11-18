package tools

import (
	"context"
	"encoding/json"
)

// withProfileNotes wraps the tools that rank things so that their answers carry what the player's
// profile means for the ranking. optimizar_talentos names the objective it optimizes: the engine
// ranks builds by kills per hour and has no survival objective yet, so a hardcore player is
// pointed at the death fraction of each option. analizar_equipo gets the preferred role and the
// hardcore preference for the stats it puts first. The wrapped answer keeps every number the
// tool gave.
func withProfileNotes(r *Registry) {
	for i := range r.defs {
		d := r.defs[i]
		if d.Handler == nil {
			continue
		}
		var note func(p profileView) map[string]any
		switch d.Name {
		case "optimizar_talentos":
			note = talentsProfileNote
		case "analizar_equipo":
			note = gearProfileNote
		default:
			continue
		}
		inner := d.Handler
		r.defs[i].Handler = func(ctx context.Context, args json.RawMessage) (Result, error) {
			res, err := inner(ctx, args)
			if err != nil || res.IsError {
				return res, err
			}
			var m map[string]any
			if len(res.Content) == 0 || json.Unmarshal([]byte(res.Content[0].Text), &m) != nil || m == nil {
				return res, nil
			}
			m["perfil"] = note(profileOf())
			return jsonResult(m)
		}
	}
}

// profileView is the part of the profile the notes use.
type profileView struct {
	Hardcore bool
	Spec     string
}

func profileOf() profileView {
	p := profile()
	return profileView{Hardcore: p.Hardcore, Spec: p.Spec}
}

func talentsProfileNote(p profileView) map[string]any {
	note := map[string]any{"objetivo": "muertes por hora", "hardcore": p.Hardcore}
	if p.Hardcore {
		note["nota"] = "Tienes una sola vida: mira también la fracción de muertes (fraccionMuertes) de cada opción; el motor ordena por muertes por hora, no por supervivencia."
	}
	return note
}

func gearProfileNote(p profileView) map[string]any {
	note := map[string]any{"hardcore": p.Hardcore}
	nota := ""
	if p.Spec != "" {
		note["rol"] = p.Spec
		nota = "Al elegir qué ranura mejorar y qué stats priorizar, sigue el rol del jugador: " + p.Spec + "."
	}
	if p.Hardcore {
		if nota != "" {
			nota += " "
		}
		nota += "Con una sola vida, da más peso al aguante (vida, armadura) que al daño."
	}
	if nota != "" {
		note["nota"] = nota
	}
	return note
}
