-- test_session.lua: pruebas offline de Session.lua (historial de sesion, contrato ON-DEMAND) y del
-- carrusel de Channel.lua con reloj simulado. Cubre: XP entre subidas de nivel, oro, reparto de tiempo,
-- corte por AFK (10 min) y por pausa, reanudacion tras /reload, tope de sesiones anteriores, enemigos /
-- muertes / misiones / zonas, muestras de XP cada 60 s, tipo 14 en el canal, resumen y estado, y el
-- carrusel (<= 15 s para los tipos clave) con tamanos realistas. Mide el coste por refresco.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_session.lua
package.path = "tools/lua/?.lua;" .. package.path
local W = require("wowstub")

local failures = 0
local function outln(s) io.write(tostring(s), "\n") end
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

-- Juego simulado (globals de WoW). W.installGameAPI da time/GetTime/C_Timer/InCombatLockdown y UnitLevel
-- (de S.level): por eso las APIs del jugador se instalan DESPUES, en installPlayerAPI.
local P = {}
local function resetPlayer()
    P.level, P.xp, P.max, P.money = 18, 100, 1000, 6000
    P.afk, P.mounted, P.resting = false, false, false
    P.mapID, P.x, P.y = 1429, 0.45, 0.67
end
local function installPlayerAPI()
    W.installGameAPI()
    _G.UnitLevel = function() return P.level end
    _G.UnitXP = function() return P.xp end
    _G.UnitXPMax = function() return P.max end
    _G.GetMoney = function() return P.money end
    _G.UnitIsAFK = function() return P.afk end
    _G.IsMounted = function() return P.mounted end
    _G.IsResting = function() return P.resting end
end
resetPlayer()
installPlayerAPI()

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Channel.lua")
load("CompaneroWoW/Session.lua")
-- Posicion: Collectors.lua no se carga aqui; el mismo contrato (mapID, x, y).
ns.GetPlayerPosition = function() return P.mapID, P.x, P.y end

local S, C, Ses, J = ns.Session, ns.Channel, ns.Session, ns.Json
local KEY = "Tester-Ravencrest"

local function advance(sec)
    W.state.now = W.state.now + sec
    W.state.clock = W.state.clock + sec
end
local function tick() return Ses.Tick() end
-- Tiempo activo: cada 5 s el personaje se mueve (cuenta como actividad) y hay un tick.
local function active(sec, perTick)
    for _ = 1, sec / 5 do
        P.x = P.x + 0.001
        if perTick then perTick() end
        advance(5)
        tick()
    end
end
-- Tiempo sin actividad (AFK o quieto): hay ticks pero no cambia nada.
local function idle(sec)
    for _ = 1, sec / 5 do
        advance(5)
        tick()
    end
end
local function ses() return CompaneroWoWDB.characters[KEY].session end
local function base() return CompaneroWoWDB.characters[KEY].sessionBase end

-- Nuevo arranque limpio (sin datos guardados) o /reload (solo se olvida la memoria).
local function freshGame()
    W.reset()
    installPlayerAPI()
    CompaneroWoWDB = { settings = {}, characters = {}, knowledge = { quests = {}, npcs = {} } }
    resetPlayer()
    Ses.ResetForTests()
end
local function login()
    Ses.OnEvent("PLAYER_LOGIN")
    tick()
end
local function reload()
    Ses.ResetForTests()
    login()
end

-- ---------------------------------------------------------------------------
print("(a) XP entre subidas de nivel y oro")
freshGame()
P.xp = 900
login()
eq("nivel de inicio", ses().level.start, 18)
eq("sin XP al empezar", ses().xpGained, 0)
P.xp = 980
Ses.OnEvent("PLAYER_XP_UPDATE")
eq("XP +80 dentro del nivel", ses().xpGained, 80)
-- Sube de nivel: lo que faltaba (1000-980=20) + el XP del nuevo nivel (50).
P.level, P.xp, P.max = 19, 50, 1100
Ses.OnEvent("PLAYER_LEVEL_UP", 19)
eq("subida de nivel suma lo que faltaba + XP nuevo", ses().xpGained, 150)
eq("nivel actual 19", ses().level.now, 19)
P.xp = 150
Ses.OnEvent("PLAYER_XP_UPDATE")
eq("XP tras la subida sigue sumando", ses().xpGained, 250)
-- Salto de dos niveles entre lecturas (sin eventos): el intermedio usa UnitXPMax actual (aproximacion).
P.level, P.xp, P.max = 21, 10, 1200
tick()
eq("salto de dos niveles: (1100-150) + 10 + 1*1200", ses().xpGained, 250 + 950 + 10 + 1200)
eq("nivel 21 tras el salto", ses().level.now, 21)
-- Oro.
P.money = 9000
tick()
eq("oro inicial guardado", ses().money.start, 6000)
eq("oro actual", ses().money.now, 9000)
-- Sin cambios no suma XP.
local before = ses().xpGained
tick()
eq("sin cambios de XP no suma", ses().xpGained, before)

