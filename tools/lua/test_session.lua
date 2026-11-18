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

-- ---------------------------------------------------------------------------
print("(h) Canal: tipo 14 encolado y carrusel")
freshGame()
login()
tick()
active(30, function() P.xp = P.xp + 10 end)
C.state.sched = C.NewScheduler()
C.state.carouselTodo = nil
C.state.hash = {}
C.state.lastBuilt = {}
C.state.lastCheck = {}
C.state.dirty = {}
local ok14 = C.testing.refreshType(14, ns.Now())
check("refresco del tipo 14 encola", ok14 == nil and C.state.sched:pending() >= 1)
local job14
for _, j in ipairs(C.state.sched.jobs) do
    if j.type == 14 then job14 = j end
end
check("tipo 14 en la cola", job14 ~= nil)
check("tipo 14 lleva xpGained", job14 and string.find(job14.str, '"xpGained":', 1, true) ~= nil)
check("listas vacias salen como []", job14 and string.find(job14.str, '"questsAccepted":[]', 1, true) ~= nil)
check("tipo 14 no lleva campos internos", job14 and string.find(job14.str, "sessionBase", 1, true) == nil)
-- Carrusel: con todo el resto vacio, el 14 sale en el primer ciclo.
C.state.sched = C.NewScheduler()
C.state.carouselTodo = nil
local seen14 = false
for i = 1, 60 do
    local ch = C.Tick(ns.Now() + i * 0.2, 860)
    if ch and ch.type == 14 and ch.index == 0 then seen14 = true end
end
check("carrusel envia el tipo 14", seen14)

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
print("(j) Carrusel <= 15 s con knowledge grande (2000 PNJ), reloj simulado y tamanos realistas")
local KEYS = { 1, 12, 2, 4, 14 }
local STEP = 0.2
local isKey = {}
for _, t in ipairs(KEYS) do isKey[t] = true end

local function bulkList(n, width)
    local out = {}
    for i = 1, n do out[i] = { id = i, title = "Misión " .. i, text = string.rep("t", width) } end
    return out
end

local function bytesToString(data)
    local b = {}
    for i = 1, #data do b[i] = string.char(data[i]) end
    return table.concat(b)
end

-- Estado del canal limpio para una simulacion nueva.
local function resetChannel()
    C.state.sched = C.NewScheduler()
    C.state.carouselTodo = nil
    C.state.hash = {}
    C.state.lastBuilt = {}
    C.state.lastCheck = {}
    C.state.dirty = {}
    C.state.completedSet = nil
    C.state.know = {
        cache = { quests = {}, npcs = {} }, bytes = 0, scan = nil, scanned = false,
        out = { items = {}, head = 1, set = { quests = {}, npcs = {} } },
        snap = { items = {}, head = 1 }, snapAt = nil,
    }
end

