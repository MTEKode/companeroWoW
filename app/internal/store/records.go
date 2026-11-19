package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// The readers of gamedata take the history from here first and fall back to SavedVariables.
// Both builders return the shape the addon itself sends, so the readers stay the same: a combat
// record like the msgType 7 body, and the world block like the SavedVariables "world" table.

// CombatRecords returns the newest limit combats of the character, in the addon's record shape
// (newest first). It returns nil when the store has none for the character.
func (s *Store) CombatRecords(charKey string, limit int) ([]any, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`SELECT combat_id, started_at, duration, zone, died, health_pct, targets, damage, dps, healing, taken
		FROM combats WHERE char_key = ? ORDER BY combat_id DESC LIMIT ?`, charKey, limit)
	if err != nil {
		return nil, fmt.Errorf("read combats: %w", err)
	}
	type head struct {
		id, started, died, health   int64
		durationF                   float64
		zone, targets               string
		damage, dps, healing, taken float64
	}
	var heads []head
	for rows.Next() {
		var h head
		var zone string
		if err := rows.Scan(&h.id, &h.started, &h.durationF, &zone, &h.died, &h.health, &h.targets, &h.damage, &h.dps, &h.healing, &h.taken); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan combat: %w", err)
		}
		h.zone = zone
		heads = append(heads, h)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate combats: %w", err)
	}
	_ = rows.Close()
	if len(heads) == 0 {
		return nil, nil
	}
	out := make([]any, 0, len(heads))
	for _, h := range heads {
		rec, err := s.combatRecord(charKey, h.id)
		if err != nil {
			return nil, err
		}
		rec["id"] = float64(h.id)
		rec["startedAt"] = float64(h.started)
		rec["duration"] = h.durationF
		rec["zone"] = h.zone
		rec["died"] = h.died != 0
		var targets []any
		if h.targets != "" && h.targets != "[]" {
			for _, n := range jsonStrings(h.targets) {
				targets = append(targets, map[string]any{"name": n})
			}
		}
		rec["targets"] = nonNilList(targets)
		rec["damageDone"] = map[string]any{"total": h.damage, "dps": h.dps, "spells": rec["_dmg"]}
		rec["healingDone"] = map[string]any{"total": h.healing, "spells": rec["_heal"]}
		rec["damageTaken"] = map[string]any{"total": h.taken, "sources": rec["_taken"]}
		delete(rec, "_dmg")
		delete(rec, "_heal")
		delete(rec, "_taken")
		if h.health >= 0 {
			rec["resourcesAtEnd"] = map[string]any{"healthPct": float64(h.health)}
		}
		out = append(out, rec)
	}
	return out, nil
}

// combatRecord reads the spells, the damage taken and the death of one combat.
func (s *Store) combatRecord(charKey string, id int64) (map[string]any, error) {
	rec := map[string]any{}
	dmg, heal, taken := []any{}, []any{}, []any{}
	rows, err := s.db.Query(`SELECT kind, spell_id, name, amount, hits FROM combat_spells WHERE char_key = ? AND combat_id = ?
		ORDER BY amount DESC`, charKey, id)
	if err != nil {
		return nil, fmt.Errorf("read spells of combat %d: %w", id, err)
	}
	for rows.Next() {
		var kind, name string
		var spell, hits int64
		var amount float64
		if err := rows.Scan(&kind, &spell, &name, &amount, &hits); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan spell: %w", err)
		}
		sp := map[string]any{"name": name, "spellID": float64(spell), "amount": amount, "hits": float64(hits)}
		if kind == "heal" {
			heal = append(heal, sp)
		} else {
			dmg = append(dmg, sp)
		}
	}
	_ = rows.Close()
	rows, err = s.db.Query(`SELECT source, amount FROM combat_taken WHERE char_key = ? AND combat_id = ? ORDER BY amount DESC`, charKey, id)
	if err != nil {
		return nil, fmt.Errorf("read damage taken of combat %d: %w", id, err)
	}
	for rows.Next() {
		var source string
		var amount float64
		if err := rows.Scan(&source, &amount); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan damage taken: %w", err)
		}
		taken = append(taken, map[string]any{"name": source, "amount": amount})
	}
	_ = rows.Close()
	rec["_dmg"], rec["_heal"], rec["_taken"] = dmg, heal, taken

	var killer, zone, hitsJSON string
	err = s.db.QueryRow(`SELECT killer, zone, last_hits_json FROM deaths WHERE char_key = ? AND combat_id = ?`, charKey, id).
		Scan(&killer, &zone, &hitsJSON)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("read death of combat %d: %w", id, err)
	default:
		var hits []any
		for _, h := range jsonObjects(hitsJSON) {
			hits = append(hits, map[string]any{
				"source": h["fuente"], "spellName": h["hechizo"], "amount": h["cantidad"],
			})
		}
		rec["death"] = map[string]any{"killer": killer, "lastHits": nonNilList(hits)}
	}
	return rec, nil
}

