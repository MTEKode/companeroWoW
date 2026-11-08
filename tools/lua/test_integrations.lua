-- test_integrations.lua: pruebas offline de Integrations.lua (WhatsTraining, RXPGuides, SkillUpForever),
-- /compa estado, /compa marca guia, y de las correcciones de auditoria (repintado de la franja,
-- throttle de QUEST_LOG_UPDATE, tope de knowledge.npcs, contador de errores silenciados).
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_integrations.lua
package.path = "tools/lua/?.lua;" .. package.path
local W = require("wowstub")

-- Captura del chat (ns.Say -> print).
local printed = {}
local function outln(s) io.write(tostring(s), "\n") end
_G.print = function(msg) printed[#printed + 1] = tostring(msg) end
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
load("CompaneroWoW/Knowledge.lua")
load("CompaneroWoW/Events.lua")
load("CompaneroWoW/Integrations.lua")
load("CompaneroWoW/Marks.lua")

local S = W.state
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

-- Reinicia el estado de prueba. El reloj nunca retrocede (los modulos guardan tiempos de la sesion).
local clock = 1000
local function reset(addonOpts)
    W.reset()
    W.uninstallAddons() -- sin esto, un addon instalado antes seguiria presente
    clock = clock + 1000
    S.clock = clock
    S.now = 1700000000 + clock
    W.installAddons(addonOpts)
    W.calls = {}
end
local function advance(sec)
    clock = clock + sec
    S.clock = clock
    S.now = S.now + sec
end

local KEY = "Tester-Ravencrest"
local function char() return CompaneroWoWDB.characters[KEY] end

-- Tamano aproximado (cadenas y claves) para comprobar el limite de 20 KB por bloque.
local function approxBytes(v)
    local t = type(v)
    if t == "string" then return #v + 2 end
    if t == "number" or t == "boolean" then return 8 end
    if t ~= "table" then return 0 end
    local n = 0
    for k, x in pairs(v) do n = n + approxBytes(k) + approxBytes(x) end
    return n
end

-- ---------------------------------------------------------------------------
-- 1) Forma del contrato con los tres addons presentes
-- ---------------------------------------------------------------------------
outln("contrato:")
reset()
ns.EnsureDB()
ns.ExportIntegrationsNow()
local c = char()
check("exporta la entrada del personaje", type(c) == "table")