-- Simula simSec s con npcCount PNJ en knowledge (y 200 misiones). Si changeAt, cambia un PNJ a esa
-- hora (s desde el inicio). Devuelve las medidas del canal.
local function simulate(npcCount, simSec, changeAt)
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
    -- Macros al maximo (120 de cuenta y 18 de personaje, cuerpos de 255 bytes): tipo 13, el ultimo en prioridad.
    local function macroList(n, base)
        local out = {}
        for i = 1, n do out[i] = { index = base + i, name = "Macro " .. i, icon = "INV_Misc_QuestionMark", body = string.rep("m", 255) } end
        return out
    end
    c.macros = {
        account = { used = 120, max = 120, list = macroList(120, 0) },
        character = { used = 18, max = 18, list = macroList(18, 120) },
    }
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
    local knowBytes = 0
    for _, kind in ipairs({ "quests", "npcs" }) do
        for _, rec in pairs(CompaneroWoWDB.knowledge[kind]) do knowBytes = knowBytes + #ns.Json.Encode(rec) end
    end
    resetChannel()

    local starts, completions = {}, {}
    local tickSum, tickMax, ticks = 0, 0, 0
    local dts = {}
    local msgs9, partial9, bytes9Win = {}, {}, {}
    local t0 = W.state.clock
    local nextSessionTick = 0
    local changedAt = nil
    for step = 1, simSec / STEP do
        advance(STEP)
        local now = W.state.clock
        local el = now - t0
        if changeAt and not changedAt and el >= changeAt then
            CompaneroWoWDB.knowledge.npcs[1005].name = "PNJ cambiado"
            changedAt = el
        end
        if step * STEP >= nextSessionTick then
            P.xp = P.xp + 3
            P.x = P.x + 0.001
            Ses.Tick()
            nextSessionTick = nextSessionTick + 5
        end
        local c0 = os.clock()
        local ch = C.Tick(now, 860)
        local dt = os.clock() - c0
        tickSum, ticks = tickSum + dt, ticks + 1
        dts[#dts + 1] = dt
        if dt > tickMax then tickMax = dt end
        if ch then
            if ch.index == 0 then
                starts[ch.type] = starts[ch.type] or {}
                table.insert(starts[ch.type], now)
            end
            if ch.index == ch.count - 1 then
                completions[ch.type] = completions[ch.type] or {}
                table.insert(completions[ch.type], now)
            end
            if ch.type == 9 then
                local w = math.floor(el / 15)
                bytes9Win[w] = (bytes9Win[w] or 0) + #ch.data
                local p = partial9[ch.id] or { parts = {} }
                partial9[ch.id] = p
                p.parts[ch.index + 1] = bytesToString(ch.data)
                if ch.index == ch.count - 1 then
                    msgs9[#msgs9 + 1] = { t = el, s = table.concat(p.parts) }
                    partial9[ch.id] = nil
                end
            end
        end
    end
    return {
        starts = starts, completions = completions, msgs9 = msgs9, bytes9Win = bytes9Win,
        knowBytes = knowBytes, changedAt = changedAt, simSec = simSec,
        tickSum = tickSum, tickMax = tickMax, ticks = ticks, dts = dts,
    }
end

-- Percentil p (0..1) de una lista de tiempos.
local function percentile(list, p)
    local copy = {}
    for i, v in ipairs(list) do copy[i] = v end
    table.sort(copy)
    return copy[math.max(1, math.ceil(#copy * p))]
end

local function maxGap(list)
    local m = 0
    for i = 2, #list do
        local g = list[i] - list[i - 1]
        if g > m then m = g end
    end
    return m
end

-- Comprobaciones de cadencia (tipos clave, pesado) y de latencia, sobre una simulacion.
local SLACK = 0.25 -- un tick de 0.2 s y redondeo
local function checkCadence(label, r)
    local keyOk = true
    for _, t in ipairs(KEYS) do
        local list = r.starts[t] or {}
        local g = maxGap(list)
        if #list < 10 or g > 15 + SLACK then keyOk = false end
        outln(string.format("  info  %s tipo %2d: %3d envios, hueco maximo %.1f s", label, t, #list, g))
    end
    check(label .. ": tipos clave (1,12,2,4,14) se reenvian cada <= 15 s", keyOk)
    local worst = 0
    for _, t in ipairs(KEYS) do
        local st, co = r.starts[t] or {}, r.completions[t] or {}
        for i = 1, math.min(#st, #co) do
            if co[i] - st[i] > worst then worst = co[i] - st[i] end
        end
    end
    check(label .. ": tipos clave terminan <= 5 s desde que empiezan", worst <= 5, tostring(worst))
    local heavy = r.starts[10] or {}
    outln(string.format("  info  %s tipo 10 (pesado): %d envios, hueco maximo %.1f s", label, #heavy, maxGap(heavy)))
    check(label .. ": tipo pesado (10) <= 60 s", #heavy >= 2 and maxGap(heavy) <= 60 + SLACK)
end

-- Base: sin PNJ.
local base = simulate(0, 900)
checkCadence("sin PNJ", base)

-- Grande: 2000 PNJ.
local big = simulate(2000, 900, 450)
checkCadence("2000 PNJ", big)
outln(string.format("  info  2000 PNJ: knowledge %d bytes en total", big.knowBytes))

-- Cobertura: cada PNJ llega en algun mensaje del tipo 9 (deltas o volcado).
local all9 = {}
for _, m in ipairs(big.msgs9) do all9[#all9 + 1] = m.s end
local blob = table.concat(all9)
local missing = 0
for i = 1, 2000 do
    if not string.find(blob, '"' .. (1000 + i) .. '":', 1, true) then missing = missing + 1 end
end
check("2000 PNJ: todos llegan en mensajes del tipo 9", missing == 0, tostring(missing) .. " sin enviar")

-- Cambio: el PNJ modificado llega como delta en menos de 60 s.
local changeOk, changeLag = false, nil
for _, m in ipairs(big.msgs9) do
    if m.t >= big.changedAt and string.find(m.s, "PNJ cambiado", 1, true) then
        changeOk, changeLag = true, m.t - big.changedAt
        break
    end
end
check("cambio de un PNJ llega en <= 60 s (delta)", changeOk and changeLag <= 60, tostring(changeLag))

-- No se reenvia todo cada ciclo: en regimen, cada ventana de 15 s pesa una fraccion del volcado.
local worstWin, sum9Late = 0, 0
for w, bytes in pairs(big.bytes9Win) do
    if w * 15 >= 400 then
        sum9Late = sum9Late + bytes
        if bytes > worstWin then worstWin = bytes end
    end
end
outln(string.format("  info  2000 PNJ, regimen (>400 s): tipo 9 hasta %d bytes por 15 s y %d bytes en total (volcado = %d)",
    worstWin, sum9Late, big.knowBytes))
check("tipo 9 en regimen: cada ventana de 15 s < 25% del knowledge", worstWin < 0.25 * big.knowBytes)
check("tipo 9 en regimen: menos de 1.5 volcados en 500 s", sum9Late < 1.5 * big.knowBytes)

-- Coste por tick con el knowledge grande (la revision reparte el trabajo en tandas). os.clock tiene
-- resolucion de ~1 ms en Windows: se da la media (sobre miles de ticks) y el maximo, no percentiles.
local ticksOver1ms = 0
for _, d in ipairs(big.dts) do if d >= 0.001 then ticksOver1ms = ticksOver1ms + 1 end end
outln(string.format("  info  2000 PNJ: Tick medio %.1f us, maximo %.1f us, ticks >= 1 ms: %d de %d",
    big.tickSum / big.ticks * 1e6, big.tickMax * 1e6, ticksOver1ms, big.ticks))
check("2000 PNJ: Tick medio < 300 us", big.tickSum / big.ticks < 0.0003)

-- ---------------------------------------------------------------------------
print("(k) Medicion de coste por refresco (luajit, orientativa)")
do
    local c0 = os.clock()
    for _ = 1, 500 do Ses.Tick() end
    local per = (os.clock() - c0) / 500
    outln(string.format("  info  Session.Tick medio: %.1f us por tick (cada 5 s)", per * 1e6))
    check("coste de Session.Tick < 1 ms", per < 0.001)
end
check("coste medio del canal por refresco (0.2 s) < 1 ms", big.tickSum / big.ticks < 0.001)

if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
