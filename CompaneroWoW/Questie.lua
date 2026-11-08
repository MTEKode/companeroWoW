-- Questie.lua: exporta a CompaneroWoWDB.characters[key].questie los datos de Questie
-- (si esta instalado) que necesita la app. Solo LEE QuestieDB / ZoneDB; nunca modifica nada.
-- Contrato compartido: ver la seccion QUESTIE EXPORT CONTRACT de la tarea T11/T12.
-- Coordenadas siempre uiMapID + 0..1. Cualquier fallo -> sin datos, nunca error de Lua.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean
local bit = bit

local MAX_QUESTS = 35
local MAX_ZONE_QUESTS = 40
local MAX_NPCS = 40
local MAX_STARTER_SPAWNS = 5
local MAX_OBJECTIVE_SPAWNS = 10
local EXPORT_THROTTLE = 10 -- segundos entre exportaciones automaticas

-- Estado en memoria (no se guarda): ultima exportacion, pendiente, caches por zona.
ns.Questie = ns.Questie or {}
local Q = ns.Questie

-- Mascaras de npcFlags de Questie que dan rol a un PNJ (orden = prioridad).
local ROLE_FLAGS = {
    { "FLIGHT_MASTER", "flightmaster" },
    { "INNKEEPER", "innkeeper" },
    { "VENDOR", "vendor" },
}

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function round3(v)
    return math.floor(v * 1000 + 0.5) / 1000
end

-- ---------------------------------------------------------------------------
-- Deteccion de Questie
-- ---------------------------------------------------------------------------

-- Devuelve QuestieDB y ZoneDB (ZoneDB puede ser nil) o nil si Questie no esta cargado.
-- ImportModule crea un marcador vacio si el modulo no existe: por eso se comprueba la funcion.
local function questieModules()
    local loader = _G.QuestieLoader
    if type(loader) ~= "table" or type(loader.ImportModule) ~= "function" then return nil end
    local ok, db = pcall(loader.ImportModule, loader, "QuestieDB")
    if not ok or type(db) ~= "table" then return nil end
    if type(db.QueryQuestSingle) ~= "function" or type(db.QueryNPCSingle) ~= "function" then return nil end
    local okZ, zone = pcall(loader.ImportModule, loader, "ZoneDB")
    if not okZ or type(zone) ~= "table" or type(zone.GetUiMapIdByAreaId) ~= "function" then zone = nil end
    return db, zone
end

-- Campo de una mision en QuestieDB. nil si falla.
local function questField(db, qid, key)
    return ns.SafeCall(db.QueryQuestSingle, qid, key)
end

local ENTITY_QUERY = { npc = "QueryNPCSingle", object = "QueryObjectSingle", item = "QueryItemSingle" }

local function entityName(db, kind, id)
    local fn = db[ENTITY_QUERY[kind]]
    if type(fn) ~= "function" then return nil end
    return Str(ns.SafeCall(fn, id, "name"))
end

-- ---------------------------------------------------------------------------
-- Coordenadas y orden
-- ---------------------------------------------------------------------------

