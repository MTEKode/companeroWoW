-- Core.lua: inicializacion de CompaneroWoWDB, snapshots y comandos /compa, /companero.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Clean = ns.Call, ns.Num, ns.Clean

ns.VERSION = "0.1.0"
ns.SCHEMA = 1

local SNAPSHOT_THROTTLE = 5  -- segundos
local PERIODIC_SNAPSHOT = 60 -- segundos entre snapshots periodicos (dentro del mundo, fuera de combate)
local lastSnapshot = nil     -- tiempo (GetTime) del ultimo snapshot
local timerPending = false   -- hay un snapshot diferido por throttle
local combatPending = false  -- hay un snapshot diferido por combate
local loading = false        -- true entre PLAYER_LEAVING_WORLD / PLAYER_LOGOUT y PLAYER_ENTERING_WORLD
local periodicStarted = false
-- Campo protegido -> hay una lectura vacia pendiente de confirmar (dos lecturas consecutivas).
local emptyPending = {}

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

-- true solo si el jugador esta dentro del mundo y los datos de las APIs son fiables:
-- no hay pantalla de carga/salida (loading) y el cliente no reporta sesion cerrada.
function ns.IsFullyInWorld()
    if loading then return false end
    if Clean(Call("IsLoggedIn")) == false then return false end
    if Clean(Call("UnitExists", "player")) == false then return false end
    return true
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

-- Campos protegidos: si pasan de datos a vacio/cero, no se aceptan sin confirmar.
-- Ruta = { campo, subcampo? }. Para inventory se protege la lista de objetos.
local PROTECTED = {
    { "professions" },
    { "quests" },
    { "inventory", "items" },
    { "equipment" },
    { "spells" },
    { "reputations" },
    { "character", "money" },
}

local function pathKey(path)
    return table.concat(path, ".")
end

local function getPath(t, path)
    local cur = t
    for _, k in ipairs(path) do
        if type(cur) ~= "table" then return nil end
        cur = cur[k]
    end
    return cur
end

