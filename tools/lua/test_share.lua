-- test_share.lua: activacion del envio bajo demanda (T38). Cubre: reposo sin OnUpdate y con la franja
-- oculta; activacion por tecla y por /compa enviar; orden del primer envio; ventana de 20 s y
-- vuelta al reposo; pulsar otra vez alarga la ventana; byte 30 = shareRemainingSec; /compa strip
-- on|off; /compa estado; asignacion de la tecla la primera vez (libre, ocupada, en combate);
-- Bindings.xml y .toc; coste por trama.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_share.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

local failures = 0
local function outln(s) io.write(s, "\n") end
local function check(name, cond, detail)
    if cond then
        outln("  ok    " .. name)
    else
        failures = failures + 1
        outln("  FAIL  " .. name .. (detail and (" -> " .. detail) or ""))
    end
end
local function eq(name, got, want)
    check(name, got == want, string.format("got %s, want %s", tostring(got), tostring(want)))
end

-- ---------------------------------------------------------------------------
-- Entorno: reloj controlado, frames y texturas falsos, API minima
-- ---------------------------------------------------------------------------
local clock = 1000
local inCombat = false
local chat = {}
_G.print = function(...)
    local parts = {}
    for i = 1, select("#", ...) do parts[#parts + 1] = tostring((select(i, ...))) end
    chat[#chat + 1] = table.concat(parts, " ")
end
local function lastChat()
    return chat[#chat] or ""
end
local function chatHas(text)
    for _, m in ipairs(chat) do
        if string.find(m, text, 1, true) then return true end
    end
    return false
end

local function newTexture()
    local t = {}
    return setmetatable(t, { __index = function() return function(self) return self end end })
end

local function newFrame()
    local f = { scripts = {}, shown = false, events = {} }
    function f:SetScript(name, fn) self.scripts[name] = fn end
    function f:Show() self.shown = true end
    function f:Hide() self.shown = false end
    function f:RegisterEvent(e) self.events[e] = true end
    function f:CreateTexture() return newTexture() end
    return setmetatable(f, { __index = function() return function(self) return self end end })
end

_G.GetTime = function() return clock end
_G.time = function() return 1700000000 end
_G.InCombatLockdown = function() return inCombat end
_G.CreateFrame = function() return newFrame() end
_G.UIParent = {
    GetWidth = function() return 1920 end,
    GetHeight = function() return 768 end,
    GetEffectiveScale = function() return 1 end,
}
_G.GetPhysicalScreenSize = function() return 1920, 1080 end
_G.SlashCmdList = {}

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Storage.lua")
load("CompaneroWoW/Collectors.lua")
load("CompaneroWoW/Bridge.lua")
load("CompaneroWoW/Channel.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Share.lua")

local KEY = "Tester-Realm"
ns.CharKey = function() return KEY end
ns.Say = function(msg) chat[#chat + 1] = msg end
local C, Sh = ns.Channel, ns.Share

-- Datos del personaje: lo que envia cada tipo (sin sesion, sin conocimiento).
local function freshDB()
    CompaneroWoWDB = nil
    C.testing.reset() -- canal limpio: sin trozos de una activacion anterior
    ns.EnsureDB()
    CompaneroWoWDB.characters[KEY] = {
        character = { name = "Tester", level = 60, money = 100 },
        quests = { { id = 1 } },
        location = { mapID = 1 },
        equipment = { { slot = 1 } },
        inventory = { items = { { id = 1, n = 5 } } },
        professions = { { name = "Tailoring" } },
        actionBars = { { slot = 1, id = 7 } },
        completedQuests = { 1, 2, 3 },
        macros = { account = { used = 1, max = 120, list = {} } },
        sheet = { level = 60 },
    }
    chat = {}
end

freshDB()
ns.InitBridge()
local F = ns.bridge.frame

-- Registro de los primeros trozos de cada activacion (orden de envio).
local firstOrder = {}
local realNext = C.NextChunk
C.NextChunk = function(N, frameNo)
    local ch = realNext(N, frameNo)
    if ch and ch.index == 0 then firstOrder[#firstOrder + 1] = ch.type end
    return ch
end

-- Ejecuta las tramas de la franja (OnUpdate) durante 'sec' segundos. Devuelve el coste maximo y
-- la media por trama (os.clock) y el instante en que la ventana se cerro (o nil).
local function run(sec, dt)
    local maxT, sumT, n = 0, 0, 0
    local closedAt
    local steps = math.floor(sec / dt + 0.5)
    for _ = 1, steps do
        clock = clock + dt
        local wasSharing = C.IsSharing()
        local onUpdate = F.scripts.OnUpdate
        if onUpdate then
            local c0 = os.clock()
            onUpdate(F, dt)
            local e = os.clock() - c0
            sumT, n = sumT + e, n + 1
            if e > maxT then maxT = e end
        end
        if wasSharing and not C.IsSharing() and not closedAt then closedAt = clock end
    end
    return maxT, (n > 0 and sumT / n or 0), closedAt
end

-- ---------------------------------------------------------------------------
outln("(a) Reposo: sin OnUpdate, franja oculta, nada que serializar")
check("reposo: la franja existe", F ~= nil and ns.bridge.frame == F)
check("reposo: sin OnUpdate registrado", F.scripts.OnUpdate == nil)
check("reposo: franja oculta", F.shown == false)
check("reposo: no hay activacion", not C.IsSharing())
local restMax = run(30, 0.05) -- no hay OnUpdate: no ocurre nada
check("reposo: 30 s sin tramas (no hay OnUpdate)", restMax == 0)
check("reposo: sigue sin OnUpdate y oculta", F.scripts.OnUpdate == nil and F.shown == false)
eq("reposo: el canal no tiene trabajos", C.state.sched and C.state.sched:pending() or 0, 0)
C.Pump(clock, 1)
eq("reposo: Pump no serializa nada", C.state.sched and C.state.sched:pending() or 0, 0)
check("reposo: /compa estado: Reposo", string.find(Sh.StatusLine(), "Reposo", 1, true) ~= nil, Sh.StatusLine())

-- ---------------------------------------------------------------------------
outln("(b) Activacion por tecla y por /compa enviar")
_G.CompaneroWoW_ShareKey()
check("tecla: activa la ventana", C.IsSharing())
check("tecla: registra OnUpdate", F.scripts.OnUpdate ~= nil)
check("tecla: franja visible", F.shown == true)
check("tecla: sin mensaje en el chat", not chatHas("Compartiendo datos"))
run(25, 0.05)
check("tecla: la ventana acaba", not C.IsSharing())
check("tecla: al acabar, OnUpdate fuera", F.scripts.OnUpdate == nil)
check("tecla: al acabar, franja oculta", F.shown == false)

freshDB()
firstOrder = {}
SlashCmdList.COMPA("enviar")
check("/compa enviar: activa la ventana", C.IsSharing())
check("/compa enviar: mensaje de 20 s", chatHas("Compartiendo datos durante 20 s"), lastChat())
check("/compa enviar: franja visible y con OnUpdate", F.shown and F.scripts.OnUpdate ~= nil)
run(25, 0.05)
check("/compa enviar: vuelve al reposo", not C.IsSharing() and F.scripts.OnUpdate == nil and not F.shown)

-- ---------------------------------------------------------------------------
outln("(c) Orden del primer envio")
freshDB()
firstOrder = {}
_G.CompaneroWoW_ShareKey()
run(25, 0.05)
-- Con datos: personaje, hoja, misiones, equipo, bolsas, profesiones, macros, barras,
-- misiones completadas, metadatos y, el ultimo, el pesado (10). Sesion (14) y conocimiento (9) vacios.
eq("orden: 1,12,2,4,3,5,13,8,6,11,10", table.concat(firstOrder, ","), "1,12,2,4,3,5,13,8,6,11,10")

-- ---------------------------------------------------------------------------
outln("(d) Ventana de 20 s, reactivacion y expiracion")
freshDB()
local start = clock
_G.CompaneroWoW_ShareKey()
eq("ventana: 20 s restantes al pulsar", C.RemainingSec(clock), 20)
local _, _, closedAt = run(25, 0.05)
check("ventana: se cierra como mucho a los 20 s", closedAt ~= nil and closedAt - start <= 20 + 1e-6,
    closedAt and string.format("cerrada a %.2f s", closedAt - start) or "no se cerro")

-- Pulsar otra vez (a mitad de la ventana): el tiempo restante vuelve a 20 y la ventana acaba 20 s
-- despues de la ultima pulsacion. Un tipo pesado grande impide que la ventana acabe antes de tiempo
-- (acabaria 5 s despues de enviarlo todo).
freshDB()
CompaneroWoWDB.characters[KEY].gearCandidates = string.rep("g", 40000)
start = clock
_G.CompaneroWoW_ShareKey()
run(3, 0.05)
local left = C.RemainingSec(clock)
check("re-pulsar: al cabo de 3 s quedan menos de 20", left > 0 and left < 20, tostring(left))
local pressedAt = clock
_G.CompaneroWoW_ShareKey()
eq("re-pulsar: vuelve a 20 s", C.RemainingSec(clock), 20)
check("re-pulsar: sigue activa", C.IsSharing())
local _, _, closed2 = run(25, 0.05)
check("re-pulsar: acaba 20 s despues de la ultima pulsacion",
    closed2 ~= nil and math.abs((closed2 - pressedAt) - 20) <= 0.25,
    closed2 and string.format("cerro a %.2f s de la pulsacion", closed2 - pressedAt) or "no cerro")
check("re-pulsar: vuelve al reposo", not C.IsSharing() and F.scripts.OnUpdate == nil)

-- ---------------------------------------------------------------------------
outln("(e) Byte 30 de la franja: shareRemainingSec (freeBagSlots ya no viaja aqui)")
freshDB()
_G.CompaneroWoW_ShareKey()
run(1.0, 0.05)
local p = ns.bridge.payload
check("byte 30 presente (32 bytes)", p ~= nil and #p == 32)
local left30 = C.RemainingSec(clock)
check("byte 30 = segundos restantes (+-1 por el refresco de 0.2 s)",
    p and p[31] >= left30 and p[31] <= left30 + 1, p and (p[31] .. " vs " .. left30))
C.Stop()
ns.StripRest()
check("tras cortar la ventana, la franja vuelve al reposo", F.scripts.OnUpdate == nil and not F.shown)

-- ---------------------------------------------------------------------------
outln("(f) /compa strip on|off")
freshDB()
SlashCmdList.COMPA("strip off")
check("strip off: guardado", CompaneroWoWDB.settings.strip == false)
_G.CompaneroWoW_ShareKey()
check("strip off: la tecla no comparte", not C.IsSharing())
chat = {}
check("strip off: /compa enviar no comparte", Sh.Command() == false)
check("strip off: /compa enviar avisa", chatHas("desactivado"))
SlashCmdList.COMPA("strip on")
check("strip on: vuelve a compartir", CompaneroWoWDB.settings.strip == true)
_G.CompaneroWoW_ShareKey()
check("strip on: la tecla comparte", C.IsSharing())
SlashCmdList.COMPA("strip off")
check("strip off durante una ventana: la corta", not C.IsSharing() and F.scripts.OnUpdate == nil and not F.shown)
SlashCmdList.COMPA("strip on")

-- ---------------------------------------------------------------------------
outln("(g) /compa estado: Reposo / Compartiendo (Ns) y la tecla")
freshDB()
chat = {}
SlashCmdList.COMPA("estado")
check("estado en reposo: Reposo", chatHas("Reposo"))
check("estado: muestra la tecla de compartir", chatHas("Tecla de compartir:"))
_G.CompaneroWoW_ShareKey()
check("estado: 'Compartiendo (N s)' durante la ventana", Sh.StatusLine():find("Compartiendo (", 1, true) ~= nil, Sh.StatusLine())
C.Stop()
ns.StripRest()

-- ---------------------------------------------------------------------------
outln("(h) Primera vez: asignacion de CTRL-SHIFT-SPACE")
local bind = { action = {}, keys = {}, saved = 0, setCalls = {} }
_G.GetBindingAction = function(key) return bind.action[key] or "" end
_G.GetBindingKey = function(action) return bind.keys[action] end
_G.SetBinding = function(key, action)
    bind.setCalls[#bind.setCalls + 1] = { key, action }
    bind.action[key] = action
    bind.keys[action] = key
    return true
end
_G.SaveBindings = function() bind.saved = bind.saved + 1 end
_G.GetCurrentBindingSet = function() return 1 end
local function resetBinding()
    bind.action, bind.keys, bind.saved, bind.setCalls = {}, {}, 0, {}
    CompaneroWoWDB.settings.bindingDone = nil
    chat = {}
end

resetBinding()
eq("libre -> asignada: resultado", Sh.FirstRunBinding(), "bound")
eq("libre -> asignada: SetBinding de la tecla sugerida", bind.setCalls[1] and bind.setCalls[1][1], "CTRL-SHIFT-SPACE")
eq("libre -> asignada: a la accion de compartir", bind.setCalls[1] and bind.setCalls[1][2], "COMPANEROWOW_SHARE")
eq("libre -> asignada: se guarda el conjunto", bind.saved, 1)
check("libre -> asignada: mensaje en espanol", chatHas("asignada"))
check("libre -> asignada: marca hecha", CompaneroWoWDB.settings.bindingDone == true)
eq("libre -> asignada: segunda vez no hace nada", Sh.FirstRunBinding(), "done")
eq("libre -> asignada: sin nuevas llamadas", #bind.setCalls, 1)

resetBinding()
bind.action["CTRL-SHIFT-SPACE"] = "JUMP"
eq("ocupada por otra: resultado", Sh.FirstRunBinding(), "keptOther")
eq("ocupada por otra: no se cambia", #bind.setCalls, 0)
eq("ocupada por otra: la tecla sigue siendo de la otra accion", bind.action["CTRL-SHIFT-SPACE"], "JUMP")
check("ocupada por otra: pide elegir en Teclas > AddOns", chatHas("Teclas › AddOns › Compañero WoW"))

resetBinding()
inCombat = true
eq("en combate: aplazado", Sh.FirstRunBinding(), "deferred")
eq("en combate: no toca la tecla", #bind.setCalls, 0)
check("en combate: no marca como hecho", CompaneroWoWDB.settings.bindingDone == nil)
inCombat = false
eq("fuera de combate: asigna", Sh.FirstRunBinding(), "bound")

resetBinding()
bind.keys["COMPANEROWOW_SHARE"] = "CTRL-B"
bind.action["CTRL-B"] = "COMPANEROWOW_SHARE"
eq("accion ya con otra tecla: se respeta", Sh.FirstRunBinding(), "already")
eq("accion ya con otra tecla: sin SetBinding", #bind.setCalls, 0)

-- ---------------------------------------------------------------------------
outln("(i) Bindings.xml, .toc y nombres")
local function readFile(path)
    local f = io.open(path, "r")
    if not f then return nil end
    local s = f:read("a")
    f:close()
    return s
end
local xml = readFile("CompaneroWoW/Bindings.xml") or ""
check("Bindings.xml: accion COMPANEROWOW_SHARE", string.find(xml, 'name="COMPANEROWOW_SHARE"', 1, true) ~= nil)
check("Bindings.xml: cabecera COMPANEROWOW", string.find(xml, 'header="COMPANEROWOW"', 1, true) ~= nil)
check("Bindings.xml: llama a CompaneroWoW_ShareKey()", string.find(xml, "CompaneroWoW_ShareKey()", 1, true) ~= nil)
local toc = readFile("CompaneroWoW/CompaneroWoW.toc") or ""
check("toc: carga Bindings.xml", string.find(toc, "\nBindings.xml", 1, true) ~= nil)
check("toc: carga Share.lua", string.find(toc, "\nShare.lua", 1, true) ~= nil)
eq("textos: cabecera", _G.BINDING_HEADER_COMPANEROWOW, "Compañero WoW")
eq("textos: nombre de la accion", _G.BINDING_NAME_COMPANEROWOW_SHARE, "Compañero: compartir datos")

-- ---------------------------------------------------------------------------
outln("(j) Coste por trama durante la activacion")
freshDB()
_G.CompaneroWoW_ShareKey()
local maxT, avgT = run(20, 0.05)
outln(string.format("  info  activacion: OnUpdate medio %.3f ms, maximo %.3f ms (reloj de Windows, 1 ms)", avgT * 1000, maxT * 1000))
check("activacion: media por trama <= 1 ms", avgT <= 0.001, string.format("%.3f ms", avgT * 1000))
C.Stop()
ns.StripRest()
check("reposo otra vez: sin OnUpdate", F.scripts.OnUpdate == nil)

if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