-- Spawns de Questie { [areaId] = { {x, y}, ... } } (x,y en 0..100) -> lista { {mapID, x, y}, ... }
-- en 0..1. Se descarta cualquier punto que no se pueda convertir a uiMapID.
local function convertSpawns(raw, zone, onlyArea)
    local out = {}
    if type(raw) ~= "table" or not zone then return out end
    for areaId, points in pairs(raw) do
        if onlyArea == nil or areaId == onlyArea then
            local mapID = Num(ns.SafeCall(zone.GetUiMapIdByAreaId, zone, areaId))
            if mapID and type(points) == "table" then
                for _, p in ipairs(points) do
                    local x, y = type(p) == "table" and Num(p[1]), type(p) == "table" and Num(p[2])
                    if x and y and x >= 0 and x <= 100 and y >= 0 and y <= 100 then
                        out[#out + 1] = { mapID = mapID, x = round3(x / 100), y = round3(y / 100) }
                    end
                end
            end
        end
    end
    return out
end

-- Ordena: primero los del mapa del jugador por distancia; el resto en su orden original.
-- Devuelve una copia nueva.
local function nearestFirst(list, mapID, px, py)
    local keyed = {}
    for i, s in ipairs(list) do
        local d = math.huge
        if mapID and px and py and s.mapID == mapID then
            d = math.sqrt((s.x - px) ^ 2 + (s.y - py) ^ 2)
        end
        keyed[i] = { d = d, i = i, s = s }
    end
    table.sort(keyed, function(a, b)
        if a.d ~= b.d then return a.d < b.d end
        return a.i < b.i
    end)
    local out = {}
    for i, k in ipairs(keyed) do out[i] = k.s end
    return out
end

local function take(list, n)
    local out = {}
    for i = 1, math.min(#list, n) do out[i] = list[i] end
    return out
end

ns.Questie.NearestFirst = nearestFirst

-- ---------------------------------------------------------------------------
-- Starters, finishers y objetivos
-- ---------------------------------------------------------------------------

-- slots de startedBy / finishedBy: [1] = npcs, [2] = objetos, [3] = items.
local SLOT_KIND = { { 1, "npc" }, { 2, "object" }, { 3, "item" } }

local function listEntities(db, zone, slots, mapID, px, py)
    local out = {}
    if type(slots) ~= "table" then return out end
    for _, def in ipairs(SLOT_KIND) do
        local ids = slots[def[1]]
        if type(ids) == "table" then
            for _, rawId in ipairs(ids) do
                local id = Num(rawId)
                if id and id > 0 then
                    local kind = def[2]
                    local spawns = {}
                    if kind ~= "item" then
                        local fn = db[ENTITY_QUERY[kind]]
                        if type(fn) == "function" then
                            spawns = convertSpawns(ns.SafeCall(fn, id, "spawns"), zone)
                        end
                    end
                    local entry = {
                        kind = kind,
                        id = id,
                        name = entityName(db, kind, id),
                        spawns = take(nearestFirst(spawns, mapID, px, py), MAX_STARTER_SPAWNS),
                    }
                    out[#out + 1] = entry
                end
            end
        end
    end
    return out
end

-- Objetivos en el orden del registro de misiones (monstruos, objetos, items, killcredit).
-- logObjs: C_QuestLog.GetQuestObjectives(qid). Si el numero no coincide, no se pone 'required'.
local function buildObjectives(db, zone, qid, logObjs, mapID, px, py)
    local raw = questField(db, qid, "objectives")
    local list = {}
    if type(raw) ~= "table" then return list end

    local function add(kind, id, name, spawns)
        list[#list + 1] = {
            index = #list + 1,
            kind = kind,
            id = id,
            name = name,
            spawns = take(nearestFirst(spawns, mapID, px, py), MAX_OBJECTIVE_SPAWNS),
        }
    end

    if type(raw[1]) == "table" then -- monstruos: { npcId, texto, icono }
        for _, o in ipairs(raw[1]) do
            local id = type(o) == "table" and Num(o[1])
            if id and id > 0 then
                local spawns = convertSpawns(ns.SafeCall(db.QueryNPCSingle, id, "spawns"), zone)
                add("monster", id, entityName(db, "npc", id) or (type(o[2]) == "string" and o[2]) or nil, spawns)
            end
        end
    end
    if type(raw[2]) == "table" then -- objetos: { objectId, texto, icono }
        for _, o in ipairs(raw[2]) do
            local id = type(o) == "table" and Num(o[1])
            if id and id > 0 then
                local spawns = convertSpawns(ns.SafeCall(db.QueryObjectSingle, id, "spawns"), zone)
                add("object", id, entityName(db, "object", id) or (type(o[2]) == "string" and o[2]) or nil, spawns)
            end
        end
    end
    if type(raw[3]) == "table" then -- items: no tienen posicion propia en Questie
        for _, o in ipairs(raw[3]) do
            local id = type(o) == "table" and Num(o[1])
            if id and id > 0 then
                add("item", id, entityName(db, "item", id) or (type(o[2]) == "string" and o[2]) or nil, {})
            end
        end
    end
    if type(raw[5]) == "table" then -- killcredit: { {IdList, RootId, Text, Icon}, ... }
        for _, c in ipairs(raw[5]) do
            if type(c) == "table" then
                local root = Num(c[2])
                local id = root or (type(c[1]) == "table" and Num(c[1][1])) or nil
                if id and id > 0 then
                    local spawns = convertSpawns(ns.SafeCall(db.QueryNPCSingle, id, "spawns"), zone)
                    add("killcredit", id, entityName(db, "npc", id) or (type(c[3]) == "string" and c[3]) or nil, spawns)
                end
            end
        end
    end

    if logObjs and #logObjs == #list then
        for i, o in ipairs(list) do
            local src = logObjs[i]
            o.required = type(src) == "table" and Num(src.numRequired) or nil
        end
    end
    return list
end

-- Lista de objetivos de la mision segun el registro (solo para comparar cantidades).
local function liveObjectives(qid)
    local list = Clean(Call("C_QuestLog.GetQuestObjectives", qid))
    if type(list) ~= "table" then return {} end
    return list
end

local function buildQuest(db, zone, info, qid, mapID, px, py)
    local q = { questID = qid }
    q.name = Str(info.title) or Str(questField(db, qid, "name"))
    q.level = Num(info.level)
    q.requiredLevel = Num(questField(db, qid, "requiredLevel"))
    local startedBy = questField(db, qid, "startedBy")
    local finishedBy = questField(db, qid, "finishedBy")
    q.starters = listEntities(db, zone, startedBy, mapID, px, py)
    q.finishers = listEntities(db, zone, finishedBy, mapID, px, py)
    q.objectives = buildObjectives(db, zone, qid, liveObjectives(qid), mapID, px, py)
    local pre = questField(db, qid, "preQuestSingle")
    if type(pre) == "table" and #pre > 0 then
        local list = {}
        for _, v in ipairs(pre) do
            local n = Num(v)
            if n then list[#list + 1] = n end
        end
        if #list > 0 then q.preQuests = list end
    end
    local nextQ = Num(questField(db, qid, "nextQuestInChain"))
    if nextQ and nextQ > 0 then q.nextQuest = nextQ end
    return q
end

-- ---------------------------------------------------------------------------
-- Registro de misiones (orden del juego)
-- ---------------------------------------------------------------------------

-- Lista { {questID, info}, ... } en orden del registro, sin cabeceras.
function ns.Questie.QuestLogEntries()
    local out = {}
    local n = Num(Call("C_QuestLog.GetNumQuestLogEntries")) or 0
    for i = 1, n do
        local info = Clean(Call("C_QuestLog.GetInfo", i))
        if type(info) == "table" and Clean(info.isHeader) ~= true then
            local qid = Num(info.questID)
            if qid and qid > 0 then out[#out + 1] = { questID = qid, info = info } end
        end
    end
    return out
end

-- ---------------------------------------------------------------------------
-- Datos de zona y PNJ (cacheados por zona: cambian solo al cambiar de zona)
-- ---------------------------------------------------------------------------

-- Misiones que se pueden coger en la zona (ZoneDB.GetZonesWithQuests, mapa completo).
local function zoneCandidates(zone, areaId)
    if Q.zoneCache and Q.zoneCache.areaId == areaId then return Q.zoneCache.list end
    local list = {}
    if type(zone.GetZonesWithQuests) == "function" then
        local map = ns.SafeCall(zone.GetZonesWithQuests, false)
        if type(map) == "table" and type(map[areaId]) == "table" then
            for qid in pairs(map[areaId]) do
                if type(qid) == "number" then list[#list + 1] = qid end
            end
            table.sort(list)
        end
    end
    Q.zoneCache = { areaId = areaId, list = list }
    return list
end

-- Primer PNJ/objeto que da la mision y tiene posicion en el mapa dado.
local function starterOnMap(db, zone, qid, mapID)
    local slots = questField(db, qid, "startedBy")
    if type(slots) ~= "table" then return nil end
    for _, def in ipairs({ { 1, "npc" }, { 2, "object" } }) do
        local ids = slots[def[1]]
        if type(ids) == "table" then
            for _, rawId in ipairs(ids) do
                local id = Num(rawId)
                local fn = id and db[ENTITY_QUERY[def[2]]]
                if id and type(fn) == "function" then
                    local spawns = convertSpawns(ns.SafeCall(fn, id, "spawns"), zone)
                    for _, s in ipairs(spawns) do
                        if s.mapID == mapID then
                            return { kind = def[2], id = id, name = entityName(db, def[2], id), mapID = s.mapID, x = s.x, y = s.y }
                        end
                    end
                end
            end
        end
    end
    return nil
end

local function completedFlag(qid)
    return Clean(Call("C_QuestLog.IsQuestFlaggedCompleted", qid)) == true
end

local function zoneQuestList(db, zone, mapID, areaId, level, inLog)
    local found = {}
    for _, qid in ipairs(zoneCandidates(zone, areaId)) do
        if not inLog[qid] and not completedFlag(qid) then
            local req = Num(questField(db, qid, "requiredLevel"))
            local lvl = Num(questField(db, qid, "questLevel")) or req
            if lvl and lvl > 0 and (req == nil or req <= level) and lvl >= level - 5 and lvl <= level + 3 then
                local starter = starterOnMap(db, zone, qid, mapID)
                if starter then
                    found[#found + 1] = {
                        questID = qid,
                        name = Str(questField(db, qid, "name")),
                        level = lvl,
                        requiredLevel = req,
                        starter = starter,
                        _dist = math.abs(lvl - level),
                    }
                end
            end
        end
    end
    table.sort(found, function(a, b)
        if a._dist ~= b._dist then return a._dist < b._dist end
        return a.questID < b.questID
    end)
    local out = {}
    for i = 1, math.min(#found, MAX_ZONE_QUESTS) do
        found[i]._dist = nil
        out[i] = found[i]
    end
    return out
end

-- PNJs con rol (vuelo, posada, vendedor) con posicion en la zona. Cacheado por zona.
local function zoneNpcCandidates(db, zone, areaId)
    if Q.npcCache and Q.npcCache.areaId == areaId then return Q.npcCache.list end
    local list = {}
    local flagTable = db.npcFlags
    local pointers = db.NPCPointers
    if type(flagTable) == "table" and type(pointers) == "table" then
        for id in pairs(pointers) do
            local flags = Num(ns.SafeCall(db.QueryNPCSingle, id, "npcFlags"))
            if flags and flags > 0 then
                local role = nil
                for _, rf in ipairs(ROLE_FLAGS) do
                    local mask = Num(flagTable[rf[1]])
                    if mask and mask > 0 and bit.band(flags, mask) == mask then
                        role = rf[2]
                        break
                    end
                end
                if role then
                    local name = Str(ns.SafeCall(db.QueryNPCSingle, id, "name"))
                    if name and string.sub(name, 1, 5) ~= "[DND]" then
                        local spawns = convertSpawns(ns.SafeCall(db.QueryNPCSingle, id, "spawns"), zone, areaId)
                        if #spawns > 0 then
                            list[#list + 1] = {
                                id = id,
                                name = name,
                                role = role,
                                subtitle = Str(ns.SafeCall(db.QueryNPCSingle, id, "subName")),
                                spawns = spawns,
                            }
                        end
                    end
                end
            end
        end
    end
    table.sort(list, function(a, b) return a.id < b.id end)
    Q.npcCache = { areaId = areaId, list = list }
    return list
end

local function zoneNpcs(db, zone, areaId, mapID, px, py)
    local out = {}
    for _, c in ipairs(zoneNpcCandidates(db, zone, areaId)) do
        local near = nearestFirst(c.spawns, mapID, px, py)[1]
        if near then
            local e = { id = c.id, name = c.name, role = c.role, mapID = near.mapID, x = near.x, y = near.y }
            if c.subtitle then e.subtitle = c.subtitle end
            out[#out + 1] = e
        end
    end
    -- Mas cercanos primero; sin posicion del jugador, el orden por id.
    local keyed = {}
    for i, e in ipairs(out) do
        local d = math.huge
        if px and py and e.mapID == mapID then d = math.sqrt((e.x - px) ^ 2 + (e.y - py) ^ 2) end
        keyed[i] = { d = d, i = i, e = e }
    end
    table.sort(keyed, function(a, b)
        if a.d ~= b.d then return a.d < b.d end
        return a.i < b.i
    end)
    local res = {}
    for i = 1, math.min(#keyed, MAX_NPCS) do res[i] = keyed[i].e end
    return res
end

-- ---------------------------------------------------------------------------
-- Exportacion
-- ---------------------------------------------------------------------------

-- Construye la tabla questie completa. Sin Questie: { available = false }.
-- Cada parte va protegida: un fallo omite esa parte, nunca lanza error.
function ns.BuildQuestieExport()
    local db, zone = questieModules()
    if not db then return { available = false } end

    local out = { available = true, exportedAt = ns.Now() }
    local version = Str(Call("C_AddOns.GetAddOnMetadata", "Questie", "Version"))
        or Str(Call("GetAddOnMetadata", "Questie", "Version"))
    if version then out.version = version end

    local mapID, px, py = ns.GetPlayerPosition()
    local level = Num(Call("UnitLevel", "player")) or 1

    local entries = ns.Questie.QuestLogEntries()
    local inLog = {}
    local quests = {}
    local count = 0
    for _, e in ipairs(entries) do
        inLog[e.questID] = true
        if count < MAX_QUESTS then
            local ok, q = pcall(buildQuest, db, zone, e.info, e.questID, mapID, px, py)
            if ok and type(q) == "table" then
                quests[e.questID] = q
                count = count + 1
            end
        end
    end
    out.quests = quests

    local areaId = nil
    if zone and mapID and type(zone.GetAreaIdByUiMapId) == "function" then
        areaId = Num(ns.SafeCall(zone.GetAreaIdByUiMapId, zone, mapID))
    end
    if zone and mapID and areaId then
        local ok, list = pcall(zoneQuestList, db, zone, mapID, areaId, level, inLog)
        if ok and type(list) == "table" then
            out.zoneQuests = { mapID = mapID, list = list }
        end
        local okN, npcs = pcall(zoneNpcs, db, zone, areaId, mapID, px, py)
        if okN and type(npcs) == "table" then out.npcs = npcs end
    end
    return out
end

-- Exporta ahora (sin throttle) y guarda en la entrada del personaje. Devuelve la tabla o nil.
function ns.ExportQuestieNow()
    if ns.InCombat() then return nil end
    local key = ns.CharKey()
    if not key then return nil end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    local data = ns.SafeCall(ns.BuildQuestieExport) or { available = false }
    c.questie = data
    Q.last = data
    Q.lastAt = nowSec()
    return data
end

-- Exportacion automatica: fuera de combate, como maximo una vez cada 10 s.
-- Si cae dentro de la ventana, deja una exportacion diferida.
function ns.RequestQuestieExport()
    if ns.InCombat() then return false end
    local t = nowSec()
    if Q.lastAt == nil or t - Q.lastAt >= EXPORT_THROTTLE then
        return ns.ExportQuestieNow() ~= nil
    end
    if not Q.pending and ns.Get("C_Timer.After") then
        Q.pending = true
        ns.Call("C_Timer.After", EXPORT_THROTTLE - (t - Q.lastAt), function()
            Q.pending = false
            ns.SafeCall(ns.RequestQuestieExport)
        end)
    end
    return false
end

-- Linea para /compa estado: cuantas misiones y de la zona se exportaron.
function ns.QuestieStatusLine()
    local db = ns.EnsureDB()
    local key = ns.CharKey()
    local c = key and db.characters[key]
    local data = type(c) == "table" and c.questie or nil
    if type(data) ~= "table" then return "Questie: sin exportar todavía (/compa sync)" end
    if data.available ~= true then return "Questie: no disponible" end
    local nQuests, nZone = 0, 0
    if type(data.quests) == "table" then for _ in pairs(data.quests) do nQuests = nQuests + 1 end end
    if type(data.zoneQuests) == "table" and type(data.zoneQuests.list) == "table" then
        nZone = #data.zoneQuests.list
    end
    return string.format("Questie %s: %d misiones exportadas, %d disponibles en la zona",
        data.version or "?", nQuests, nZone)
end
