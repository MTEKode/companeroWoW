-- Core.lua: inicializacion de CompaneroWoWDB, snapshots y comandos /compa, /companero.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Clean = ns.Call, ns.Num, ns.Clean

ns.VERSION = "0.1.0"
ns.SCHEMA = 2 -- esquema de SavedVariables con bandeja (Storage.lua)

local SNAPSHOT_THROTTLE = 120 -- segundos minimos entre dos snapshots (fuera de combate)
local PERIODIC_SNAPSHOT = 120  -- segundos entre snapshots periodicos (dentro del mundo, fuera de combate)
local lastSnapshot = nil     -- tiempo (GetTime) del ultimo snapshot
local lastHashes = {}        -- [claveDelPersonaje][campo] = hash del ultimo valor escrito en SavedVariables
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

-- Inicializa y migra CompaneroWoWDB sin borrar datos existentes. Storage.Attach migra el esquema 1
-- a la bandeja y enruta los campos de memoria (Storage.lua); despues se asegura la forma minima.
function ns.EnsureDB()
    if type(CompaneroWoWDB) ~= "table" then CompaneroWoWDB = {} end
    local db = CompaneroWoWDB
    if ns.Storage then ns.Storage.Attach() end
    if db.schema == nil then db.schema = ns.SCHEMA end
    if type(db.settings) ~= "table" then db.settings = {} end
    if db.settings.strip == nil then db.settings.strip = true end
    -- Marco en el juego (UI.lua): desactivado por defecto. Quien ya usaba la ventana de la app lo
    -- tiene en false; se activa con /compa marco on.
    if db.settings.frame == nil then db.settings.frame = false end
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

-- Marco en el juego (settings.frame). false: la tecla y /compa enviar/sync no lo abren ni le dan el
-- foco; /compa, /compa ? quedan inactivos (se usa la ventana de la app).
local FRAME_OFF_MSG = "El marco está desactivado: usa la ventana de la app o /compa marco on"

function ns.IsFrameOn()
    local db = CompaneroWoWDB
    return type(db) == "table" and type(db.settings) == "table" and db.settings.frame == true
end

-- Guarda la preferencia (settings.frame). Al apagarla, si el marco esta abierto se cierra.
function ns.SetFrameEnabled(on)
    on = on and true or false
    if type(CompaneroWoWDB) == "table" then
        if type(CompaneroWoWDB.settings) ~= "table" then CompaneroWoWDB.settings = {} end
        CompaneroWoWDB.settings.frame = on
    end
    if not on and ns.UI then ns.SafeCall(ns.UI.Hide) end
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

-- Hash del valor codificado en JSON (nil si no se puede codificar: entonces siempre se escribe).
local function valueHash(v)
    local json = ns.Json and ns.Json.Encode(v)
    if not json then return nil end
    return ns.HashText(json)
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
    local hashes = lastHashes[key] or {}
    lastHashes[key] = hashes
    for _, entry in ipairs(FIELDS) do
        local name, v = entry[1], fresh[entry[1]]
        if v ~= nil then
            -- Campo sin cambios desde la ultima escritura (mismo hash): no se reescribe en SavedVariables.
            local h = valueHash(v)
            if not (h and h == hashes[name] and c[name] ~= nil) then
                c[name] = v
                hashes[name] = h
            end
        end
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
    -- Estado actual (characters[k].state): la ultima copia de cada bloque, sobrescrita (Storage.lua).
    if ns.Storage then ns.Storage.SaveState(c) end
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
    say("Comandos (con el marco activado, /compa sin texto lo abre; /compa ? <pregunta> pregunta directamente):")
    say("  /compa marco on|off - marco en el juego (por defecto off: se usa la ventana de la app; la tecla solo comparte datos)")
    say("  /compa ? <texto> - envía una pregunta a Compañero (requiere el marco activado; la respuesta llega por el portapapeles: pulsa Ctrl+V en el marco)")
    say("  /compa sync - actualiza los datos (misiones del juego, Questie si está, WhatsTraining, SkillUpForever, equipo) y los envía todos durante 20 s, sin recargar")
    say("  /compa sync reload - igual, y además recarga la interfaz (escribe SavedVariables)")
    say("  /compa enviar - comparte los datos que han cambiado durante 20 s (igual que la tecla Compañero: compartir datos)")
    say("  /compa strip on|off - on: compartir con la tecla (por defecto); off: no compartir nunca")
    say("  /compa canal captura|tira - captura: datos en capturas de pantalla (por defecto); tira: franja de píxeles en vivo")
    say("  /compa estado - muestra el estado actual (datos guardados y bandeja)")
    say("  /compa limpiar - vacía la bandeja de envío (pide confirmación: /compa limpiar si)")
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
    if ns.Share then say(ns.Share.StatusLine()) end
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
    if ns.Storage then say(ns.Storage.StatusLine()) end
    say("Compartir datos: " .. (ns.IsStripOn() and "activado" or "desactivado (/compa strip on)"))
    say("Marco en el juego: " .. (ns.IsFrameOn() and "activado (/compa marco off para desactivarlo)" or "desactivado (/compa marco on para activarlo)"))
    if ns.Share then say("Canal de envío: " .. ns.Share.Channel()) end
    if ns.Shot then
        for _, line in ipairs(ns.Shot.StatusLines()) do say(line) end
    end
    if ns.Ask then say(ns.Ask.StatusLine()) end
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
    if ns.WorldStatusLine then say(ns.WorldStatusLine()) end
    if ns.HealthStatusLine then say(ns.HealthStatusLine()) end
    if ns.BridgeDebugLines then
        for _, line in ipairs(ns.BridgeDebugLines()) do say(line) end
    end
    if ns.Channel then
        for _, line in ipairs(ns.Channel.StatusLines()) do say(line) end
    end
