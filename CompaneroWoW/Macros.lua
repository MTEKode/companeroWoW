-- Macros.lua: exporta las macros del jugador (cuenta y personaje) y ofrece
-- /compa macro lista | crear | parte | borrar | deshacer | barra.
-- Exportacion: al entrar al mundo, con UPDATE_MACROS (con throttle) y en /compa sync. Solo lee.
-- Las macros solo cambian cuando el jugador escribe el comando, fuera de combate, con copia de seguridad.
-- APIs estandar de macros: GetNumMacros, GetMacroInfo, CreateMacro, EditMacro, DeleteMacro, PickupMacro.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean

local M = {}
ns.Macros = M

local NAME_MAX = 16            -- largo maximo del nombre de una macro
local BODY_MAX = 255           -- largo maximo del cuerpo (bytes)
local MAX_BACKUPS = 3
local DEFAULT_ICON = "INV_MISC_QUESTIONMARK"
local ACCOUNT_MAX_DEFAULT = 120
local CHAR_MAX_DEFAULT = 18
local SLOT_MAX = 120           -- ranuras de accion 1..120 (ver Bars.lua)
local THROTTLE = 2             -- s entre exportaciones por UPDATE_MACROS
local LOGIN_DELAY = 3          -- s tras entrar al mundo
local COMBAT_MSG = "No se pueden cambiar las macros en combate. Inténtalo al salir de combate."
local UNAVAILABLE_MSG = "Las macros no están disponibles en este cliente. No se ha cambiado nada."

local pendingTimer = false
local pendingCombat = false
local parts = nil              -- partes recibidas de /compa macro parte (en memoria)

local function say(msg)
    if ns.Say then ns.Say(msg) else ns.Call("print", msg) end
end

local function trim(s)
    return (string.gsub(s or "", "^%s*(.-)%s*$", "%1"))
end

-- Maximos de macros: constantes de la interfaz (120 de cuenta, 18 de personaje por defecto).
local function accountMax()
    return Num(_G.MAX_ACCOUNT_MACROS) or ACCOUNT_MAX_DEFAULT
end

local function characterMax()
    return Num(_G.MAX_CHARACTER_MACROS) or CHAR_MAX_DEFAULT
end

local function canMacro()
    return ns.Get("GetNumMacros") ~= nil and ns.Get("GetMacroInfo") ~= nil
        and ns.Get("CreateMacro") ~= nil and ns.Get("EditMacro") ~= nil and ns.Get("DeleteMacro") ~= nil
end

local function canPlace()
    return ns.Get("PickupMacro") ~= nil and ns.Get("PlaceAction") ~= nil
        and ns.Get("ClearCursor") ~= nil and ns.Get("GetCursorInfo") ~= nil
end

-- Cuenta de macros usadas y maximos: g (cuenta), c (personaje), gmax, cmax.
local function counts()
    local g, c = ns.Call("GetNumMacros")
    return Num(g) or 0, Num(c) or 0, accountMax(), characterMax()
end

-- Macro del indice i: { index, name, icon, body }, o nil si no hay.
local function readMacro(i)
    local name, icon, body = ns.Call("GetMacroInfo", i)
    name = Str(name)
    if not name then return nil end
    if type(icon) ~= "number" and type(icon) ~= "string" then icon = nil end
    return { index = i, name = name, icon = icon, body = Str(body) or "" }
end

-- Macro por nombre: { index, name, icon, body, scope = "cuenta" | "personaje" } o nil.
local function findByName(name)
    local g, c, gmax = counts()
    for i = 1, g do
        local m = readMacro(i)
        if m and m.name == name then m.scope = "cuenta"; return m end
    end
    for i = 1, c do
        local m = readMacro(gmax + i)
        if m and m.name == name then m.scope = "personaje"; return m end
    end
    return nil
end

-- ---------------------------------------------------------------------------
-- Exportacion
-- ---------------------------------------------------------------------------