// WorldDoc returns the world block the store holds (flights, vendors, trainers, quest chains and
// consumables), in the shape of the SavedVariables "world" table. It returns nil when the store has
// no world data at all.
func (s *Store) WorldDoc() (map[string]any, error) {
	doc := map[string]any{}
	factions := map[string]map[string]any{}
	faction := func(f string) map[string]any {
		if factions[f] == nil {
			factions[f] = map[string]any{"nodes": map[string]any{}, "costs": map[string]any{}}
		}
		return factions[f]
	}

	nodes, err := s.db.Query(`SELECT node_id, name, map_id, faction, x, y, has_pos FROM flight_nodes`)
	if err != nil {
		return nil, fmt.Errorf("read flight nodes: %w", err)
	}
	for nodes.Next() {
		var id, mapID, hasPos int64
		var name, f string
		var x, y float64
		if err := nodes.Scan(&id, &name, &mapID, &f, &x, &y, &hasPos); err != nil {
			_ = nodes.Close()
			return nil, fmt.Errorf("scan flight node: %w", err)
		}
		n := map[string]any{"nodeID": float64(id), "name": name, "mapID": float64(mapID)}
		if hasPos != 0 {
			n["x"], n["y"] = x, y
		}
		faction(f)["nodes"].(map[string]any)[strconv.FormatInt(id, 10)] = n
	}
	_ = nodes.Close()

	routes, err := s.db.Query(`SELECT r.from_id, r.to_id, r.copper, r.hops, r.seconds, a.faction
		FROM flight_routes r JOIN flight_nodes a ON a.node_id = r.from_id`)
	if err != nil {
		return nil, fmt.Errorf("read flight routes: %w", err)
	}
	for routes.Next() {
		var from, to, copper, hops int64
		var seconds float64
		var f string
		if err := routes.Scan(&from, &to, &copper, &hops, &seconds, &f); err != nil {
			_ = routes.Close()
			return nil, fmt.Errorf("scan flight route: %w", err)
		}
		key := fmt.Sprintf("%d>%d", from, to)
		faction(f)["costs"].(map[string]any)[key] = map[string]any{"copper": float64(copper), "hops": float64(hops), "seconds": seconds}
	}
	_ = routes.Close()
	if len(factions) > 0 {
		flight := map[string]any{}
		for f, block := range factions {
			flight[f] = block
		}
		known, err := s.knownFlights()
		if err != nil {
			return nil, err
		}
		if len(known) > 0 {
			flight["known"] = known
		}
		doc["flight"] = flight
	}

	vendors, err := s.vendorsDoc()
	if err != nil {
		return nil, err
	}
	if len(vendors) > 0 {
		doc["vendors"] = vendors
	}
	trainers, err := s.trainersDoc()
	if err != nil {
		return nil, err
	}
	if len(trainers) > 0 {
		doc["trainers"] = trainers
	}
	chains, err := s.chainsDoc()
	if err != nil {
		return nil, err
	}
	if len(chains) > 0 {
		doc["questChains"] = chains
	}
	consumables, err := s.consumablesDoc()
	if err != nil {
		return nil, err
	}
	if len(consumables) > 0 {
		doc["consumables"] = consumables
	}
	if len(doc) == 0 {
		return nil, nil
	}
	return doc, nil
}

func (s *Store) knownFlights() (map[string]any, error) {
	rows, err := s.db.Query(`SELECT char_key, node_id FROM flight_known ORDER BY char_key, node_id`)
	if err != nil {
		return nil, fmt.Errorf("read known flights: %w", err)
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var key string
		var id int64
		if err := rows.Scan(&key, &id); err != nil {
			return nil, fmt.Errorf("scan known flight: %w", err)
		}
		list, _ := out[key].([]any)
		out[key] = append(list, float64(id))
	}
	return out, rows.Err()
}

func (s *Store) vendorsDoc() (map[string]any, error) {
	rows, err := s.db.Query(`SELECT npc_id, name, map_id, x, y, faction FROM vendors`)
	if err != nil {
		return nil, fmt.Errorf("read vendors: %w", err)
	}
	out := map[string]any{}
	type vend struct {
		id      int64
		name    string
		mapID   int64
		x, y    float64
		faction string
	}
	var vs []vend
	for rows.Next() {
		var v vend
		if err := rows.Scan(&v.id, &v.name, &v.mapID, &v.x, &v.y, &v.faction); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan vendor: %w", err)
		}
		vs = append(vs, v)
	}
	_ = rows.Close()
	for _, v := range vs {
		items, err := s.db.Query(`SELECT item_id, name, price, stack, limited, requires_rep FROM vendor_items WHERE npc_id = ? ORDER BY item_id`, v.id)
		if err != nil {
			return nil, fmt.Errorf("read vendor items: %w", err)
		}
		var list []any
		for items.Next() {
			var id, price, stack, limited int64
			var name, rep string
			if err := items.Scan(&id, &name, &price, &stack, &limited, &rep); err != nil {
				_ = items.Close()
				return nil, fmt.Errorf("scan vendor item: %w", err)
			}
			list = append(list, map[string]any{"itemID": float64(id), "name": name, "price": float64(price),
				"stack": float64(stack), "limited": limited != 0, "requiresRep": rep})
		}
		_ = items.Close()
		m := map[string]any{"npcID": float64(v.id), "name": v.name, "mapID": float64(v.mapID), "faction": v.faction, "items": nonNilList(list)}
		if v.x != 0 || v.y != 0 {
			m["x"], m["y"] = v.x, v.y
		}
		out[strconv.FormatInt(v.id, 10)] = m
	}
	return out, nil
}

