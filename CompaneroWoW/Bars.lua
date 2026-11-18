-- Bars.lua: exporta las barras de accion (actionBars, COMBAT REVIEW + ACTION BARS CONTRACT)
-- y ofrece /compa barras | aplicar | deshacer.
-- Exportacion: fuera de combate, al entrar al mundo, con ACTIONBAR_SLOT_CHANGED (con throttle)
-- y en /compa sync. Nunca se cambia nada sin que el jugador lo escriba.
-- Solo APIs estandar de acciones: GetActionInfo, GetBindingKey, PickupSpell/C_Spell.PickupSpell,
-- PickupItem, PickupMacro, PickupAction, PlaceAction, ClearCursor, GetCursorInfo.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean

local B = {}
ns.Bars = B

local SLOT_MAX = 120          -- slots 1..120 del contrato
local MAX_PAIRS = 40          -- pares por comando
local MAX_CMD_LEN = 255       -- longitud maxima del argumento de un comando
local MAX_BACKUPS = 3
local THROTTLE = 2            -- s entre exportaciones por ACTIONBAR_SLOT_CHANGED
local LOGIN_DELAY = 3         -- s tras entrar al mundo (los datos de acciones tardan un poco)

-- Barras numeradas: 1 principal, 2..5 multibarras. first = primer slot de la barra (12 slots cada una).
-- Bar 1 (ACTIONBUTTONn): slots 1..12; los slots 13..24 son la pagina 2 y usan los mismos nombres.
local BARS = {
    { bar = 1, first = 1,  binding = "ACTIONBUTTON" },
    { bar = 2, first = 61, binding = "MULTIACTIONBAR1BUTTON", frame = "MultiBarBottomLeft" },
    { bar = 3, first = 49, binding = "MULTIACTIONBAR2BUTTON", frame = "MultiBarBottomRight" },
    { bar = 4, first = 25, binding = "MULTIACTIONBAR3BUTTON", frame = "MultiBarRight" },
    { bar = 5, first = 37, binding = "MULTIACTIONBAR4BUTTON", frame = "MultiBarLeft" },
}

local KIND_OF = { spell = "spell", item = "item", macro = "macro" }

local pendingTimer = false   -- hay una exportacion diferida por throttle
local pendingCombat = false  -- hay una exportacion pendiente de salir de combate

local function say(msg)
    if ns.Say then ns.Say(msg) else ns.Call("print", msg) end
end

local function trim(s)
    return (string.gsub(s or "", "^%s*(.-)%s*$", "%1"))
end

-- ---------------------------------------------------------------------------
-- Lectura de barras
-- ---------------------------------------------------------------------------

-- Nombre del binding de un slot, o nil si el slot no tiene binding estandar.
local function bindingFor(slot)
    for _, b in ipairs(BARS) do
        if slot >= b.first and slot < b.first + 12 then
            return b.binding .. tostring(slot - b.first + 1)
        end
    end
    if slot >= 13 and slot <= 24 then return "ACTIONBUTTON" .. tostring(slot - 12) end
    return nil
end

-- Entrada de un slot: { kind, id, name } o nil si esta vacio.
local function slotInfo(slot)
    local t, id = ns.Call("GetActionInfo", slot)
    t = Str(t)
    if t == nil then return nil end
    id = Num(id)
    local kind = KIND_OF[t] or "other"
    local name
    if kind == "spell" and id then
        name = Str(Call("C_Spell.GetSpellName", id))
    elseif kind == "item" and id then
        name = Str(Call("C_Item.GetItemNameByID", id)) or Str(Call("GetItemInfo", id))
    elseif kind == "macro" and id then
        name = Str(Call("GetMacroInfo", id))
    end
    return { kind = kind, id = id, name = name }
end