-- Bloque macros completo (sin escribir).
function M.Collect()
    local g, c, gmax, cmax = counts()
    local block = {
        exportedAt = ns.Now(),
        account = { used = g, max = gmax, list = {} },
        character = { used = c, max = cmax, list = {} },
    }
    for i = 1, g do
        local m = readMacro(i)
        if m then block.account.list[#block.account.list + 1] = m end
    end
    for i = 1, c do
        local m = readMacro(gmax + i)
        if m then block.character.list[#block.character.list + 1] = m end
    end
    return block
end

local function charRecord()
    local key = ns.CharKey()
    if not key then return nil end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    return c
end

-- Escribe macros del personaje. Un fallo ({available=false}) nunca sustituye a un bloque bueno.
local function writeBlock(block)
    local c = charRecord()
    if not c then return false end
    if block.available == false and type(c.macros) == "table" and c.macros.available ~= false then return true end
    c.macros = block
    -- Las macros tambien van al estado guardado: llegan a la app con una recarga, sin depender del canal.
    if ns.Storage then ns.Storage.SaveState(c) end
    return true
end

-- Exporta ahora. false si esta en combate (queda pendiente) o no hay API.
function M.ExportNow()
    if ns.InCombat() then
        pendingCombat = true
        return false
    end
    if ns.IsFullyInWorld and not ns.IsFullyInWorld() then return false end
    if not canMacro() then
        return writeBlock({ available = false, reason = "API de macros no disponible" })
    end
    return writeBlock(M.Collect())
end

local function requestExport(delay)
    if ns.InCombat() then
        pendingCombat = true
        return
    end
    if pendingTimer then return end
    if not ns.Get("C_Timer.After") then
        ns.SafeCall(M.ExportNow)
        return
    end
    pendingTimer = true
    ns.Call("C_Timer.After", delay, function()
        pendingTimer = false
        ns.SafeCall(M.ExportNow)
    end)
end

-- ---------------------------------------------------------------------------
-- Copias de seguridad (las 3 mas recientes, en el personaje)
-- ---------------------------------------------------------------------------

local function saveBackup(c, bk)
    bk.at = ns.Now()
    if type(c.macroBackup) ~= "table" then c.macroBackup = {} end
    table.insert(c.macroBackup, 1, bk)
    while #c.macroBackup > MAX_BACKUPS do table.remove(c.macroBackup) end
end

-- ---------------------------------------------------------------------------
-- Validacion y parseo
-- ---------------------------------------------------------------------------

-- Separadores del cuerpo: "\n" literal (barra invertida y n) o ";;" -> salto de linea.
local function bodyFromText(text)
    local s = string.gsub(text or "", "\\n", "\n")
    s = string.gsub(s, ";;", "\n")
    return trim(s)
end

-- nombre|icono|cuerpo. Con dos campos, nombre|cuerpo (sin icono).
local function splitFields(text)
    local name, rest = string.match(text, "^([^|]*)|(.*)$")
    if not name then return nil end
    local icon, body = string.match(rest, "^([^|]*)|(.*)$")
    if not icon then return name, nil, rest end
    return name, icon, body
end

-- Icono: numero (fileID), nombre de icono o el por defecto.
local function iconFrom(text)
    local t = trim(text or "")
    if t == "" then return DEFAULT_ICON end
    if string.match(t, "^%d+$") then return tonumber(t) end
    t = string.gsub(t, "^[Ii]nterface[\\/][Ii]cons[\\/]", "")
    return t
end

-- Valida una macro. Devuelve { name, icon, body } o nil y el motivo (en castellano).
local function validate(name, icon, body)
    local n = trim(name)
    if n == "" then return nil, "Falta el nombre de la macro." end
    if #n > NAME_MAX then
        return nil, string.format("El nombre «%s» tiene %d caracteres (máximo %d).", n, #n, NAME_MAX)
    end
    local b = bodyFromText(body)
    if b == "" then return nil, "La macro no tiene cuerpo." end
    if #b > BODY_MAX then
        return nil, string.format("El cuerpo tiene %d caracteres (máximo %d). Acórtalo o divídelo.", #b, BODY_MAX)
    end
    return { name = n, icon = iconFrom(icon), body = b }
end

local function scopeLabel(scope)
    return scope == "cuenta" and "de cuenta" or "de este personaje"
end

-- ---------------------------------------------------------------------------
-- Comandos
-- ---------------------------------------------------------------------------

-- /compa macro crear <nombre>|<icono>|<cuerpo>: crea la macro de personaje o la edita (copia antes).
function M.Create(args)
    if ns.InCombat() then
        say(COMBAT_MSG)
        return
    end
    local name, icon, body = splitFields(args or "")
    if not name then
        say("Uso: /compa macro crear <nombre>|<icono>|<cuerpo> (líneas separadas por \\n o ;;)")
        return
    end
    local v, err = validate(name, icon, body)
    if not v then
        say(err .. " No se ha cambiado nada.")
        return
    end
    if not canMacro() then
        say(UNAVAILABLE_MSG)
        return
    end
    local c = charRecord()
    if not c then
        say("Sin personaje activo: no se puede guardar la copia de seguridad.")
        return
    end
    local found = findByName(v.name)
    if found then
        saveBackup(c, { accion = "editar", name = v.name,
            previous = { name = found.name, icon = found.icon, body = found.body, scope = found.scope } })
        ns.Call("EditMacro", found.index, v.name, v.icon, v.body)
        local now = findByName(v.name)
        if not now or now.body ~= v.body then
            say("No se pudo actualizar la macro «" .. v.name .. "». Copia guardada: /compa macro deshacer.")
        else
            say(string.format("Macro «%s» actualizada (%s). Copia de seguridad guardada: /compa macro deshacer.",
                v.name, scopeLabel(found.scope)))
        end
        M.ExportNow()
        return
    end
    local _, cnt, _, cmax = counts()
    if cnt >= cmax then
        say(string.format("Macros de personaje llenas: %d de %d usadas. Borra una con /compa macro borrar <nombre>. No se ha cambiado nada.",
            cnt, cmax))
        return
    end
    ns.Call("CreateMacro", v.name, v.icon, v.body, true)
    if not findByName(v.name) then
        say("No se pudo crear la macro «" .. v.name .. "». No se ha cambiado nada.")
        return
    end
    saveBackup(c, { accion = "crear", name = v.name })
    say(string.format("Macro «%s» creada (de este personaje, %d de %d). Deshacer: /compa macro deshacer.",
        v.name, cnt + 1, cmax))
    M.ExportNow()
end

-- /compa macro parte <n>/<total> <texto>: junta las partes y crea la macro en la ultima.
function M.Part(args)
    if ns.InCombat() then
        say(COMBAT_MSG)
        return
    end
    local n, total, text = string.match(args or "", "^(%d+)/(%d+)%s(.*)$")
    if not n then
        say("Uso: /compa macro parte <n>/<total> <texto>")
        return
    end
    n, total = tonumber(n), tonumber(total)
    if total < 2 or n < 1 or n > total then
        parts = nil
        say("Parte fuera de rango (de 1 a " .. total .. " con total de al menos 2). Empieza de nuevo por la parte 1.")
        return
    end
    if n == 1 then parts = { total = total, texts = {} } end
    if not parts or parts.total ~= total or n ~= #parts.texts + 1 then
        parts = nil
        say("Falta una parte anterior: envía las partes en orden, empezando por /compa macro parte 1/" .. total .. ".")
        return
    end
    parts.texts[n] = text
    if n < total then
        say(string.format("Parte %d de %d recibida. Sigue con la parte %d.", n, total, n + 1))
        return
    end
    local full = table.concat(parts.texts)
    parts = nil
    M.Create(full)
end

-- /compa macro borrar <nombre> (copia antes).
function M.Delete(args)
    if ns.InCombat() then
        say(COMBAT_MSG)
        return
    end
    local name = trim(args)
    if name == "" then
        say("Uso: /compa macro borrar <nombre>")
        return
    end
    if not canMacro() then
        say(UNAVAILABLE_MSG)
        return
    end
    local found = findByName(name)
    if not found then
        say("No encuentro ninguna macro llamada «" .. name .. "». No se ha cambiado nada.")
        return
    end
    local c = charRecord()
    if not c then
        say("Sin personaje activo: no se puede guardar la copia de seguridad.")
        return
    end
    saveBackup(c, { accion = "borrar", name = found.name,
        previous = { name = found.name, icon = found.icon, body = found.body, scope = found.scope } })
    ns.Call("DeleteMacro", found.index)
    if findByName(name) then
        say("No se pudo borrar la macro «" .. name .. "». Copia guardada: /compa macro deshacer.")
    else
        say(string.format("Macro «%s» borrada. Deshacer: /compa macro deshacer.", name))
    end
    M.ExportNow()
end

-- /compa macro deshacer: restaura la copia mas reciente y la retira de la lista.
function M.Undo()
    if ns.InCombat() then
        say(COMBAT_MSG)
        return
    end
    if not canMacro() then
        say(UNAVAILABLE_MSG)
        return
    end
    local c = charRecord()
    if not c or type(c.macroBackup) ~= "table" or #c.macroBackup == 0 then
        say("No hay copias de seguridad de macros.")
        return
    end
    local bk = table.remove(c.macroBackup, 1)
    local prev = bk.previous
    local current = findByName(bk.name)
    if bk.accion == "crear" then
        if current then ns.Call("DeleteMacro", current.index) end
        say(findByName(bk.name) and ("No se pudo retirar «" .. bk.name .. "».")
            or ("Macro «" .. bk.name .. "» retirada (deshecha la creación)."))
    elseif bk.accion == "editar" and current then
        ns.Call("EditMacro", current.index, prev.name, prev.icon, prev.body)
        say("Macro «" .. prev.name .. "» restaurada a su versión anterior.")
    else
        -- borrada (o editada sin rastro): se recrea de personaje si hay hueco.
        local _, cnt, _, cmax = counts()
        if cnt >= cmax then
            say(string.format("No hay hueco para recrear «%s»: macros de personaje %d de %d.", prev.name, cnt, cmax))
        else
            ns.Call("CreateMacro", prev.name, prev.icon, prev.body, prev.scope ~= "cuenta")
            say(findByName(prev.name) and ("Macro «" .. prev.name .. "» recreada.")
                or ("No se pudo recrear «" .. prev.name .. "»."))
        end
    end
    M.ExportNow()
    if #c.macroBackup > 0 then
        say(string.format("Quedan %d copias de seguridad de macros.", #c.macroBackup))
    end
end

-- /compa macro lista: cuantas macros hay y sus nombres.
function M.List()
    local g, c, gmax, cmax = counts()
    say(string.format("Macros: cuenta %d de %d, personaje %d de %d.", g, gmax, c, cmax))
    local names = {}
    for i = 1, g + c do
        local idx = i <= g and i or (gmax + (i - g))
        local m = readMacro(idx)
        if m then names[#names + 1] = m.name end
    end
    if #names > 0 then say("Nombres: " .. table.concat(names, ", ") .. ".") end
end

-- /compa macro barra <nombre> <slot>: pone la macro en una ranura (copia del slot, como /compa barras).
function M.Place(args)
    if ns.InCombat() then
        say(COMBAT_MSG)
        return
    end
    local name, slotText = string.match(trim(args), "^(.-)%s+(%d+)$")
    local slot = tonumber(slotText)
    if not name or name == "" or not slot then
        say("Uso: /compa macro barra <nombre> <slot> (slots 1-" .. SLOT_MAX .. ")")
        return
    end
    if slot < 1 or slot > SLOT_MAX then
        say("El slot debe estar entre 1 y " .. SLOT_MAX .. ". No se ha cambiado nada.")
        return
    end
    if not canPlace() or not canMacro() then
        say(UNAVAILABLE_MSG)
        return
    end
    local found = findByName(name)
    if not found then
        say("No encuentro ninguna macro llamada «" .. name .. "». No se ha cambiado nada.")
        return
    end
    if not ns.Bars or not ns.Bars.BackupSlots or not ns.Bars.BackupSlots({ slot }) then
        say("Sin personaje activo o sin barras: no se puede guardar la copia de seguridad.")
        return
    end
    ns.Call("ClearCursor")
    ns.Call("PickupMacro", found.index)
    ns.Call("PlaceAction", slot)
    ns.Call("ClearCursor")
    local kind = ns.Bars.SlotKindOf and ns.Bars.SlotKindOf(slot)
    if kind == "macro" then
        say(string.format("Macro «%s» colocada en la ranura %d. Deshacer: /compa barras deshacer.", found.name, slot))
    else
        say(string.format("No se pudo colocar «%s» en la ranura %d. Copia guardada: /compa barras deshacer.", found.name, slot))
    end
    M.ExportNow()
end

-- Punto de entrada de /compa macro (raw = texto tras "macro", con mayusculas).
function M.Command(raw)
    local sub, args = string.match(raw or "", "^%s*(%S*)%s?(.*)$")
    sub = string.lower(sub or "")
    if sub == "" then
        say("Uso: /compa macro lista | crear <nombre>|<icono>|<cuerpo> | parte <n>/<total> <texto> | borrar <nombre> | deshacer | barra <nombre> <slot>")
    elseif sub == "crear" then
        M.Create(args or "")
    elseif sub == "parte" then
        M.Part(args or "")
    elseif sub == "borrar" then
        M.Delete(args or "")
    elseif sub == "deshacer" then
        M.Undo()
    elseif sub == "lista" then
        M.List()
    elseif sub == "barra" then
        M.Place(args or "")
    else
        say("Uso: /compa macro lista | crear <nombre>|<icono>|<cuerpo> | parte <n>/<total> <texto> | borrar <nombre> | deshacer | barra <nombre> <slot>")
    end
end

-- Linea para /compa estado: macros exportadas y copias guardadas.
function M.StatusLine()
    local c = charRecord()
    local block = c and c.macros
    local backups = (c and type(c.macroBackup) == "table") and #c.macroBackup or 0
    if type(block) ~= "table" or block.available == false or type(block.character) ~= "table" then
        return "Macros: sin exportar (copias de seguridad: " .. backups .. ")"
    end
    return string.format("Macros: cuenta %d de %d, personaje %d de %d, copias de seguridad: %d",
        block.account.used, block.account.max, block.character.used, block.character.max, backups)
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

local handlers = {
    PLAYER_LOGIN = function() requestExport(LOGIN_DELAY) end,
    PLAYER_ENTERING_WORLD = function() requestExport(LOGIN_DELAY) end,
    UPDATE_MACROS = function() requestExport(THROTTLE) end,
    PLAYER_REGEN_ENABLED = function()
        if pendingCombat then
            pendingCombat = false
            requestExport(THROTTLE)
        end
    end,
}

function M.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(M.OnEvent, event, ...)
    end)
end

-- Para pruebas: borra el estado en memoria.
function M.ResetForTests()
    pendingTimer = false
    pendingCombat = false
    parts = nil
end

-- Puntos de entrada para Core.lua (/compa sync, /compa estado).
ns.ExportMacrosNow = M.ExportNow
ns.MacrosStatusLine = M.StatusLine

return M
