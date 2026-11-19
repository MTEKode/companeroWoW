package store

// viewStatements are the read-only views the AI queries through consultar_db. Their names and
// column names are part of the tool contract: VIEWS.md documents them (in Spanish) and the
// tests check that both stay in step. Views read base tables; the validator (see sqlval.go)
// keeps the base tables out of a query.
var viewStatements = []string{
	// Personaje: the core fields of each character.
	`CREATE VIEW v_personaje AS
	SELECT c.char_key AS personaje, c.name AS nombre, c.realm AS reino, c.race AS raza, c.class AS clase,
	       c.level AS nivel, c.xp AS xp, c.xp_max AS xp_max, c.money AS dinero_cobre,
	       datetime(c.updated_at, 'unixepoch', 'localtime') AS actualizado
	FROM characters c`,

	// Misiones: the character's quest log, the quests its quest window offers, and whether each is accessible.
	`CREATE VIEW v_misiones AS
	SELECT ql.char_key AS personaje, ql.quest_id AS mision_id,
	       COALESCE(NULLIF(q.title, ''), ql.title) AS titulo,
	       COALESCE(NULLIF(q.level, 0), ql.level) AS nivel,
	       COALESCE(w.zone, '') AS zona,
	       CASE WHEN ql.complete = 1 THEN 'lista_para_entregar' ELSE 'en_registro' END AS estado,
	       1 AS en_registro,
	       COALESCE(w.accessible, 0) AS accesible,
	       COALESCE(q.objectives_text, '') AS objetivos
	FROM quest_log ql
	LEFT JOIN quests q ON q.quest_id = ql.quest_id
	LEFT JOIN quest_window w ON w.char_key = ql.char_key AND w.quest_id = ql.quest_id
	UNION ALL
	SELECT w.char_key, w.quest_id,
	       COALESCE(NULLIF(w.name, ''), q.title, ''),
	       COALESCE(NULLIF(w.level, 0), q.level, w.required_level),
	       w.zone,
	       CASE WHEN w.accessible = 1 THEN 'accesible' ELSE 'no_accesible' END,
	       0,
	       w.accessible,
	       COALESCE(q.objectives_text, '')
	FROM quest_window w
	LEFT JOIN quests q ON q.quest_id = w.quest_id
	WHERE NOT EXISTS (SELECT 1 FROM quest_log ql WHERE ql.char_key = w.char_key AND ql.quest_id = w.quest_id)`,

	// Misiones conocidas: every quest the addon has learned in play (not per character).
	`CREATE VIEW v_misiones_conocidas AS
	SELECT q.quest_id AS mision_id, q.title AS titulo, q.level AS nivel,
	       q.giver_name AS entrega_npc, COALESCE(gz.name, '') AS entrega_zona,
	       q.turnin_name AS entrega_final_npc, COALESCE(tz.name, '') AS entrega_final_zona,
	       q.objectives_text AS objetivos, q.description_text AS descripcion,
	       (SELECT COALESCE(SUM(r.amount), 0) FROM quest_rewards r WHERE r.quest_id = q.quest_id AND r.kind = 'money') AS recompensa_cobre,
	       (SELECT COALESCE(SUM(r.amount), 0) FROM quest_rewards r WHERE r.quest_id = q.quest_id AND r.kind = 'xp') AS recompensa_xp,
	       (SELECT json_group_array(json_object('objeto_id', r.item_id, 'cantidad', r.amount, 'tipo', r.kind))
	          FROM quest_rewards r WHERE r.quest_id = q.quest_id AND r.item_id <> 0) AS recompensas_objetos,
	       datetime(q.last_seen, 'unixepoch', 'localtime') AS visto_ultima_vez
	FROM quests q
	LEFT JOIN maps gz ON gz.map_id = q.giver_map_id
	LEFT JOIN maps tz ON tz.map_id = q.turnin_map_id`,

	// NPCs: every NPC learned, with its zone and roles (quest_giver, turnin, vendor, trainer).
	`CREATE VIEW v_npcs AS
	SELECT n.npc_id AS npc_id, n.name AS nombre, COALESCE(m.name, '') AS zona, n.map_id AS zona_id,
	       n.x AS x, n.y AS y,
	       COALESCE((SELECT group_concat(r.role, ',') FROM npc_roles r WHERE r.npc_id = n.npc_id), '') AS roles,
	       n.reaction AS reaccion
	FROM npcs n
	LEFT JOIN maps m ON m.map_id = n.map_id`,

	// Vendedores y objetos: what each vendor sells, one row per item.
	`CREATE VIEW v_vendedores_objetos AS
	SELECT v.npc_id AS vendedor_npc_id, v.name AS vendedor, COALESCE(m.name, '') AS zona,
	       v.faction AS faccion, vi.item_id AS objeto_id,
	       COALESCE(NULLIF(vi.name, ''), i.name, '') AS objeto,
	       vi.price AS precio_cobre, vi.stack AS cantidad_por_compra,
	       vi.limited AS limitado, vi.requires_rep AS requiere_reputacion
	FROM vendors v
	JOIN vendor_items vi ON vi.npc_id = v.npc_id
	LEFT JOIN items i ON i.item_id = vi.item_id
	LEFT JOIN maps m ON m.map_id = v.map_id`,

	// Instructores y servicios: what each trainer teaches, with its cost and requirements.
	`CREATE VIEW v_instructores_servicios AS
	SELECT t.npc_id AS instructor_npc_id, t.name AS instructor, COALESCE(m.name, '') AS zona,
	       t.kind AS tipo, t.profession AS profesion, s.name AS servicio, s.rank AS rango,
	       s.cost AS coste_cobre, s.level AS nivel_requerido, s.skill_name AS habilidad,
	       s.skill_rank AS habilidad_requerida, s.available AS disponible
	FROM trainers t
	JOIN trainer_services s ON s.npc_id = t.npc_id
	LEFT JOIN maps m ON m.map_id = t.map_id`,

	// Vuelos: the measured flight routes between flight nodes.
	`CREATE VIEW v_vuelos AS
	SELECT a.node_id AS origen_id, a.name AS origen, COALESCE(ma.name, '') AS origen_zona,
	       b.node_id AS destino_id, b.name AS destino, COALESCE(mb.name, '') AS destino_zona,
	       a.faction AS faccion, r.copper AS coste_cobre, r.hops AS saltos, r.seconds AS segundos,
	       (SELECT json_group_array(k.char_key) FROM flight_known k WHERE k.node_id = a.node_id) AS origen_conocido_por
	FROM flight_routes r
	JOIN flight_nodes a ON a.node_id = r.from_id
	JOIN flight_nodes b ON b.node_id = r.to_id
	LEFT JOIN maps ma ON ma.map_id = a.map_id
	LEFT JOIN maps mb ON mb.map_id = b.map_id`,

	// Objetos: the items seen, with their stats (estadisticas is a JSON object) and weapon damage.
	`CREATE VIEW v_objetos AS
	SELECT i.item_id AS objeto_id, i.name AS nombre, i.link AS enlace, i.item_level AS nivel_objeto,
	       i.required_level AS nivel_requerido, i.quality AS calidad, i.equip_loc AS ranura,
	       i.class AS clase, i.subclass AS subclase, i.weapon_min AS dano_min,
	       i.weapon_max AS dano_max, i.weapon_speed AS velocidad,
	       COALESCE((SELECT json_group_object(s.stat, s.value) FROM item_stats s WHERE s.item_id = i.item_id), '{}') AS estadisticas
	FROM items i`,

	// Consumibles: food, drink and potions, with what they restore.
	`CREATE VIEW v_consumibles AS
	SELECT c.item_id AS objeto_id, c.name AS nombre, c.kind AS tipo, c.required_level AS nivel_requerido,
	       c.health AS vida, c.mana AS mana, c.seconds AS segundos,
	       c.skill_name AS habilidad, c.skill_rank AS habilidad_requerida
	FROM consumables c`,

	// Combates: one row per recorded encounter.
	`CREATE VIEW v_combates AS
	SELECT c.char_key AS personaje, c.combat_id AS combate_id,
	       datetime(c.started_at, 'unixepoch', 'localtime') AS inicio,
	       c.duration AS duracion_s, c.zone AS zona, c.damage AS dano_total, c.dps AS dps,
	       c.healing AS curacion_total, c.taken AS dano_recibido, c.died AS murio,
	       c.health_pct AS vida_final_pct, c.targets AS objetivos_json
	FROM combats c`,

	// Combate hechizos: the spells of each encounter, damage done ('dano') or healing ('curacion').
	`CREATE VIEW v_combate_hechizos AS
	SELECT s.char_key AS personaje, s.combat_id AS combate_id,
	       CASE s.kind WHEN 'dmg' THEN 'dano' WHEN 'heal' THEN 'curacion' ELSE s.kind END AS tipo,
	       s.spell_id AS hechizo_id, s.name AS hechizo, s.amount AS cantidad, s.hits AS golpes
	FROM combat_spells s`,

	// Muertes: the encounters that ended in a death, with the killer and the last hits.
	`CREATE VIEW v_muertes AS
	SELECT d.char_key AS personaje, d.combat_id AS combate_id,
	       datetime(d.died_at, 'unixepoch', 'localtime') AS momento, d.zone AS zona,
	       d.killer AS asesino, d.last_hits_json AS ultimos_golpes_json, c.duration AS duracion_s
	FROM deaths d
	LEFT JOIN combats c ON c.char_key = d.char_key AND c.combat_id = d.combat_id`,

	// Sesiones: play sessions, the app's own (personaje empty) and the addon's.
	`CREATE VIEW v_sesiones AS
	SELECT s.id AS sesion_id, s.char_key AS personaje,
	       datetime(s.started_at, 'unixepoch', 'localtime') AS inicio,
	       CASE WHEN s.ended_at > 0 THEN datetime(s.ended_at, 'unixepoch', 'localtime') END AS fin,
	       s.open AS abierta, s.start_level AS nivel_inicio, s.end_level AS nivel_fin,
	       s.levels_gained AS niveles_subidos, s.xp_gained_pct AS xp_ganado_pct,
	       s.money_start AS dinero_inicio_cobre, s.money_end AS dinero_fin_cobre,
	       s.quests_completed AS misiones_completadas_json, s.quests_turned_in AS misiones_entregadas_json,
	       s.zones AS zonas_json, s.deaths AS muertes
	FROM sessions s`,

	// Equipo: every change of the item in each slot; actual is 1 for the item worn now.
	`CREATE VIEW v_equipo AS
	SELECT h.char_key AS personaje, h.slot AS ranura_id, COALESCE(i.equip_loc, '') AS ranura,
	       h.item_id AS objeto_id, COALESCE(i.name, '') AS objeto, i.item_level AS nivel_objeto,
	       i.quality AS calidad, datetime(h.changed_at, 'unixepoch', 'localtime') AS desde,
	       CASE WHEN h.id = (SELECT MAX(h2.id) FROM equipment_history h2
	                         WHERE h2.char_key = h.char_key AND h2.slot = h.slot) THEN 1 ELSE 0 END AS actual
	FROM equipment_history h
	LEFT JOIN items i ON i.item_id = h.item_id`,

	// Candidatos de equipo: equippable rewards and loot the character can get, with the source of each.
	`CREATE VIEW v_candidatos_equipo AS
	SELECT g.char_key AS personaje, g.item_id AS objeto_id, COALESCE(i.name, '') AS objeto,
	       i.item_level AS nivel_objeto, i.required_level AS nivel_requerido, i.equip_loc AS ranura,
	       i.quality AS calidad, g.source_kind AS fuente_tipo, g.quest_id AS mision_id,
	       g.dungeon_key AS mazmorra, g.boss AS jefe, g.source AS fuente, g.choice AS a_elegir,
	       COALESCE((SELECT json_group_object(s.stat, s.value) FROM item_stats s WHERE s.item_id = g.item_id), '{}') AS estadisticas
	FROM gear_candidates g
	LEFT JOIN items i ON i.item_id = g.item_id`,

	// Macros: account and character macros.
	`CREATE VIEW v_macros AS
	SELECT m.scope AS ambito, m.char_key AS personaje, m.idx AS indice, m.name AS nombre, m.body AS cuerpo
	FROM macros m`,

	// Barras: the action bar slots of each character.
	`CREATE VIEW v_barras AS
	SELECT b.char_key AS personaje, b.slot AS ranura, b.kind AS tipo, b.name AS nombre,
	       b.item_id AS id, b.key AS tecla
	FROM action_bars b`,

	// Mazmorras: dungeons with their bosses and loot (botin_json lists boss and item per loot row).
	`CREATE VIEW v_mazmorras AS
	SELECT d.dungeon_key AS mazmorra_clave, d.name AS nombre, d.zone AS zona,
	       d.min_level AS nivel_min, d.max_level AS nivel_max, d.source AS fuente,
	       d.quests_json AS misiones_json,
	       COALESCE((SELECT json_group_array(b.boss) FROM dungeon_bosses b WHERE b.dungeon_key = d.dungeon_key), '[]') AS jefes_json,
	       COALESCE((SELECT json_group_array(json_object('jefe', l.boss, 'objeto_id', l.item_id))
	                 FROM dungeon_loot l WHERE l.dungeon_key = d.dungeon_key), '[]') AS botin_json
	FROM dungeons d`,
}

// viewNames lists the views the AI may read, in the order of the contract.
var viewNames = []string{
	"v_personaje", "v_misiones", "v_misiones_conocidas", "v_npcs", "v_vendedores_objetos",
	"v_instructores_servicios", "v_vuelos", "v_objetos", "v_consumibles", "v_combates",
	"v_combate_hechizos", "v_muertes", "v_sesiones", "v_equipo", "v_candidatos_equipo",
	"v_macros", "v_barras", "v_mazmorras",
}
