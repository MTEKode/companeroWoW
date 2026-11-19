package store

import (
	"errors"
	"fmt"
	"strings"
)

// The read-only SQL check behind consultar_db. The statement is tokenized the way SQLite reads it
// (comments dropped, string literals and quoted identifiers kept whole), and then:
//
//   - it is a single SELECT or WITH ... SELECT (one optional trailing semicolon);
//   - no statement word that writes, attaches, or reads the schema appears anywhere (INSERT, PRAGMA,
//     ATTACH, sqlite_master, ...), quoted or not;
//   - no identifier names a base table or an internal table (quests, messages, cache, feedback,
//     wft_*...): the views are the only objects the statement may name, and a CTE may not shadow them;
//   - whatever follows FROM or JOIN is a view, a CTE of the statement, or a parenthesized subquery.
//
// The check is the first of two layers. The query then runs on a connection opened read-only
// (mode=ro and PRAGMA query_only), so a statement this check misjudged still cannot write.

// maxSQLLen bounds the statement text.
const maxSQLLen = 4000

// Limits of a query: rows returned by default, the most a caller may ask for, and the time it may run.
const (
	DefaultQueryRows = 100
	MaxQueryRows     = 500
)

// baseTables are the tables behind the views. Their names may not appear in a query at all.
var baseTables = map[string]bool{
	"characters": true, "character_snapshots": true, "quest_log": true, "quest_objectives": true,
	"quest_window": true, "completed_quests": true, "quest_rewards": true, "quest_chains": true,
	"quests": true, "npcs": true, "npc_roles": true, "maps": true, "vendors": true, "vendor_items": true,
	"trainers": true, "trainer_services": true, "flight_nodes": true, "flight_routes": true,
	"flight_known": true, "items": true, "item_stats": true, "consumables": true,
	"equipment_history": true, "gear_candidates": true, "combats": true, "combat_spells": true,
	"combat_taken": true, "deaths": true, "sessions": true, "macros": true, "action_bars": true,
	"dungeons": true, "dungeon_bosses": true, "dungeon_loot": true, "ingest_log": true,
	// Chat history, the web cache, feedback and the wowforevertalents cache: never exposed.
	"messages": true, "cache": true, "feedback": true, "wft_pages": true, "wft_sitemap": true,
	"wft_meta": true, "wft_parsed": true,
}

// deniedWords may not appear as an identifier or keyword (quoted or not): statements that write, attach,
// or read the schema, and functions that reach the file system or load code.
var deniedWords = map[string]bool{
	"insert": true, "update": true, "delete": true, "drop": true, "alter": true, "create": true,
	"replace": false, // a function name too (replace(x, a, b)); handled by the statement check
	"attach":  true, "detach": true, "pragma": true, "vacuum": true, "reindex": true, "analyze": true,
	"begin": true, "commit": true, "rollback": true, "savepoint": true, "release": true,
	"trigger": true, "into": true, "returning": true, "truncate": true, "upsert": true,
	"load_extension": true, "readfile": true, "writefile": true, "edit": true, "fts3_tokenizer": true,
}

// tokKind is the kind of a SQL token.
type tokKind int

const (
	tkWord   tokKind = iota // a bare word: keyword, function or identifier
	tkQuoted                // "name", [name] or `name`: a quoted identifier (text is the name)
	tkString                // 'text'
	tkNumber
	tkPunct // one of ( ) , . ; and any other single symbol
)

type sqlTok struct {
	kind tokKind
	text string // lower-cased for words and quoted identifiers
	pos  int    // byte offset in the statement
	end  int
}

// ErrNotReadOnly is returned for a statement the check refuses.
var ErrNotReadOnly = errors.New("consulta no permitida")

