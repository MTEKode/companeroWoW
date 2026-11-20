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
local MAX_ZONE_NPCS = 60       -- PNJs con rol en la zona actual
local MAX_CAPITAL_NPCS = 120   -- PNJs con rol en las capitales de la faccion (total)
local MAX_STARTER_SPAWNS = 5
local MAX_OBJECTIVE_SPAWNS = 10
local EXPORT_THROTTLE = 10 -- segundos entre exportaciones automaticas

-- Estado en memoria (no se guarda): ultima exportacion, pendiente, caches por zona.
ns.Questie = ns.Questie or {}
local Q = ns.Questie

-- Mascaras de QuestieDB.npcFlags (juego Forever: VENDOR=4, TRAINER=16, INNKEEPER=128...) que dan rol.
-- Orden = prioridad. Los nombres son las claves de db.npcFlags.
local ROLE_FLAGS = {
    { "FLIGHT_MASTER", "flightmaster" },
    { "INNKEEPER", "innkeeper" },
    { "BANKER", "banker" },
    { "AUCTIONEER", "auctioneer" },
    { "TRAINER", "trainer" },
    { "VENDOR", "vendor" },
}

-- Palabras de subName (ingles o espanol, en minusculas) que dan rol cuando npcFlags no basta.
-- Orden = prioridad: "vuelo" antes que "maestro" (Maestro de vuelo = vuelo, no entrenador).
local ROLE_WORDS = {
    { "flightmaster", { "flight master", "vuelo" } },
    { "innkeeper", { "innkeeper", "posadero", "posadera" } },
    { "banker", { "banker", "banquero", "banquera" } },
    { "auctioneer", { "auctioneer", "subastador", "subastadora" } },
    { "trainer", { "trainer", "instructor", "instructora", "maestro", "maestra" } },
    { "vendor", { "vendor", "vendedor", "vendedora", "proveedor", "proveedora", "suministros" } },
}

