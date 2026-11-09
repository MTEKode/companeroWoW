-- QuestWindow.lua: ventana de misiones del personaje (questWindow del contrato GEAR / QUEST-WINDOW).
-- Solo LEE QuestieDB / ZoneDB (Forever) y ForeverDungeonJournal (recompensas). Nunca modifica nada.
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
QW.MAX_BYTES = 130 * 1024 -- tope aproximado del bloque en SavedVariables
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

-- rewards = { choice = {...}, fixed = {...} } o nil si la mision no tiene recompensas conocidas.
local function rewardsFor(idx, qid)
    local r = idx[qid]
    if not r then return nil end
    if r.choice then return { choice = copyList(r.items), fixed = {} } end
    return { choice = {}, fixed = copyList(r.items) }
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
    e.starter = firstGiver(db, zone, questField(db, qid, "startedBy"), env.mapID, env.px, env.py)
    e.finisher = firstGiver(db, zone, questField(db, qid, "finishedBy"), env.mapID, env.px, env.py)
    e.rewards = rewardsFor(env.rewardIdx, qid)
    return e
end

-- Trabajo de la ventana (coroutine). env = { db, zone, me, rewardIdx, mapID, px, py }.
-- me = estado del personaje (ver QW.Evaluate). Deja en res.block la ventana y en res.rewardItems
-- la lista { {itemID, questID, choice} } de recompensas de las misiones incluidas.
function QW.Work(env, res)
    local db, zone = env.db, env.zone
    local me = env.me
    local level = me.level or 1
    if not db or not zone then
        res.block = { available = false, reason = "Questie no está cargado" }
        res.rewardItems = {}
        return
    end
    local map = ns.SafeCall(zone.GetZonesWithQuests, false)
    if type(map) ~= "table" then
        res.block = { available = false, reason = "Questie no tiene zonas de misiones" }
        res.rewardItems = {}
        return
    end

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
    env.zoneNames = env.zoneNames or {}
    local quests, rewardItems, bytes = {}, {}, 0
    local limit = math.min(#found, QW.MAX_QUESTS)
    for i = 1, limit do
        local f = found[i]
        local e = buildEntry(env, f)
        local size = QW.ApproxBytes(e) + 8
        if bytes + size > QW.MAX_BYTES then break end
        bytes = bytes + size
        quests[f.qid] = e
        if e.rewards then
            local choice = e.rewards.choice
            for _, itemID in ipairs(choice) do
                rewardItems[#rewardItems + 1] = { itemID = itemID, questID = f.qid, choice = true }
            end
            for _, itemID in ipairs(e.rewards.fixed) do
                rewardItems[#rewardItems + 1] = { itemID = itemID, questID = f.qid }
            end
        end
        if i % CHECK_EVERY == 0 then coroutine.yield(1) end
    end

    res.block = {
        available = true, exportedAt = ns.Now(), level = level,
        minLevel = level - QW.BELOW, maxLevel = level + QW.ABOVE, quests = quests,
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