// tokenizeSQL splits a statement into tokens. It fails on an unterminated string, quoted identifier
// or comment, on a NUL byte, and on any non-ASCII character outside a string or quoted identifier
// (which keeps look-alike names out of the statement).
func tokenizeSQL(s string) ([]sqlTok, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return nil, fmt.Errorf("%w: el texto contiene un byte nulo", ErrNotReadOnly)
	}
	var out []sqlTok
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < len(s) && s[i+1] == '-':
			nl := strings.IndexByte(s[i:], '\n')
			if nl < 0 {
				i = len(s)
			} else {
				i += nl + 1
			}
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("%w: comentario /* sin cerrar", ErrNotReadOnly)
			}
			i += 2 + end + 2
		case c == '\'' || c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			j := i + 1
			var b strings.Builder
			for {
				if j >= len(s) {
					return nil, fmt.Errorf("%w: texto o nombre sin cerrar", ErrNotReadOnly)
				}
				if s[j] == closer {
					if closer != ']' && j+1 < len(s) && s[j+1] == closer { // doubled quote: a literal quote
						b.WriteByte(closer)
						j += 2
						continue
					}
					j++
					break
				}
				b.WriteByte(s[j])
				j++
			}
			kind := tkString
			if c != '\'' {
				kind = tkQuoted
			}
			text := b.String()
			if kind == tkQuoted {
				text = strings.ToLower(text)
			}
			out = append(out, sqlTok{kind: kind, text: text, pos: i, end: j})
			i = j
		case isDigit(c):
			j := i
			for j < len(s) && (isDigit(s[j]) || s[j] == '.' || s[j] == 'e' || s[j] == 'E' || s[j] == 'x' || s[j] == 'X' ||
				(s[j] >= 'a' && s[j] <= 'f') || (s[j] >= 'A' && s[j] <= 'F')) {
				j++
			}
			out = append(out, sqlTok{kind: tkNumber, text: s[i:j], pos: i, end: j})
			i = j
		case isWordStart(c):
			j := i
			for j < len(s) && isWordPart(s[j]) {
				j++
			}
			out = append(out, sqlTok{kind: tkWord, text: strings.ToLower(s[i:j]), pos: i, end: j})
			i = j
		case c >= 0x80:
			return nil, fmt.Errorf("%w: solo se admiten caracteres ASCII fuera de los textos", ErrNotReadOnly)
		case strings.IndexByte("(),.;*+-/%=<>!&|~?:@$#^{}", c) >= 0:
			out = append(out, sqlTok{kind: tkPunct, text: string(c), pos: i, end: i + 1})
			i++
		default:
			return nil, fmt.Errorf("%w: carácter no permitido %q", ErrNotReadOnly, c)
		}
	}
	return out, nil
}

