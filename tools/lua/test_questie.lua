-- test_questie.lua: pruebas offline de la exportacion de Questie, /compa marca y /compa sync.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_questie.lua
package.path = "tools/lua/?.lua;" .. package.path
local W = require("wowstub")

-- Captura del chat (ns.Say -> print).
local printed = {}
local function outln(s) io.write(tostring(s), "\n") end
_G.print = function(msg) printed[#printed + 1] = tostring(msg) end
local function lastPrinted() return printed[#printed] or "" end
local function anyPrinted(fragment)
    for _, m in ipairs(printed) do
        if string.find(m, fragment, 1, true) then return true end
    end
    return false
end

SlashCmdList = {}
CompaneroWoWDB = nil
W.installGameAPI()

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Collectors.lua")
load("CompaneroWoW/Native.lua")
load("CompaneroWoW/Bridge.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Questie.lua")
load("CompaneroWoW/Marks.lua")

local failures = 0
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
local function near(name, got, want, tol)
    tol = tol or 1e-6
    check(name, type(got) == "number" and math.abs(got - want) <= tol,
        string.format("got %s, want %s", tostring(got), tostring(want)))
end

-- Escenario base: Elwynn, nivel 9, en (0.45, 0.67). Tres misiones en el registro.
local function scenario()
    W.reset()
    printed = {}
    W.installGameAPI()
    W.installQuestie()
    local S = W.state
    S.questLog = {
        { isHeader = true, title = "Elwynn" },
        { questID = 2001, title = "Peligro en la granja", level = 10 },
        { questID = 2003, title = "Cosecha de Elwynn", level = 9 },
        { questID = 2007, title = "Alfas", level = 9 },
    }
    S.objectives[2001] = {
        { text = "Lobos eliminados", type = "monster", finished = false, numRequired = 8, numFulfilled = 3 },
        { text = "Revisa el cofre", type = "object", finished = true, numRequired = 1, numFulfilled = 1 },
    }
    S.objectives[2003] = {
        { text = "Buzón revisado", type = "object", finished = true, numRequired = 1, numFulfilled = 1 },
    }
    S.objectives[2007] = {
        { text = "Lobos alfa", type = "killcredit", finished = false, numRequired = 3, numFulfilled = 0 },
    }
    S.complete[2003] = true
    CompaneroWoWDB = nil
    ns.EnsureDB()
    ns.Questie.lastAt = nil
    ns.Questie.pending = false
    ns.Questie.zoneCache = nil
    ns.Questie.ResetNpc()
    return S
end

local function inRange(v) return type(v) == "number" and v >= 0 and v <= 1 end

-- Los PNJs se construyen en segundo plano (ticker): avanza los tickers hasta que no quede trabajo.
local function pumpNpcs()
    local guard = 0
    while W.activeTickers() > 0 and guard < 100000 do
        W.tickAll()
        guard = guard + 1
    end
end
-- Exportacion completa: arranca el trabajo de PNJs, lo termina y vuelve a exportar.
local function buildFull()
    ns.BuildQuestieExport()
    pumpNpcs()
    return ns.BuildQuestieExport()
end

-- Distancia y orden de referencia (independiente del addon).
local function nearestOf(points, px, py)
    local best, bd = nil, math.huge
    for _, p in ipairs(points) do
        local d = math.sqrt((p.x - px) ^ 2 + (p.y - py) ^ 2)
        if d < bd then best, bd = p, d end
    end
    return best
end

-- ---------------------------------------------------------------------------
outln("(a) Questie ausente -> {available=false}, sin errores")
scenario()
W.uninstallQuestie()
local absent = ns.BuildQuestieExport()
eq("sin Questie: available = false", absent.available, false)
local nKeys = 0
for _ in pairs(absent) do nKeys = nKeys + 1 end
eq("sin Questie: sin quests ni zonas (claves: available, reason, sources)", nKeys, 3)
local okAbsent = pcall(ns.ExportQuestieNow)
check("ExportQuestieNow sin Questie no lanza error", okAbsent)
local stored = CompaneroWoWDB.characters["Tester-Ravencrest"]
eq("sin Questie: se guarda questie.available=false", stored and stored.questie and stored.questie.available, false)
check("estado sin Questie: texto 'no instalado (opcional)'", string.find(ns.QuestieStatusLine(), "no instalado (opcional", 1, true) ~= nil)

-- ---------------------------------------------------------------------------
outln("(b) exportacion con Questie: forma del contrato")
scenario()
local early = ns.BuildQuestieExport()
check("la exportacion no espera a los PNJ: quests presentes", early.quests ~= nil and early.zoneQuests ~= nil)
eq("la exportacion no espera a los PNJ: npcs ausente al principio", early.npcs, nil)
local data = buildFull()
eq("available = true", data.available, true)
eq("version de Questie", data.version, "12.6.4")
check("exportedAt presente", type(data.exportedAt) == "number")
local nq = 0
for _ in pairs(data.quests) do nq = nq + 1 end
eq("3 misiones del registro exportadas", nq, 3)
local q1 = data.quests[2001]
check("2001 existe", q1 ~= nil)
eq("2001 nombre", q1 and q1.name, "Peligro en la granja")
eq("2001 level", q1 and q1.level, 10)
eq("2001 requiredLevel", q1 and q1.requiredLevel, 5)
eq("2001 nextQuest", q1 and q1.nextQuest, 2002)
eq("2001 preQuests[2]", q1 and q1.preQuests and q1.preQuests[2], 121)

local st = q1.starters[1]
eq("starter npc kind", st.kind, "npc")
eq("starter npc id", st.id, 1000)
eq("starter npc name", st.name, "Marshal Dughan")
eq("starter spawn mapID", st.spawns[1].mapID, 1429)
near("starter spawn x = 0.452 (45.2 %)", st.spawns[1].x, 0.452, 1e-9)
near("starter spawn y = 0.671 (67.1 %)", st.spawns[1].y, 0.671, 1e-9)

local fin = q1.finishers[1]
eq("finisher id", fin.id, 1001)
near("finisher mas cercano primero (0.5, 0.5)", fin.spawns[1].x, 0.5, 1e-9)
eq("finisher spawns caben (2)", #fin.spawns, 2)

eq("2001 tiene 2 objetivos", #q1.objectives, 2)
local o1 = q1.objectives[1]
eq("objetivo 1 index", o1.index, 1)
eq("objetivo 1 kind monster", o1.kind, "monster")
eq("objetivo 1 id", o1.id, 1100)
eq("objetivo 1 name", o1.name, "Lobo")
eq("objetivo 1 required (registro)", o1.required, 8)
eq("objetivo 1 spawns <= 10", #o1.spawns <= 10, true)
eq("objetivo 1 con 10 spawns (12 disponibles)", #o1.spawns, 10)
-- El primer spawn debe ser el mas cercano al jugador (calculado por fuerza bruta sobre los 12).
local lobo = {}
for _, p in ipairs(W.NPC[1100].spawns[12]) do lobo[#lobo + 1] = { x = p[1] / 100, y = p[2] / 100 } end
local nearLobo = nearestOf(lobo, 0.45, 0.67)
near("objetivo 1: primer spawn = mas cercano x", o1.spawns[1].x, nearLobo.x, 1e-9)
near("objetivo 1: primer spawn = mas cercano y", o1.spawns[1].y, nearLobo.y, 1e-9)
local o2 = q1.objectives[2]
eq("objetivo 2 kind object", o2.kind, "object")
eq("objetivo 2 spawns: area sin uiMap descartada", #o2.spawns, 1)
eq("objetivo 2 spawn mapID", o2.spawns[1].mapID, 1429)

-- Quest 2003: objetivo de objeto, todo completado.
local q3 = data.quests[2003]
eq("2003 un objetivo", q3 and #q3.objectives, 1)
eq("2003 objeto 3000 nombre", q3 and q3.objectives[1].name, "Buzón de Elwynn")

-- Quest 2007: killcredit con RootId 1101.
local q7 = data.quests[2007]
eq("2007 objetivo killcredit", q7 and q7.objectives[1].kind, "killcredit")
eq("2007 killcredit id = RootId", q7 and q7.objectives[1].id, 1101)

-- Todas las coordenadas en 0..1 y mapID presente.
local allOk = true
for _, q in pairs(data.quests) do
    for _, list in ipairs({ q.starters, q.finishers }) do
        for _, e in ipairs(list) do
            for _, s in ipairs(e.spawns) do
                if not (inRange(s.x) and inRange(s.y) and type(s.mapID) == "number") then allOk = false end
            end
        end
    end
    for _, o in ipairs(q.objectives) do
        for _, s in ipairs(o.spawns) do
            if not (inRange(s.x) and inRange(s.y) and type(s.mapID) == "number") then allOk = false end
        end
    end
end
check("todas las coordenadas son uiMapID + 0..1", allOk)

-- Zona actual (Elwynn, 1429) y PNJs.
eq("zoneQuests.mapID", data.zoneQuests and data.zoneQuests.mapID, 1429)
eq("zoneQuests: solo 2005 (2001/2003/2007 en registro, 2002 sin PNJ, 2004 nivel, 2010 otra zona)",
    data.zoneQuests and #data.zoneQuests.list, 1)
local zq = data.zoneQuests.list[1]
eq("zoneQuests 2005", zq.questID, 2005)
eq("zoneQuests starter kind", zq.starter.kind, "npc")
eq("zoneQuests starter id 1002", zq.starter.id, 1002)
eq("zoneQuests starter mapID", zq.starter.mapID, 1429)
near("zoneQuests starter x 0.40", zq.starter.x, 0.4, 1e-9)

-- PNJs: 2 de la zona (Elwynn) + 5 de Ventormenta (capital Alliance), sin [DND] ni sin rol/subtitulo.
local npcIds = {}
for _, n in ipairs(data.npcs or {}) do npcIds[n.id] = n end
eq("npcs: 7 PNJ (2 zona + 5 capital, sin [DND])", data.npcs and #data.npcs, 7)
eq("npcs: zona primero (mapID 1429)", data.npcs and data.npcs[1].mapID, 1429)
eq("npcs: vuelo 1002 rol flightmaster", npcIds[1002] and npcIds[1002].role, "flightmaster")
eq("npcs: subtitulo 'Vuelo'", npcIds[1002] and npcIds[1002].subtitle, "Vuelo")
eq("npcs: posadera 1003 rol innkeeper", npcIds[1003] and npcIds[1003].role, "innkeeper")
eq("npcs: [DND] excluido", npcIds[1004], nil)
eq("npcs: sin rol ni subtitulo (Dughan, Kel) excluidos", (npcIds[1000] or npcIds[1001]), nil)
eq("npcs: coordenadas 0..1", inRange(npcIds[1002] and npcIds[1002].x), true)

-- Capital de Ventormenta (uiMap 84): roles por subtitulo, por npcFlags y "other".
local lirio = npcIds[1200]
eq("capital: 1200 rol trainer por subtitulo 'Herbalism Trainer'", lirio and lirio.role, "trainer")
eq("capital: nombre ingles sin traduccion", lirio and lirio.name, "Lirio the Herbalist")
eq("capital: subtitulo conservado", lirio and lirio.subtitle, "Herbalism Trainer")
eq("capital: uiMapID de Ventormenta = 84", lirio and lirio.mapID, 84)
near("capital: x 0.40", lirio and lirio.x, 0.4, 1e-9)
near("capital: y 0.60", lirio and lirio.y, 0.6, 1e-9)
eq("capital: 1201 banquero por flag BANKER", npcIds[1201] and npcIds[1201].role, "banker")
eq("capital: 1202 solo subtitulo 'Armero' -> other", npcIds[1202] and npcIds[1202].role, "other")
eq("capital: 1202 subtitulo 'Armero' conservado", npcIds[1202] and npcIds[1202].subtitle, "Armero")
eq("capital: 1204 instructor por flag TRAINER", npcIds[1204] and npcIds[1204].role, "trainer")
eq("capital: 1205 vendedor por flag VENDOR", npcIds[1205] and npcIds[1205].role, "vendor")
eq("capital de Orgrimmar (1203) no sale siendo Alliance", npcIds[1203], nil)

-- Localizacion: nombres y subtitulos en esES cuando Questie los traduce; si no, ingles.
scenario()
W.state.locale = "esES"
local esNpcs = {}
for _, n in ipairs(buildFull().npcs or {}) do esNpcs[n.id] = n end
eq("esES: nombre localizado de 1200", esNpcs[1200] and esNpcs[1200].name, "Lirio el herborista")
eq("esES: subtitulo 'Instructora de Herboristería' -> trainer", esNpcs[1200] and esNpcs[1200].role, "trainer")
eq("esES: subtitulo localizado conservado", esNpcs[1200] and esNpcs[1200].subtitle, "Instructora de Herboristería")
eq("esES: sin traduccion -> ingles (Armero Olaf)", esNpcs[1202] and esNpcs[1202].name, "Armero Olaf")
eq("esES: 1002 sigue igual", esNpcs[1002] and esNpcs[1002].subtitle, "Vuelo")

-- Faccion Horde: capital Orgrimmar; Ventormenta fuera.
scenario()
W.state.faction = "Horde"
local hordeNpcs = {}
for _, n in ipairs(buildFull().npcs or {}) do hordeNpcs[n.id] = n end
eq("Horde: 3 PNJ (2 zona + 1 capital)", #(buildFull().npcs or {}), 3)
eq("Horde: cocinera 1203 rol vendor por 'Suministros'", hordeNpcs[1203] and hordeNpcs[1203].role, "vendor")
eq("Horde: uiMapID de Orgrimmar = 85", hordeNpcs[1203] and hordeNpcs[1203].mapID, 85)
eq("Horde: sin PNJ de Ventormenta", hordeNpcs[1200], nil)

-- Faccion desconocida: solo la zona.
scenario()
W.state.faction = nil
eq("sin faccion: solo los 2 PNJ de la zona", #(buildFull().npcs or {}), 2)

-- Un PNJ que falla en Questie se omite; los demas siguen.
scenario()
W.uninstallQuestie()
W.installQuestie({ failNpc = 1200 })
local failNpcData = buildFull()
check("fallo en PNJ: la exportacion sigue disponible", failNpcData.available == true and failNpcData.npcs ~= nil)
local failIds = {}
for _, n in ipairs(failNpcData.npcs or {}) do failIds[n.id] = n end
eq("fallo en PNJ: 1200 omitido", failIds[1200], nil)
check("fallo en PNJ: 1201 sigue", failIds[1201] ~= nil)

-- Limites: 72 candidatos en la zona -> 60; 135 en la capital -> 120. Mas cercanos primero.
scenario()
W.uninstallQuestie()
W.installQuestie({ zoneFill = 70, capitalFill = 130 })
local capped = buildFull()
local zoneN, capN, lastD = 0, 0, -1
local sortedOk = true
for _, n in ipairs(capped.npcs or {}) do
    if n.mapID == 1429 then
        zoneN = zoneN + 1
        local d = math.sqrt((n.x - 0.45) ^ 2 + (n.y - 0.67) ^ 2)
        if d < lastD - 1e-12 then sortedOk = false end
        lastD = d
    elseif n.mapID == 84 then
        capN = capN + 1
    end
end
eq("limite zona: 60 PNJ (de 72)", zoneN, 60)
eq("limite capital: 120 PNJ (de 135)", capN, 120)
eq("limite total: 180 PNJ", #(capped.npcs or {}), 180)
check("zona: mas cercanos primero", sortedOk)

-- Tamano aproximado del bloque questie (SavedVariables) con 180 PNJ: < 60 KB.
local function serializedSize(v)
    if type(v) == "table" then
        local n = 2
        for k, x in pairs(v) do n = n + #tostring(k) + 3 + serializedSize(x) + 1 end
        return n
    elseif type(v) == "string" then
        return #v + 2
    end
    return #tostring(v)
end
check("bloque questie < 60 KB con 180 PNJ", serializedSize(capped) < 60 * 1024, tostring(serializedSize(capped)))

-- ---------------------------------------------------------------------------
outln("(c) caps y omisiones")
scenario()
for i = 1, 40 do
    W.state.questLog[#W.state.questLog + 1] = { questID = 5000 + i, title = "Extra " .. i, level = 9 }
end
local capped = buildFull()
local nCap = 0
for _ in pairs(capped.quests) do nCap = nCap + 1 end
eq("maximo 35 misiones", nCap, 35)

scenario()
W.uninstallQuestie()
W.installQuestie({ noZone = true })
local noZone = ns.BuildQuestieExport()
eq("sin ZoneDB: available = true", noZone.available, true)
eq("sin ZoneDB: sin zoneQuests", noZone.zoneQuests, nil)
eq("sin ZoneDB: sin npcs", noZone.npcs, nil)
eq("sin ZoneDB: la mision sigue (sin spawns)", noZone.quests[2001] and #noZone.quests[2001].starters[1].spawns, 0)

scenario()
W.installQuestie({ failQuest = 2001 })
local failing = ns.BuildQuestieExport()
check("error en un campo de Questie no rompe la exportacion", failing.quests[2001] ~= nil)
eq("error en QueryQuestSingle: 2001 sin starters", failing.quests[2001] and #failing.quests[2001].starters, 0)
eq("error en QueryQuestSingle: 2003 sigue completa", failing.quests[2003] and #failing.quests[2003].objectives, 1)

-- ---------------------------------------------------------------------------
outln("(d) throttle 10 s, combate y /compa sync")
scenario()
S = W.state
ns.Questie.lastAt = nil
S.clock = 100
check("primera exportacion automatica se hace", ns.RequestQuestieExport() == true)
local key = "Tester-Ravencrest"
local first = CompaneroWoWDB.characters[key].questie.exportedAt
S.now = S.now + 1
S.clock = 105
check("dentro de 10 s: no exporta", ns.RequestQuestieExport() == false)
eq("dentro de 10 s: deja un temporizador de 5 s", #W.callsNamed("C_Timer.After") >= 1 and W.callsNamed("C_Timer.After")[1].args[1], 5)
S.clock = 110
for _, t in ipairs(S.timers) do t.fn() end
S.timers = {}
eq("el temporizador exporta al cumplir 10 s", CompaneroWoWDB.characters[key].questie.exportedAt ~= first, true)

S.inCombat = true
S.now = S.now + 100
S.clock = 500
local before = CompaneroWoWDB.characters[key].questie.exportedAt
check("en combate: no exporta", ns.RequestQuestieExport() == false)
eq("en combate: datos sin cambios", CompaneroWoWDB.characters[key].questie.exportedAt, before)
S.inCombat = false

S.now = S.now + 5
S.clock = 501
SlashCmdList.COMPA("sync")
eq("/compa sync exporta aunque el throttle este activo", CompaneroWoWDB.characters[key].questie.exportedAt ~= before, true)
check("/compa sync recarga la interfaz", #W.callsNamed("ReloadUI") == 1)

-- Estado: cuenta de misiones y de la zona.
check("estado: 3 misiones y 1 en zona", string.find(ns.QuestieStatusLine(), "3 misiones exportadas, 1 disponibles", 1, true) ~= nil,
    ns.QuestieStatusLine())
SlashCmdList.COMPA("estado")
check("/compa estado imprime la linea de Questie", anyPrinted("Questie 12.6.4: 3 misiones exportadas"))

-- ---------------------------------------------------------------------------
outln("(e) analisis de /compa marca (puro)")
local P = ns.Marks.ParseArgs
local p1 = P("45.2 67,1 Texto")
eq("coords con punto y coma", p1.kind, "coords")
eq("sin mapID", p1.mapID, nil)
near("x 45.2 -> 0.452", p1.x, 0.452, 1e-9)
near("y 67,1 -> 0.671", p1.y, 0.671, 1e-9)
eq("texto libre", p1.text, "Texto")
local p2 = P("1429 45 67")
eq("mapID explicito (1429 > 100)", p2.mapID, 1429)
near("x 45 -> 0.45", p2.x, 0.45, 1e-9)
eq("sin texto", p2.text, "")
local p3 = P("1429 45 67 Entregar A Marshal")
eq("mapID + texto conserva mayusculas", p3.text, "Entregar A Marshal")
local p4 = P("37 45 67 hola", function(id) return id == 37 end)
eq("mapID pequeno confirmado por isMap", p4.mapID, 37)
eq("auto sin argumentos", P("").kind, "auto")
eq("borrar", P("borrar").kind, "clear")
eq("BORRAR", P("BORRAR").kind, "clear")
eq("texto no numerico -> error", P("hola").kind, "error")
eq("fuera de 0..100 -> error", P("150 10").kind, "error")

-- ---------------------------------------------------------------------------
outln("(f) /compa marca con coordenadas, mapa explicito y pin")
scenario()
S = W.state
printed = {}
W.reset() -- limpia llamadas; el estado queda por defecto (Elwynn)
ns.Questie.ResetNpc()
S = W.state
W.installGameAPI()
W.installQuestie()
printed = {}
SlashCmdList.COMPA("marca 45.2 67,1 Entregar A Marshal")
local setCalls = W.callsNamed("C_Map.SetUserWaypoint")
eq("SetUserWaypoint llamado una vez", #setCalls, 1)
local pt = setCalls[1] and setCalls[1].args[1]
eq("punto en uiMap 1429", pt and pt.uiMapID, 1429)
near("punto x = 0.452", pt and pt.position.x, 0.452, 1e-9)
near("punto y = 0.671", pt and pt.position.y, 0.671, 1e-9)
local st1 = W.callsNamed("C_SuperTrack.SetSuperTrackedUserWaypoint")
eq("supertrack del punto de usuario = true", st1[1] and st1[1].args[1], true)
check("CanSetUserWaypointOnMap consultado", #W.callsNamed("C_Map.CanSetUserWaypointOnMap") >= 1)
check("chat: marca puesta en Elwynn con coordenadas y texto",
    anyPrinted("Marca puesta en Elwynn (45.2, 67.1): Entregar A Marshal"), lastPrinted())
check("pin de Compañero registrado en WorldMapFrame", #W.callsNamed("WorldMapFrame:AddDataProvider") == 1)
local pins = W.callsNamed("SetPoint")
local lastPin = pins[#pins]
check("pin colocado en el canvas", lastPin ~= nil)
if lastPin then
    near("pin: x = 0.452 * 800", lastPin.args[4], 0.452 * 800, 1e-6)
    near("pin: y = -0.671 * 600", lastPin.args[5], -0.671 * 600, 1e-6)
end

W.calls = {}
SlashCmdList.COMPA("marca 1436 10 20 Westfall")
local pt2 = W.callsNamed("C_Map.SetUserWaypoint")[1]
eq("mapID explicito 1436", pt2 and pt2.args[1].uiMapID, 1436)
check("chat con nombre de Westfall", anyPrinted("Marca puesta en Westfall (10.0, 20.0): Westfall"), lastPrinted())

W.calls = {}
W.state.noWaypointMaps[1429] = true
SlashCmdList.COMPA("marca 50 50")
eq("mapa no permitido: no hay SetUserWaypoint", #W.callsNamed("C_Map.SetUserWaypoint"), 0)
check("mapa no permitido: avisa en el chat", anyPrinted("Blizzard no permite marcar"), lastPrinted())
W.state.noWaypointMaps[1429] = nil

W.calls = {}
W.state.shownMap = 1436
ns.Marks.Refresh()
check("pin oculto si el mapa mostrado es otro", #W.callsNamed("Hide") >= 1)
W.state.shownMap = 1429

W.calls = {}
SlashCmdList.COMPA("marca borrar")
eq("borrar: ClearUserWaypoint", #W.callsNamed("C_Map.ClearUserWaypoint"), 1)
local st2 = W.callsNamed("C_SuperTrack.SetSuperTrackedUserWaypoint")
eq("borrar: supertrack = false", st2[1] and st2[1].args[1], false)
eq("borrar: se olvida la marca", ns.Marks.mark, nil)
eq("borrar: se quita de SavedVariables", CompaneroWoWDB.mark, nil)
SlashCmdList.COMPA("marca borrar")
check("borrar sin marca: avisa", anyPrinted("No hay ninguna marca"), lastPrinted())

SlashCmdList.COMPA("")
check("ayuda: incluye 'marca'", anyPrinted("/compa marca <x> <y>"))

-- ---------------------------------------------------------------------------
outln("(g) /compa marca sin argumentos (Questie)")
scenario()
S = W.state
S.superTracked = 2001
printed = {}
W.calls = {}
SlashCmdList.COMPA("marca")
-- 2001: primer objetivo pendiente = Lobo -> spawn mas cercano.
local lobos = {}
for _, p in ipairs(W.NPC[1100].spawns[12]) do lobos[#lobos + 1] = { x = p[1] / 100, y = p[2] / 100 } end
local expectLobo = nearestOf(lobos, 0.45, 0.67)
local lp = W.callsNamed("C_Map.SetUserWaypoint")[1]
check("auto: marca puesta", lp ~= nil)
if lp then
    near("auto: marca en el Lobo mas cercano (x)", lp.args[1].position.x, expectLobo.x, 1e-9)
    near("auto: marca en el Lobo mas cercano (y)", lp.args[1].position.y, expectLobo.y, 1e-9)
end
check("auto: chat dice el objetivo y la mision", anyPrinted("Lobo (Peligro en la granja)"), lastPrinted())

-- Todos completados -> entrega (finisher 1001, spawn mas cercano (0.5,0.5)).
scenario()
S = W.state
S.superTracked = 2003
W.calls = {}
SlashCmdList.COMPA("marca")
local dp = W.callsNamed("C_Map.SetUserWaypoint")[1]
check("auto completado: marca puesta", dp ~= nil)
if dp then near("auto completado: entrega en (0.5, 0.5)", dp.args[1].position.x, 0.5, 1e-9) end
check("auto completado: chat 'Entregar a Granjero Kel'", anyPrinted("Entregar a Granjero Kel"), lastPrinted())

-- Sin supertrack: primera mision del registro (2001).
scenario()
S = W.state
S.superTracked = 0
W.calls = {}
SlashCmdList.COMPA("marca")
check("sin supertrack: usa la primera mision del registro", #W.callsNamed("C_Map.SetUserWaypoint") == 1)

-- Sin Questie: no marca y avisa.
scenario()
W.uninstallQuestie()
W.calls = {}
SlashCmdList.COMPA("marca")
eq("sin Questie: no marca", #W.callsNamed("C_Map.SetUserWaypoint"), 0)
check("sin Questie: avisa sin exigir Questie", anyPrinted("No sé dónde está el siguiente paso"), lastPrinted())
check("sin Questie: avisa que es opcional", anyPrinted("Questie no está instalado (es opcional)"), lastPrinted())

-- Registro desincronizado con Questie -> no marca a ciegas.
scenario()
W.state.objectives[2001] = { { text = "x", finished = false } }
W.calls = {}
SlashCmdList.COMPA("marca")
eq("objetivos no emparejados: no marca", #W.callsNamed("C_Map.SetUserWaypoint"), 0)
check("objetivos no emparejados: avisa", anyPrinted("No puedo emparejar"), lastPrinted())

-- ---------------------------------------------------------------------------
-- ---------------------------------------------------------------------------
outln("(h) PNJs en segundo plano: troceado, sin combate, sin trabajo sincrono")
local Q = ns.Questie

-- Sin trabajo sincrono: la exportacion y SnapshotAll no recorren los PNJ.
scenario()
W.uninstallQuestie()
W.installQuestie({ zoneFill = 2000 })
S = W.state
S.npcQueries = 0
local early2 = ns.BuildQuestieExport()
check("exportacion: pocas consultas de PNJ (sin escaneo)", S.npcQueries < 100, tostring(S.npcQueries))
eq("exportacion: npcs ausente hasta terminar el trabajo", early2.npcs, nil)
check("exportacion: indice no construido de forma sincrona", Q.npcIndex == nil)
check("exportacion: trabajo de PNJs pendiente", ns.Questie.PendingNpcTasks() > 0)

scenario()
W.uninstallQuestie()
W.installQuestie({ zoneFill = 2000 })
S = W.state
S.npcQueries = 0
ns.SnapshotAll()
check("SnapshotAll: pocas consultas de PNJ (sin escaneo)", S.npcQueries < 100, tostring(S.npcQueries))
check("SnapshotAll: indice no construido de forma sincrona", Q.npcIndex == nil)
pumpNpcs()
check("SnapshotAll: el trabajo termina en segundo plano", Q.npcIndex ~= nil)

-- Troceado: un tick avanza por unidades (200 PNJ) y no termina con 2000+ PNJ.
scenario()
W.uninstallQuestie()
W.installQuestie({ zoneFill = 2000 })
S = W.state
ns.BuildQuestieExport()
S.npcQueries = 0
W.tickAll()
check("un tick: avanza pero no termina", Q.npcIndex == nil and S.npcQueries > 0, tostring(S.npcQueries))
check("un tick: acotado (<= 200 PNJ x 3 consultas)", S.npcQueries <= 200 * 3 + 10, tostring(S.npcQueries))
pumpNpcs()
check("tras los ticks: indice listo", Q.npcIndex ~= nil)
local saved = CompaneroWoWDB.characters["Tester-Ravencrest"].questie
check("tras los ticks: la exportacion se guarda sola", saved and saved.npcs ~= nil)
eq("PNJ guardados: 60 zona + 5 capital", #((saved and saved.npcs) or {}), 65)

-- Presupuesto de tiempo: cada lectura de reloj cuesta 1 ms; el tick corta al llegar a 4 ms.
scenario()
W.uninstallQuestie()
W.installQuestie({ zoneFill = 2000 })
S = W.state
S.msStep = 1
ns.BuildQuestieExport()
local worst = 0
for _ = 1, 5 do
    local before = S.npcQueries
    W.tickAll()
    worst = math.max(worst, S.npcQueries - before)
end
check("presupuesto de tiempo: ningun tick hace mas de 40 consultas", worst <= 40, tostring(worst))
check("presupuesto de tiempo: sigue sin terminar tras 5 ticks", Q.npcIndex == nil and ns.Questie.PendingNpcTasks() > 0)
S.msStep = 0
pumpNpcs()
check("presupuesto de tiempo: termina al seguir", Q.npcIndex ~= nil)

-- Combate: no avanza; fuera de combate, termina.
scenario()
S = W.state
S.inCombat = true
ns.BuildQuestieExport()
for _ = 1, 5 do W.tickAll() end
check("en combate: el trabajo de PNJs no avanza", Q.npcIndex == nil and ns.Questie.PendingNpcTasks() > 0)
S.inCombat = false
pumpNpcs()
check("fuera de combate: el trabajo termina", Q.npcIndex ~= nil)

-- Mismo resultado en dos construcciones (indice y candidatos recalculados).
scenario()
local first = buildFull()
ns.Questie.ResetNpc()
local second = buildFull()
local same = #first.npcs == #second.npcs
for i = 1, #first.npcs do
    local a, b = first.npcs[i], second.npcs[i]
    if a.id ~= b.id or a.role ~= b.role or a.mapID ~= b.mapID or a.x ~= b.x or a.y ~= b.y then same = false end
end
check("mismo resultado al recalcular desde cero", same and #first.npcs == 7)

if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
