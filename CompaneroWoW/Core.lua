-- Core.lua: inicializacion de CompaneroWoWDB, snapshots y comandos /compa, /companero.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num = ns.Call, ns.Num

ns.VERSION = "0.1.0"
ns.SCHEMA = 1

local SNAPSHOT_THROTTLE = 5 -- segundos
local lastSnapshot = nil     -- tiempo (GetTime) del ultimo snapshot
local timerPending = false   -- hay un snapshot diferido por throttle
local combatPending = false  -- hay un snapshot diferido por combate

local function say(msg)
    ns.Call("print", "|cff00ff00CompaneroWoW|r: " .. msg)
end
ns.Say = say -- para Marks.lua y Questie.lua

-- Inicializa y migra CompaneroWoWDB sin borrar datos existentes.
function ns.EnsureDB()
    if type(CompaneroWoWDB) ~= "table" then CompaneroWoWDB = {} end
    local db = CompaneroWoWDB
    if db.schema == nil then db.schema = ns.SCHEMA end -- migracion desde esquema sin version
    if type(db.settings) ~= "table" then db.settings = {} end
    if db.settings.strip == nil then db.settings.strip = true end
    if type(db.characters) ~= "table" then db.characters = {} end
    if type(db.maps) ~= "table" then db.maps = {} end
    if type(db.knowledge) ~= "table" then db.knowledge = {} end
    if type(db.knowledge.quests) ~= "table" then db.knowledge.quests = {} end
    if type(db.knowledge.npcs) ~= "table" then db.knowledge.npcs = {} end
    return db
end

function ns.InCombat()
    return ns.Call("InCombatLockdown") == true
end

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

-- Orden de escritura de los campos del personaje.
local FIELDS = {
    { "character", "CollectCharacter" },
    { "location", "CollectLocation" },
    { "quests", "CollectQuests" },
    { "inventory", "CollectInventory" },
    { "equipment", "CollectEquipment" },
    { "group", "CollectGroup" },
    { "spells", "CollectSpells" },
    { "talents", "CollectTalents" },
    { "professions", "CollectProfessions" },
    { "reputations", "CollectReputations" },
}

-- Ejecuta todos los collectors y guarda el resultado en characters[CharKey()].
-- Un collector que falla conserva el dato anterior. Devuelve true si se ejecuto.
function ns.SnapshotAll()
    ns.EnsureDB()
    if ns.InCombat() then
        combatPending = true
        return false
    end
    local key = ns.CharKey()
    if not key then return false end
    local db = CompaneroWoWDB
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    c.updatedAt = ns.Now()
    for _, entry in ipairs(FIELDS) do
        local collect = ns[entry[2]]
        local value = ns.SafeCall(collect)
        if value ~= nil then c[entry[1]] = value end
    end
    -- Exportacion de Questie: fuera de combate, con su propio throttle de 10 s (Questie.lua).
    ns.SafeCall(ns.RequestQuestieExport)
    -- Exportacion de WhatsTraining / RXPGuides / SkillUpForever: mismo criterio (Integrations.lua).
    ns.SafeCall(ns.RequestIntegrationsExport)
    lastSnapshot = nowSec()
    return true
end

-- Snapshot con throttle (maximo uno cada SNAPSHOT_THROTTLE s). Diferido en combate.
function ns.RequestSnapshot()
    if ns.InCombat() then
        combatPending = true
        return false
    end
    local t = nowSec()
    if lastSnapshot == nil or t - lastSnapshot >= SNAPSHOT_THROTTLE then
        return ns.SnapshotAll()
    end
    if not timerPending and ns.Get("C_Timer.After") then
        timerPending = true
        local wait = SNAPSHOT_THROTTLE - (t - lastSnapshot)
        ns.Call("C_Timer.After", wait, function()
            timerPending = false
            ns.RequestSnapshot()
        end)
    end
    return false
end

-- ---------------------------------------------------------------------------
-- Eventos de snapshot
-- ---------------------------------------------------------------------------

local SNAPSHOT_EVENTS = {
    "PLAYER_ENTERING_WORLD", "QUEST_LOG_UPDATE", "BAG_UPDATE_DELAYED",
    "PLAYER_EQUIPMENT_CHANGED", "SKILL_LINES_CHANGED", "PLAYER_LEVEL_UP",
    "ZONE_CHANGED_NEW_AREA", "GROUP_ROSTER_UPDATE", "UPDATE_FACTION",
}

local function onEvent(event, name)
    if event == "ADDON_LOADED" then
        if name == ADDON_NAME then ns.EnsureDB() end
    elseif event == "PLAYER_LOGIN" then
        ns.EnsureDB()
        ns.InitBridge()
        if ns.Marks then ns.SafeCall(ns.Marks.Init) end
        ns.RequestSnapshot()
    elseif event == "PLAYER_LOGOUT" then
        ns.SnapshotAll()
    elseif event == "PLAYER_REGEN_ENABLED" then
        if combatPending then
            combatPending = false
            ns.RequestSnapshot()
        end
    else
        ns.RequestSnapshot()
    end