func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isWordStart(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isWordPart(c byte) bool  { return isWordStart(c) || isDigit(c) || c == '$' }

// ValidateReadOnlySQL checks that s is one read-only SELECT over the views and returns the statement
// text without its trailing semicolon. The error explains the refusal in Spanish and wraps ErrNotReadOnly.
func ValidateReadOnlySQL(s string) (string, error) {
	body, _, err := checkReadOnly(s)
	return body, err
}

// checkReadOnly does the work of ValidateReadOnlySQL and also returns the tokens of the statement body.
func checkReadOnly(s string) (string, []sqlTok, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil, fmt.Errorf("%w: la consulta está vacía", ErrNotReadOnly)
	}
	if len(s) > maxSQLLen {
		return "", nil, fmt.Errorf("%w: la consulta tiene más de %d caracteres", ErrNotReadOnly, maxSQLLen)
	}
	toks, err := tokenizeSQL(s)
	if err != nil {
		return "", nil, err
	}
	// One statement: a semicolon may only end the text.
	bodyEnd := len(s)
	if n := len(toks); n > 0 && toks[n-1].kind == tkPunct && toks[n-1].text == ";" {
		bodyEnd = toks[n-1].pos
		toks = toks[:n-1]
	}
	for _, t := range toks {
		if t.kind == tkPunct && t.text == ";" {
			return "", nil, fmt.Errorf("%w: solo se admite una sentencia (sobra un ';')", ErrNotReadOnly)
		}
	}
	if len(toks) == 0 {
		return "", nil, fmt.Errorf("%w: la consulta está vacía", ErrNotReadOnly)
	}
	first := toks[0]
	if first.kind == tkWord && deniedWords[first.text] {
		return "", nil, fmt.Errorf("%w: la palabra %s no está permitida; la consulta solo lee", ErrNotReadOnly, strings.ToUpper(first.text))
	}
	if first.kind != tkWord || (first.text != "select" && first.text != "with") {
		return "", nil, fmt.Errorf("%w: solo se admite SELECT o WITH ... SELECT", ErrNotReadOnly)
	}

	views := map[string]bool{}
	for _, v := range viewNames {
		views[v] = true
	}
	ctes := cteNames(toks)

	for i, t := range toks {
		if t.kind != tkWord && t.kind != tkQuoted {
			continue
		}
		name := t.text
		switch {
		case strings.HasPrefix(name, "sqlite_") || strings.HasPrefix(name, "pragma_"):
			return "", nil, fmt.Errorf("%w: el esquema de SQLite no se consulta (%s)", ErrNotReadOnly, name)
		case deniedWords[name]:
			return "", nil, fmt.Errorf("%w: la palabra %s no está permitida; la consulta solo lee", ErrNotReadOnly, strings.ToUpper(name))
		case name == "replace" && !nextIsOpen(toks, i):
			return "", nil, fmt.Errorf("%w: REPLACE solo se admite como función replace(...)", ErrNotReadOnly)
		case baseTables[name] && !views[name]:
			return "", nil, fmt.Errorf("%w: la tabla %s no se puede consultar; usa las vistas (v_personaje, v_misiones, ...)", ErrNotReadOnly, name)
		}
	}
	// Second pass: what follows FROM and JOIN must be a view, a CTE or a subquery. It runs after the
	// name checks above, so a base table named there is reported as the table it is.
	for i, t := range toks {
		name := t.text
		if t.kind == tkWord && (name == "from" || name == "join") && i+1 < len(toks) {
			next := toks[i+1]
			if next.kind == tkPunct && next.text == "(" {
				continue // a subquery or a table-valued function (json_each...)
			}
			if next.kind == tkWord || next.kind == tkQuoted {
				if views[next.text] || ctes[next.text] || nextIsOpen(toks, i+1) {
					continue
				}
				return "", nil, fmt.Errorf("%w: %s no es una vista conocida; usa una de: %s", ErrNotReadOnly, next.text, strings.Join(viewNames, ", "))
			}
		}
	}
	return s[:bodyEnd], toks, nil
}

// nextIsOpen reports whether the token after index i is an opening parenthesis (a function call).
func nextIsOpen(toks []sqlTok, i int) bool {
	return i+1 < len(toks) && toks[i+1].kind == tkPunct && toks[i+1].text == "("
}

// cteNames returns the names a WITH clause defines: "name AS (" or "name(cols) AS (", right after
// WITH, RECURSIVE or a comma.
func cteNames(toks []sqlTok) map[string]bool {
	out := map[string]bool{}
	for i := 1; i < len(toks); i++ {
		t := toks[i]
		if t.kind != tkWord && t.kind != tkQuoted {
			continue
		}
		prev := toks[i-1]
		afterWith := prev.kind == tkWord && (prev.text == "with" || prev.text == "recursive")
		afterComma := prev.kind == tkPunct && prev.text == ","
		if !afterWith && !afterComma {
			continue
		}
		j := i + 1
		if j < len(toks) && toks[j].kind == tkPunct && toks[j].text == "(" { // column list
			depth := 0
			for ; j < len(toks); j++ {
				if toks[j].kind == tkPunct && toks[j].text == "(" {
					depth++
				} else if toks[j].kind == tkPunct && toks[j].text == ")" {
					depth--
					if depth == 0 {
						j++
						break
					}
				}
			}
		}
		if j+1 < len(toks) && toks[j].kind == tkWord && toks[j].text == "as" && toks[j+1].kind == tkPunct && toks[j+1].text == "(" {
			out[t.text] = true
		}
	}
	return out
}
