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
    ns.Questie.npcCache = nil
    return S
end

local function inRange(v) return type(v) == "number" and v >= 0 and v <= 1 end

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
eq("sin Questie: solo la clave available", nKeys, 1)
local okAbsent = pcall(ns.ExportQuestieNow)
check("ExportQuestieNow sin Questie no lanza error", okAbsent)
local stored = CompaneroWoWDB.characters["Tester-Ravencrest"]
eq("sin Questie: se guarda questie.available=false", stored and stored.questie and stored.questie.available, false)
check("estado sin Questie: texto 'no disponible'", string.find(ns.QuestieStatusLine(), "no disponible", 1, true) ~= nil)

-- ---------------------------------------------------------------------------
outln("(b) exportacion con Questie: forma del contrato")
scenario()
local data = ns.BuildQuestieExport()
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

local npcIds = {}
for _, n in ipairs(data.npcs or {}) do npcIds[n.id] = n end
eq("npcs: 2 PNJ con rol (sin [DND])", data.npcs and #data.npcs, 2)
eq("npcs: vuelo 1002 rol flightmaster", npcIds[1002] and npcIds[1002].role, "flightmaster")
eq("npcs: subtitulo 'Vuelo'", npcIds[1002] and npcIds[1002].subtitle, "Vuelo")
eq("npcs: posadera 1003 rol innkeeper", npcIds[1003] and npcIds[1003].role, "innkeeper")
eq("npcs: [DND] excluido", npcIds[1004], nil)
eq("npcs: coordenadas 0..1", inRange(npcIds[1002] and npcIds[1002].x), true)

-- ---------------------------------------------------------------------------
outln("(c) caps y omisiones")
scenario()
for i = 1, 40 do
    W.state.questLog[#W.state.questLog + 1] = { questID = 5000 + i, title = "Extra " .. i, level = 9 }
end
local capped = ns.BuildQuestieExport()
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
check("sin Questie: avisa", anyPrinted("Questie no está disponible"), lastPrinted())

-- Registro desincronizado con Questie -> no marca a ciegas.
scenario()
W.state.objectives[2001] = { { text = "x", finished = false } }
W.calls = {}
SlashCmdList.COMPA("marca")
eq("objetivos no emparejados: no marca", #W.callsNamed("C_Map.SetUserWaypoint"), 0)
check("objetivos no emparejados: avisa", anyPrinted("No puedo emparejar"), lastPrinted())

-- ---------------------------------------------------------------------------
if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