end

local eventFrame = ns.NewFrame("Frame")
if eventFrame then
    ns.Method(eventFrame, "RegisterEvent", "ADDON_LOADED")
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_LOGIN")
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_LOGOUT")
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_REGEN_ENABLED")
    for _, ev in ipairs(SNAPSHOT_EVENTS) do
        ns.Method(eventFrame, "RegisterEvent", ev)
    end
    ns.Method(eventFrame, "SetScript", "OnEvent", function(_, event, arg1)
        ns.SafeCall(onEvent, event, arg1)
    end)
end

-- ---------------------------------------------------------------------------
-- Comandos /compa y /companero
-- ---------------------------------------------------------------------------

local function showHelp()
    say("Comandos:")
    say("  /compa sync - guarda los datos (Questie, WhatsTraining, RXP, SkillUpForever) y recarga la interfaz (escribe SavedVariables)")
    say("  /compa strip on|off - activa o desactiva la franja de píxeles")
    say("  /compa estado - muestra el estado actual")
    say("  /compa marca <x> <y> [texto] - marca un punto en tu mapa (x e y de 0 a 100)")
    say("  /compa marca <mapID> <x> <y> [texto] - marca un punto en un mapa concreto")
    say("  /compa marca - marca el siguiente paso de tu misión (con Questie)")
    say("  /compa marca guia - marca el paso actual de la guía de RXP (si lo expone)")
    say("  /compa marca borrar - quita la marca de Compañero")
end

local function showStatus()
    local db = ns.EnsureDB()
    local mapID, x, y = ns.GetPlayerPosition()
    local mapName = (mapID and db.maps[mapID]) or "desconocido"
    local coords = (x and y) and string.format("%.1f, %.1f", x * 100, y * 100) or "?"
    say(string.format("Zona: %s (mapa %s), coordenadas: %s", mapName, mapID and tostring(mapID) or "?", coords))
    local tracked = Num(Call("C_SuperTrack.GetSuperTrackedQuestID"))
    if tracked and tracked > 0 then
        local title = ns.Str(Call("C_QuestLog.GetTitleForQuestID", tracked)) or tostring(tracked)
        say("Misión seguida: " .. title)
    else
        say("Misión seguida: ninguna")
    end
    local nQuests, nNpcs = 0, 0
    for _ in pairs(db.knowledge.quests) do nQuests = nQuests + 1 end
    for _ in pairs(db.knowledge.npcs) do nNpcs = nNpcs + 1 end
    say(string.format("Conocimiento: %d misiones, %d PNJ", nQuests, nNpcs))
    say("Franja de píxeles: " .. (ns.IsStripOn() and "activa" or "inactiva"))
    if ns.QuestieStatusLine then say(ns.QuestieStatusLine()) end
    if ns.IntegrationsStatusLines then
        for _, line in ipairs(ns.IntegrationsStatusLines()) do say(line) end
    end
    if ns.HealthStatusLine then say(ns.HealthStatusLine()) end
    if ns.BridgeDebugLines then
        for _, line in ipairs(ns.BridgeDebugLines()) do say(line) end
    end
end

local function handleSlash(msg)
    local cmd, rest = string.match(msg or "", "^%s*(%S*)%s*(.-)%s*$")
    rest = rest or ""
    local raw = rest -- con mayusculas: los textos de marca las conservan
    cmd = string.lower(cmd or "")
    rest = string.lower(rest)
    if cmd == "sync" then
        if ns.InCombat() then
            say("No se puede sincronizar en combate. Inténtalo al salir de combate.")
            return
        end
        if ns.ExportQuestieNow then ns.ExportQuestieNow() end -- sin throttle: accion explicita, antes de recargar
        if ns.ExportIntegrationsNow then ns.ExportIntegrationsNow() end
        ns.SnapshotAll()
        say("Datos guardados. Recargando la interfaz...")
        ns.Call("ReloadUI")
    elseif cmd == "marca" then
        if ns.Marks then ns.Marks.Command(raw) else say("Marcas no disponibles.") end
    elseif cmd == "strip" then
        if rest == "on" then
            ns.SetStripEnabled(true)
            say("Franja de píxeles activada.")
        elseif rest == "off" then
            ns.SetStripEnabled(false)
            say("Franja de píxeles desactivada.")
        else
            say("Uso: /compa strip on|off")
        end
    elseif cmd == "estado" then
        showStatus()
    else
        showHelp()
    end
end

SLASH_COMPA1 = "/compa"
SLASH_COMPA2 = "/companero"
if type(SlashCmdList) == "table" then
    SlashCmdList["COMPA"] = function(msg)
        ns.SafeCall(handleSlash, msg)
    end
end