-- Areas (areaId de Questie) de las capitales de cada faccion (QuestieDB corrections/enum/zones.lua).
local CAPITALS = {
    Alliance = { 1519, 1537, 1657 }, -- Ventormenta, Forjaz, Darnassus
    Horde = { 1637, 1638, 1497 },    -- Orgrimmar, Cima del Trueno, Entrañas
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
    local ok, db = ns.Pcall(loader.ImportModule, loader, "QuestieDB")
    if not ok or type(db) ~= "table" then return nil end
    if type(db.QueryQuestSingle) ~= "function" or type(db.QueryNPCSingle) ~= "function" then return nil end
    local okZ, zone = ns.Pcall(loader.ImportModule, loader, "ZoneDB")
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
ns.Questie.ConvertSpawns = convertSpawns -- para QuestWindow.lua y Gear.lua
ns.Questie.Modules = questieModules

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

-- Rol por npcFlags (claves de db.npcFlags, las de este juego). nil si no tiene ninguno.
local function roleFromFlags(db, flags)
    local flagTable = db.npcFlags
    if not flags or flags <= 0 or type(flagTable) ~= "table" then return nil end
    for _, rf in ipairs(ROLE_FLAGS) do
        local mask = Num(flagTable[rf[1]])
        if mask and mask > 0 and bit.band(flags, mask) == mask then return rf[2] end
    end
    return nil
end

-- Rol por subName (ingles o espanol, sin mayusculas). nil si no encaja con ninguna palabra.
local function roleFromSubtitle(subtitle)
    if not subtitle then return nil end
    local s = string.lower(subtitle)
    for _, rw in ipairs(ROLE_WORDS) do
        for _, word in ipairs(rw[2]) do
            if string.find(s, word, 1, true) then return rw[1] end
        end
    end
    return nil
end

-- ---------------------------------------------------------------------------
-- Trabajo incremental de PNJs: coroutines troceadas por tick (C_Timer.NewTicker).
-- La exportacion NO espera: mientras el trabajo corre, questie.npcs no aparece; al terminar
-- se reexporta sola (ExportQuestieNow). Nunca corre en combate ni de forma sincrona en SnapshotAll.
-- Presupuesto por tick: NPC_TICK_MS (si existe debugprofilestop) y NPC_TICK_UNITS (un PNJ = una unidad).
-- ---------------------------------------------------------------------------

local NPC_TICK_SEC = 0.05   -- intervalo entre ticks
local NPC_TICK_MS = 4       -- presupuesto de tiempo por tick
local NPC_TICK_UNITS = 200  -- presupuesto de unidades por tick
local NPC_CACHE_MAX = 6     -- listas de candidatos (una por zona) que se guardan

local npcTasks = {}         -- { co, kind = "index" | "cands", key, onDone, cancelled }
local npcTicker = nil       -- ticker de C_Timer.NewTicker activo
local npcTickAfter = false  -- fallback sin NewTicker: cadena de C_Timer.After

local function nowMs()
    return Num(ns.Call("debugprofilestop"))
end

local function stopTicker()
    if npcTicker then
        ns.Method(npcTicker, "Cancel")
        npcTicker = nil
    end
end

-- Avanza las tareas dentro del presupuesto. Devuelve true si queda trabajo.
local function runTasks()
    -- En combate no se avanza: los trabajos siguen pendientes y se retoman fuera de combate.
    if ns.InCombat() then return #npcTasks > 0 end
    local t0 = nowMs()
    local units, i = 0, 1
    while i <= #npcTasks and units < NPC_TICK_UNITS do
        local now = nowMs()
        if t0 and now and now - t0 >= NPC_TICK_MS then break end
        local task = npcTasks[i]
        if task.cancelled then
            table.remove(npcTasks, i)
        else
            local ok, n = coroutine.resume(task.co)
            if not ok then
                table.remove(npcTasks, i)
                task.error = tostring(n)
                if task.kind == "index" then Q.npcIndexFailed = true end
            elseif coroutine.status(task.co) == "dead" then
                table.remove(npcTasks, i)
                if task.onDone then ns.SafeCall(task.onDone, task) end
            else
                -- Trabajo en curso (yield 1): sigue en esta misma tarea hasta agotar el presupuesto.
                -- Esperando (yield 0): pasa a la siguiente tarea.
                local cost = Num(n) or 1
                units = units + math.max(1, cost)
                if cost <= 0 then i = i + 1 end
            end
        end
    end
    return #npcTasks > 0
end

local function tickAfter()
    ns.Call("C_Timer.After", NPC_TICK_SEC, function()
        if runTasks() then tickAfter() else npcTickAfter = false end
    end)
end

local function ensureTicker()
    if npcTicker or npcTickAfter or #npcTasks == 0 then return end
    npcTicker = ns.Call("C_Timer.NewTicker", NPC_TICK_SEC, function()
        if not runTasks() then stopTicker() end
    end)
    if not npcTicker then
        npcTickAfter = true
        tickAfter()
    end
end

local function addTask(task)
    npcTasks[#npcTasks + 1] = task
    ensureTicker()
end

-- Indice global de PNJs con rol o subtitulo (cacheado en la sesion). Una pasada incremental
-- sobre NPCPointers. Entradas { id, name, role, subtitle? }; role = "other" si solo tiene subtitulo.
-- Los nombres vienen ya localizados por QuestieDB (l10n del cliente); sin traduccion, en ingles.
local function indexWork(db)
    return function()
        local ids, n = {}, 0
        local pointers = db.NPCPointers
        if type(pointers) == "table" then
            for id in pairs(pointers) do
                n = n + 1
                ids[n] = id
                if n % 500 == 0 then coroutine.yield(1) end
            end
        end
        local list = {}
        for i = 1, n do
            local id = Num(ids[i])
            if id and id > 0 then
                local subtitle = Str(ns.SafeCall(db.QueryNPCSingle, id, "subName"))
                if subtitle == "" then subtitle = nil end
                local role = roleFromFlags(db, Num(ns.SafeCall(db.QueryNPCSingle, id, "npcFlags")))
                    or roleFromSubtitle(subtitle)
                if role or subtitle then
                    local name = Str(ns.SafeCall(db.QueryNPCSingle, id, "name"))
                    if name and string.sub(name, 1, 5) ~= "[DND]" then
                        list[#list + 1] = { id = id, name = name, role = role or "other", subtitle = subtitle }
                    end
                end
            end
            coroutine.yield(1)
        end
        table.sort(list, function(a, b) return a.id < b.id end)
        Q.npcIndex = list
    end
end

local function ensureIndex(db)
    if Q.npcIndex or Q.npcIndexFailed then return end
    for _, t in ipairs(npcTasks) do
        if t.kind == "index" and not t.cancelled then return end
    end
    addTask({ kind = "index", co = coroutine.create(indexWork(db)) })
end

-- Candidatos de una zona: PNJs del indice con spawn en 'areas' (ids de area de Questie).
-- Espera al indice si aun no esta listo. Devuelve { { npc, spawns = { [areaId] = {{mapID,x,y}...} } } }.
local function candWork(db, zone, areas, key)
    return function()
        while not Q.npcIndex do
            if Q.npcIndexFailed then return end
            coroutine.yield(0)
        end
        local cands = {}
        for _, npc in ipairs(Q.npcIndex) do
            local raw = ns.SafeCall(db.QueryNPCSingle, npc.id, "spawns")
            if type(raw) == "table" then
                local byArea, any = {}, false
                for _, areaId in ipairs(areas) do
                    local spawns = convertSpawns(raw, zone, areaId)
                    if #spawns > 0 then
                        byArea[areaId] = spawns
                        any = true
                    end
                end
                if any then cands[#cands + 1] = { npc = npc, spawns = byArea } end
            end
            coroutine.yield(1)
        end
        Q.npcCands = Q.npcCands or {}
        local count = 0
        for _ in pairs(Q.npcCands) do count = count + 1 end
        if count >= NPC_CACHE_MAX then Q.npcCands = {} end
        Q.npcCands[key] = cands
    end
end

-- Al terminar una lista de candidatos, reexporta (solo si quedo guardada).
local function onCandsDone(task)
    if Q.npcCands and Q.npcCands[task.key] then ns.SafeCall(ns.ExportQuestieNow) end
end

-- Pide la lista de candidatos de la zona (idempotente). Cancela la de otra zona: solo una a la vez.
local function requestNpcCandidates(db, zone, areas, key)
    if Q.npcCands and Q.npcCands[key] then return end
    if Q.npcIndexFailed then return end
    for _, t in ipairs(npcTasks) do
        if t.kind == "cands" and t.key == key and not t.cancelled then return end
    end
    for _, t in ipairs(npcTasks) do
        if t.kind == "cands" then t.cancelled = true end
    end
    ensureIndex(db)
    addTask({ kind = "cands", key = key, co = coroutine.create(candWork(db, zone, areas, key)), onDone = onCandsDone })
end

-- Cancela y olvida todo el trabajo de PNJs (el indice y las zonas guardadas). Para pruebas y recargas.
function ns.Questie.ResetNpc()
    for _, t in ipairs(npcTasks) do t.cancelled = true end
    npcTasks = {}
    stopTicker()
    npcTickAfter = false
    Q.npcIndex = nil
    Q.npcIndexFailed = nil
    Q.npcCands = nil
end

-- Numero de tareas de PNJs pendientes (para pruebas).
function ns.Questie.PendingNpcTasks()
    return #npcTasks
end

-- Entradas { id, name, role, subtitle?, mapID, x, y } de los candidatos con spawn en 'areaIds'
-- (el spawn mas cercano al jugador), excluyendo los ids de 'skip'. Mas cercanos primero;
-- sin posicion del jugador en ese mapa, orden por id.
local function npcEntries(cands, areaIds, skip, mapID, px, py)
    local keyed = {}
    for _, c in ipairs(cands) do
        if not (skip and skip[c.npc.id]) then
            local spawns = {}
            for _, areaId in ipairs(areaIds) do
                for _, s in ipairs(c.spawns[areaId] or {}) do spawns[#spawns + 1] = s end
            end
            local near = nearestFirst(spawns, mapID, px, py)[1]
            if near then
                local e = { id = c.npc.id, name = c.npc.name, role = c.npc.role, mapID = near.mapID, x = near.x, y = near.y }
                if c.npc.subtitle then e.subtitle = c.npc.subtitle end
                local d = math.huge
                if px and py and near.mapID == mapID then d = math.sqrt((near.x - px) ^ 2 + (near.y - py) ^ 2) end
                keyed[#keyed + 1] = { d = d, i = #keyed + 1, e = e }
            end
        end
    end
    table.sort(keyed, function(a, b)
        if a.d ~= b.d then return a.d < b.d end
        return a.i < b.i
    end)
    local out = {}
    for i, k in ipairs(keyed) do out[i] = k.e end
    return out
end

-- PNJs con rol para la zona actual (max MAX_ZONE_NPCS) y para las capitales de la faccion
-- (max MAX_CAPITAL_NPCS, sin repetir los de la zona). Devuelve zona primero, luego capitales.
-- Devuelve nil mientras la lista de candidatos de la zona no esta lista (se pide en segundo plano).
local function zoneNpcs(db, zone, areaId, faction, mapID, px, py)
    local areas, capitalAreas = { areaId }, {}
    for _, a in ipairs(CAPITALS[faction] or {}) do
        capitalAreas[#capitalAreas + 1] = a
        if a ~= areaId then areas[#areas + 1] = a end
    end
    local key = table.concat(areas, ",")
    local cands = Q.npcCands and Q.npcCands[key]
    if not cands then
        requestNpcCandidates(db, zone, areas, key)
        return nil
    end
    local zoneList = take(npcEntries(cands, { areaId }, nil, mapID, px, py), MAX_ZONE_NPCS)
    local skip = {}
    for _, e in ipairs(zoneList) do skip[e.id] = true end
    local capList = take(npcEntries(cands, capitalAreas, skip, mapID, px, py), MAX_CAPITAL_NPCS)
    local out = {}
    for _, e in ipairs(zoneList) do out[#out + 1] = e end
    for _, e in ipairs(capList) do out[#out + 1] = e end
    return out
end

-- ---------------------------------------------------------------------------
-- Exportacion
-- ---------------------------------------------------------------------------

-- Marca el origen "questie" en cada elemento del bloque y el resumen de fuentes.
local function tagQuestie(out)
    local function tagList(list)
        for _, e in ipairs(list or {}) do e.source = "questie" end
    end
    for _, q in pairs(out.quests or {}) do
        q.source = "questie"
        tagList(q.starters)
        tagList(q.finishers)
        tagList(q.objectives)
    end
    if type(out.zoneQuests) == "table" then
        for _, z in ipairs(out.zoneQuests.list or {}) do
            z.source = "questie"
            if z.starter then z.starter.source = "questie" end
        end
    end
    tagList(out.npcs)
    out.sources = ns.Native.SourceSummary({ questie = true })
end

-- Construye la tabla questie completa. Sin Questie (opcional): { available = false, reason }.
-- Cada parte va protegida: un fallo omite esa parte, nunca lanza error.
function ns.BuildQuestieExport()
    local db, zone = questieModules()
    if not db then
        return {
            available = false, reason = "Questie no está instalado (opcional)",
            sources = ns.Native.SourceSummary({}),
        }
    end

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
            local ok, q = ns.Pcall(buildQuest, db, zone, e.info, e.questID, mapID, px, py)
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
        local ok, list = ns.Pcall(zoneQuestList, db, zone, mapID, areaId, level, inLog)
        if ok and type(list) == "table" then
            out.zoneQuests = { mapID = mapID, list = list }
        end
        local faction = Str(Call("UnitFactionGroup", "player"))
        local okN, npcs = ns.Pcall(zoneNpcs, db, zone, areaId, faction, mapID, px, py)
        if okN and type(npcs) == "table" then out.npcs = npcs end
    end
    tagQuestie(out)
    return out
end

-- Exporta ahora (sin throttle) y guarda en la entrada del personaje. Devuelve la tabla o nil.
function ns.ExportQuestieNow()
    if ns.InCombat() then return nil end
    if not ns.IsFullyInWorld() then return nil end -- carga/salida: Questie puede estar a medias
    local key = ns.CharKey()
    if not key then return nil end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    local data = ns.SafeCall(ns.BuildQuestieExport) or { available = false }
    -- Una exportacion no disponible no sustituye a una buena anterior.
    c.questie = ns.PreferAvailable(c.questie, data)
    Q.last = c.questie
    Q.lastAt = nowSec()
    -- El resumen de Questie (questieStatus) va al estado del personaje en cuanto cambia.
    if ns.Storage then ns.Storage.SaveState(c) end
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
    if data.available ~= true then return "Questie: no instalado (opcional; el addon funciona sin él)" end
    local nQuests, nZone = 0, 0
    if type(data.quests) == "table" then for _ in pairs(data.quests) do nQuests = nQuests + 1 end end
    if type(data.zoneQuests) == "table" and type(data.zoneQuests.list) == "table" then
        nZone = #data.zoneQuests.list
    end
    return string.format("Questie %s: %d misiones exportadas, %d disponibles en la zona",
        data.version or "?", nQuests, nZone)
end
