-- Native.lua: datos de misiones que da el propio juego (APIs de Blizzard), sin Questie.
-- Fuente "juego". Questie y ForeverDungeonJournal son OPCIONALES: si no estan, el addon sigue
-- funcionando con lo que Blizzard expone y con lo que aprende jugando ("aprendido").
--
-- APIs usadas (warcraft.wiki.gg, comprobadas por existencia en tiempo de ejecucion):
--   C_QuestLine.GetAvailableQuestLines(uiMapID)  -> QuestLineInfo[] (questID, questName, x, y, ...)
--   C_QuestLog.GetNextWaypoint(questID)          -> mapID, x, y   (siguiente paso de una mision del registro)
--   C_QuestLog.GetQuestsOnMap(uiMapID)           -> QuestPOIMapInfo[] (questID, x, y, inProgress, ...)
--   C_QuestLog.GetQuestObjectives(questID)       -> objetivos del registro
--   C_QuestLog.IsQuestFlaggedCompleted(questID)  -> completada alguna vez
--   C_QuestLog.GetQuestDifficultyLevel(questID)  -> nivel de la mision
--   GetNumQuestLogChoices / GetQuestLogChoiceInfo / GetNumQuestLogRewards / GetQuestLogRewardInfo
--   C_Map.GetMapInfo(uiMapID)                    -> UiMapDetails (name, parentMapID)
--   GetNumTrainerServices / GetTrainerServiceInfo (TRAINER_SHOW)
--   EJ_GetInstanceByIndex(index, isRaid)         -> Encounter Journal (mazmorras), si existe
-- Coordenadas siempre uiMapID + 0..1. Cualquier fallo -> sin datos, nunca error de Lua.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Get, Num, Str, Clean = ns.Call, ns.Get, ns.Num, ns.Str, ns.Clean

local N = {}
ns.Native = N

N.SOURCE = "juego"
N.MAP_DEPTH = 3          -- mapa actual + 2 padres (zona -> continente -> mundo)
N.MAX_AVAILABLE = 60
N.MAX_POIS = 40
N.MAX_ACTIVE = 35
N.MAX_LEARNED_NPCS = 60
N.EXPORT_THROTTLE = 5    -- segundos entre exportaciones automaticas

local function inUnit(v)
    return type(v) == "number" and v >= 0 and v <= 1
end

-- ---------------------------------------------------------------------------
-- Fuentes: resumen { juego, questie, fdj, aprendido } (booleanos) para cualquier bloque.
-- ---------------------------------------------------------------------------

-- Questie instalado (QuestieDB con las funciones que usa el addon).
function N.QuestieInstalled()
    local loader = _G.QuestieLoader
    return type(loader) == "table" and type(loader.ImportModule) == "function"
end

-- ForeverDungeonJournal cargado.
function N.FdjInstalled()
    local fdj = _G.ForeverDungeonJournal_NS
    return type(fdj) == "table" and type(fdj.DB) == "table"
end

-- set = { juego = true, questie = true, ... } (claves sueltas). Devuelve el resumen con las cuatro claves.
function N.SourceSummary(set)
    set = set or {}
    return {
        juego = set.juego == true,
        questie = set.questie == true,
        fdj = set.fdj == true,
        aprendido = set.aprendido == true,
    }
end

ns.QuestieInstalled = N.QuestieInstalled

-- Linea para /compa estado: que fuentes estan activas ahora.
function N.SourcesStatusLine()
    local db = ns.EnsureDB()
    local k = type(db.knowledge) == "table" and db.knowledge or {}
    local nQuests, nNpcs = 0, 0
    for _ in pairs(type(k.quests) == "table" and k.quests or {}) do nQuests = nQuests + 1 end
    for _ in pairs(type(k.npcs) == "table" and k.npcs or {}) do nNpcs = nNpcs + 1 end
    local juego = Get("C_QuestLine.GetAvailableQuestLines") ~= nil and "activo" or "no disponible"
    local questie = N.QuestieInstalled() and "instalado" or "no instalado (opcional)"
    local fdj = N.FdjInstalled() and "instalado" or "no instalado (opcional)"
    return string.format("Fuentes: juego %s · Questie %s · ForeverDungeonJournal %s · aprendido %d misiones, %d PNJ",
        juego, questie, fdj, nQuests, nNpcs)