-- Escribe en t[path]. Si un nivel intermedio falta, no crea tablas nuevas (no pisa el resto).
local function setPath(t, path, v)
    local cur = t
    for i = 1, #path - 1 do
        cur = cur[path[i]]
        if type(cur) ~= "table" then return end
    end
    cur[path[#path]] = v
end

local function isEmptyValue(v)
    if v == nil then return true end
    if type(v) == "number" then return v == 0 end
    if type(v) == "table" then return next(v) == nil end
    return false
end

-- Decide que valor guardar para un campo protegido. Devuelve el valor y el nuevo estado "pendiente".
--  - Sin lectura nueva: se conserva el anterior.
--  - Vacio/cero sobre dato previo fuera del mundo (carga/salida): se conserva el anterior.
--  - Vacio/cero sobre dato previo dentro del mundo: la primera lectura queda pendiente
--    y solo se acepta si la siguiente lectura consecutiva sigue vacia.
--  - Cualquier otro caso: se acepta el valor nuevo.
local function resolveProtected(oldValue, newValue, fullyIn, wasPending)
    if newValue == nil then return oldValue, false end
    if isEmptyValue(newValue) and not isEmptyValue(oldValue) then
        if not fullyIn then return oldValue, false end
        if not wasPending then return oldValue, true end
        return newValue, false
    end
    return newValue, false
end

-- Aplica resolveProtected a cada campo protegido de fresh frente a c (el ultimo snapshot guardado).
local function mergeProtected(c, fresh, key, fullyIn)
    for _, path in ipairs(PROTECTED) do
        local pk = key .. "/" .. pathKey(path)
        local oldValue = getPath(c, path)
        local newValue = getPath(fresh, path)
        local value, pending = resolveProtected(oldValue, newValue, fullyIn, emptyPending[pk] == true)
        emptyPending[pk] = pending or nil
        if value ~= newValue then setPath(fresh, path, value) end
    end
end

-- Ejecuta todos los collectors y guarda el resultado en characters[CharKey()].
-- Un collector que falla conserva el dato anterior. Fuera del mundo (carga/salida) no se
-- escriben campos vacios sobre datos previos. Devuelve true si se ejecuto.
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
    local fullyIn = ns.IsFullyInWorld()
    local fresh = {}
    for _, entry in ipairs(FIELDS) do
        local value = ns.SafeCall(ns[entry[2]])
        if value ~= nil then fresh[entry[1]] = value end
    end
    mergeProtected(c, fresh, key, fullyIn)
    for _, entry in ipairs(FIELDS) do
        if fresh[entry[1]] ~= nil then c[entry[1]] = fresh[entry[1]] end
    end
    if fullyIn then
        c.updatedAt = ns.Now()
        -- Exportacion de Questie: fuera de combate, con su propio throttle de 10 s (Questie.lua).
        ns.SafeCall(ns.RequestQuestieExport)
        -- Bloque nativo (misiones del juego, pasos activos, PNJ aprendidos): Native.lua, con throttle propio.
        ns.SafeCall(ns.RequestNativeExport)
        -- Exportacion de WhatsTraining / SkillUpForever: mismo criterio (Integrations.lua).
        ns.SafeCall(ns.RequestIntegrationsExport)
    end
    lastSnapshot = nowSec()
    return true
end

-- Snapshot con throttle (maximo uno cada SNAPSHOT_THROTTLE s). Diferido en combate.
-- Fuera del mundo no hace nada: el ultimo snapshot bueno se conserva.
function ns.RequestSnapshot()
    if ns.InCombat() then
        combatPending = true
        return false
    end
    if not ns.IsFullyInWorld() then return false end
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
    "QUEST_LOG_UPDATE", "BAG_UPDATE_DELAYED",
    "PLAYER_EQUIPMENT_CHANGED", "SKILL_LINES_CHANGED", "PLAYER_LEVEL_UP",
    "ZONE_CHANGED_NEW_AREA", "GROUP_ROSTER_UPDATE", "UPDATE_FACTION",
}

-- Marca la hora de salida. Sin collectors: al salir, las APIs pueden devolver vacio/cero.
-- PLAYER_LEAVING_WORLD tambien ocurre en pantallas de carga: solo se actualiza la marca.
local function markLoggedOut()
    local key = ns.CharKey()
    if not key then return end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    c.loggedOutAt = ns.Now()
end

-- Snapshot periodico (cada PERIODIC_SNAPSHOT s): solo dentro del mundo y fuera de combate.
local function startPeriodic()
    if periodicStarted then return end
    periodicStarted = true
    ns.Call("C_Timer.NewTicker", PERIODIC_SNAPSHOT, function()
        ns.SafeCall(function()
            if ns.IsFullyInWorld() and not ns.InCombat() then ns.RequestSnapshot() end
        end)
    end)
end

local function onEvent(event, name)
    if event == "ADDON_LOADED" then
        if name == ADDON_NAME then ns.EnsureDB() end
    elseif event == "PLAYER_LOGIN" then
        ns.EnsureDB()
        ns.InitBridge()
        if ns.Marks then ns.SafeCall(ns.Marks.Init) end
        ns.RequestSnapshot()
    elseif event == "PLAYER_ENTERING_WORLD" then
        loading = false
        startPeriodic()
        ns.RequestSnapshot()
    elseif event == "PLAYER_LEAVING_WORLD" or event == "PLAYER_LOGOUT" then
        loading = true
        markLoggedOut()
    elseif event == "PLAYER_CAMPING" then
        ns.RequestSnapshot() -- ultimo momento seguro antes de desconectar
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
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_ENTERING_WORLD")
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_LEAVING_WORLD")
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_LOGOUT")
    ns.Method(eventFrame, "RegisterEvent", "PLAYER_CAMPING")
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
    say("  /compa sync - guarda los datos (misiones del juego, Questie si está, WhatsTraining, SkillUpForever, equipo) y recarga la interfaz (escribe SavedVariables)")
    say("  /compa strip on|off - activa o desactiva la franja de píxeles")
    say("  /compa estado - muestra el estado actual")
    say("  /compa marca <x> <y> [texto] - marca un punto en tu mapa (x e y de 0 a 100)")
    say("  /compa marca <mapID> <x> <y> [texto] - marca un punto en un mapa concreto")
    say("  /compa marca - marca el siguiente paso de tu misión (con las guías del juego; Questie si está instalado)")
    say("  /compa marca borrar - quita la marca de Compañero")
    say("  /compa sesion - resumen de la sesión actual (XP/h, siguiente nivel, misiones, enemigos, muertes)")
    say("  /compa barras - resumen de tus barras de acción")
    say("  /compa barras aplicar <slot>:<spellID>,... - coloca hechizos conocidos (fuera de combate, con copia de seguridad)")
    say("  /compa barras deshacer - restaura la última copia de seguridad de barras")
    say("  /compa macro lista - cuenta y nombres de tus macros")
    say("  /compa macro crear <nombre>|<icono>|<cuerpo> - crea (o actualiza) una macro de personaje, fuera de combate")
    say("  /compa macro borrar <nombre> | deshacer | barra <nombre> <slot> - borra, deshace o pone una macro en una ranura")
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
    if ns.Native and ns.Native.SourcesStatusLine then say(ns.Native.SourcesStatusLine()) end
    if ns.GearStatusLines then
        for _, line in ipairs(ns.GearStatusLines()) do say(line) end
    end
    if ns.IntegrationsStatusLines then
        for _, line in ipairs(ns.IntegrationsStatusLines()) do say(line) end
    end
    if ns.CombatStatusLines then
        for _, line in ipairs(ns.CombatStatusLines()) do say(line) end
    end
    if ns.BarsStatusLine then say(ns.BarsStatusLine()) end
    if ns.Session then say(ns.Session.StatusLine()) end
    if ns.MacrosStatusLine then say(ns.MacrosStatusLine()) end
    if ns.SheetStatusLine then say(ns.SheetStatusLine()) end
    if ns.HealthStatusLine then say(ns.HealthStatusLine()) end
    if ns.BridgeDebugLines then
        for _, line in ipairs(ns.BridgeDebugLines()) do say(line) end
    end
    if ns.Channel then
        for _, line in ipairs(ns.Channel.StatusLines()) do say(line) end
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
        if ns.ExportNativeNow then ns.ExportNativeNow() end -- misiones del juego (Native.lua)
        if ns.ExportQuestieNow then ns.ExportQuestieNow() end -- sin throttle: accion explicita, antes de recargar
        if ns.ExportIntegrationsNow then ns.ExportIntegrationsNow() end
        if ns.FlushGear then ns.FlushGear() end -- equipo, misiones, candidatos y mazmorras (sin tope de tiempo)
        if ns.ExportBarsNow then ns.ExportBarsNow() end -- barras de accion (actionBars)
        if ns.ExportMacrosNow then ns.ExportMacrosNow() end -- macros de cuenta y personaje (Macros.lua)
        if ns.ExportSheetNow then ns.ExportSheetNow() end -- hoja de personaje y hechizos (Sheet.lua)
        ns.SnapshotAll()
        if ns.Channel then ns.Channel.ForceCarousel() end -- canal en vivo: reenvia todo al cargar la app
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
    elseif cmd == "barras" then
        if ns.Bars then ns.Bars.Command(raw) else say("Barras no disponibles.") end
    elseif cmd == "sesion" then
        if ns.Session then
            for _, line in ipairs(ns.Session.SummaryLines()) do say(line) end
        else
            say("Sesión no disponible.")
        end
    elseif cmd == "macro" then
        if ns.Macros then ns.Macros.Command(raw) else say("Macros no disponibles.") end
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