end

-- Abre el marco y pone el cursor en la pregunta (/compa sin texto).
local function openFrame()
    if not ns.UI then return showHelp() end
    if ns.UI.Open() then
        ns.UI.FocusQuestion()
    else
        say("No se puede abrir el marco en este cliente.")
    end
end

local function handleSlash(msg)
    local question = string.match(msg or "", "^%s*%?%s*(.-)%s*$")
    if question then
        -- /compa ? <texto>: pregunta directa (el texto conserva sus mayusculas). Solo con el marco activado.
        if not ns.IsFrameOn() then
            say(FRAME_OFF_MSG)
        elseif ns.Ask then
            ns.Ask.Send(question)
        else
            say("Preguntas no disponibles.")
        end
        return
    end
    local cmd, rest = string.match(msg or "", "^%s*(%S*)%s*(.-)%s*$")
    rest = rest or ""
    local raw = rest -- con mayusculas: los textos de marca las conservan
    cmd = string.lower(cmd or "")
    rest = string.lower(rest)
    if cmd == "" then
        if ns.IsFrameOn() then openFrame() else showHelp() end
    elseif cmd == "marco" then
        if rest == "on" then
            ns.SetFrameEnabled(true)
            say("Marco en el juego: activado. /compa marco off para desactivarlo.")
            openFrame() -- abierto a mano: aqui si pone el cursor en la pregunta
        elseif rest == "off" then
            ns.SetFrameEnabled(false)
            say("Marco en el juego: desactivado. Usa la ventana de la app; la tecla solo comparte datos.")
        elseif rest == "" then
            say("Marco en el juego: " .. (ns.IsFrameOn() and "activado" or "desactivado") .. " (/compa marco on|off).")
        else
            say("Uso: /compa marco on|off")
        end
    elseif cmd == "ayuda" then
        showHelp()
    elseif cmd == "sync" then
        if ns.InCombat() then
            say("No se puede sincronizar en combate. Inténtalo al salir de combate.")
            return
        end
        if ns.ExportNativeNow then ns.ExportNativeNow() end -- misiones del juego (Native.lua)
        if ns.ExportQuestieNow then ns.ExportQuestieNow() end -- sin throttle: accion explicita
        if ns.ExportIntegrationsNow then ns.ExportIntegrationsNow() end
        if ns.FlushGear then ns.FlushGear() end -- equipo, misiones, candidatos y mazmorras (sin tope de tiempo)
        if ns.ExportBarsNow then ns.ExportBarsNow() end -- barras de accion (actionBars)
        if ns.ExportMacrosNow then ns.ExportMacrosNow() end -- macros de cuenta y personaje (Macros.lua)
        if ns.ExportSheetNow then ns.ExportSheetNow() end -- hoja de personaje y hechizos (Sheet.lua)
        ns.SnapshotAll()
        if rest == "reload" then
            say("Datos guardados. Recargando la interfaz...")
            ns.Call("ReloadUI")
        elseif ns.Share then
            -- Sin recarga: todo el personaje durante la ventana de envio (sin saltarse lo que no cambio).
            ns.Share.Activate(true, { full = true })
        else
            say("Compartir no disponible.")
        end
    elseif cmd == "enviar" then
        if ns.Share then ns.Share.Command() else say("Compartir no disponible.") end
    elseif cmd == "marca" then
        if ns.Marks then ns.Marks.Command(raw) else say("Marcas no disponibles.") end
    elseif cmd == "canal" then
        if ns.Share then ns.Share.ChannelCommand(rest) else say("Canal no disponible.") end
    elseif cmd == "strip" then
        if rest == "on" then
            ns.SetStripEnabled(true)
            say("Compartir datos: activado (con la tecla o /compa enviar).")
        elseif rest == "off" then
            ns.SetStripEnabled(false)
            say("Compartir datos: desactivado. No se enviará nada, tampoco con la tecla.")
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
    elseif cmd == "limpiar" then
        if not ns.Storage then
            say("Bandeja no disponible.")
        elseif rest ~= "si" then
            local n, bytes = ns.Storage.Stats()
            say(string.format("Esto vacía la bandeja de envío (%d elementos, %.1f KB) sin enviarlos. Para confirmar: /compa limpiar si", n, bytes / 1024))
        else
            local n = ns.Storage.Clear()
            say(string.format("Bandeja vaciada: %d elemento(s) eliminado(s).", n))
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