-- ---------------------------------------------------------------------------
print("(b) Corte por AFK y pausas (10 min)")
freshGame()
login()
tick()
active(60)
eq("60 s activos", ses().playedSec, 60)
local start1 = ses().startedAt
P.afk = true
idle(300)
eq("AFK 5 min: no corta la sesion", ses().startedAt, start1)
eq("AFK 5 min: el tiempo no cuenta", ses().playedSec, 60)
P.afk = false
active(60)
eq("vuelta tras 5 min: 120 s activos", ses().playedSec, 120)
eq("sin sesiones anteriores todavia", #ses().previous, 0)
P.afk = true
idle(600)
eq("AFK 10 min: el tiempo no cuenta", ses().playedSec, 120)
P.afk = false
local lastActiveWall = base().activeAt
active(5)
eq("vuelta tras 10 min: una sesion anterior", #ses().previous, 1)
local prev1 = ses().previous[1]
eq("anterior: 120 s activos", prev1.playedSec, 120)
eq("anterior: termina en su ultima actividad", prev1.endedAt, lastActiveWall)
eq("anterior: sin fin posterior al AFK", prev1.endedAt < ses().startedAt, true)
check("sesion nueva: empieza despues", ses().startedAt > start1)
eq("sesion nueva: tiempo a cero al empezar", ses().playedSec, 0)

-- Pausa de otro tipo: sin AFK pero sin ningun cambio (quieto) durante 10 min tambien corta.
freshGame()
login()
tick()
active(30)
idle(610)
active(5)
eq("pausa quieta 10 min tambien corta", #ses().previous, 1)

-- ---------------------------------------------------------------------------
print("(c) Reanudacion tras /reload")
freshGame()
login()
tick()
P.xp = 100
active(60, function() P.xp = P.xp + 20 end)
local startR = ses().startedAt
eq("antes del reload: XP 240", ses().xpGained, 240)
eq("antes del reload: 60 s", ses().playedSec, 60)
-- /reload: 2 min despues, con 50 XP ganados durante la recarga.
advance(120)
P.xp = P.xp + 50
reload()
eq("reanuda: misma hora de inicio", ses().startedAt, startR)
eq("reanuda: el XP de la recarga cuenta", ses().xpGained, 290)
eq("reanuda: tiempo jugado intacto", ses().playedSec, 60)
eq("reanuda: sin sesiones anteriores", #ses().previous, 0)
-- /reload tras >= 10 min: nueva sesion y la anterior pasa a la lista.
advance(700)
P.xp = P.xp + 5
reload()
check("gap >= 10 min: nueva sesion", ses().startedAt > startR)
eq("gap >= 10 min: la anterior se guarda", #ses().previous, 1)
eq("gap >= 10 min: XP de la anterior (incluye el de la pausa)", ses().previous[1].xpGained, 295)
eq("gap >= 10 min: nueva sesion a cero", ses().xpGained, 0)

-- ---------------------------------------------------------------------------
print("(d) Tope de sesiones anteriores (5)")
freshGame()
login()
tick()
local starts = {}
for i = 1, 7 do
    active(10)
    starts[i] = ses().startedAt
    idle(610)
    active(5) -- la actividad abre una sesion nueva y cierra la anterior
end
eq("tras 7 sesiones, se guardan 5", #ses().previous, 5)
eq("las 5 mas recientes (la mas antigua se pierde)", ses().previous[1].startedAt, starts[3])
eq("la ultima guardada es la 7", ses().previous[5].startedAt, starts[7])

-- ---------------------------------------------------------------------------
print("(e) Reparto del tiempo: combate, montura, descanso, otro")
freshGame()
login()
tick()
W.state.inCombat = true
active(100)
W.state.inCombat = false
P.mounted = true
active(50)
P.mounted = false
P.resting = true
active(20)
P.resting = false
active(30)
local ts = ses().timeSplit
eq("combate 100 s", ts.combatSec, 100)
eq("montura 50 s", ts.mountedSec, 50)
eq("descanso 20 s", ts.restingSec, 20)
eq("otro 30 s", ts.otherSec, 30)
eq("suma de cubos = tiempo jugado", ts.combatSec + ts.mountedSec + ts.restingSec + ts.otherSec, ses().playedSec)

-- ---------------------------------------------------------------------------
print("(f) Enemigos, muertes, misiones y zonas")
freshGame()
login()
tick()
Ses.OnEvent("PARTY_KILL", "Creature-0-1-0-1-100-0000000001")
Ses.OnEvent("PARTY_KILL", "Creature-0-1-0-1-100-0000000001") -- mismo GUID: no cuenta dos veces
Ses.OnEvent("PARTY_KILL", "Creature-0-1-0-1-100-0000000002")
eq("enemigos: 2 GUID distintos", ses().kills, 2)
Ses.OnEvent("PLAYER_DEAD")
eq("muertes: 1", ses().deaths, 1)
Ses.OnEvent("QUEST_TURNED_IN", 166, 100, 50)
Ses.OnEvent("QUEST_TURNED_IN", 167)
eq("misiones completadas (en orden)", table.concat(ses().questsCompleted, ","), "166,167")
Ses.OnEvent("QUEST_ACCEPTED", 3, 168)
Ses.OnEvent("QUEST_ACCEPTED", 169)
eq("misiones aceptadas (ambas firmas)", table.concat(ses().questsAccepted, ","), "168,169")
eq("zona inicial guardada", ses().zones[1], 1429)
P.mapID = 1436
Ses.OnEvent("ZONE_CHANGED_NEW_AREA")
eq("zona nueva por evento", ses().zones[2], 1436)
P.mapID = 1433
tick()
eq("zona nueva por posicion", ses().zones[3], 1433)
tick()
eq("misma zona no se repite", #ses().zones, 3)

-- ---------------------------------------------------------------------------
print("(g) Muestras de XP: cada 60 s, ultimas 60")
freshGame()
login()
tick()
local t0 = ses().startedAt
for _ = 1, 100 * 60 / 5 do -- 100 min de actividad
    P.xp = P.xp + 1
    P.x = P.x + 0.001
    advance(5)
    tick()
end
eq("se guardan 60 muestras", #ses().xpSamples, 60)
local gapsOk = true
for i = 2, #ses().xpSamples do
    if ses().xpSamples[i].t - ses().xpSamples[i - 1].t ~= 60 then gapsOk = false end
end
check("muestras separadas 60 s", gapsOk)
eq("la primera conservada es la 41 (t0 + 41 min)", ses().xpSamples[1].t, t0 + 41 * 60)
check("XP/h de la ventana positivo", ses().xpPerHour > 0, tostring(ses().xpPerHour))

-- ---------------------------------------------------------
print("(h) Activacion: tipo 14 en el plan y en la cola")
freshGame()
login()
tick()
active(30, function() P.xp = P.xp + 10 end)
C.testing.reset()
C.Begin(W.state.clock)
C.Pump(W.state.clock, 1e9)
local job14
for _, j in ipairs(C.state.sched.jobs) do
    if j.type == 14 then job14 = j end
end
check("activacion: tipo 14 encolado", job14 ~= nil)
check("tipo 14 lleva xpGained", job14 and string.find(job14.str, '"xpGained":', 1, true) ~= nil)
check("listas vacias salen como []", job14 and string.find(job14.str, '"questsAccepted":[]', 1, true) ~= nil)
check("tipo 14 no lleva campos internos", job14 and string.find(job14.str, "sessionBase", 1, true) == nil)
C.Stop()
-- Reposo: la sesion sigue contando, pero no se serializa nada.
local before = C.state.sched and C.state.sched:pending() or 0
active(10, function() P.xp = P.xp + 5 end)
eq("reposo: el canal no serializa la sesion", (C.state.sched and C.state.sched:pending() or 0) - before, 0)
-- ---------------------------------------------------------------------------
print("(i) Resumen (/compa sesion) y estado (/compa estado)")
freshGame()
login()
tick()
P.xp = 300
Ses.OnEvent("PLAYER_XP_UPDATE")
active(120, function() P.xp = P.xp + 5 end)
Ses.OnEvent("PARTY_KILL", "Creature-0-1-0-1-100-0000000009")
Ses.OnEvent("QUEST_TURNED_IN", 166)
local lines = Ses.SummaryLines()
check("resumen: varias lineas", #lines >= 5, tostring(#lines))
check("resumen: en espanol con XP/h", string.find(table.concat(lines, "\n"), "XP/h", 1, true) ~= nil)
check("resumen: siguiente nivel con ETA", string.find(table.concat(lines, "\n"), "Siguiente nivel en", 1, true) ~= nil)
check("resumen: quests y enemigos", string.find(table.concat(lines, "\n"), "Misiones: 1 completadas", 1, true) ~= nil
    and string.find(table.concat(lines, "\n"), "Enemigos: 1.", 1, true) ~= nil)
local status = Ses.StatusLine()
check("estado: una sola linea", string.find(status, "\n", 1, true) == nil and string.find(status, "Sesión:", 1, true) == 1, status)
eq("formato miles", Ses.fmtInt(1234567), "1.234.567")
eq("formato duracion", Ses.fmtDur(3600 + 720), "1 h 12 min")
eq("json: lista vacia con marca", J.Encode(J.Array()), "[]")
eq("json: tabla vacia sin marca", J.Encode({}), "{}")

-- ---------------------------------------------------------------------------
-- ---------------------------------------------------------
print("(j) Activacion con knowledge grande (2000 PNJ): ventana de 20 s, reloj simulado")
local STEP = 0.2

local function bulkList(n, width)
    local out = {}
    for i = 1, n do out[i] = { id = i, name = string.rep("x", width) .. i } end
    return out
end

local function bytesToString(data)
    local b = {}
    for i = 1, #data do b[i] = string.char(data[i]) end
    return table.concat(b)
end

-- Personaje con datos realistas (tamanos aproximados) y npcCount PNJ en knowledge.
local function fillGame(npcCount)
    freshGame()
    login()
    tick()
    local c = {}
    c.character = { name = "Tester", level = 18, money = 6000, class = "WARRIOR" }
    c.reputations = bulkList(30, 40)
    c.talents = { points = 10 }
    c.quests = bulkList(200, 60)
    c.location = { mapID = 1429 }
    c.native = bulkList(40, 50)
    c.equipment = bulkList(16, 30)
    c.gear = bulkList(20, 30)
    c.sheet = { text = string.rep("s", 6000) }
    c.inventory = { items = bulkList(120, 20) }
    c.professions = bulkList(4, 40)
    c.questie = { data = string.rep("q", 30000) }
    c.combats = {}
    CompaneroWoWDB.characters[KEY] = c
    CompaneroWoWDB.knowledge.quests = {}
    for i = 1, 200 do CompaneroWoWDB.knowledge.quests[i] = { questID = i, title = "Conocimiento " .. i } end
    CompaneroWoWDB.knowledge.npcs = {}
    for i = 1, npcCount do
        CompaneroWoWDB.knowledge.npcs[1000 + i] = {
            npcID = 1000 + i, name = "PNJ número " .. i, mapID = 1429, x = 0.25, y = 0.5,
            reaction = "friendly", isQuestGiver = true, firstSeen = 1700000000, lastSeen = 1700000000,
        }
    end
    C.testing.reset()
end

-- Trozos del tipo 9 a medio recibir, entre activaciones (un mensaje puede empezar en una ventana
-- y acabar en la siguiente: el planificador no pierde trozos al volver al reposo).
local partial9 = {}

-- Una activacion completa con reloj simulado (una trama cada STEP). Devuelve medidas.
local function shareRun()
    -- El presupuesto de Channel.Pump mide con debugprofilestop: el del stub esta congelado (msStep = 0),
    -- asi que aqui se usa el reloj real del proceso para medir de verdad.
    _G.debugprofilestop = function() return os.clock() * 1000 end
    C.Begin(W.state.clock)
    local starts, completions, msgs9, snap9 = {}, {}, {}, {}
    local bytes9 = 0
    local sumT, maxT, frames = 0, 0, 0
    local nextSession, elapsed = 0, 0
    while C.IsSharing() and elapsed < 60 do
        advance(STEP)
        elapsed = elapsed + STEP
        frames = frames + 1
        if elapsed >= nextSession then
            P.xp = P.xp + 3
            Ses.Tick()
            nextSession = nextSession + 5
        end
        local now = W.state.clock
        if not C.Update(now) then break end
        local c0 = os.clock()
        C.Pump(now, C.PUMP_BUDGET_MS)
        local dt = os.clock() - c0
        sumT, maxT = sumT + dt, math.max(maxT, dt)
        -- Solo hay un trabajo del tipo 9 en cola: el trozo de tipo 9 es delta u optativo segun ese trabajo.
        local nineOptional = false
        for _, j in ipairs(C.state.sched and C.state.sched.jobs or {}) do
            if j.type == 9 then nineOptional = j.optional end
        end
        local ch = C.NextChunk(860, frames)
        if ch then
            if ch.index == 0 then starts[ch.type] = starts[ch.type] or elapsed end
            if ch.index == ch.count - 1 then completions[ch.type] = completions[ch.type] or elapsed end
            if ch.type == 9 then
                bytes9 = bytes9 + #ch.data
                local p = partial9[ch.id] or { parts = {}, count = ch.count }
                partial9[ch.id] = p
                p.parts[ch.index + 1] = bytesToString(ch.data)
                local whole = true
                for i = 1, p.count do
                    if p.parts[i] == nil then whole = false break end
                end
                if whole then
                    local list = nineOptional and snap9 or msgs9
                    list[#list + 1] = table.concat(p.parts)
                    partial9[ch.id] = nil
                end
            end
        end
    end
    return {
        starts = starts, completions = completions, msgs9 = msgs9, snap9 = snap9, bytes9 = bytes9,
        frames = frames, elapsed = elapsed, avg = sumT / math.max(1, frames), max = maxT,
    }
end

local KEYS = { 1, 12, 2, 4, 14 }
local function checkShare(label, r)
    for _, t in ipairs(KEYS) do
        outln(string.format("  info  %s tipo %2d: primer trozo a %.1f s", label, t, r.starts[t] or -1))
    end
    check(label .. ": tipos clave salen dentro de la ventana", (function()
        for _, t in ipairs(KEYS) do
            if r.starts[t] == nil or r.starts[t] > C.SHARE_SECONDS then return false end
        end
        return true
    end)())
    check(label .. ": tipo 1 (personaje) completo antes que el 2", (r.completions[1] or 99) <= (r.starts[2] or 0) + 1)
    check(label .. ": la ventana no pasa de 20 s", r.elapsed <= C.SHARE_SECONDS + STEP + 1e-9, tostring(r.elapsed))
    outln(string.format("  info  %s: %d tramas, serializacion media %.3f ms, maxima %.3f ms",
        label, r.frames, r.avg * 1000, r.max * 1000))
    check(label .. ": serializacion media <= 1 ms por trama", r.avg <= 0.001, string.format("%.3f ms", r.avg * 1000))
end

-- Sin PNJ: la activacion envia los tipos clave.
fillGame(0)
local base = shareRun()
checkShare("sin PNJ", base)
check("sin PNJ: vuelve al reposo al acabar", not C.IsSharing())

-- 2000 PNJ: el conocimiento llega en varias activaciones (lo que cabe en cada ventana). Cuando
-- una activacion ya no trae deltas, todos los PNJ han llegado al menos una vez.
fillGame(2000)
local seen, runs, firstRun = {}, 0, nil
local lastRun
repeat
    runs = runs + 1
    lastRun = shareRun()
    if runs == 1 then
        firstRun = lastRun
        checkShare("2000 PNJ", lastRun)
        check("2000 PNJ: el conocimiento sale como tipo 9 (delta)", #lastRun.msgs9 >= 1)
    end
    for _, m in ipairs(lastRun.msgs9) do
        for id in string.gmatch(m, '"(%d+)":{') do seen[id] = true end
    end
until #lastRun.msgs9 == 0 or runs >= 12
local missing = 0
for i = 1, 2000 do
    if not seen[tostring(1000 + i)] then missing = missing + 1 end
end
outln(string.format("  info  2000 PNJ: %d activaciones; %d bytes de conocimiento en la primera", runs, firstRun.bytes9))
check("2000 PNJ: todos los PNJ llegan en las activaciones (sin faltantes)", missing == 0, tostring(missing) .. " faltan")
check("2000 PNJ: la primera ventana no basta para todo (se reparte)", runs > 1)
check("2000 PNJ: tras enviarlo todo, la activacion siguiente no reenvia deltas", #lastRun.msgs9 == 0)

-- Cambio de un PNJ entre activaciones: el delta trae solo ese PNJ.
CompaneroWoWDB.knowledge.npcs[1005].name = "PNJ cambiado"
local changed = shareRun()
local deltaText = table.concat(changed.msgs9, "")
check("cambio de un PNJ: hay delta", #changed.msgs9 >= 1)
check("cambio de un PNJ: el delta trae el 1005", string.find(deltaText, '"1005":', 1, true) ~= nil)
check("cambio de un PNJ: el delta no trae el 1500", string.find(deltaText, '"1500":', 1, true) == nil)


-- ---------------------------------------------------------------------------
print("(k) Medicion de coste por refresco (luajit, orientativa)")
do
    local c0 = os.clock()
    for _ = 1, 500 do Ses.Tick() end
    local per = (os.clock() - c0) / 500
    outln(string.format("  info  Session.Tick medio: %.1f us por tick (cada 5 s)", per * 1e6))
    check("coste de Session.Tick < 1 ms", per < 0.001)
end

if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
