-- Share.lua: activacion del envio de datos bajo demanda. Se activa con la accion de teclado
-- "Compañero: compartir datos" (Bindings.xml, por defecto CTRL-SHIFT-SPACE; con el marco en el juego
-- activado, ademas lo abre sin foco: UI.lua) o con /compa enviar, /compa sync (todo, sin recarga).
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

-- Canal de envio: "captura" (capturas de pantalla, Shot.lua; por defecto) o "tira" (franja de
-- pixeles en vivo, Bridge.lua). Se guarda en settings.channel.
Sh.CHANNELS = { captura = true, tira = true }
Sh.DEFAULT_CHANNEL = "captura"

function Sh.Channel()
    local db = CompaneroWoWDB
    local v = type(db) == "table" and type(db.settings) == "table" and db.settings.channel or nil
    if v == "tira" then return "tira" end
    return Sh.DEFAULT_CHANNEL
end

-- /compa canal [tira|captura]: sin argumento, muestra el canal en uso.
function Sh.ChannelCommand(rest)
    rest = string.lower(rest or "")
    if rest == "" then
        say("Canal de envío: " .. Sh.Channel() .. " (/compa canal tira||captura).")
        return
    end
    if not Sh.CHANNELS[rest] then
        say("Uso: /compa canal tira||captura")
        return
    end
    if type(CompaneroWoWDB) == "table" then
        if type(CompaneroWoWDB.settings) ~= "table" then CompaneroWoWDB.settings = {} end
        CompaneroWoWDB.settings.channel = rest
    end
    if rest == "captura" then
        say("Canal de envío: capturas de pantalla. Los datos salen en capturas del juego (Ctrl+V tras la respuesta sigue igual).")
    else
        say("Canal de envío: franja de píxeles en vivo (requiere ver la franja en pantalla).")
    end
end

-- Activa la ventana de envio, o la reactiva (pulsar otra vez la alarga; solo sale lo que cambio).
-- opts.full = true: envia todo (/compa sync). Antes pide una revision de datos (throttle de Core.lua:
-- como mucho cada 120 s, fuera de combate). Devuelve true si la ventana empezo. Con la franja
-- desactivada (/compa strip off) nunca.
-- Canal "captura" (por defecto): Shot.Start hace la captura. Si no se puede (sin Screenshot, bloqueado,
-- en combate sin pregunta), avisa y, salvo en combate, cae a la franja.
-- opts.question = true: viene de una pregunta (sale aunque estemos en combate).
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
    if Sh.Channel() == "captura" and ns.Shot then
        local res, why = ns.Shot.Start(now, opts)
        if res == "started" then
            if fromCommand then
                say(opts and opts.full and "Sincronizando todo en una captura de pantalla (sin recargar la interfaz)."
                    or "Enviando datos en una captura de pantalla.")
            end
            return true
        elseif res == "combat" then
            say("En combate no se envían datos: solo preguntas. Sal de combate para compartir.")
            return false
        elseif res == "busy" then
            say("Ya se está enviando una captura. Espera un momento.")
            return false
        elseif res == "failed" then
            return false
        end
        -- unavailable: se cae a la franja de pixeles.
        say("Captura no disponible (" .. (why or "sin motivo") .. "): se envía por la franja de píxeles. /compa canal tira para usarla siempre.")
    end
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

-- Tecla (Bindings.xml): empieza la ventana de envio (sin pregunta, solo datos). Con el marco en el
-- juego activado (settings.frame), ademas lo abre (sin foco: UI.lua); con el marco desactivado, la
-- tecla no toca el marco.
function Sh.Key()
    if ns.IsFrameOn() and ns.UI then ns.SafeCall(ns.UI.OnShareKey) end
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
    return "Envío: " .. state .. ". Canal: " .. Sh.Channel() .. ". Tecla de compartir: " .. key
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
