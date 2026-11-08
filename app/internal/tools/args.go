package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Tool arguments are read leniently: a model often names a parameter in another way
// ("query" for "consulta", "questID" for "quest_id") or sends a number as text. Each tool
// lists the canonical name first and the accepted aliases after it; keys are compared
// after lower-casing and dropping "_" and "-", so "quest_id", "questID" and "questId" are
// the same key. Unknown keys are ignored. The schemas keep the canonical names only.

// Alias lists, canonical name first.
var (
	aliasText    = []string{"consulta", "query", "q", "texto", "text", "busqueda", "búsqueda", "search"}
	aliasSite    = []string{"sitio", "site", "dominio", "domain"}
	aliasQuestID = []string{"quest_id", "id", "questid", "mision"}
	aliasMapID   = []string{"map_id", "mapid", "mapa"}
	aliasURL     = []string{"url", "link", "enlace"}
	aliasLimit   = []string{"limite", "limit", "max"}
)

// leadingInt finds the first whole number in a text such as "95999" or "#95999".
var leadingInt = regexp.MustCompile(`-?\d+`)

// argValues is the object of a tool call, keyed by the raw names the model sent.
type argValues map[string]json.RawMessage

// parseArgs reads the arguments of a call. Empty or null arguments are an empty object;
// anything that is not a JSON object is an error.
func parseArgs(args json.RawMessage) (argValues, error) {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return argValues{}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("argumentos no válidos: %v", err)
	}
	if raw == nil {
		return argValues{}, nil
	}
	return argValues(raw), nil
}

// normKey lower-cases a key and drops the separators that models vary.
func normKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(k)
}

// raw returns the value of the first alias present (aliases are in priority order). Null
// values count as absent. When two keys match the same alias, the smaller raw name wins,
// so the result does not depend on map order.
func (a argValues) raw(aliases ...string) (json.RawMessage, bool) {
	best, bestKey := -1, ""
	var bestVal json.RawMessage
	for k, v := range a {
		if string(bytes.TrimSpace(v)) == "null" {
			continue
		}
		idx := -1
		nk := normKey(k)
		for i, alias := range aliases {
			if normKey(alias) == nk {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		if best < 0 || idx < best || (idx == best && k < bestKey) {
			best, bestKey, bestVal = idx, k, v
		}
	}
	return bestVal, best >= 0
}

// text returns the first non-blank text among the aliases. Numbers are accepted as text.
func (a argValues) text(aliases ...string) (string, bool) {
	v, ok := a.raw(aliases...)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		s = strings.TrimSpace(s)
		return s, s != ""
	}
	var n json.Number
	if err := json.Unmarshal(v, &n); err == nil {
		return n.String(), true
	}
	return "", false
}

// integer returns the whole number held by the first alias present. It accepts JSON
// numbers and text that contains a number ("95999", "#95999", " 95999 "). ok is false
// when no alias is present; an error means the value holds no number at all.
func (a argValues) integer(aliases ...string) (value int64, ok bool, err error) {
	v, found := a.raw(aliases...)
	if !found {
		return 0, false, nil
	}
	var n json.Number
	if json.Unmarshal(v, &n) == nil {
		if i, perr := strconv.ParseInt(n.String(), 10, 64); perr == nil {
			return i, true, nil
		}
		if f, perr := n.Float64(); perr == nil {
			return int64(f), true, nil
		}
		return 0, true, errors.New("debe ser un número entero")
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		if m := leadingInt.FindString(s); m != "" {
			if i, perr := strconv.ParseInt(m, 10, 64); perr == nil {
				return i, true, nil
			}
		}
	}
	return 0, true, errors.New("debe ser un número entero")
}

// intPtr is integer as a pointer, for optional arguments. A value that holds no number
// counts as absent so that a bad limit falls back to the default.
func (a argValues) intPtr(aliases ...string) *int {
	i, ok, err := a.integer(aliases...)
	if !ok || err != nil {
		return nil
	}
	v := int(i)
	return &v
}
