package tools

import (
	"strings"

	"github.com/MTEKode/companeroWoW/app/internal/theory"
	"github.com/MTEKode/companeroWoW/app/internal/theory/warrior"
)

// The model names its talents and abilities in English (the site's names). The answers show the names the
// player's client shows: the sheet's name of the same spell id (warrior.ClientName). A name with no spell on
// the sheet stays English.

// namesOf returns the translation of the English names of the model for the sheet of the player's client.
func namesOf(sh theory.Sheet) func(string) string {
	return func(english string) string {
		if name, ok := warrior.ClientName(english, sh); ok {
			return name
		}
		return english
	}
}

// localLabel translates a label of the model: a talent label carries its rank ("Improved Tactical Mastery
// 2/5"), and only the name is translated.
func localLabel(label string, tr func(string) string) string {
	if i := strings.LastIndex(label, " "); i > 0 && strings.Contains(label[i+1:], "/") {
		return tr(label[:i]) + label[i:]
	}
	return tr(label)
}

// localReport returns rep with the names of its talents, abilities and rotation in the player's language.
// The numbers are not touched.
func localReport(rep theory.Report, tr func(string) string) theory.Report {
	out := rep
	if rep.Talentos != nil {
		out.Talentos = make(map[string]int, len(rep.Talentos))
		for name, rank := range rep.Talentos {
			out.Talentos[tr(name)] = rank
		}
	}
	if rep.Rotacion != nil {
		out.Rotacion = make([]string, len(rep.Rotacion))
		for i, label := range rep.Rotacion {
			out.Rotacion[i] = localLabel(label, tr)
		}
	}
	if rep.Habilidades != nil {
		out.Habilidades = append([]theory.AbilityStat(nil), rep.Habilidades...)
		for i := range out.Habilidades {
			out.Habilidades[i].Nombre = tr(out.Habilidades[i].Nombre)
		}
	}
	return out
}

// localRotation returns the rotation plan with its priority lists and their reports in the player's language.
func localRotation(plan theory.RotationPlan, tr func(string) string) theory.RotationPlan {
	out := plan
	out.Opciones = make([]theory.RotationOption, len(plan.Opciones))
	for i, opt := range plan.Opciones {
		o := opt
		o.Prioridad = make([]string, len(opt.Prioridad))
		for j, label := range opt.Prioridad {
			o.Prioridad[j] = localLabel(label, tr)
		}
		o.Resultado = localReport(opt.Resultado, tr)
		out.Opciones[i] = o
	}
	return out
}
