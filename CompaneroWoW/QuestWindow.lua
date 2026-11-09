-- QuestWindow.lua: ventana de misiones del personaje (questWindow del contrato GEAR / QUEST-WINDOW).
-- Lee QuestieDB / ZoneDB y ForeverDungeonJournal (ambos opcionales) y las APIs nativas (Native.lua). Nunca modifica nada.
--
-- Reglas (calculadas con el estado real del personaje, no con datos fijos):
--   - Raza: mascara requiredRaces con el bit de la raza (LibQuestieDB.Enum.raceMaskById, 2^bit).
--   - Clase: mascara requiredClasses con el bit 2^(classID-1) (QuestiePlayer.Initialize).
--   - Faccion: una mascara de faccion completa solo vale para su faccion.
--   - Exclusivas: una mision de exclusiveTo ya completada bloquea la mision.
--   - preQuestSingle: basta UNA completada (alternativas). preQuestGroup: TODAS completadas;
--     un prerrequisito sin completar cuenta como cumplido si una de SUS exclusivas esta completada
--     (QuestieDB:IsPreQuestGroupFulfilled). Ids negativos: sin el paso por exclusivas.
--   - Ventana: no completada, permitida para el personaje, requiredLevel <= nivel + 3 y
--     questLevel >= nivel - 5. accessibleNow = nivel suficiente y prerrequisitos completos.
--
-- Las funciones de reglas son puras (para pruebas). El trabajo grande es una coroutine que cede
-- (coroutine.yield) solo en bucles de nivel superior, nunca dentro de pcall.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean

local QW = {}
ns.QuestWindow = QW

QW.MAX_QUESTS = 400
QW.BELOW = 5          -- questLevel >= nivel - BELOW
QW.ABOVE = 3          -- requiredLevel <= nivel + ABOVE
QW.MAX_BYTES = 150 * 1024 -- tope aproximado del bloque en SavedVariables (130 KB + origen por elemento)
local CHECK_EVERY = 25    -- elementos entre dos cesiones de la coroutine

-- Bits de raza de respaldo (los de QuestieDB/src/corrections/enum/expansions.lua, id de raza -> bit).
local RACE_BIT_FALLBACK = {
    [1] = 1, [2] = 2, [3] = 4, [4] = 8, [5] = 16, [6] = 32, [7] = 64, [8] = 128,
    [9] = 256, [10] = 512, [11] = 1024, [22] = 2097152, [24] = 8388608, [25] = 16777216,
    [26] = 33554432, [95] = 4294967296, [96] = 8589934592,
}
-- Mascaras de faccion de respaldo (Alliance: Humano, Enano, Elfo de la noche, Gnomo; Horda: Orco, No-muerto, Tauren, Trol).
local ALLIANCE_FALLBACK = 1 + 4 + 8 + 64
local HORDE_FALLBACK = 2 + 16 + 32 + 128

-- Bit de raza para un id de raza (3er valor de UnitRace). nil si se desconoce.
function QW.RaceBit(raceID)
    if raceID == nil then return nil end
    local e = _G.LibQuestieDB and _G.LibQuestieDB.Enum
    local map = e and e.raceMaskById
    if type(map) == "table" and map[raceID] then return map[raceID] end
    return RACE_BIT_FALLBACK[raceID]
end

-- Bit de clase para un id de clase (3er valor de UnitClass): 2^(id-1), como QuestiePlayer.
function QW.ClassBit(classID)
    if classID == nil then return nil end
    return 2 ^ (classID - 1)
end

local function factionMask(name)
    local e = _G.LibQuestieDB and _G.LibQuestieDB.Enum
    local m = e and e.factionRaceMasks
    local v = type(m) == "table" and m[name] or nil
    if v == nil then return name == "Alliance" and ALLIANCE_FALLBACK or HORDE_FALLBACK end
    return v
end

-- true si la mascara permite el bit. Mascara nil o 0 = sin restriccion. Aritmetica (no bit.band):
-- las mascaras de Forever superan los 32 bits.
function QW.MaskHas(mask, bit)
    if mask == nil or mask == 0 then return true end
    if bit == nil then return false end
    return (mask % (2 * bit)) >= bit