local tr = c.training
eq("training.available", tr.available, true)
eq("training.source", tr.source, "WhatsTraining")
eq("training.exportedAt", tr.exportedAt, S.now)
eq("training.availableCount", tr.availableCount, 3)
eq("training: 6 exportados (KNOWN e IGNORED fuera)", #tr.spells, 6)
eq("training: primero el disponible de menor nivel", tr.spells[1].spellID, 202)
eq("training: orden disponibles por nivel (2)", tr.spells[2].spellID, 203)
eq("training: orden disponibles por nivel (3)", tr.spells[3].spellID, 201)
eq("training: despues el siguiente nivel", tr.spells[4].status, "NEXTLEVEL")
eq("training: otros estados -> OTHER", tr.spells[6].status, "OTHER")
eq("training: coste en cobre", tr.spells[3].cost, 1200)
eq("training: nombre", tr.spells[3].name, "Golpe heroico")
eq("training: rango en espanol", tr.spells[3].rank, "Rango 2")
eq("training: nivel", tr.spells[3].level, 18)
check("training: rango ausente -> nil", tr.spells[1].rank == nil)

local g = c.guide
eq("guide.available", g.available, true)
eq("guide.source", g.source, "RXPGuides")
eq("guide.group", g.group, "Leveling")
eq("guide.guideName", g.guideName, "Elwynn 1-10")
eq("guide.stepIndex", g.stepIndex, 12)
eq("guide.stepId", g.stepId, "s12")
eq("guide: sin coordenadas si RXP no las expone", g.stepMapID, nil)
eq("guide: sin stepText si RXP no lo expone", g.stepText, nil)

local p = c.profPlan
eq("profPlan.available", p.available, true)
eq("profPlan.source", p.source, "SkillUpForever")
eq("profPlan: una profesion", #p.professions, 1)
local prof = p.professions[1]
eq("prof.skillLineID", prof.skillLineID, 197)
eq("prof.name", prof.name, "Sastrería")
eq("prof.rank", prof.rank, 68)
eq("prof.maxRank", prof.maxRank, 75)
eq("prof: dos pasos", #prof.nextSteps, 2)
eq("step.recipe", prof.nextSteps[1].recipe, "Túnica de lino")
eq("step.fromSkill", prof.nextSteps[1].fromSkill, 68)
eq("step.toSkill", prof.nextSteps[1].toSkill, 72)
eq("step.materials: itemID", prof.nextSteps[1].materials[1].itemID, 2589)
eq("step.materials: nombre localizado", prof.nextSteps[1].materials[1].name, "Tela de lino")
eq("step.materials: cantidad", prof.nextSteps[1].materials[1].count, 6)
check("step.materials: sin nombre -> nil", prof.nextSteps[1].materials[2].name == nil)
eq("shopping: solo lo que falta", #prof.shopping, 1)
eq("shopping: itemID", prof.shopping[1].itemID, 2589)
eq("shopping: cantidad = missing", prof.shopping[1].count, 30)
check("RequiredMaterials llamado con receta y cantidad",
    #W.callsNamed("SkillUpForever.RequiredMaterials") == 2
    and W.callsNamed("SkillUpForever.RequiredMaterials")[1].args[1] == 3908
    and W.callsNamed("SkillUpForever.RequiredMaterials")[1].args[2] == 3)

check("health: luaErrors numerico", type(c.health) == "table" and type(c.health.luaErrors) == "number")
check("bloques < 20 KB", approxBytes(c.training) < 20480 and approxBytes(c.guide) < 20480
    and approxBytes(c.profPlan) < 20480)

-- ---------------------------------------------------------------------------
-- 2) Topes
-- ---------------------------------------------------------------------------
outln("topes:")
reset({ wtSpells = W.manySpells(60) })
ns.ExportIntegrationsNow()
eq("training: como mucho 40 hechizos", #char().training.spells, 40)
eq("training: availableCount cuenta todos", char().training.availableCount, 60)
local sorted = true
for i = 2, #char().training.spells do
    if char().training.spells[i - 1].level > char().training.spells[i].level then sorted = false end
end
check("training: disponibles ordenados por nivel", sorted)

local mixed = W.manySpells(45)
for i = 1, 10 do
    mixed[#mixed + 1] = { id = 7000 + i, name = "Siguiente " .. i, level = 1, cost = 0, status = W.WT_STATUS.NEXTLEVEL }
end
reset({ wtSpells = mixed })
ns.ExportIntegrationsNow()
local allAvail = true
for _, sp in ipairs(char().training.spells) do
    if sp.status ~= "AVAILABLE" then allAvail = false end
end
check("training: los disponibles van antes aunque el siguiente nivel sea menor", allAvail)

-- Mas de 8 profesiones y muchos materiales: el bloque se mantiene dentro de 20 KB.
reset()
local bigProfs = {}
for i = 1, 8 do
    local recipes = {}
    for r = 1, 5 do
        recipes[r] = { spellID = 9000 + i * 10 + r, name = string.rep("R", 40) .. i .. r, count = 5, fromRank = 1, toRank = 2 }
    end
    bigProfs[i] = { name = "Prof" .. i, skillLineID = 500 + i, rank = 1, maxRank = 75, recipes = recipes }
end
W.state.addons.profs = bigProfs
ns.ExportIntegrationsNow()
check("profPlan: como mucho 8 profesiones", #char().profPlan.professions <= 8)
check("profPlan: bloque < 20 KB", approxBytes(char().profPlan) < 20480, tostring(approxBytes(char().profPlan)))
check("profPlan: pasos como mucho 5", #char().profPlan.professions[1].nextSteps <= 5)

-- ---------------------------------------------------------------------------
-- 3) Addons ausentes o no listos -> available = false, nunca error
-- ---------------------------------------------------------------------------
outln("addons ausentes:")
reset({ noWT = true, noRXP = true, noSkillUp = true })
ns.ExportIntegrationsNow()
c = char()
eq("sin WhatsTraining: available=false", c.training.available, false)
check("sin WhatsTraining: motivo", type(c.training.reason) == "string" and string.find(c.training.reason, "WhatsTraining", 1, true) ~= nil)
eq("sin RXPGuides: available=false", c.guide.available, false)
eq("sin SkillUpForever: available=false", c.profPlan.available, false)
check("sin addons: sigue habiendo health", type(c.health) == "table")

reset({ wtReady = false })
ns.ExportIntegrationsNow()
eq("WhatsTraining no listo: available=false", char().training.available, false)

reset()
_G.RXPCData = nil
ns.ExportIntegrationsNow()
eq("RXPCData ausente: available=false", char().guide.available, false)
reset()
_G.RXPCData.currentGuideName = nil
ns.ExportIntegrationsNow()
eq("sin guia activa: available=false", char().guide.available, false)

reset()
_G.SkillUpForever.API.version = 2
ns.ExportIntegrationsNow()
eq("version de API no soportada: available=false", char().profPlan.available, false)

-- ---------------------------------------------------------------------------
-- 4) Throttle, combate y disparadores
-- ---------------------------------------------------------------------------
outln("exportacion:")
reset()
local A = W.state.addons
A.skillCalls = 0
ns.RequestIntegrationsExport()
eq("primera peticion exporta", A.skillCalls, 1)
advance(1)
ns.RequestIntegrationsExport()
ns.RequestIntegrationsExport()
ns.RequestIntegrationsExport()
eq("peticiones dentro de 10 s no exportan", A.skillCalls, 1)
eq("una sola espera diferida", #W.callsNamed("C_Timer.After"), 1)
advance(9)
W.runTimers()
eq("la espera diferida exporta al cumplirse el plazo", A.skillCalls, 2)

-- /compa sync exporta sin throttle.
ns.RequestIntegrationsExport()
local before = A.skillCalls
SlashCmdList.COMPA("sync")
check("/compa sync exporta sin esperar al throttle", A.skillCalls == before + 1)
check("/compa sync recarga la interfaz", #W.callsNamed("ReloadUI") == 1)

-- Combate: no exporta; al salir de combate se hace la exportacion pendiente.
advance(60)
A.skillCalls = 0
S.inCombat = true
check("en combate no se exporta", ns.RequestIntegrationsExport() == false and A.skillCalls == 0)
check("en combate ExportIntegrationsNow devuelve nil", ns.ExportIntegrationsNow() == nil)
S.inCombat = false
ns.Integrations.OnEvent("PLAYER_REGEN_ENABLED")
eq("al salir de combate exporta lo pendiente", A.skillCalls, 1)

-- Eventos: nivel, habilidades, cierre del entrenador, WhatsTraining Ready.
advance(60)
A.skillCalls = 0
ns.Integrations.OnEvent("PLAYER_LEVEL_UP", 10)
eq("PLAYER_LEVEL_UP exporta", A.skillCalls, 1)
advance(60)
ns.Integrations.OnEvent("SKILL_LINES_CHANGED")
eq("SKILL_LINES_CHANGED exporta", A.skillCalls, 2)
advance(60)
ns.Integrations.OnEvent("TRAINER_CLOSED")
eq("TRAINER_CLOSED exporta", A.skillCalls, 3)

-- Login: registra callbacks de WhatsTraining, arranca el sondeo de RXP y exporta tras la espera.
advance(60)
A.skillCalls = 0
W.calls = {}
ns.Integrations.OnEvent("PLAYER_LOGIN")
check("login: registra Ready y SpellsChanged de WT_API",
    #W.callsNamed("WT_API.RegisterCallback") == 2)
local tick = W.callsNamed("C_Timer.NewTicker")
check("login: sondeo de RXP cada 5 s", #tick == 1 and tick[1].args[1] == 5, tick[1] and tostring(tick[1].args[1]) or "sin ticker")
local esperas = {}
for _, t in ipairs(W.callsNamed("C_Timer.After")) do esperas[#esperas + 1] = t.args[1] end
check("login: reintento a los 3 s y a los 15 s", table.concat(esperas, ",") == "3,15", table.concat(esperas, ","))
eq("login: el sondeo de RXP existe", W.activeTickers(), 1)
W.runTimers()
eq("login: exportacion tras la espera", A.skillCalls, 1)

-- Sondeo: sin cambios no exporta; un cambio de paso exporta (throttle respetado).
advance(60)
A.skillCalls = 0
W.tickAll()
eq("sondeo sin cambios no exporta", A.skillCalls, 0)
_G.RXPCData.currentStep = 13
_G.RXPCData.currentStepId = "s13"
W.tickAll()
eq("sondeo con cambio de paso exporta", A.skillCalls, 1)
eq("el paso exportado es el nuevo", char().guide.stepIndex, 13)

advance(60)
A.skillCalls = 0
W.fireWT("Ready")
eq("WhatsTraining Ready exporta", A.skillCalls, 1)

-- ---------------------------------------------------------------------------
-- 5) /compa estado y /compa marca guia
-- ---------------------------------------------------------------------------
outln("estado y marcas:")
reset()
SlashCmdList.COMPA("sync")
printed = {}
SlashCmdList.COMPA("estado")
check("estado: WhatsTraining con hechizos disponibles", anyPrinted("WhatsTraining: 3 hechizos disponibles"))
check("estado: RXP con guia y paso", anyPrinted("RXP: guía «Elwynn 1-10», paso 12"))
check("estado: SkillUpForever con profesiones", anyPrinted("SkillUpForever: 1 profesiones"))
check("estado: errores capturados", anyPrinted("Errores capturados: "))

reset({ noWT = true, noRXP = true, noSkillUp = true })
SlashCmdList.COMPA("sync")
printed = {}
SlashCmdList.COMPA("estado")
check("estado sin addons: indica no disponible", anyPrinted("WhatsTraining: no disponible")
    and anyPrinted("RXP: no disponible") and anyPrinted("SkillUpForever: no disponible"))

-- Parseo de "guia" y "guía".
eq("ParseArgs guia", ns.Marks.ParseArgs("guia").kind, "guide")
eq("ParseArgs guía", ns.Marks.ParseArgs("guía").kind, "guide")

-- Sin coordenadas del paso: aviso y ninguna marca.
reset()
ns.EnsureDB()
printed = {}
SlashCmdList.COMPA("marca guia")
check("marca guia sin coordenadas: avisa", anyPrinted("No hay coordenadas del paso de RXP"))
eq("marca guia sin coordenadas: no pone marca", S.userWaypoint, nil)
eq("marca guia sin coordenadas: no guarda marca", ns.Marks.mark, nil)

-- Con coordenadas (fuente simulada): marca con la API de Marks.
local origStep = ns.Integrations.GuideStep
ns.Integrations.GuideStep = function() return { mapID = 1429, x = 0.45, y = 0.67, text = "Paso 12" } end
reset()
ns.EnsureDB()
ns.ExportIntegrationsNow()
eq("guide exporta coordenadas cuando existen", char().guide.stepMapID, 1429)
eq("guide exporta stepX", char().guide.stepX, 0.45)
eq("guide exporta stepText", char().guide.stepText, "Paso 12")
printed = {}
SlashCmdList.COMPA("marca guia")
check("marca guia con coordenadas: pone la marca", S.userWaypoint ~= nil)
eq("marca guia: mapa de la marca", ns.Marks.mark and ns.Marks.mark.mapID, 1429)
check("marca guia: confirma en el chat", anyPrinted("Marca puesta en Elwynn"))
ns.Integrations.GuideStep = origStep

-- ---------------------------------------------------------------------------
-- 6) Auditoria: repintado de la franja solo si cambian los bytes
-- ---------------------------------------------------------------------------
outln("auditoria:")
reset()
ns.EnsureDB()
ns.InitBridge()
local function setColorCount()
    return #W.callsNamed("SetColorTexture")
end
W.calls = {}
ns.BridgeRefresh()
local first = setColorCount()
eq("franja: primer refresco pinta los 29 bloques", first, 29)
W.calls = {}
ns.BridgeRefresh()
ns.BridgeRefresh()
eq("franja: refrescos sin cambio no repintan", setColorCount(), 0)
S.level = 10
W.calls = {}
ns.BridgeRefresh()
check("franja: un cambio de bytes repinta", setColorCount() == 29)

-- Throttle de QUEST_LOG_UPDATE: como mucho 2 barridos por segundo.
reset()
ns.EnsureDB()
local scans = 0
local realNum = _G.C_QuestLog.GetNumQuestLogEntries
_G.C_QuestLog.GetNumQuestLogEntries = function() scans = scans + 1; return realNum() end
S.questLog = {
    { questID = 2001, title = "Peligro", isHeader = false, isComplete = false },
}
ns.Events.OnEvent("QUEST_LOG_UPDATE") -- primera lectura: linea base
local base = scans
eq("QUEST_LOG_UPDATE: primera lectura escanea", base, 1)
local t0 = S.clock
for i = 1, 20 do
    S.clock = t0 + i * 0.01 -- rafaga dentro de 0.5 s
    ns.Events.OnEvent("QUEST_LOG_UPDATE")
end
eq("rafaga de QUEST_LOG_UPDATE: solo un barrido inmediato", scans, base)
S.clock = t0 + 1
W.runTimers()
eq("rafaga: un barrido diferido al final", scans, base + 1)

-- La transicion a completa sigue detectandose (en el barrido siguiente).
S.questLog[1].isComplete = true
S.complete[2001] = true
local seq0 = ns.bridge.eventSeq or 0
S.clock = t0 + 2
ns.Events.OnEvent("QUEST_LOG_UPDATE")
check("quest completa se registra tras el throttle", (ns.bridge.eventSeq or 0) ~= seq0)
_G.C_QuestLog.GetNumQuestLogEntries = realNum

-- Tope de knowledge.npcs: 2000 en total, 150 por mapa, expulsando al menos visto recientemente.
reset()
ns.EnsureDB()
CompaneroWoWDB.knowledge.npcs = {}
local curID = 0
_G.UnitExists = function() return true end
_G.UnitGUID = function() return "Creature-0-0-0-0-" .. curID .. "-0000000000" end
_G.UnitIsPlayer = function() return false end
_G.UnitCanAttack = function() return false end
local function target(id, map)
    curID = id
    S.mapID = map
    S.now = S.now + 1
    ns.Knowledge.OnEvent("PLAYER_TARGET_CHANGED")
end
for i = 1, 2500 do target(i, 1000 + (i % 25)) end
local total = 0
for _ in pairs(CompaneroWoWDB.knowledge.npcs) do total = total + 1 end
check("knowledge.npcs: como mucho 2000 en total", total <= 2000, tostring(total))
check("knowledge.npcs: los mas recientes se conservan", CompaneroWoWDB.knowledge.npcs[2500] ~= nil)
check("knowledge.npcs: los mas antiguos se expulsan", CompaneroWoWDB.knowledge.npcs[1] == nil)

reset()
CompaneroWoWDB.knowledge.npcs = {}
for i = 1, 300 do target(10000 + i, 2000) end
local onMap = 0
for _, rec in pairs(CompaneroWoWDB.knowledge.npcs) do
    if rec.mapID == 2000 then onMap = onMap + 1 end
end
check("knowledge.npcs: como mucho 150 por mapa", onMap <= 150, tostring(onMap))
check("knowledge.npcs: el PNJ reciente sigue", CompaneroWoWDB.knowledge.npcs[10300] ~= nil)
check("knowledge.npcs: el PNJ mas antiguo del mapa se expulsa", CompaneroWoWDB.knowledge.npcs[10001] == nil)

-- Un PNJ ya conocido se actualiza sin expulsar a nadie.
local countBefore = 0
for _ in pairs(CompaneroWoWDB.knowledge.npcs) do countBefore = countBefore + 1 end
target(10300, 2000)
local countAfter = 0
for _ in pairs(CompaneroWoWDB.knowledge.npcs) do countAfter = countAfter + 1 end
eq("PNJ conocido: no cambia el numero", countAfter, countBefore)
_G.UnitExists, _G.UnitGUID, _G.UnitIsPlayer, _G.UnitCanAttack = nil, nil, nil, nil

-- Contador de errores silenciados.
reset()
ns.EnsureDB()
local e0 = ns.errorCount
ns.SafeCall(function() error("boom-test") end)
eq("SafeCall cuenta el error", ns.errorCount, e0 + 1)
check("lastError contiene el mensaje", ns.lastError and string.find(ns.lastError, "boom-test", 1, true) ~= nil, tostring(ns.lastError))
local ok = ns.Pcall(function() error("otro-error") end)
check("Pcall devuelve false y cuenta", ok == false and ns.errorCount == e0 + 2)
ns.SafeCall(function() error(string.rep("x", 500)) end)
check("lastError se recorta a 200 caracteres", #ns.lastError <= 200)
check("Pcall correcto no cuenta", ns.Pcall(function() return 7 end) == true and ns.errorCount == e0 + 3)
check("estado: linea de errores", ns.HealthStatusLine():find("Errores capturados: " .. ns.errorCount, 1, true) ~= nil)

-- Un bloque que falla se marca no disponible y su error entra en health.
reset()
ns.EnsureDB()
_G.SkillUpForever.API.Professions = function() error("roto") end
local before2 = ns.errorCount
ns.ExportIntegrationsNow()
check("bloque que falla: no disponible", char().profPlan.available == false)
check("bloque que falla: el error llega a health", char().health.luaErrors >= before2 + 1
    and char().health.lastError ~= nil)

-- ---------------------------------------------------------------------------
if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
