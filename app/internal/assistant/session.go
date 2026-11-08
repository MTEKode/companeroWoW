package assistant

import (
	"errors"
)

// SessionSummary is the short Spanish summary of the play session in progress, or of the
// last saved one. It makes no AI call. When there is no session yet, the text says so.
func (a *Assistant) SessionSummary() (string, error) {
	if a.g == nil {
		return "", errors.New("no hay datos del juego")
	}
	v, err := a.g.SessionSummary()
	if err != nil {
		return "", err
	}
	m := asMap(v)
	if !truthy(m["available"]) {
		if hint, ok := m["hint"].(string); ok && hint != "" {
			return hint, nil
		}
		return "", errors.New("no hay sesión registrada")
	}
	text, _ := m["texto"].(string)
	return text, nil
}