end

-- Raza y faccion. Una mascara que es la de una faccion completa solo vale para esa faccion.
function QW.RaceAllowed(requiredRaces, raceBit, faction)
    if requiredRaces == nil or requiredRaces == 0 then return true end
    if not QW.MaskHas(requiredRaces, raceBit) then return false end
    if faction == "Horde" and requiredRaces == factionMask("Alliance") then return false end
    if faction == "Alliance" and requiredRaces == factionMask("Horde") then return false end
    return true
end

-- Ordena y quita duplicados (lista de ids nueva).
local function sortedUnique(list)
    local seen, out = {}, {}
    for _, id in ipairs(list or {}) do
        if not seen[id] then
            seen[id] = true
            out[#out + 1] = id
        end
    end
    table.sort(out)
    return out
end
QW.SortedUnique = sortedUnique

-- Evalua una mision para el personaje.
--   q  = { requiredLevel, requiredRaces, requiredClasses, preQuestSingle, preQuestGroup, exclusiveTo }
--   me = { level, faction, raceBit, classBit, completed = {[qid]=true}, exclusiveOf = fn(qid) -> {ids} }
-- Devuelve { allowed = bool (raza, clase, faccion, sin exclusiva completada),
--            accessibleNow = bool (nivel y prerrequisitos), missing = {ids} }.
function QW.Evaluate(q, me)
    local completed = me.completed or {}
    local out = { allowed = true, accessibleNow = true, missing = {} }
    if not QW.RaceAllowed(q.requiredRaces, me.raceBit, me.faction) then out.allowed = false end
    if not QW.MaskHas(q.requiredClasses, me.classBit) then out.allowed = false end
    for _, id in ipairs(q.exclusiveTo or {}) do
        if completed[id] then out.allowed = false end
    end

    -- Una sola de las alternativas basta.
    local single = q.preQuestSingle or {}
    if #single > 0 then
        local any = false
        for _, id in ipairs(single) do
            if completed[id] then any = true break end
        end
        if not any then
            out.accessibleNow = false
            for _, id in ipairs(single) do out.missing[#out.missing + 1] = id end
        end
    end

    -- Todas las del grupo. Con id positivo, una exclusiva completada del prerrequisito tambien cuenta.
    for _, id in ipairs(q.preQuestGroup or {}) do
        local need = math.abs(id)
        if not completed[need] then
            local ok = false
            if id > 0 and me.exclusiveOf then
                for _, alt in ipairs(me.exclusiveOf(need) or {}) do
                    if completed[alt] then ok = true break end
                end
            end
            if not ok then
                out.accessibleNow = false
                out.missing[#out.missing + 1] = need
            end
        end
    end

    if (q.requiredLevel or 0) > (me.level or 0) then out.accessibleNow = false end
    out.missing = sortedUnique(out.missing)
    return out
end

-- Entra en la ventana: nivel de la mision >= nivel-5 y requisito <= nivel+3 (sin nivel -> no entra).
function QW.InWindow(questLevel, requiredLevel, level)
    if questLevel == nil then return false end
    return questLevel >= level - QW.BELOW and (requiredLevel or 0) <= level + QW.ABOVE
end

-- Tamano aproximado de un valor serializado (cadenas + 2, numeros/booleanos 8, claves incluidas).
function QW.ApproxBytes(v)
    local t = type(v)
    if t == "string" then return #v + 2 end
    if t == "number" or t == "boolean" then return 8 end
    if t ~= "table" then return 0 end
    local n = 0
    for k, x in pairs(v) do n = n + QW.ApproxBytes(k) + QW.ApproxBytes(x) end
    return n
end

-- Lista de ids numericos (sin ids no numericos). nil si el valor no es una lista.
local function idList(v)
    if type(v) ~= "table" then return nil end
    local out = {}
    for _, x in ipairs(v) do
        local n = Num(x)
        if n then out[#out + 1] = n end
    end
    return out
end
QW.IdList = idList

-- ---------------------------------------------------------------------------
-- Recompensas de ForeverDungeonJournal (QuestRewards / Dungeons: rewardItems, rewardChoice)
-- ---------------------------------------------------------------------------

-- { [questID] = { choice = bool, items = {itemID...} } }. Vacio si la journal no esta cargada.
function QW.RewardIndex()
    local idx = {}
    local fdj = _G.ForeverDungeonJournal_NS
    local db = type(fdj) == "table" and fdj.DB or nil
    if type(db) ~= "table" then return idx end
    for _, dungeon in pairs(db) do
        if type(dungeon) == "table" and type(dungeon.quests) == "table" then
            for _, q in ipairs(dungeon.quests) do
                local id = type(q) == "table" and Num(q.id) or nil
                if id and idx[id] == nil and type(q.rewardItems) == "table" then
                    local items = {}
                    for _, r in ipairs(q.rewardItems) do
                        local itemID = type(r) == "table" and Num(r[1]) or nil
                        if itemID then items[#items + 1] = itemID end
                    end
                    if #items > 0 then idx[id] = { choice = q.rewardChoice == true, items = items } end
                end
            end
        end
    end
    return idx
end

local function copyList(list)
    local out = {}
    for i, v in ipairs(list) do out[i] = v end
    return out
end

-- Indice inverso de Questie (item.questRewards). Cache de sesion: QW.reverse =
-- { byQuest = { [questID] = { {itemID, requiredLevel}, ... } }, items = n }. nil hasta construirlo.
QW.reverse = nil
QW.REVERSE_MAX_PER_QUEST = 12
local REVERSE_CHECK = 40        -- objetos del indice por trozo de coroutine (cada uno son 2-4 lecturas)
local REVERSE_KEYS_CHECK = 1000 -- claves de ItemPointers por trozo al recogerlas

-- rewards = { choice = {...}, fixed = {...} } o nil si la mision no tiene recompensas conocidas.
-- Primero ForeverDungeonJournal (idx, tal cual). Si no lo tiene, el indice inverso de Questie:
-- Questie no distingue elegible de fija, asi que todo va en choice con rewardsKnownAs = "questie-reverse".
-- Objetos sin requiredLevel en la ficha se conservan (el filtro de candidatos los mira despues).
local function rewardsFor(idx, qid, reverse, level)
    local r = idx[qid]
    if r then
        if r.choice then return { choice = copyList(r.items), fixed = {} } end
        return { choice = {}, fixed = copyList(r.items) }
    end
    local list = reverse and reverse.byQuest[qid] or nil
    if not list then return nil end
    local items = {}
    for _, e in ipairs(list) do
        if e.requiredLevel == nil or e.requiredLevel <= level + QW.ABOVE then
            items[#items + 1] = e.itemID
            if #items >= QW.REVERSE_MAX_PER_QUEST then break end
        end
    end
    if #items == 0 then return nil end
    return { choice = items, fixed = {}, rewardsKnownAs = "questie-reverse" }
end

-- Construye el indice inverso recorriendo QuestieDB.ItemPointers en trozos (coroutine; no cede
-- dentro de pcall). Guarda solo objetos de clase Armadura (4) o Arma (2) con questRewards.
-- Una vez por sesion. Devuelve el indice, o nil si Questie no expone ItemPointers (se reintenta
-- en la siguiente exportacion).
function QW.ReverseIndexWork(db)
    if QW.reverse then return QW.reverse end
    local pointers = type(db) == "table" and db.ItemPointers or nil
    if type(pointers) ~= "table" then return nil end
    local ids, n = {}, 0
    for id in pairs(pointers) do
        if type(id) == "number" then
            n = n + 1
            ids[n] = id
            if n % REVERSE_KEYS_CHECK == 0 then coroutine.yield(1) end
        end
    end
    local byQuest, kept = {}, 0
    for i = 1, n do
        local id = ids[i]
        local cls = Num(ns.SafeCall(db.QueryItemSingle, id, "class"))
        if cls == 2 or cls == 4 then
            local rewards = idList(ns.SafeCall(db.QueryItemSingle, id, "questRewards"))
            if rewards and #rewards > 0 then
                local req = Num(ns.SafeCall(db.QueryItemSingle, id, "requiredLevel"))
                for _, qid in ipairs(rewards) do
                    local list = byQuest[qid]
                    if not list then
                        list = {}
                        byQuest[qid] = list
                    end
                    list[#list + 1] = { itemID = id, requiredLevel = req }
                end
                kept = kept + 1
            end
        end
        if i % REVERSE_CHECK == 0 then coroutine.yield(1) end
    end
    -- Orden estable por id dentro de cada mision (listas cortas).
    local k = 0
    for _, list in pairs(byQuest) do
        table.sort(list, function(a, b) return a.itemID < b.itemID end)
        k = k + 1
        if k % REVERSE_CHECK == 0 then coroutine.yield(1) end
    end
    QW.reverse = { byQuest = byQuest, items = kept }
    return QW.reverse
end

-- ---------------------------------------------------------------------------
-- Trabajo de Questie (PNJs y zonas)
-- ---------------------------------------------------------------------------

-- Nombre de la zona (C_Map.GetMapInfo del uiMapID del area de Questie). Cacheado en env.
local function zoneNameOf(env, areaId)
    if areaId == nil or env.zone == nil then return nil end
    local cached = env.zoneNames[areaId]
    if cached ~= nil then return cached or nil end
    local uiMap = Num(ns.SafeCall(env.zone.GetUiMapIdByAreaId, env.zone, areaId))
    local name = nil
    if uiMap then
        local info = Clean(Call("C_Map.GetMapInfo", uiMap))
        name = type(info) == "table" and Str(info.name) or nil
    end
    env.zoneNames[areaId] = name or false
    return name
end

-- Primer PNJ (o objeto) con posicion que da la mision: el mas cercano en el mapa del jugador.
-- slots = startedBy / finishedBy de QuestieDB: [1] npcs, [2] objetos.
local function firstGiver(db, zone, slots, mapID, px, py)
    if type(slots) ~= "table" or zone == nil then return nil end
    local kinds = { { 1, "npc", "QueryNPCSingle" }, { 2, "object", "QueryObjectSingle" } }
    for _, def in ipairs(kinds) do
        local ids = slots[def[1]]
        local fn = db[def[3]]
        if type(ids) == "table" and type(fn) == "function" then
            for _, rawId in ipairs(ids) do
                local id = Num(rawId)
                if id and id > 0 then
                    local spawns = ns.Questie.ConvertSpawns(ns.SafeCall(fn, id, "spawns"), zone)
                    local near = ns.Questie.NearestFirst(spawns, mapID, px, py)[1]
                    if near then
                        return {
                            kind = def[2], id = id, name = Str(ns.SafeCall(fn, id, "name")),
                            mapID = near.mapID, x = near.x, y = near.y,
                        }
                    end
                end
            end
        end
    end
    return nil
end

local function questField(db, qid, key)
    return ns.SafeCall(db.QueryQuestSingle, qid, key)
end

-- Campos de reglas de una mision (QuestieDB).
local function questRules(db, qid, requiredLevel)
    local function q(key) return ns.SafeCall(db.QueryQuestSingle, qid, key) end
    return {
        requiredLevel = requiredLevel,
        requiredRaces = Num(q("requiredRaces")),
        requiredClasses = Num(q("requiredClasses")),
        preQuestSingle = idList(q("preQuestSingle")),
        preQuestGroup = idList(q("preQuestGroup")),
        exclusiveTo = idList(q("exclusiveTo")),
    }
end

-- Entrada de una mision de la ventana (con coordenadas de inicio y fin).
local function buildEntry(env, f)
    local db, zone = env.db, env.zone
    local qid = f.qid
    local e = {
        questID = qid,
        name = Str(ns.SafeCall(db.QueryQuestSingle, qid, "name")),
        level = f.lvl,
        requiredLevel = f.req,
        zone = zoneNameOf(env, env.areaOf[qid]),
        accessibleNow = f.ev.accessibleNow,
        missingPrereqs = f.ev.missing,
    }
    e.source = "questie"
    e.starter = firstGiver(db, zone, questField(db, qid, "startedBy"), env.mapID, env.px, env.py)
    e.finisher = firstGiver(db, zone, questField(db, qid, "finishedBy"), env.mapID, env.px, env.py)
    e.rewards = rewardsFor(env.rewardIdx, qid, env.reverse, env.me.level)
    return e
end

-- ---------------------------------------------------------------------------
-- Sin Questie (o junto a ella): lo que el juego ofrece ahora y lo que el addon aprendio jugando.
-- ---------------------------------------------------------------------------

-- Nombre del mapa (cacheado en env.zoneNames con clave de uiMapID).
local function mapNameOf(env, mapID)
    if not mapID then return nil end
    local key = "m" .. tostring(mapID)
    local cached = env.zoneNames[key]
    if cached ~= nil then return cached or nil end
    local name = ns.Native.MapName(mapID)
    env.zoneNames[key] = name or false
    return name
end

-- Recompensas de una mision: del registro (API de Blizzard) o aprendidas (QUEST_DETAIL / QUEST_COMPLETE).
-- Devuelve { rewards = {choice, fixed, source}, items = { {itemID, questID, choice, source} } } o nil.
local function nativeRewards(qid, inLog, rec)
    if inLog then
        local r = ns.Native.Rewards(qid)
        if r then
            local items = {}
            for _, id in ipairs(r.choice) do items[#items + 1] = { itemID = id, questID = qid, choice = true, source = "juego" } end
            for _, id in ipairs(r.fixed) do items[#items + 1] = { itemID = id, questID = qid, source = "juego" } end
            return { rewards = { choice = copyList(r.choice), fixed = copyList(r.fixed), source = "juego" }, items = items }
        end
    end
    local learned = rec and rec.rewards and rec.rewards.items
    if type(learned) ~= "table" then return nil end
    local choice, fixed, items = {}, {}, {}
    for _, it in ipairs(learned) do
        local id = type(it) == "table" and Num(it.itemID) or nil
        if id then
            if it.choice == true then
                choice[#choice + 1] = id
                items[#items + 1] = { itemID = id, questID = qid, choice = true, source = "aprendido" }
            else
                fixed[#fixed + 1] = id
                items[#items + 1] = { itemID = id, questID = qid, source = "aprendido" }
            end
        end
    end
    if #items == 0 then return nil end
    return { rewards = { choice = choice, fixed = fixed, source = "aprendido" }, items = items }
end

-- Anade a `quests` (misiones ya calculadas por Questie, si las hay) lo que el juego ofrece ahora en el
-- mapa y lo aprendido. Reglas:
--   - Una mision que Questie ya trae y el juego ofrece ahora pasa a accessibleNow = true (manda el juego).
--   - Una mision nueva ofrecida ahora: source "juego", accessibleNow = true, nivel si Blizzard lo da.
--   - Una mision aprendida que no se ofrece ahora: source "aprendido", accessibleNow = false ("pronto").
--     Sin nivel conocido no entra (no se puede filtrar por la ventana).
--   - Prerrequisitos: solo los da Questie; sin Questie missingPrereqs queda vacio.
--   - Recompensas de las misiones del registro (API) o aprendidas; las de Questie no se tocan.
-- Entradas nuevas no pasan de QW.MAX_QUESTS. Devuelve el uso de fuentes { juego, aprendido }.
function QW.MergeNative(env, quests, rewardItems)
    local level = (env.me and env.me.level) or 1
    local used = { juego = false, aprendido = false }
    local learned = type(CompaneroWoWDB) == "table" and type(CompaneroWoWDB.knowledge) == "table"
        and CompaneroWoWDB.knowledge.quests or {}
    if type(learned) ~= "table" then learned = {} end

    local offered = {}
    for _, o in ipairs(ns.Native.AvailableQuests(env.mapID)) do offered[o.questID] = o end
    local inLog = {}
    for _, e in ipairs(ns.Native.LogEntries()) do inLog[e.questID] = true end

    local ids, seen, count = {}, {}, 0
    for qid in pairs(offered) do if not seen[qid] then seen[qid] = true; ids[#ids + 1] = qid end end
    for qid in pairs(learned) do if not seen[qid] then seen[qid] = true; ids[#ids + 1] = qid end end
    for qid in pairs(inLog) do if not seen[qid] then seen[qid] = true; ids[#ids + 1] = qid end end
    for _ in pairs(quests) do count = count + 1 end
    table.sort(ids)

    for i, qid in ipairs(ids) do
        local o = offered[qid]
        local rec = type(learned[qid]) == "table" and learned[qid] or nil
        local e = quests[qid]
        local lg = inLog[qid] == true
        if e then
            if o then e.accessibleNow = true end
        elseif count < QW.MAX_QUESTS then
            -- Sin requisito de nivel en la API: el nivel de la mision hace de requisito (un nivel 20 no entra a nivel 9).
            local lvl = nil
            if o or lg then lvl = ns.Native.QuestLevel(qid) end
            if not lvl and rec then lvl = Num(rec.level) end
            local keep
            if lvl then keep = QW.InWindow(lvl, lvl, level) else keep = (o ~= nil) or lg end
            -- La llamada a la API de completadas va al final: solo para las que pasarian la ventana.
            if keep and (o or lg or rec) and not ns.Native.Completed(qid) then
                local source = (o or lg) and "juego" or "aprendido"
                local entry = {
                    questID = qid,
                    name = (o and o.name) or (rec and Str(rec.title)) or Str(Call("C_QuestLog.GetTitleForQuestID", qid)),
                    level = lvl,
                    zone = mapNameOf(env, (o and o.mapID) or (rec and rec.offeredAt and rec.offeredAt.mapID)),
                    accessibleNow = (o ~= nil) or lg,
                    missingPrereqs = {},
                    source = source,
                    inLog = lg or nil,
                }
                if rec and rec.giverX and rec.giverY then
                    entry.starter = { kind = "npc", name = Str(rec.giverName), mapID = rec.giverMapID,
                        x = rec.giverX, y = rec.giverY, source = "aprendido" }
                elseif o and o.x and o.y then
                    entry.starter = { kind = "zone", mapID = o.mapID, x = o.x, y = o.y, source = "juego" }
                end
                quests[qid] = entry
                count = count + 1
                used[source] = true
                local r = nativeRewards(qid, inLog[qid] == true, rec)
                if r then
                    entry.rewards = r.rewards
                    for _, it in ipairs(r.items) do rewardItems[#rewardItems + 1] = it end
                end
            end
        end
        if i % CHECK_EVERY == 0 then coroutine.yield(1) end
    end
    -- Recompensas de misiones de Questie que no las traen: las del registro de Blizzard.
    for qid, e in pairs(quests) do
        if not e.rewards and inLog[qid] then
            local r = nativeRewards(qid, true, nil)
            if r then
                e.rewards = r.rewards
                for _, it in ipairs(r.items) do rewardItems[#rewardItems + 1] = it end
                used.juego = true
            end
        end
    end
    return used
end

-- Trabajo de la ventana (coroutine). env = { db, zone, me, rewardIdx, reverse, mapID, px, py, zoneNames }.
-- db y zone (Questie) son OPCIONALES: sin ellos la ventana sale de lo que ofrece el juego y de lo aprendido.
-- me = estado del personaje (ver QW.Evaluate). Deja en res.block la ventana y en res.rewardItems
-- la lista { {itemID, questID, choice, source} } de recompensas de las misiones incluidas.
function QW.Work(env, res)
    local db, zone = env.db, env.zone
    local me = env.me
    local level = me.level or 1
    local quests, rewardItems, bytes = {}, {}, 0
    env.zoneNames = env.zoneNames or {}
    local usedQuestie = false

    local map = db and zone and ns.SafeCall(zone.GetZonesWithQuests, false) or nil
    if type(map) == "table" then
        usedQuestie = true
        -- 1) Todas las misiones de las zonas de Questie (la primera zona en orden de areaId).
        local areas = {}
        for areaId in pairs(map) do
            if type(areaId) == "number" then areas[#areas + 1] = areaId end
        end
        table.sort(areas)
        local areaOf, n = {}, 0
        for _, areaId in ipairs(areas) do
            local set = map[areaId]
            if type(set) == "table" then
                for qid in pairs(set) do
                    if type(qid) == "number" and areaOf[qid] == nil then areaOf[qid] = areaId end
                    n = n + 1
                    if n % CHECK_EVERY == 0 then coroutine.yield(1) end
                end
            end
        end
        local qids = {}
        for qid in pairs(areaOf) do qids[#qids + 1] = qid end
        table.sort(qids)
        coroutine.yield(1)

        -- 2) Filtro: no completada, ventana de nivel, permitida para el personaje.
        local found = {}
        for i, qid in ipairs(qids) do
            if not me.completed[qid] then
                local req = Num(questField(db, qid, "requiredLevel"))
                local lvl = Num(questField(db, qid, "questLevel"))
                if lvl == nil or lvl <= 0 then lvl = req end
                if QW.InWindow(lvl, req, level) then
                    local ev = QW.Evaluate(questRules(db, qid, req), me)
                    if ev.allowed then found[#found + 1] = { qid = qid, lvl = lvl, req = req, ev = ev } end
                end
            end
            if i % CHECK_EVERY == 0 then coroutine.yield(1) end
        end

        -- 3) Mas cercanas al nivel primero; tope de 400 y de tamano.
        table.sort(found, function(a, b)
            local da, dbb = math.abs(a.lvl - level), math.abs(b.lvl - level)
            if da ~= dbb then return da < dbb end
            return a.qid < b.qid
        end)
        env.areaOf = areaOf
        local limit = math.min(#found, QW.MAX_QUESTS)
        for i = 1, limit do
            local f = found[i]
            local e = buildEntry(env, f)
            local size = QW.ApproxBytes(e) + 8
            if bytes + size > QW.MAX_BYTES then break end
            bytes = bytes + size
            quests[f.qid] = e
            if e.rewards then
                -- choice = true solo si la eleccion viene de ForeverDungeonJournal (no del indice de Questie).
                local fromFdj = e.rewards.rewardsKnownAs == nil
                local src = fromFdj and "fdj" or "questie"
                for _, itemID in ipairs(e.rewards.choice) do
                    rewardItems[#rewardItems + 1] = { itemID = itemID, questID = f.qid, choice = fromFdj or nil, source = src }
                end
                for _, itemID in ipairs(e.rewards.fixed) do
                    rewardItems[#rewardItems + 1] = { itemID = itemID, questID = f.qid, source = src }
                end
            end
            if i % CHECK_EVERY == 0 then coroutine.yield(1) end
        end
    end

    -- 4) Lo nativo: lo que el juego ofrece ahora y lo aprendido (con o sin Questie).
    local nat = QW.MergeNative(env, quests, rewardItems)
    local usedFdj = false
    for _, r in ipairs(rewardItems) do
        if r.source == "fdj" then usedFdj = true end
    end

    res.block = {
        available = true, exportedAt = ns.Now(), level = level,
        minLevel = level - QW.BELOW, maxLevel = level + QW.ABOVE, quests = quests,
        sources = ns.Native.SourceSummary({
            juego = nat.juego, questie = usedQuestie, fdj = usedFdj, aprendido = nat.aprendido,
        }),
    }
    res.rewardItems = rewardItems
end

-- Mision de la ventana con su estado, para /compa estado: { now, soon }.
function QW.Counts(block)
    local now, soon = 0, 0
    if type(block) ~= "table" or block.available ~= true or type(block.quests) ~= "table" then
        return nil
    end
    for _, q in pairs(block.quests) do
        if q.accessibleNow then now = now + 1 else soon = soon + 1 end
    end
    return { now = now, soon = soon }
end

return QW
