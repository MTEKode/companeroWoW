-- Events.lua: detecta eventos en vivo y los registra en la franja (lastEventCode, eventSeq, eventArg).
-- Codigos segun el CONTRATO COMPARTIDO (byte 24).
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Clean = ns.Call, ns.Num, ns.Clean

local CODE = {
    LEVEL_UP = 1,
    QUEST_OBJECTIVES_COMPLETE = 2,
    QUEST_TURNED_IN = 3,
    ZONE_CHANGED = 4,
    PROFESSION_SKILL_CHANGED = 5,
    QUEST_ACCEPTED = 6,
    PLAYER_DIED = 7,
}

-- Estado de deteccion. La primera lectura de cada fuente es linea base (no emite eventos).
ns.events = ns.events or {}
local E = ns.events
E.completed = E.completed or {}     -- [questID] = true cuando ya se marco como completa
E.questBaselined = E.questBaselined or false
E.profRanks = E.profRanks or nil    -- [skillLineID | nombre] = rango

-- Ejecuta fn tras unos segundos si C_Timer existe; si no, de inmediato.
local function delayed(sec, fn)
    if ns.Get("C_Timer.After") then
        ns.Call("C_Timer.After", sec, function() ns.SafeCall(fn) end)
    else
        ns.SafeCall(fn)
    end
end

local function questDone(qid, info)
    if Clean(Call("C_QuestLog.IsComplete", qid)) == true then return true end
    return type(info) == "table" and Clean(info.isComplete) == true
end

-- Recorre el registro de misiones y emite QUEST_OBJECTIVES_COMPLETE por cada transicion a completa.
local function scanQuestCompletions()
    if not ns.Get("C_QuestLog.GetNumQuestLogEntries") then return end
    local n = Num(Call("C_QuestLog.GetNumQuestLogEntries"))
    if n == nil then return end
    local done = {}
    for i = 1, n do
        local info = Clean(Call("C_QuestLog.GetInfo", i))
        if type(info) == "table" and Clean(info.isHeader) ~= true then
            local qid = Num(info.questID)
            if qid and questDone(qid, info) then done[qid] = true end
        end
    end
    if E.questBaselined then
        for qid in pairs(done) do
            if not E.completed[qid] then ns.RecordEvent(CODE.QUEST_OBJECTIVES_COMPLETE, qid) end
        end
    end
    E.completed = done
    E.questBaselined = true
end

-- QUEST_LOG_UPDATE llega en rafagas: se coalesce a como mucho 2 barridos por segundo.
-- Si cae dentro de la ventana, se deja un barrido diferido (uno solo).
local SCAN_MIN_INTERVAL = 0.5 -- segundos entre barridos
local lastScanAt = nil
local scanPending = false

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function requestQuestScan()
    local t = nowSec()
    if lastScanAt == nil or t - lastScanAt >= SCAN_MIN_INTERVAL then
        lastScanAt = t
        scanQuestCompletions()
        return
    end
    if not scanPending and ns.Get("C_Timer.After") then
        scanPending = true
        ns.Call("C_Timer.After", SCAN_MIN_INTERVAL - (t - lastScanAt), function()
            scanPending = false
            ns.SafeCall(requestQuestScan)
        end)
    end
end

-- QUEST_WATCH_UPDATE(questID): una mision seguida que pasa a completa.
local function onQuestWatch(qid)
    qid = Num(qid)
    if not qid or not E.questBaselined then return end
    if not E.completed[qid] and Clean(Call("C_QuestLog.IsComplete", qid)) == true then
        E.completed[qid] = true
        ns.RecordEvent(CODE.QUEST_OBJECTIVES_COMPLETE, qid)
    end
end

-- QUEST_TURNED_IN(questID, xp, money).
local function onTurnedIn(qid)
    qid = Num(qid)
    if not qid then return end
    E.completed[qid] = nil
    ns.RecordEvent(CODE.QUEST_TURNED_IN, qid)
end

-- QUEST_ACCEPTED(questLogIndex, questID) o QUEST_ACCEPTED(questID).
local function onQuestAccepted(a, b)
    local qid = Num(b) or Num(a)
    if qid then ns.RecordEvent(CODE.QUEST_ACCEPTED, qid) end
end

-- PLAYER_LEVEL_UP(newLevel, ...).
local function onLevelUp(level)
    local lv = Num(level) or Num(Call("UnitLevel", "player"))
    if lv then ns.RecordEvent(CODE.LEVEL_UP, lv) end
end

-- ZONE_CHANGED_NEW_AREA: el nuevo mapID (0 si se desconoce).
local function onZoneChanged()
    local mapID = ns.GetPlayerPosition()
    ns.RecordEvent(CODE.ZONE_CHANGED, mapID or 0)
end

-- Compara rangos de profesion y emite PROFESSION_SKILL_CHANGED por cada cambio.
local function scanProfessions()
    local cur = {}
    for _, p in ipairs(ns.CollectProfessions() or {}) do
        local key = p.skillLineID or p.name
        if key ~= nil then cur[key] = p.rank or 0 end
    end
    if E.profRanks then
        for key, rank in pairs(cur) do
            if E.profRanks[key] ~= rank then
                ns.RecordEvent(CODE.PROFESSION_SKILL_CHANGED, type(key) == "number" and key or 0)
            end
        end
    end
    E.profRanks = cur
end

local handlers = {
    PLAYER_LOGIN = function()
        -- Linea base tras cargar el registro de misiones y las profesiones.
        delayed(2, function()
            scanQuestCompletions()
            scanProfessions()
        end)
    end,
    PLAYER_LEVEL_UP = onLevelUp,
    QUEST_LOG_UPDATE = requestQuestScan,
    QUEST_WATCH_UPDATE = onQuestWatch,
    QUEST_TURNED_IN = onTurnedIn,
    QUEST_ACCEPTED = onQuestAccepted,
    ZONE_CHANGED_NEW_AREA = onZoneChanged,
    SKILL_LINES_CHANGED = scanProfessions,
    CHAT_MSG_SKILL = scanProfessions,
    PLAYER_DEAD = function() ns.RecordEvent(CODE.PLAYER_DIED, 0) end,
}

-- Despacho de eventos (tambien usado por tools/lua/test_integrations.lua).
ns.Events = {
    OnEvent = function(event, ...)
        local h = handlers[event]
        if h then h(...) end
    end,
}

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(ns.Events.OnEvent, event, ...)
    end)
end