func (s *Store) trainersDoc() (map[string]any, error) {
	rows, err := s.db.Query(`SELECT npc_id, name, map_id, x, y, kind, profession FROM trainers`)
	if err != nil {
		return nil, fmt.Errorf("read trainers: %w", err)
	}
	type tr struct {
		id, mapID        int64
		name, kind, prof string
		x, y             float64
	}
	var ts []tr
	for rows.Next() {
		var t tr
		if err := rows.Scan(&t.id, &t.name, &t.mapID, &t.x, &t.y, &t.kind, &t.prof); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan trainer: %w", err)
		}
		ts = append(ts, t)
	}
	_ = rows.Close()
	out := map[string]any{}
	for _, t := range ts {
		svc, err := s.db.Query(`SELECT name, rank, cost, level, available, skill_name, skill_rank FROM trainer_services
			WHERE npc_id = ? ORDER BY name, rank`, t.id)
		if err != nil {
			return nil, fmt.Errorf("read services: %w", err)
		}
		var list []any
		for svc.Next() {
			var name, rank, avail, skill string
			var cost, level, skillRank int64
			if err := svc.Scan(&name, &rank, &cost, &level, &avail, &skill, &skillRank); err != nil {
				_ = svc.Close()
				return nil, fmt.Errorf("scan service: %w", err)
			}
			m := map[string]any{"name": name, "rank": rank, "cost": float64(cost), "level": float64(level), "available": avail}
			if skill != "" {
				m["skill"] = map[string]any{"name": skill, "rank": float64(skillRank)}
			}
			list = append(list, m)
		}
		_ = svc.Close()
		m := map[string]any{"npcID": float64(t.id), "name": t.name, "mapID": float64(t.mapID), "kind": t.kind,
			"profession": t.prof, "services": nonNilList(list)}
		if t.x != 0 || t.y != 0 {
			m["x"], m["y"] = t.x, t.y
		}
		out[strconv.FormatInt(t.id, 10)] = m
	}
	return out, nil
}

func (s *Store) chainsDoc() (map[string]any, error) {
	rows, err := s.db.Query(`SELECT quest_id, next_id, chain_name, name, pre_json FROM quest_chains`)
	if err != nil {
		return nil, fmt.Errorf("read quest chains: %w", err)
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var id, next int64
		var chain, name, pre string
		if err := rows.Scan(&id, &next, &chain, &name, &pre); err != nil {
			return nil, fmt.Errorf("scan chain: %w", err)
		}
		var preList []any
		for _, p := range jsonInts(pre) {
			preList = append(preList, float64(p))
		}
		out[strconv.FormatInt(id, 10)] = map[string]any{"next": float64(next), "chainName": chain, "name": name, "pre": nonNilList(preList)}
	}
	return out, rows.Err()
}

func (s *Store) consumablesDoc() (map[string]any, error) {
	rows, err := s.db.Query(`SELECT item_id, name, kind, required_level, health, mana, seconds, skill_name, skill_rank FROM consumables`)
	if err != nil {
		return nil, fmt.Errorf("read consumables: %w", err)
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var id, req, health, mana, seconds, rank int64
		var name, kind, skill string
		if err := rows.Scan(&id, &name, &kind, &req, &health, &mana, &seconds, &skill, &rank); err != nil {
			return nil, fmt.Errorf("scan consumable: %w", err)
		}
		m := map[string]any{"itemID": float64(id), "name": name, "kind": kind, "requiredLevel": float64(req),
			"restores": map[string]any{"health": float64(health), "mana": float64(mana), "seconds": float64(seconds)}}
		if skill != "" {
			m["skill"] = []any{skill, float64(rank)}
		}
		out[strconv.FormatInt(id, 10)] = m
	}
	return out, rows.Err()
}

// ---- JSON helpers for the *_json columns ---------------------------------------------

func nonNilList(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

// jsonStrings decodes a JSON array of strings (nil on bad input).
func jsonStrings(s string) []string {
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// jsonInts decodes a JSON array of integers (nil on bad input).
func jsonInts(s string) []int64 {
	var out []int64
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// jsonObjects decodes a JSON array of objects (nil on bad input).
func jsonObjects(s string) []map[string]any {
	var out []map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
