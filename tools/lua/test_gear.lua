-- test_gear.lua: pruebas offline de Gear.lua y QuestWindow.lua (equipo, proficiencias, candidatos,
-- mazmorras, ventana de misiones), de la carga asincrona de objetos, de los topes, de la pausa en
-- combate y de la retirada de RXPGuides.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_gear.lua
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
local function count(t)
    local n = 0
    for _ in pairs(t or {}) do n = n + 1 end
    return n
end
local function sameSet(list, want)
    local a, b = {}, {}
    for _, v in ipairs(list or {}) do a[#a + 1] = tostring(v) end
    for _, v in ipairs(want) do b[#b + 1] = tostring(v) end
    table.sort(a)
    table.sort(b)
    return table.concat(a, ",") == table.concat(b, ",")
end
local function keysOf(t)
    local out = {}
    for k in pairs(t or {}) do out[#out + 1] = k end
    table.sort(out)
    return out
end

-- Captura de frames: Core y Gear registran sus eventos en frames propios.
local frames = {}
_G.CreateFrame = function()
    local f = { events = {} }
    function f:RegisterEvent(name) self.events[name] = true end
    function f:SetScript(name, fn) if name == "OnEvent" then self.onEvent = fn end end
    frames[#frames + 1] = f
    return f
end

local printed = {}
_G.print = function(msg) printed[#printed + 1] = tostring(msg) end

SlashCmdList = {}
CompaneroWoWDB = nil
W.installGameAPI()

local world = { loggedIn = true, exists = true }
_G.IsLoggedIn = function() return world.loggedIn end
_G.UnitExists = function(unit)
    if unit == "player" then return world.exists end
    return nil
end

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Collectors.lua")
load("CompaneroWoW/Native.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Questie.lua")
load("CompaneroWoW/QuestWindow.lua")
load("CompaneroWoW/Gear.lua")
load("CompaneroWoW/Marks.lua")

local gearFrame
for _, f in ipairs(frames) do
    if f.events.ITEM_DATA_LOAD_RESULT then gearFrame = f end
end
assert(gearFrame and gearFrame.onEvent, "Gear.lua no registro sus eventos")

local G = ns.Gear
local QW = ns.QuestWindow
local S = W.state
local KEY = "Tester-Ravencrest"
local function char() return CompaneroWoWDB and CompaneroWoWDB.characters[KEY] or nil end
local function fire(event, ...) gearFrame.onEvent(gearFrame, event, ...) end

-- Ejecuta ticks y temporizadores hasta que no queda trabajo (o hasta el limite).
local function settle(limit)
    S.clock = S.clock + 11 -- el throttle de exportacion es de 10 s: el reloj avanza entre pasadas
    for _ = 1, (limit or 5000) do
        W.tickAll()
        W.runTimers()
        if not G.Busy() and #S.timers == 0 then break end
    end
end

-- Estado de partida: dentro del mundo, sacerdote humano nivel 19, Alianza, misiones completadas 3011 y 3100.
local function setup(opts)
    W.reset()
    W.uninstallAddons()
    W.uninstallGear()
    world.loggedIn, world.exists = true, true
    CompaneroWoWDB = nil
    G.ResetForTests()
    W.installGear(opts)
    S.flagged[3100] = true
    S.flagged[3011] = true
end

-- ---------------------------------------------------------------------------
outln("reglas de accesibilidad (QuestWindow.Evaluate):")
do
    local me = { level = 19, faction = "Alliance", raceBit = 1, classBit = 16, completed = { [120] = true } }
    local ev = QW.Evaluate({ requiredLevel = 10, preQuestSingle = { 120, 121 } }, me)
    eq("single: basta una completada -> accesible", ev.accessibleNow, true)
    eq("single: sin faltantes si una cumple", #ev.missing, 0)
    local ev2 = QW.Evaluate({ requiredLevel = 10, preQuestSingle = { 130, 131 } }, me)
    eq("single: ninguna completada -> no accesible", ev2.accessibleNow, false)
    check("single: faltan todas las alternativas", sameSet(ev2.missing, { 130, 131 }))

    local ev3 = QW.Evaluate({ requiredLevel = 10, preQuestGroup = { 1, 2 } }, { level = 19, completed = { [1] = true } })
    eq("group: falta una -> no accesible", ev3.accessibleNow, false)
    check("group: falta solo la no completada", sameSet(ev3.missing, { 2 }))
    local ev4 = QW.Evaluate({ requiredLevel = 10, preQuestGroup = { 1, 2 } }, { level = 19, completed = { [1] = true, [2] = true } })
    eq("group: todas completadas -> accesible", ev4.accessibleNow, true)

    local exclusive = { [7] = { 8 } }
    local meX = { level = 19, completed = { [8] = true }, exclusiveOf = function(id) return exclusive[id] or {} end }
    eq("group: exclusiva completada cuenta como cumplida", QW.Evaluate({ requiredLevel = 1, preQuestGroup = { 7 } }, meX).accessibleNow, true)
    local evNeg = QW.Evaluate({ requiredLevel = 1, preQuestGroup = { -7 } }, meX)
    eq("group: id negativo no admite la exclusiva", evNeg.accessibleNow, false)
    check("group: id negativo aparece como faltante", sameSet(evNeg.missing, { 7 }))

    eq("nivel: requiredLevel > nivel -> no accesible", QW.Evaluate({ requiredLevel = 20 }, me).accessibleNow, false)
    eq("completada: exclusiva ya hecha bloquea", QW.Evaluate({ exclusiveTo = { 120 } }, me).allowed, false)

    -- Raza / faccion / clase con mascaras de Forever.
    eq("faccion: mascara Alianza para Horda -> no permitida", QW.RaceAllowed(W.RACE_ALLIANCE, 2, "Horde"), false)
    eq("faccion: mascara Alianza para Alianza humano -> permitida", QW.RaceAllowed(W.RACE_ALLIANCE, 1, "Alliance"), true)
    eq("raza: mascara Horda para humano -> no permitida", QW.RaceAllowed(W.RACE_HORDE, 1, "Alliance"), false)
    eq("raza: sin mascara -> permitida", QW.RaceAllowed(0, 1, "Alliance"), true)
    eq("clase: guerrero (1) para sacerdote (16) -> no", QW.MaskHas(1, 16), false)
    eq("clase: sacerdote (16) para sacerdote (16) -> si", QW.MaskHas(16, 16), true)
    eq("mascara de 33 bits (Forever) -> bit alto", QW.MaskHas(4294967296 + 1, 4294967296), true)
    eq("clase: 2^(classID-1)", QW.ClassBit(5), 16)
end

outln("limites de la ventana (QuestWindow.InWindow):")
eq("nivel 14 entra (nivel-5)", QW.InWindow(14, 5, 19), true)
eq("nivel 13 fuera", QW.InWindow(13, 5, 19), false)
eq("requisito 22 entra (nivel+3)", QW.InWindow(19, 22, 19), true)
eq("requisito 23 fuera", QW.InWindow(19, 23, 19), false)
eq("sin nivel de mision: fuera", QW.InWindow(nil, 5, 19), false)

-- ---------------------------------------------------------------------------
outln("exportacion del personaje (sacerdote humano, nivel 19):")
setup()
G.FlushNow()
local c = char()
check("exportacion: personaje guardado", c ~= nil)
check("gear: slots 5, 15, 16 con objeto", c.gear[5] ~= nil and c.gear[15] ~= nil and c.gear[16] ~= nil)
eq("gear[5] slot CHEST", c.gear[5].slot, "CHEST")
eq("gear[5] clase Armor", c.gear[5].class, "Armor")
eq("gear[5] subclase Cloth", c.gear[5].subclass, "Cloth")
eq("gear[5] ilvl 22", c.gear[5].itemLevel, 22)
eq("gear[5] stats keys tal cual", c.gear[5].stats.ITEM_MOD_INTELLECT_SHORT, 5)
eq("gear[16] arma Daggers", c.gear[16].subclass, "Daggers")
eq("gear: contados 3 equipados", count(c.gear), 3)

eq("proficiencias: armadura Cloth", table.concat(c.proficiencies.armor, ","), "Cloth")
check("proficiencias: armas (una lista)", sameSet(c.proficiencies.weapons,
    { "One-Handed Maces", "Staves", "Daggers", "Wands" }), table.concat(c.proficiencies.weapons, ","))
eq("proficiencias: armorLoc localizado", c.proficiencies.armorLoc[1], "Cloth")
eq("proficiencias: sin dual wield", c.proficiencies.dualWield, false)
eq("completedQuests: lista ordenada", c.completedQuests, "3011,3100")

local qw = c.questWindow
eq("questWindow disponible", qw.available, true)
eq("questWindow nivel 19", qw.level, 19)
eq("questWindow minLevel 14", qw.minLevel, 14)
eq("questWindow maxLevel 22", qw.maxLevel, 22)
local want = { 3001, 3002, 3003, 3004, 3006, 3012, 3013, 3014 }
check("ventana: solo misiones permitidas no completadas y en rango", sameSet(keysOf(qw.quests), want),
    table.concat(keysOf(qw.quests), ","))
eq("3005 fuera (clase guerrero)", qw.quests[3005], nil)
eq("3007 fuera (raza de Horda)", qw.quests[3007], nil)
eq("3010 fuera (exclusiva completada)", qw.quests[3010], nil)
eq("3011 fuera (completada)", qw.quests[3011], nil)
eq("3008 fuera (requisito 30)", qw.quests[3008], nil)
eq("3009 fuera (nivel 2)", qw.quests[3009], nil)
eq("3001 accesible ahora", qw.quests[3001].accessibleNow, true)
eq("3002 pronta (falta 3001)", qw.quests[3002].accessibleNow, false)
check("3002 faltante = {3001}", sameSet(qw.quests[3002].missingPrereqs, { 3001 }))
check("3003 faltante = {3001}", sameSet(qw.quests[3003].missingPrereqs, { 3001 }))
eq("3004 accesible (3100 completada)", qw.quests[3004].accessibleNow, true)
check("3012 faltante = {3200}", sameSet(qw.quests[3012].missingPrereqs, { 3200 }))
eq("3013 pronta por nivel", qw.quests[3013].accessibleNow, false)
eq("3001 zona Elwynn", qw.quests[3001].zone, "Elwynn")
eq("3001 inicio: PNJ 5001", qw.quests[3001].starter.id, 5001)
eq("3001 inicio: uiMapID 1429", qw.quests[3001].starter.mapID, 1429)
eq("3001 inicio: x 0.45", qw.quests[3001].starter.x, 0.45)
eq("3001 inicio: y 0.6", qw.quests[3001].starter.y, 0.6)
eq("3001 fin: PNJ 5001", qw.quests[3001].finisher.id, 5001)
eq("3014 sin inicio", qw.quests[3014].starter, nil)
eq("3001 recompensa: elegir 4101/4102", table.concat(qw.quests[3001].rewards.choice, ","), "4101,4102")
eq("3002 recompensa fija 4103", table.concat(qw.quests[3002].rewards.fixed, ","), "4103")
eq("3001 (FDJ) sin marca de Questie", qw.quests[3001].rewards.rewardsKnownAs, nil)

-- Indice inverso de Questie (item.questRewards) para misiones sin datos de FDJ.
check("3003 (Questie): elige entre 4113 y 4114 (sin 4116 de nivel 30)",
    sameSet(qw.quests[3003].rewards.choice, { 4113, 4114 }), table.concat(qw.quests[3003].rewards.choice, ","))
eq("3003 (Questie): marca questie-reverse", qw.quests[3003].rewards.rewardsKnownAs, "questie-reverse")
eq("3003 (Questie): sin recompensa fija", #qw.quests[3003].rewards.fixed, 0)
check("3013 (Questie): 4117 de nivel 22 entra", sameSet(qw.quests[3013].rewards.choice, { 4117 }))
check("3012 (Questie): objeto sin requiredLevel en la ficha se conserva",
    sameSet(qw.quests[3012].rewards.choice, { 4118 }))

local cand = c.gearCandidates
eq("candidatos disponibles", cand.available, true)
eq("candidatos pendientes: 2 (uncached)", cand.pending, 2)
check("candidatos: sin los no cargados aun (incluye recompensas de Questie usables)",
    sameSet(keysOf(cand.items), { 4101, 4103, 4106, 4109, 4113, 4117, 4118 }),
    table.concat(keysOf(cand.items), ","))
check("candidato 4113: fuente de mision de Questie sin choice",
    cand.items[4113] and cand.items[4113].sources[1].kind == "quest" and cand.items[4113].sources[1].questID == 3003
    and cand.items[4113].sources[1].choice == nil)
check("candidato 4101: fuente de mision eligible", cand.items[4101] and cand.items[4101].sources[1].kind == "quest"
    and cand.items[4101].sources[1].questID == 3001 and cand.items[4101].sources[1].choice == true)
eq("candidato 4109 clase Weapon", cand.items[4109].class, "Weapon")
eq("Questie filtra consumibles sin cargarlos", (W.GEAR.infoCalls[4108] or 0), 0)

local dg = c.dungeons
eq("mazmorras disponibles", dg.available, true)
eq("mazmorras: 2 en rango (Cripta Alta fuera)", #dg.list, 2)
eq("mazmorra 1: mina-prueba", dg.list[1].key, "mina-prueba")
eq("mazmorra 1: rango 15-24", dg.list[1].minLevel .. "-" .. dg.list[1].maxLevel, "15-24")
eq("mazmorra 1: entrada uiMapID 1429", dg.list[1].mapID, 1429)
eq("mazmorra 1: jefe A con botin", dg.list[1].bosses[1].loot[1], 4105)
eq("mazmorra 2: cueva-baja", dg.list[2].key, "cueva-baja")

-- Estado de /compa estado.
local lines = G.StatusLines()
eq("estado: cuatro lineas", #lines, 4)
check("estado: misiones accesibles ahora / pronto", string.find(lines[2], "4 accesibles ahora, 4 pronto", 1, true) ~= nil, lines[2])
check("estado: candidatos con pendientes", string.find(lines[3], "pendientes de cargar", 1, true) ~= nil, lines[3])
check("estado: equipo y mazmorras", string.find(lines[1], "3 objetos equipados", 1, true) ~= nil
    and string.find(lines[4], "2", 1, true) ~= nil, lines[1] .. " | " .. lines[4])

-- ---------------------------------------------------------------------------
outln("reglas de candidatos (Gear.IsUsable):")
do
    local prof = { armor = { "Cloth" }, weapons = { "Daggers", "Staves", "One-Handed Maces", "Wands" }, dualWield = false }
    eq("tela usable", G.IsUsable({ classID = 4, subclassID = 1, equipLoc = "INVTYPE_CHEST" }, prof), true)
    eq("placas no usable", G.IsUsable({ classID = 4, subclassID = 4, equipLoc = "INVTYPE_CHEST" }, prof), false)
    eq("escudo no usable", G.IsUsable({ classID = 4, subclassID = 6, equipLoc = "INVTYPE_SHIELD" }, prof), false)
    eq("capa (miscelanea) usable", G.IsUsable({ classID = 4, subclassID = 0, equipLoc = "INVTYPE_CLOAK" }, prof), true)
    eq("hacha no usable", G.IsUsable({ classID = 2, subclassID = 0, equipLoc = "INVTYPE_WEAPONMAINHAND" }, prof), false)
    eq("daga usable", G.IsUsable({ classID = 2, subclassID = 15, equipLoc = "INVTYPE_WEAPON" }, prof), true)
    eq("daga de mano izquierda sin doble empunadura: no", G.IsUsable({ classID = 2, subclassID = 15, equipLoc = "INVTYPE_WEAPONOFFHAND" }, prof), false)
    prof.dualWield = true
    eq("daga de mano izquierda con doble empunadura: si", G.IsUsable({ classID = 2, subclassID = 15, equipLoc = "INVTYPE_WEAPONOFFHAND" }, prof), true)
    eq("sin hueco de equipo: no", G.IsUsable({ classID = 0, subclassID = 0, equipLoc = "" }, prof), false)
end

-- Doble empunadura real: la proficiencia sale de C_SpellBook.IsSpellKnown(674).
setup()
W.GEAR.dualWield = true
G.FlushNow()
check("dual wield: 4111 aparece como candidata", char().gearCandidates.items[4111] ~= nil)

-- ---------------------------------------------------------------------------
outln("objetos sin cargar (carga asincrona):")
setup()
G.FlushNow()
eq("no bloquea: la exportacion termina con pendientes", char().gearCandidates.pending, 2)
check("pide la carga de 4105", W.GEAR.requested[4105] == true)
check("pide la carga de 4112", W.GEAR.requested[4112] == true)
W.loadItem(4105)
fire("ITEM_DATA_LOAD_RESULT", 4105, true)
settle()
check("tras ITEM_DATA_LOAD_RESULT: 4105 entra", char().gearCandidates.items[4105] ~= nil)
eq("pendientes: 1 tras la primera carga", char().gearCandidates.pending, 1)
W.loadItem(4112)
fire("ITEM_DATA_LOAD_RESULT", 4112, true)
settle()
check("tras la segunda carga: 4112 entra", char().gearCandidates.items[4112] ~= nil)
eq("sin pendientes al final", char().gearCandidates.pending, nil)
eq("total de candidatos: 9", count(char().gearCandidates.items), 9)

setup()
G.FlushNow()
fire("ITEM_DATA_LOAD_RESULT", 4105, false)
settle()
eq("carga fallida: el objeto se descarta sin error", char().gearCandidates.items[4105], nil)
eq("carga fallida: solo 4112 sigue pendiente", char().gearCandidates.pending, 1)

-- ---------------------------------------------------------------------------
-- ---------------------------------------------------------------------------
outln("indice inverso de Questie (item.questRewards):")
local function hasItem(list, id)
    for _, e in ipairs(list or {}) do
        if e.itemID == id then return true end
    end
    return false
end
setup()
G.FlushNow()
local rev = ns.QuestWindow.reverse
check("indice: construido en la exportacion", rev ~= nil)
eq("indice: 3003 tiene 3 objetos de equipo", rev and #rev.byQuest[3003], 3)
check("indice: el pergamino (consumible) no entra", rev and not hasItem(rev.byQuest[3003], 4115))
local function entryOf(list, id)
    for _, e in ipairs(list or {}) do
        if e.itemID == id then return e end
    end
    return nil
end
local e4116 = rev and entryOf(rev.byQuest[3003], 4116)
check("indice: 4116 entra con requiredLevel 30 de su ficha", e4116 ~= nil and e4116.requiredLevel == 30)
check("indice: se leyo questRewards", (W.GEAR.questRewardReads or 0) > 0)
W.GEAR.questRewardReads = 0
G.FlushNow()
eq("indice: segunda exportacion de la sesion no lo vuelve a leer", W.GEAR.questRewardReads or 0, 0)
check("indice: la sesion lo conserva", ns.QuestWindow.reverse == rev)

-- Se construye en trozos: un tick con presupuesto corto no lo termina.
setup({ bulkItems = 320 })
S.msStep = 1
fire("PLAYER_LEVEL_UP")
W.tickAll()
check("indice: un tick corto no lo termina", ns.QuestWindow.reverse == nil and G.Busy())
S.msStep = 0
settle()
check("indice: se completa en varios ticks", ns.QuestWindow.reverse ~= nil)

-- ---------------------------------------------------------------------------
outln("topes (400 misiones, 300 candidatos, 6 mazmorras) y tamano:")
setup({ bulkQuests = 450, bulkItems = 320, bulkDungeons = 8 })
G.FlushNow()
c = char()
eq("ventana: tope de 400 misiones", count(c.questWindow.quests), 400)
eq("candidatos: tope de 300", count(c.gearCandidates.items), 300)
eq("mazmorras: tope de 6", #c.dungeons.list, 6)
local bytes = QW.ApproxBytes(c.gear) + QW.ApproxBytes(c.proficiencies) + QW.ApproxBytes(c.completedQuests)
    + QW.ApproxBytes(c.questWindow) + QW.ApproxBytes(c.gearCandidates) + QW.ApproxBytes(c.dungeons)
outln(string.format("  info  tamano estimado de bloques de equipo al tope: %.1f KB", bytes / 1024))
check("tamano: bloques de equipo < 300 KB (deja margen a 400 KB totales)", bytes < 300 * 1024,
    string.format("%.1f KB", bytes / 1024))
check("tamano: cada bloque dentro de su limite", QW.ApproxBytes(c.questWindow) <= QW.MAX_BYTES + 1024)

-- ---------------------------------------------------------------------------
outln("combate y tiempo por tick:")
setup({ bulkQuests = 450 })
S.msStep = 1 -- cada lectura de reloj cuenta 1 ms: el presupuesto de 4 ms corta el trabajo
fire("PLAYER_LEVEL_UP")
W.tickAll()
check("presupuesto: un tick no termina el trabajo grande", char() == nil or char().questWindow == nil)
check("presupuesto: sigue pendiente tras el primer tick", G.Busy())
S.inCombat = true
for _ = 1, 20 do W.tickAll() end
check("combate: el trabajo se pausa (no escribe)", char() == nil or char().questWindow == nil)
S.inCombat = false
fire("PLAYER_REGEN_ENABLED")
S.msStep = 0
settle()
check("fuera de combate: el trabajo se reanuda y escribe", char().questWindow ~= nil)
S.msStep = 0

setup()
S.inCombat = true
G.RequestExport()
W.tickAll()
check("combate: pedir exportacion no escribe", char() == nil or char().gear == nil)
S.inCombat = false
fire("PLAYER_REGEN_ENABLED")
settle()
check("combate: al salir se exporta", char().gear ~= nil)

-- Login: exportacion tras la espera (Questie y addons listos).
setup()
fire("PLAYER_LOGIN")
check("login: programa la exportacion tras 5 s", #W.callsNamed("C_Timer.After") >= 1)
settle()
check("login: exporta tras la espera", char() ~= nil and char().gear ~= nil)

-- Sin Questie: la ventana sale del juego (available = true, sin fuente Questie), no se queda la antigua.
setup()
G.FlushNow()
_G.QuestieLoader = nil
G.RequestExport()
G.FlushNow()
check("sin Questie: la ventana sigue disponible (juego y aprendido)", char().questWindow.available == true)
eq("sin Questie: la fuente questie queda en false", char().questWindow.sources.questie, false)
_G.QuestieLoader = nil

-- ---------------------------------------------------------------------------
outln("ForeverDungeonJournal ausente:")
setup()
_G.ForeverDungeonJournal_NS = nil
G.FlushNow()
eq("sin FDJ: mazmorras no disponibles", char().dungeons.available, false)
check("sin FDJ: motivo", type(char().dungeons.reason) == "string")
check("sin FDJ: la ventana sigue", char().questWindow.available == true)

-- ---------------------------------------------------------------------------
outln("RXPGuides retirado:")
local function read(path)
    local f = assert(io.open(path, "rb"))
    local s = f:read("*a")
    f:close()
    return s
end
for _, file in ipairs({ "CompaneroWoW/Integrations.lua", "CompaneroWoW/Core.lua", "CompaneroWoW/Marks.lua",
                        "CompaneroWoW/CompaneroWoW.toc" }) do
    local src = read(file)
    check("sin RXP en " .. file, not string.find(src, "RXP", 1, true))
end
check("sin guia de marca en Marks", not string.find(read("CompaneroWoW/Marks.lua"), "MarkGuide", 1, true))
local parsed = ns.Marks.ParseArgs("guia", nil)
eq("/compa marca guia: error de uso", parsed.kind, "error")
local toc = read("CompaneroWoW/CompaneroWoW.toc")
check("toc: OptionalDeps con ForeverDungeonJournal", string.find(toc, "ForeverDungeonJournal", 1, true) ~= nil)
local iQ = string.find(toc, "Questie.lua", 1, true)
local iW = string.find(toc, "QuestWindow.lua", 1, true)
local iG = string.find(toc, "Gear.lua", 1, true)
check("toc: QuestWindow.lua y Gear.lua tras Questie.lua", iQ and iW and iG and iQ < iW and iW < iG)

-- ---------------------------------------------------------------------------
if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