local function visibleBars()
    local out = { 1 }
    for _, b in ipairs(BARS) do
        if b.frame then
            local f = _G[b.frame]
            if type(f) == "table" and Clean(ns.Method(f, "IsShown")) == true then
                out[#out + 1] = b.bar
            end
        end
    end
    return out
end

-- Bloque actionBars completo (sin escribir). Fuera de combate.
function B.Collect()
    local slots = {}
    for s = 1, SLOT_MAX do
        local info = slotInfo(s)
        if info then
            local bind = bindingFor(s)
            local key = bind and Str(Call("GetBindingKey", bind)) or nil
            slots[s] = { slot = s, kind = info.kind, id = info.id, name = info.name, key = key }
        end
    end
    return { exportedAt = ns.Now(), slots = slots, visibleBars = visibleBars() }
end

-- ---------------------------------------------------------------------------
-- Exportacion
-- ---------------------------------------------------------------------------

local function available()
    return ns.Get("GetActionInfo") ~= nil and ns.Get("GetBindingKey") ~= nil
end

-- Escribe actionBars del personaje. Un bloque no disponible no sustituye a uno bueno.
local function writeBlock(block)
    local key = ns.CharKey()
    if not key then return false end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    -- Un fallo ({available=false}) nunca sustituye a un bloque bueno; un bloque correcto no lleva "available".
    local old = c.actionBars
    if block.available == false and type(old) == "table" and old.available ~= false then return true end
    c.actionBars = block
    return true
end

-- Exporta ahora. false si esta en combate (queda pendiente) o no hay API.
function B.ExportNow()
    if ns.InCombat() then
        pendingCombat = true
        return false
    end
    if ns.IsFullyInWorld and not ns.IsFullyInWorld() then return false end
    if not available() then
        return writeBlock({ available = false, reason = "API de acciones no disponible" })
    end
    local block = B.Collect()
    return writeBlock(block)
end

-- Exportacion con throttle: la primera peticion arma un temporizador; las siguientes se funden en el.
local function requestExport(delay)
    if ns.InCombat() then
        pendingCombat = true
        return
    end
    if pendingTimer then return end
    if not ns.Get("C_Timer.After") then
        ns.SafeCall(B.ExportNow)
        return
    end
    pendingTimer = true
    ns.Call("C_Timer.After", delay, function()
        pendingTimer = false
        ns.SafeCall(B.ExportNow)
    end)
end

-- ---------------------------------------------------------------------------
-- Comandos
-- ---------------------------------------------------------------------------

local function cursorType()
    return Str((ns.Call("GetCursorInfo")))
end

local function canPlace()
    return (ns.Get("C_Spell.PickupSpell") or ns.Get("PickupSpell")) ~= nil
        and ns.Get("PlaceAction") ~= nil
        and ns.Get("ClearCursor") ~= nil
        and ns.Get("GetCursorInfo") ~= nil
end

local function canKnow()
    return ns.Get("C_SpellBook.IsSpellKnown") ~= nil or ns.Get("IsPlayerSpell") ~= nil
end

local function spellKnown(spellID)
    local v = Clean(Call("C_SpellBook.IsSpellKnown", spellID))
    if v == nil then v = Clean(Call("IsPlayerSpell", spellID)) end
    return v == true
end

local function pickSpell(spellID)
    if ns.Get("C_Spell.PickupSpell") then
        ns.Call("C_Spell.PickupSpell", spellID)
    else
        ns.Call("PickupSpell", spellID)
    end
end

local function pickItem(itemID)
    if ns.Get("C_Item.PickupItem") then
        ns.Call("C_Item.PickupItem", itemID)
    else
        ns.Call("PickupItem", itemID)
    end
end

-- Coloca en el slot lo que hay en el cursor (del tipo esperado) y comprueba el resultado.
local function placeFromCursor(slot, kind, id)
    if cursorType() ~= kind then return false end
    ns.Call("PlaceAction", slot)
    local got = slotInfo(slot)
    return got ~= nil and got.kind == kind and (id == nil or got.id == id)
end

-- Registro del personaje (creado si falta). nil si no hay clave.
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

local function joinNums(list)
    local parts = {}
    for i, v in ipairs(list) do parts[i] = tostring(v) end
    return table.concat(parts, ", ")
end

-- /compa barras: resumen del diseño actual.
function B.Summary()
    local block = B.Collect()
    local n = { spell = 0, item = 0, macro = 0, other = 0 }
    local total = 0
    for _, e in pairs(block.slots) do
        n[e.kind] = (n[e.kind] or 0) + 1
        total = total + 1
    end
    say(string.format("Barras: %d huecos ocupados (hechizos %d, objetos %d, macros %d, otros %d). Barras visibles: %s.",
        total, n.spell, n.item, n.macro, n.other, joinNums(block.visibleBars)))
    say("Cambiar: /compa barras aplicar <slot>:<spellID>[,<slot>:<spellID>...] (fuera de combate)")
end

-- Analiza "slot:spellID,..." y devuelve pares validos y omitidos { text, why }.
local function parsePairs(args)
    local pairsOut, skipped, seen, count = {}, {}, {}, 0
    for tok in string.gmatch(args .. ",", "([^,]*),") do
        local t = trim(tok)
        if t ~= "" then
            count = count + 1
            local s, sp = string.match(t, "^(%d+):(%d+)$")
            if not s then
                skipped[#skipped + 1] = { text = t, why = "formato no válido" }
            else
                local slot, spellID = tonumber(s), tonumber(sp)
                if slot < 1 or slot > SLOT_MAX then
                    skipped[#skipped + 1] = { text = t, why = "slot fuera de 1-" .. SLOT_MAX }
                elseif seen[slot] then
                    skipped[#skipped + 1] = { text = t, why = "slot repetido" }
                elseif not spellKnown(spellID) then
                    skipped[#skipped + 1] = { text = t, why = "hechizo no conocido" }
                else
                    seen[slot] = true
                    pairsOut[#pairsOut + 1] = { slot = slot, spellID = spellID }
                end
            end
        end
    end
    return pairsOut, skipped, count
end

local function skippedText(skipped)
    local parts = {}
    for i, s in ipairs(skipped) do parts[i] = s.text .. " (" .. s.why .. ")" end
    return table.concat(parts, "; ")
end

-- Guarda la copia de seguridad de los slots afectados (la mas reciente primero, maximo 3).
local function saveBackup(c, slots)
    local bk = { at = ns.Now(), slots = {} }
    for _, slot in ipairs(slots) do
        local info = slotInfo(slot)
        if info then
            bk.slots[slot] = { kind = info.kind, id = info.id, name = info.name }
        else
            bk.slots[slot] = { kind = "empty" }
        end
    end
    if type(c.actionBarsBackup) ~= "table" then c.actionBarsBackup = {} end
    table.insert(c.actionBarsBackup, 1, bk)
    while #c.actionBarsBackup > MAX_BACKUPS do table.remove(c.actionBarsBackup) end
    return bk
end

-- /compa barras aplicar <slot>:<spellID>[,...]
function B.Apply(args)
    if ns.InCombat() then
        say("No se pueden cambiar las barras en combate. Inténtalo al salir de combate.")
        return
    end
    args = trim(args)
    if args == "" then
        say("Uso: /compa barras aplicar <slot>:<spellID>[,<slot>:<spellID>...] (slots 1-" .. SLOT_MAX .. ")")
        return
    end
    if #args > MAX_CMD_LEN then
        say("El comando tiene más de " .. MAX_CMD_LEN .. " caracteres. Divídelo en varios comandos.")
        return
    end
    if not canPlace() or not canKnow() then
        say("Colocar acciones no está disponible en este cliente. No se ha cambiado nada.")
        return
    end
    local pairsOut, skipped, count = parsePairs(args)
    if count > MAX_PAIRS then
        say(string.format("Máximo %d pares por comando (has puesto %d). No se ha cambiado nada.", MAX_PAIRS, count))
        return
    end
    if #pairsOut == 0 then
        say("Ningún par válido. Omitidos: " .. (skippedText(skipped) ~= "" and skippedText(skipped) or "ninguno") .. ".")
        return
    end
    local c = charRecord()
    if not c then
        say("Sin personaje activo: no se puede guardar la copia de seguridad.")
        return
    end
    local slots = {}
    for _, p in ipairs(pairsOut) do slots[#slots + 1] = p.slot end
    saveBackup(c, slots)

    local placed = 0
    for _, p in ipairs(pairsOut) do
        ns.Call("ClearCursor")
        pickSpell(p.spellID)
        if placeFromCursor(p.slot, "spell", p.spellID) then
            placed = placed + 1
        else
            skipped[#skipped + 1] = { text = p.slot .. ":" .. p.spellID, why = "no se pudo colocar" }
        end
        ns.Call("ClearCursor")
    end
    B.ExportNow()
    local msg = string.format("Barras: colocados %d de %d. Copia de seguridad guardada (deshacer: /compa barras deshacer).",
        placed, #pairsOut)
    say(msg)
    if #skipped > 0 then say("Omitidos: " .. skippedText(skipped) .. ".") end
end

-- Localiza una macro por nombre (el indice puede haber cambiado). nil si no existe.
local function findMacro(e)
    if e.id and Str(Call("GetMacroInfo", e.id)) == e.name then return e.id end
    local g, p = ns.Call("GetNumMacros")
    for i = 1, (Num(g) or 0) do
        if Str(Call("GetMacroInfo", i)) == e.name then return i end
    end
    for i = 121, 120 + (Num(p) or 0) do
        if Str(Call("GetMacroInfo", i)) == e.name then return i end
    end
    return nil
end

-- Restaura un slot desde su copia. true si quedo restaurado.
local function restoreSlot(slot, e)
    ns.Call("ClearCursor")
    if e.kind == "empty" then
        ns.Call("PickupAction", slot)
        ns.Call("ClearCursor")
        return true
    end
    if e.kind == "spell" and e.id then
        pickSpell(e.id)
        local ok = placeFromCursor(slot, "spell", e.id)
        ns.Call("ClearCursor")
        return ok
    end
    if e.kind == "item" and e.id then
        pickItem(e.id)
        local ok = placeFromCursor(slot, "item", e.id)
        ns.Call("ClearCursor")
        return ok
    end
    if e.kind == "macro" then
        local idx = findMacro(e)
        if not idx then return false end
        ns.Call("PickupMacro", idx)
        local ok = placeFromCursor(slot, "macro", nil)
        ns.Call("ClearCursor")
        return ok
    end
    return false
end

-- /compa barras deshacer: restaura la copia mas reciente y la retira de la lista.
function B.Undo()
    if ns.InCombat() then
        say("No se pueden cambiar las barras en combate. Inténtalo al salir de combate.")
        return
    end
    if not canPlace() then
        say("Colocar acciones no está disponible en este cliente. No se ha cambiado nada.")
        return
    end
    local c = charRecord()
    if not c or type(c.actionBarsBackup) ~= "table" or #c.actionBarsBackup == 0 then
        say("No hay copias de seguridad de barras.")
        return
    end
    local bk = table.remove(c.actionBarsBackup, 1)
    local order = {}
    for slot in pairs(bk.slots) do order[#order + 1] = slot end
    table.sort(order)
    local restored, skipped = 0, {}
    for _, slot in ipairs(order) do
        local e = bk.slots[slot]
        if restoreSlot(slot, e) then
            restored = restored + 1
        else
            local what = e.kind == "other" and "tipo no restaurable" or "no se pudo restaurar"
            skipped[#skipped + 1] = { text = tostring(slot), why = what }
        end
    end
    ns.Call("ClearCursor")
    B.ExportNow()
    say(string.format("Barras restauradas: %d de %d slots.", restored, #order))
    if #skipped > 0 then say("No restaurados: " .. skippedText(skipped) .. ".") end
    if #c.actionBarsBackup > 0 then
        say(string.format("Quedan %d copias de seguridad.", #c.actionBarsBackup))
    end
end

-- Punto de entrada de /compa barras (raw = texto tras "barras", con mayusculas).
function B.Command(raw)
    local sub, args = string.match(raw or "", "^%s*(%S*)%s*(.-)%s*$")
    sub = string.lower(sub or "")
    if sub == "" then
        B.Summary()
    elseif sub == "aplicar" then
        B.Apply(args or "")
    elseif sub == "deshacer" then
        B.Undo()
    else
        say("Uso: /compa barras | /compa barras aplicar <slot>:<spellID>[,...] | /compa barras deshacer")
    end
end

-- Copia de seguridad de los slots dados (la misma que usa /compa barras deshacer). false sin personaje.
-- Lo usa Macros.lua al colocar una macro en una ranura.
function B.BackupSlots(slots)
    local c = charRecord()
    if not c then return false end
    saveBackup(c, slots)
    return true
end

-- Tipo y id de lo que hay en un slot ("macro", "spell"... o nil si esta vacio).
function B.SlotKindOf(slot)
    local info = slotInfo(slot)
    return info and info.kind or nil, info and info.id or nil
end

-- Linea para /compa estado: huecos ocupados, barras visibles y copias guardadas.
function B.StatusLine()
    local c = charRecord()
    local bars = c and c.actionBars
    local backups = (c and type(c.actionBarsBackup) == "table") and #c.actionBarsBackup or 0
    if type(bars) ~= "table" or bars.available == false or type(bars.slots) ~= "table" then
        return "Barras de acción: sin exportar (copias de seguridad: " .. backups .. ")"
    end
    local n = 0
    for _ in pairs(bars.slots) do n = n + 1 end
    return string.format("Barras de acción: %d huecos ocupados, barras visibles: %s, copias de seguridad: %d",
        n, joinNums(bars.visibleBars or {}), backups)
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

local handlers = {
    PLAYER_LOGIN = function() requestExport(LOGIN_DELAY) end,
    PLAYER_ENTERING_WORLD = function() requestExport(LOGIN_DELAY) end,
    ACTIONBAR_SLOT_CHANGED = function() requestExport(THROTTLE) end,
    PLAYER_REGEN_ENABLED = function()
        if pendingCombat then
            pendingCombat = false
            requestExport(THROTTLE)
        end
    end,
}

function B.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(B.OnEvent, event, ...)
    end)
end

-- Para pruebas: borra el estado en memoria.
function B.ResetForTests()
    pendingTimer = false
    pendingCombat = false
end

-- Puntos de entrada para Core.lua (/compa sync, /compa estado).
ns.ExportBarsNow = B.ExportNow
ns.BarsStatusLine = B.StatusLine

return B
