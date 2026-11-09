-- test_native.lua: pruebas offline del modo nativo (sin Questie ni ForeverDungeonJournal), de la fusion
-- con Questie cuando esta instalado, de /compa marca (Blizzard primero) y del aprendizaje jugando.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_native.lua
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

-- Captura del chat (ns.Say -> print) y de los frames (para disparar eventos a mano).
local printed = {}
_G.print = function(msg) printed[#printed + 1] = tostring(msg) end
local function lastPrinted() return printed[#printed] or "" end
local function anyPrinted(fragment)
    for _, m in ipairs(printed) do
        if string.find(m, fragment, 1, true) then return true end
    end
    return false
end

local frames = {}
SlashCmdList = {}
CompaneroWoWDB = nil
_G.CreateFrame = function()
    local f = { events = {} }
    function f:RegisterEvent(name) self.events[name] = true end
    function f:SetScript(name, fn)
        if name == "OnEvent" then self.onEvent = fn end
    end
    frames[#frames + 1] = f
    return f
end

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
load("CompaneroWoW/Knowledge.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Questie.lua")
load("CompaneroWoW/QuestWindow.lua")
load("CompaneroWoW/Gear.lua")
load("CompaneroWoW/Marks.lua")

local S = W.state
local KEY = "Tester-Ravencrest"
local QW, G, N = ns.QuestWindow, ns.Gear, ns.Native
local function char() return CompaneroWoWDB and CompaneroWoWDB.characters[KEY] or nil end
local function frameWith(event)
    for _, f in ipairs(frames) do
        if f.events[event] then return f end
    end
    return nil
end

-- Datos de juego de prueba: Elwynn (1429) dentro de Azeroth (1427).
local NATIVE = {
    mapNames = { [1429] = "Elwynn", [1427] = "Azeroth" },
    parents = { [1429] = 1427 },
    lines = {
        [1429] = {
            { questID = 9001, questName = "Un encargo sencillo", questLineName = "Ayudar", x = 0.42, y = 0.61, isHidden = false },
            { questID = 9002, questName = "Lobos cerca", x = 0.5, y = 0.5 },
        },
        [1427] = {
            { questID = 9001, questName = "duplicada en el padre", x = 0.1, y = 0.1 },
            { questID = 9100, questName = "Gran viaje", x = 0.1, y = 0.1 },
        },
    },
    levels = { [9001] = 9, [9002] = 10, [9100] = 20 },
    waypoints = { [2001] = { mapID = 1429, x = 0.31, y = 0.42 } },
    pois = {
        [1429] = {
            { questID = 2003, x = 0.2, y = 0.2, inProgress = true },
            { questID = 2007, x = 0.6, y = 0.6, inProgress = false },
        },
    },
    rewards = { [2001] = { choice = { 5001, 5002 }, fixed = { 5003 } } },
    services = {
        { name = "Golpe", rank = "Rango 1", category = "available" },
        { name = "Patada", rank = "Rango 1", category = "used" },
        { name = "Carga", rank = "Rango 2", category = "available" },
    },
}

-- Partida: en Elwynn, nivel 9, con dos misiones en el registro (2001 con objetivos, 2003).
local function scenario(native, withQuestie)
    W.uninstallNative()
    W.reset()
    printed = {}
    W.installGameAPI()
    if native ~= false then W.installNative(native or NATIVE) end
    if withQuestie then W.installQuestie() end
    _G.UnitGUID = function(unit)
        if unit == "npc" then return "Creature-0-0-0-0-1234-0000000000" end
        return nil
    end
    _G.UnitIsPlayer = function() return false end
    S.questLog = {
        { isHeader = true, title = "Elwynn" },
        { questID = 2001, title = "Peligro en la granja", level = 10 },
        { questID = 2003, title = "Cosecha de Elwynn", level = 9 },
    }
    S.objectives[2001] = {
        { text = "Lobos eliminados", type = "monster", finished = false, numRequired = 8, numFulfilled = 3 },
        { text = "Revisa el cofre", type = "object", finished = true, numRequired = 1, numFulfilled = 1 },
    }
    S.objectives[2003] = {
        { text = "Buzón revisado", type = "object", finished = true, numRequired = 1, numFulfilled = 1 },
    }
    S.level = 9
    CompaneroWoWDB = nil
    ns.Marks.mark = nil
end

-- Ejecuta QuestWindow.Work en una coroutine (como en el addon) y devuelve el resultado.
local function runWindow()
    local me = G.Me()
    for id in pairs(S.flagged) do me.completed[id] = true end
    me.exclusiveOf = function() return {} end
    local db, zone = nil, nil
    if N.QuestieInstalled() then db, zone = ns.Questie.Modules() end
    local env = { db = db, zone = zone, me = me, rewardIdx = {}, reverse = nil, mapID = 1429,
        px = 0.45, py = 0.67, zoneNames = {} }
    local res = {}
    local co = coroutine.create(function() QW.Work(env, res) end)
    while coroutine.status(co) ~= "dead" do
        local ok, err = coroutine.resume(co)
        assert(ok, err)
    end
    return res
end

-- ---------------------------------------------------------------------------
outln("Modo nativo: sin Questie ni ForeverDungeonJournal")
scenario()
check("Questie no instalado", N.QuestieInstalled() == false)
check("ForeverDungeonJournal no instalado", N.FdjInstalled() == false)
check("linea de fuentes: Questie no instalado (opcional)",
    string.find(N.SourcesStatusLine(), "Questie no instalado (opcional)", 1, true) ~= nil)

ns.ExportNativeNow()
local nat = char().native
check("bloque native disponible", nat and nat.available == true)
eq("native: fuente juego", nat.sources.juego, true)
eq("native: sin Questie", nat.sources.questie, false)
eq("native: sin FDJ", nat.sources.fdj, false)

local zq, zq9001, zqCount = nat.zoneQuests.list, nil, 0
for _, z in ipairs(zq) do
    if z.questID == 9001 then zq9001 = z end
    if z.questID == 9001 then zqCount = zqCount + 1 end
end
check("zonas: 9001 disponible", zq9001 ~= nil)
eq("zonas: 9001 una sola vez (sin duplicar desde el padre)", zqCount, 1)
eq("zonas: 9001 con origen juego", zq9001 and zq9001.source, "juego")
check("zonas: 9001 con posicion del PNJ", zq9001 and math.abs(zq9001.x - 0.42) < 1e-6 and math.abs(zq9001.y - 0.61) < 1e-6)
eq("zonas: 9001 en el mapa especifico", zq9001 and zq9001.mapID, 1429)
local zq9100 = nil
for _, z in ipairs(zq) do if z.questID == 9100 then zq9100 = z end end
check("zonas: 9100 del padre incluida", zq9100 ~= nil)

local act = nat.active[1]
check("activas: 2001 con paso siguiente de Blizzard", act and act.questID == 2001)
eq("activas: waypoint de Blizzard", act and act.waypoint and act.waypoint.mapID, 1429)
eq("activas: origen juego", act and act.source, "juego")
local poi2003, poi2007 = false, false
for _, p in ipairs(nat.pois) do
    if p.questID == 2003 then poi2003 = true end
    if p.questID == 2007 then poi2007 = true end
end
check("pois: en curso (2003) incluida", poi2003)
check("pois: no en curso (2007) excluida", not poi2007)

ns.ExportQuestieNow()
local qx = char().questie
eq("questie sin instalar: available false", qx.available, false)
check("questie sin instalar: razon opcional", string.find(qx.reason or "", "opcional", 1, true) ~= nil)
eq("questie sin instalar: sources.questie false", qx.sources.questie, false)
check("estado questie: no instalado (opcional)", string.find(ns.QuestieStatusLine(), "no instalado (opcional", 1, true) ~= nil)

-- Misiones de la ventana sin Questie: lo que el juego ofrece ahora.
local res = runWindow()
eq("ventana: disponible sin Questie", res.block.available, true)
eq("ventana: fuente questie false", res.block.sources.questie, false)
eq("ventana: fuente juego", res.block.sources.juego, true)
local w9001 = res.block.quests[9001]
check("ventana: 9001 (ofrecida ahora) incluida", w9001 ~= nil)
eq("ventana: 9001 origen juego", w9001 and w9001.source, "juego")
eq("ventana: 9001 accesible ahora", w9001 and w9001.accessibleNow, true)
eq("ventana: 9001 sin prerrequisitos (solo Questie los da)", w9001 and #w9001.missingPrereqs, 0)
check("ventana: 9100 fuera de la ventana de nivel", res.block.quests[9100] == nil)
check("ventana: 2001 del registro con recompensas de Blizzard", res.block.quests[2001] ~= nil)
local choice5001 = nil
for _, r in ipairs(res.rewardItems) do
    if r.itemID == 5001 then choice5001 = r end
end
eq("recompensas: 5001 del registro, origen juego", choice5001 and choice5001.source, "juego")
eq("recompensas: 5001 es elegible", choice5001 and choice5001.choice, true)

-- Aprendido jugando: al entrar en la zona se anota lo que se ofrece; luego ya no se ofrece.
local zoneFrame = frameWith("ZONE_CHANGED_NEW_AREA")
check("frame nativo registra ZONE_CHANGED_NEW_AREA", zoneFrame ~= nil)
zoneFrame.onEvent(zoneFrame, "ZONE_CHANGED_NEW_AREA")
local learned = CompaneroWoWDB.knowledge.quests[9001]
check("aprendido: 9001 guardada con su posicion", learned and learned.offeredAt and learned.offeredAt.mapID == 1429)
eq("aprendido: nivel de 9001 guardado", learned and learned.level, 9)
S.flagged[9002] = true
res = runWindow()
check("completada: 9002 excluida", res.block.quests[9002] == nil)
S.flagged[9002] = nil
S.native.lines = {}
res = runWindow()
local a9001 = res.block.quests[9001]
eq("aprendido: 9001 ya no se ofrece -> origen aprendido", a9001 and a9001.source, "aprendido")
eq("aprendido: 9001 no accesible ahora", a9001 and a9001.accessibleNow, false)
eq("aprendido: fuente aprendido en el resumen", res.block.sources.aprendido, true)

-- Recompensas aprendidas al ver la mision (QUEST_DETAIL), antes de aceptarla.
_G.GetQuestID = function() return 9001 end
_G.GetTitleText = function() return "Un encargo sencillo" end
_G.GetObjectiveText = function() return "" end
_G.GetQuestText = function() return "" end
_G.GetNumQuestChoices = function() return 1 end
_G.GetQuestItemLink = function() return "|Hitem:7001|h" end
_G.GetQuestItemInfo = function() return "Tunica", nil, 1, 3, true, 7001 end
local detailFrame = frameWith("QUEST_DETAIL")
check("frame de conocimiento registra QUEST_DETAIL", detailFrame ~= nil)
detailFrame.onEvent(detailFrame, "QUEST_DETAIL")
local seenRewards = CompaneroWoWDB.knowledge.quests[9001].rewards
eq("aprendido: recompensa de la mision vista", seenRewards and seenRewards.items[1].itemID, 7001)
eq("aprendido: la recompensa es elegible", seenRewards and seenRewards.items[1].choice, true)
S.native.lines = {}
res = runWindow()
local r7001 = nil
for _, r in ipairs(res.rewardItems) do
    if r.itemID == 7001 then r7001 = r end
end
eq("recompensa aprendida llega a los candidatos", r7001 and r7001.source, "aprendido")
S.native.lines = NATIVE.lines

-- Entrenadores: TRAINER_SHOW anota los servicios disponibles.
local kFrame = frameWith("TRAINER_SHOW")
check("frame de conocimiento registra TRAINER_SHOW", kFrame ~= nil)
kFrame.onEvent(kFrame, "TRAINER_SHOW")
local trainer = CompaneroWoWDB.knowledge.npcs[1234]
check("entrenador aprendido", trainer and trainer.isTrainer == true)
eq("entrenador: servicios disponibles", trainer and trainer.availableCount, 2)
eq("entrenador: nombres de servicios", trainer and trainer.services and #trainer.services, 2)
ns.ExportNativeNow()
local npc = char().native.npcs[1]
eq("exportacion native: PNJ aprendido con origen aprendido", npc and npc.source, "aprendido")
eq("exportacion native: rol entrenador", npc and npc.role, "trainer")

-- Marca: Blizzard primero.
ns.Marks.Command("")
check("marca: usa el paso siguiente de Blizzard", ns.Marks.mark ~= nil and ns.Marks.mark.mapID == 1429)
check("marca: posicion del paso de Blizzard", ns.Marks.mark and math.abs(ns.Marks.mark.x - 0.31) < 1e-6)
check("marca: avisa que el dato es del juego", anyPrinted("según el juego"), lastPrinted())

-- Sin paso de Blizzard, sin area y sin Questie: avisa, sin error ni marca nueva.
S.native.waypoints = {}
S.native.pois = {}
ns.Marks.mark = nil
ns.Marks.Command("")
check("marca sin datos: no marca", ns.Marks.mark == nil)
check("marca sin datos: avisa sin exigir Questie",
    anyPrinted("No sé dónde está el siguiente paso de «Peligro en la granja»"), lastPrinted())
check("marca sin datos: Questie opcional", anyPrinted("Questie no está instalado (es opcional)"), lastPrinted())

-- Dungeons sin FDJ: Encounter Journal si existe; si no, se omite.
local dungeons = G.BuildDungeons(9)
eq("mazmorras sin FDJ ni diario: no disponible", dungeons.available, false)
check("mazmorras sin FDJ ni diario: razon opcional", string.find(dungeons.reason or "", "opcional", 1, true) ~= nil)

scenario({ mapNames = NATIVE.mapNames, parents = NATIVE.parents, lines = NATIVE.lines,
    journal = { { instanceID = 1001, name = "Monasterio Escarlata" } } })
dungeons = G.BuildDungeons(9)
eq("diario de Blizzard: disponible", dungeons.available, true)
eq("diario de Blizzard: una mazmorra", #dungeons.list, 1)
eq("diario de Blizzard: origen juego", dungeons.list[1] and dungeons.list[1].source, "juego")
eq("diario de Blizzard: sin rango de nivel", dungeons.list[1] and dungeons.list[1].levelsKnown, false)
eq("diario de Blizzard: fuente juego en el resumen", dungeons.sources.juego, true)

-- ---------------------------------------------------------------------------
outln("Sin ninguna API nativa (juego sin misiones disponibles)")
scenario(false)
ns.ExportNativeNow()
eq("sin APIs: native no disponible", char().native.available, false)
local res0 = runWindow()
eq("sin APIs: ventana disponible", res0.block.available, true)
check("sin APIs: solo las misiones del registro (sin oferta del juego)", res0.block.quests[2001] ~= nil and res0.block.quests[9001] == nil)
eq("sin APIs: sin errores de Lua", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
outln("Con Questie instalado: fusion y marca nativa primero")
scenario(nil, true)
check("Questie instalado", N.QuestieInstalled() == true)
ns.ExportQuestieNow()
local qb = char().questie
eq("questie: disponible", qb.available, true)
eq("questie: fuente questie", qb.sources.questie, true)
eq("questie: fuente juego false en su bloque", qb.sources.juego, false)
local qq = nil
for _, q in pairs(qb.quests or {}) do qq = q break end
check("questie: misiones con origen questie", qq ~= nil and qq.source == "questie")
check("linea de fuentes: Questie instalado", string.find(N.SourcesStatusLine(), "Questie instalado", 1, true) ~= nil)

res = runWindow()
eq("ventana con Questie: fuente questie", res.block.sources.questie, true)
eq("ventana con Questie: fuente juego", res.block.sources.juego, true)
eq("ventana con Questie: 9001 (solo del juego) con origen juego", res.block.quests[9001] and res.block.quests[9001].source, "juego")
local anyQuestie = false
for _, q in pairs(res.block.quests) do
    if q.source == "questie" then anyQuestie = true end
end
check("ventana con Questie: hay misiones de Questie", anyQuestie)

ns.Marks.Command("")
check("marca con Questie: el paso de Blizzard gana", anyPrinted("según el juego"), lastPrinted())
check("marca con Questie: no usa Questie", not anyPrinted("según Questie"), lastPrinted())

S.native.waypoints = {}
S.native.pois = {}
ns.Marks.mark = nil
ns.Marks.Command("")
check("marca sin paso de Blizzard: cae a Questie", anyPrinted("según Questie"), lastPrinted())

-- ---------------------------------------------------------------------------
outln("Con ForeverDungeonJournal instalado: mazmorras y recompensas de FDJ")
scenario(nil, false)
_G.ForeverDungeonJournal_NS = {
    DB = { ["Test Dungeon"] = { level = "8-12", location = "Elwynn", entrance = { mapID = 1429, x = 0.5, y = 0.5 },
        quests = {}, bosses = {} } },
    ORDER = { "Test Dungeon" },
}
check("FDJ instalado", N.FdjInstalled() == true)
dungeons = G.BuildDungeons(9)
eq("FDJ: mazmorra en rango", #dungeons.list, 1)
eq("FDJ: origen fdj", dungeons.list[1] and dungeons.list[1].source, "fdj")
eq("FDJ: niveles conocidos", dungeons.list[1] and dungeons.list[1].levelsKnown, true)
eq("FDJ: fuente fdj en el resumen", dungeons.sources.fdj, true)
_G.ForeverDungeonJournal_NS = nil

outln("Orden de carga (toc)")
do
    local toc = assert(io.open("CompaneroWoW/CompaneroWoW.toc", "rb")):read("a")
    local iN = string.find(toc, "Native.lua", 1, true)
    local iK = string.find(toc, "Knowledge.lua", 1, true)
    local iQ = string.find(toc, "Questie.lua", 1, true)
    local iW = string.find(toc, "QuestWindow.lua", 1, true)
    check("toc: Native.lua antes que Knowledge, Questie y QuestWindow", iN and iK and iQ and iW and iN < iK and iN < iQ and iN < iW)
end

outln("")
if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
