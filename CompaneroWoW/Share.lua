-- Share.lua: activacion del envio de datos bajo demanda. Se activa con la accion de teclado
-- "Compañero: compartir datos" (Bindings.xml, por defecto CTRL-SHIFT-SPACE; ademas abre y enfoca el
-- marco, UI.lua) o con /compa enviar, /compa sync (todo, sin recarga).
-- Activar: muestra la franja y sirve la ventana de SHARE_SECONDS (Channel.lua); al acabar vuelve al
-- reposo. Tambien aqui: la asignacion de la tecla la primera vez (fuera de combate, una sola vez).
local ADDON_NAME, ns = ...
ns = ns or {}

local Call = ns.Call

local Sh = {}
ns.Share = Sh

Sh.ACTION = "COMPANEROWOW_SHARE"  -- nombre de la accion (Bindings.xml)
Sh.HEADER = "COMPANEROWOW"        -- cabecera de Teclas > AddOns
Sh.KEY = "CTRL-SHIFT-SPACE"       -- tecla sugerida (se asigna solo si esta libre)

-- Textos de la pantalla de teclas (Bindings.xml usa estos nombres globales).
_G.BINDING_HEADER_COMPANEROWOW = "Compañero WoW"
_G.BINDING_NAME_COMPANEROWOW_SHARE = "Compañero: compartir datos"

local function say(msg)
    if ns.Say then ns.Say(msg) end
end

local function nowSec()
    local t = ns.Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

-- Activa la ventana de envio, o la reactiva (pulsar otra vez la alarga; solo sale lo que cambio).
-- opts.full = true: envia todo (/compa sync). Antes pide una revision de datos (throttle de Core.lua:
-- como mucho cada 120 s, fuera de combate). Devuelve true si la ventana empezo. Con la franja
-- desactivada (/compa strip off) nunca.
function Sh.Activate(fromCommand, opts)
    if not ns.IsStripOn() then
        if fromCommand then
            say("Compartir datos está desactivado. Usa /compa strip on para activarlo.")
        end
        return false
    end
    local C = ns.Channel
    if not C then return false end
    if ns.RequestSnapshot then ns.SafeCall(ns.RequestSnapshot) end
    local now = nowSec()
    C.Begin(now, opts)
    if not ns.StripStart() then
        C.Stop()
        if fromCommand then say("No se puede compartir: no hay franja de píxeles.") end
        return false
    end
    if fromCommand then
        if opts and opts.full then
            say(string.format("Sincronizando todo durante %d s (sin recargar la interfaz).", C.SHARE_SECONDS))
        else
            say(string.format("Compartiendo datos durante %d s.", C.SHARE_SECONDS))
        end
    end
    return true
end

-- Lo llama /compa enviar.
function Sh.Command()
    return Sh.Activate(true)
end

-- Tecla (Bindings.xml): abre o enfoca el marco de Compañero (UI.lua) y empieza la ventana de envio
-- (sin pregunta, solo datos).
function Sh.Key()
    if ns.UI then ns.SafeCall(ns.UI.OnShareKey) end
    return Sh.Activate(false)
end

-- Funcion global que ejecuta la accion de teclado (Bindings.xml).
_G.CompaneroWoW_ShareKey = function()
    ns.SafeCall(Sh.Key)
end

-- Tecla actual de la accion (o nil si no tiene ninguna).
function Sh.CurrentKey()
    return ns.Str(Call("GetBindingKey", Sh.ACTION))
end

-- Primera vez: si la accion no tiene tecla y CTRL-SHIFT-SPACE esta libre, la asigna y guarda el
-- conjunto de teclas. Si la tecla es de otra accion, no se cambia y se pide elegir una.
-- Fuera de combate; una sola vez (settings.bindingDone). Devuelve el resultado:
-- "done" (ya hecho), "deferred" (en combate), "bound", "keptOther" (tecla ocupada), "already".
function Sh.FirstRunBinding()
    local db = CompaneroWoWDB
    if type(db) ~= "table" then return "none" end
    if type(db.settings) ~= "table" then db.settings = {} end
    if db.settings.bindingDone then return "done" end
    if ns.InCombat() then return "deferred" end
    if Sh.CurrentKey() then
        db.settings.bindingDone = true
        return "already"
    end
    local owner = ns.Str(Call("GetBindingAction", Sh.KEY, true))
    if owner == nil or owner == "" then
        local res = Call("SetBinding", Sh.KEY, Sh.ACTION)
        if res == false then return "failed" end -- se reintenta en la siguiente entrada
        local set = ns.Num(Call("GetCurrentBindingSet")) or 1
        Call("SaveBindings", set)
        db.settings.bindingDone = true
        say("Tecla " .. Sh.KEY .. " asignada a «Compañero: compartir datos». Cámbiala en Teclas › AddOns › Compañero WoW.")
        return "bound"
    end
    db.settings.bindingDone = true
    say(Sh.KEY .. " ya está asignada a otra acción. Elige una tecla para «Compañero: compartir datos» en Teclas › AddOns › Compañero WoW.")
    return "keptOther"
end

-- Linea para /compa estado: Reposo o Compartiendo (Ns), y la tecla.
function Sh.StatusLine()
    local left = ns.Channel and ns.Channel.RemainingSec(nowSec()) or 0
    local state = left > 0 and string.format("Compartiendo (%d s)", left) or "Reposo"
    local key = Sh.CurrentKey() or "sin asignar (Teclas › AddOns › Compañero WoW)"
    return "Envío: " .. state .. ". Tecla de compartir: " .. key
end

-- Primera ejecucion: al entrar al juego y al salir de combate (si quedo pendiente).
local frame = ns.NewFrame and ns.NewFrame("Frame")
if frame then
    ns.Method(frame, "RegisterEvent", "PLAYER_LOGIN")
    ns.Method(frame, "RegisterEvent", "PLAYER_REGEN_ENABLED")
    ns.Method(frame, "SetScript", "OnEvent", function()
        ns.SafeCall(Sh.FirstRunBinding)
    end)
end

return Sh