end

-- ---------------------------------------------------------------------------
-- Mapas
-- ---------------------------------------------------------------------------

-- uiMapID actual y sus padres (hasta MAP_DEPTH). Lista de ids, el mas especifico primero.
function N.MapChain(mapID)
    local chain, seen = {}, {}
    local cur = Num(mapID)
    for _ = 1, N.MAP_DEPTH do
        if not cur or cur <= 0 or seen[cur] then break end
        seen[cur] = true
        chain[#chain + 1] = cur
        local info = Clean(Call("C_Map.GetMapInfo", cur))
        cur = type(info) == "table" and Num(info.parentMapID) or nil
    end
    return chain
end

-- Nombre de un uiMapID (API, luego el registro de CompaneroWoWDB.maps). nil si no se conoce.
function N.MapName(mapID)
    local info = Clean(Call("C_Map.GetMapInfo", mapID))
    local name = type(info) == "table" and Str(info.name) or nil
    if name then return name end
    local db = type(CompaneroWoWDB) == "table" and CompaneroWoWDB or nil
    if db and type(db.maps) == "table" and db.maps[mapID] then return db.maps[mapID] end
    return nil
end

-- ---------------------------------------------------------------------------
-- Misiones disponibles (quest lines) y su nivel
-- ---------------------------------------------------------------------------

-- Nivel de una mision (C_QuestLog.GetQuestDifficultyLevel). nil si Blizzard no lo sabe todavia.
function N.QuestLevel(qid)
    local lvl = Num(Call("C_QuestLog.GetQuestDifficultyLevel", qid))
    if lvl and lvl > 0 then return lvl end
    return nil
end

-- Misiones que se pueden coger en el mapa y sus padres. Sin duplicados (gana el mapa mas especifico).
-- Entradas { questID, name, mapID, x?, y?, questLineName?, isDaily?, isCampaign?, source = "juego" }.
function N.AvailableQuests(mapID)
    local out, seen = {}, {}
    for _, m in ipairs(N.MapChain(mapID)) do
        local lines = Clean(Call("C_QuestLine.GetAvailableQuestLines", m))
        if type(lines) == "table" then
            for _, l in ipairs(lines) do
                local qid = type(l) == "table" and Num(l.questID) or nil
                if qid and qid > 0 and not seen[qid] and Clean(l.isHidden) ~= true and #out < N.MAX_AVAILABLE then
                    seen[qid] = true
                    local e = {
                        questID = qid,
                        name = Str(l.questName) or Str(Call("C_QuestLog.GetTitleForQuestID", qid)),
                        mapID = m,
                        questLineName = Str(l.questLineName),
                        isDaily = Clean(l.isDaily) == true or nil,
                        isCampaign = Clean(l.isCampaign) == true or nil,
                        source = N.SOURCE,
                    }
                    local x, y = Num(l.x), Num(l.y)
                    if inUnit(x) and inUnit(y) then e.x, e.y = x, y end
                    out[#out + 1] = e
                end
            end
        end
    end
    return out
end

-- ---------------------------------------------------------------------------
-- Registro en vivo: pasos siguientes, puntos de interes y recompensas
-- ---------------------------------------------------------------------------

-- Entradas { questID, info } del registro (sin cabeceras).
function N.LogEntries()
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

-- Siguiente paso de la mision (objetivo o entrega) segun Blizzard: { mapID, x, y } o nil.
function N.Waypoint(qid)
    local m, x, y = Call("C_QuestLog.GetNextWaypoint", qid)
    m, x, y = Num(m), Num(x), Num(y)
    if m and m > 0 and inUnit(x) and inUnit(y) then return { mapID = m, x = x, y = y } end
    return nil
end

-- Puntos de interes de mision en el mapa (areas de objetivo). Entradas { questID, mapID, x, y, inProgress, source }.
function N.QuestPOIs(mapID)
    local out = {}
    local list = Clean(Call("C_QuestLog.GetQuestsOnMap", mapID))
    if type(list) ~= "table" then return out end
    for _, p in ipairs(list) do
        local qid = type(p) == "table" and Num(p.questID) or nil
        local x, y = type(p) == "table" and Num(p.x) or nil, type(p) == "table" and Num(p.y) or nil
        if qid and inUnit(x) and inUnit(y) and #out < N.MAX_POIS then
            out[#out + 1] = {
                questID = qid, mapID = mapID, x = x, y = y,
                inProgress = Clean(p.inProgress) == true, source = N.SOURCE,
            }
        end
    end
    return out
end

-- Recompensas de una mision del registro: { choice = {itemID...}, fixed = {itemID...} } o nil.
-- Solo funciona con la mision en el registro (Blizzard no las da de otras).
function N.Rewards(qid)
    local out = { choice = {}, fixed = {} }
    local nChoice = Num(Call("GetNumQuestLogChoices", qid)) or 0
    for i = 1, math.min(nChoice, 10) do
        local _, _, _, _, _, itemID = Call("GetQuestLogChoiceInfo", i, qid)
        itemID = Num(itemID)
        if itemID and itemID > 0 then out.choice[#out.choice + 1] = itemID end
    end
    local nFixed = Num(Call("GetNumQuestLogRewards", qid)) or 0
    for i = 1, math.min(nFixed, 10) do
        local _, _, _, _, _, itemID = Call("GetQuestLogRewardInfo", i, qid)
        itemID = Num(itemID)
        if itemID and itemID > 0 then out.fixed[#out.fixed + 1] = itemID end
    end
    if #out.choice == 0 and #out.fixed == 0 then return nil end
    return out
end

-- Completada alguna vez (IsQuestFlaggedCompleted).
function N.Completed(qid)
    return Clean(Call("C_QuestLog.IsQuestFlaggedCompleted", qid)) == true
end

-- ---------------------------------------------------------------------------
-- Mazmorras (Encounter Journal de Blizzard), si el juego lo tiene
-- ---------------------------------------------------------------------------

-- Lista { {instanceID, name}, ... } o nil si el Encounter Journal no esta en este cliente.
-- Sin rangos de nivel: el Encounter Journal no los expone.
function N.JournalInstances()
    if not Get("EJ_GetInstanceByIndex") then return nil end
    local out = {}
    for i = 1, 40 do
        local instanceID, name = Call("EJ_GetInstanceByIndex", i, false)
        instanceID = Num(instanceID)
        if not instanceID then break end
        local s = Str(name)
        if s then out[#out + 1] = { instanceID = instanceID, name = s } end
    end
    return out
end

-- ---------------------------------------------------------------------------
-- Exportacion del bloque "native" (misiones disponibles, pasos activos, PNJ aprendidos)
-- ---------------------------------------------------------------------------

-- PNJ aprendidos (entrenadores y vendedores vistos) cerca del mapa del jugador: lista de { id, name, role, mapID, x, y, source = "aprendido" }.
local function learnedNpcs(mapID)
    local db = ns.EnsureDB()
    local npcs = type(db.knowledge) == "table" and db.knowledge.npcs or {}
    local out = {}
    for id, rec in pairs(npcs) do
        if type(rec) == "table" and (rec.isTrainer or rec.isVendor) and rec.mapID and rec.x and rec.y then
            local role = rec.isTrainer and "trainer" or "vendor"
            out[#out + 1] = {
                id = Num(id), name = Str(rec.name), role = role,
                mapID = rec.mapID, x = rec.x, y = rec.y, source = "aprendido",
                availableCount = Num(rec.availableCount),
                services = type(rec.services) == "table" and rec.services or nil,
                _near = rec.mapID == mapID and 0 or 1,
            }
        end
    end
    table.sort(out, function(a, b)
        if a._near ~= b._near then return a._near < b._near end
        return (a.id or 0) < (b.id or 0)
    end)
    local clean = {}
    for i = 1, math.min(#out, N.MAX_LEARNED_NPCS) do
        out[i]._near = nil
        clean[i] = out[i]
    end
    return clean
end

-- Bloque completo. Siempre disponible si la API de misiones existe (aunque no haya datos).
function N.BuildNativeExport()
    if Get("C_QuestLine.GetAvailableQuestLines") == nil then
        return { available = false, reason = "el juego no expone las misiones disponibles" }
    end
    local mapID = ns.GetPlayerPosition()
    local out = {
        available = true, exportedAt = ns.Now(), mapID = mapID,
        sources = N.SourceSummary({ juego = true }),
    }
    out.zoneQuests = { mapID = mapID, list = mapID and N.AvailableQuests(mapID) or {} }
    -- Pasos siguientes de las misiones del registro (el primero de cada una).
    local active, pois = {}, {}
    for _, e in ipairs(N.LogEntries()) do
        if #active < N.MAX_ACTIVE then
            local wp = N.Waypoint(e.questID)
            if wp then
                active[#active + 1] = {
                    questID = e.questID, name = Str(e.info.title), waypoint = wp, source = N.SOURCE,
                }
            end
        end
    end
    if mapID then
        for _, p in ipairs(N.QuestPOIs(mapID)) do
            if p.inProgress then pois[#pois + 1] = p end
        end
    end
    out.active = active
    out.pois = pois
    out.npcs = learnedNpcs(mapID)
    if #out.npcs > 0 then out.sources.aprendido = true end
    return out
end

-- Exporta ahora (sin throttle) a la entrada del personaje. Fuera de combate. Devuelve la tabla o nil.
function ns.ExportNativeNow()
    if ns.InCombat() then return nil end
    if not ns.IsFullyInWorld() then return nil end
    local key = ns.CharKey()
    if not key then return nil end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    local data = ns.SafeCall(N.BuildNativeExport) or { available = false, reason = "error interno" }
    c.native = ns.PreferAvailable(c.native, data)
    N.last = c.native
    return data
end

local lastAt, pending = nil, false
local function nowSec()
    local t = Num(Call("GetTime"))
    return t or ns.Now()
end

-- Exportacion automatica con throttle; en combate se difiere a PLAYER_REGEN_ENABLED.
function ns.RequestNativeExport()
    if ns.InCombat() then return false end
    local t = nowSec()
    if lastAt == nil or t - lastAt >= N.EXPORT_THROTTLE then
        lastAt = t
        return ns.ExportNativeNow() ~= nil
    end
    if not pending and ns.Get("C_Timer.After") then
        pending = true
        ns.Call("C_Timer.After", N.EXPORT_THROTTLE - (t - lastAt), function()
            pending = false
            ns.SafeCall(ns.RequestNativeExport)
        end)
    end
    return false
end

-- ---------------------------------------------------------------------------
-- Lo que se ve jugando: misiones ofrecidas en el mapa (aprendido + juego)
-- ---------------------------------------------------------------------------

-- Anota en el conocimiento las misiones que el juego ofrece ahora en el mapa (con su posicion).
function N.ScanOffered()
    local mapID = ns.GetPlayerPosition()
    if not mapID or not ns.Knowledge or not ns.Knowledge.NoteOffered then return end
    for _, e in ipairs(N.AvailableQuests(mapID)) do
        ns.Knowledge.NoteOffered(e.questID, {
            name = e.name, level = N.QuestLevel(e.questID), mapID = e.mapID, x = e.x, y = e.y,
        })
    end
end

local frame = ns.NewFrame("Frame")
if frame then
    local events = {
        "PLAYER_ENTERING_WORLD", "ZONE_CHANGED", "ZONE_CHANGED_NEW_AREA", "ZONE_CHANGED_INDOORS",
        "QUEST_ACCEPTED", "QUEST_REMOVED", "QUEST_TURNED_IN", "PLAYER_LEVEL_UP", "PLAYER_REGEN_ENABLED",
    }
    for _, ev in ipairs(events) do ns.Method(frame, "RegisterEvent", ev) end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event)
        if event == "PLAYER_REGEN_ENABLED" then
            ns.SafeCall(ns.RequestNativeExport)
            return
        end
        ns.SafeCall(N.ScanOffered)
        ns.SafeCall(ns.RequestNativeExport)
    end)
end

return N
