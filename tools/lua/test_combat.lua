-- test_combat.lua: pruebas offline de Combat.lua (revision de combates con C_DamageMeter) y de
-- Bars.lua (exportacion de barras, /compa barras aplicar | deshacer).
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_combat.lua
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
local function count(list)
    local n = 0
    for _ in pairs(list or {}) do n = n + 1 end
    return n
end

-- Mensajes de ns.Say (via print): sin colores para comparar texto.
local said = {}
local function plain(s)
    s = string.gsub(s, "|c%x%x%x%x%x%x%x%x", "")
    s = string.gsub(s, "|r", "")
    return s
end
_G.print = function(msg) said[#said + 1] = plain(tostring(msg)) end
local function clearSaid() said = {} end
local function sayHas(fragment)
    for _, s in ipairs(said) do
        if string.find(s, fragment, 1, true) then return true end
    end
    return false
end

-- Marcos capturados: Combat.lua / Bars.lua / Core.lua registran sus eventos en frames propios.
_G.CreateFrame = function()
    local f = { events = {} }
    function f:RegisterEvent(name) self.events[name] = true end
    function f:SetScript(name, fn) if name == "OnEvent" then self.onEvent = fn end end
    return f
end

SlashCmdList = {}
CompaneroWoWDB = nil
W.installGameAPI()
W.installCombat()
local BARS = W.installBars()

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Storage.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Combat.lua")
load("CompaneroWoW/Bars.lua")

local CHAR = "Tester-Ravencrest"
local ME = "Player-1-0000AAAA"

-- Reinicia el mundo de prueba (estado del juego, datos guardados y estado en memoria de los modulos).
local function newWorld()
    W.reset()
    ns.Combat.ResetForTests()
    ns.Bars.ResetForTests()
    CompaneroWoWDB = nil
    W.DM.fight = nil
    W.DM.secretInCombat = false
    W.DM.available = true
    W.state.zone = "Bosque de Elwynn"
    W.state.hp = 35
    W.state.power = 10
    W.state.target = nil
    W.state.inInstance = false
    ns.errorCount = 0
    ns.lastError = nil
    clearSaid()
    BARS = W.installBars()
end

local function chars()
    return CompaneroWoWDB and CompaneroWoWDB.characters or {}
end
local function combatsOf()
    local c = chars()[CHAR]
    return c and c.combats or {}
end

local TARGET_LOBO = { name = "Lobo", guid = "Creature-0-1-0-0-1100-0000000000", level = 3,
                      classification = "normal", canAttack = true }

-- Sesion de un combate: dano del jugador, curacion y dano recibido (por origen y hechizo).
local function setFight(duration, opts)
    opts = opts or {}
    W.setFight({
        duration = duration,
        guid = ME,
        damageDone = opts.damageDone,
        healingDone = opts.healingDone,
        damageTaken = opts.damageTaken,
    })
end

-- Inicio de combate (marca de inicio) en el instante t.
local function startCombat(t)
    W.state.clock = t
    W.state.inCombat = true
    ns.Combat.OnEvent("PLAYER_REGEN_DISABLED")
end

-- Fin de combate: sale de combate y ejecuta los retardos de lectura de sesion.
local function endCombat(t)
    W.state.clock = t
    W.state.inCombat = false
    ns.Combat.OnEvent("PLAYER_REGEN_ENABLED")
    W.runTimers()
end

-- ---------------------------------------------------------------------------
outln("Combat: inicio sin lecturas en combate")
newWorld()
W.state.target = TARGET_LOBO
setFight(34.2, { damageDone = { total = 12345, spells = { { 585, 5000 } } } })
startCombat(100)
eq("sin lecturas de C_DamageMeter al entrar en combate", #W.callsNamed("C_DamageMeter.GetCombatSessionFromType"), 0)
ns.Combat.OnEvent("PLAYER_TARGET_CHANGED") -- cambio de objetivo en combate: no lee nada
eq("PLAYER_TARGET_CHANGED en combate no lee la sesion", #W.callsNamed("C_DamageMeter.GetCombatSessionFromType"), 0)
eq("ninguna sesion guardada mientras dura el combate", #combatsOf(), 0)
endCombat(134.2)
check("tras salir de combate se lee la sesion", #W.callsNamed("C_DamageMeter.GetCombatSessionFromType") > 0)
eq("un combate guardado al salir", #combatsOf(), 1)
check("bandeja: el combate va a la bandeja (tipo 7, ruta combats)", (function() for _, it in ipairs(ns.Storage.Items()) do if it.msgType == 7 and it.payload.p[1] == "combats" then return true end end return false end)())

-- ---------------------------------------------------------------------------
outln("Combat: registro completo")
newWorld()
W.state.target = TARGET_LOBO
setFight(34.2, {
    damageDone = { total = 12345, spells = { { 585, 5000 }, { 589, 7345 } } },
    healingDone = { total = 800, spells = { { 2050, 800 } } },
    damageTaken = { total = 4000, spells = { { 9999, 1500, "Lobo" }, { 9999, 1500, "Lobo" }, { 9999, 1000, "Jefe" } } },
})
startCombat(100)
endCombat(134.2)
local r = combatsOf()[1]
check("registro existe", r ~= nil)
if r then
    eq("id 1", r.id, 1)
    eq("no murio", r.died, false)
    eq("duracion 34.2", r.duration, 34.2)
    eq("zona", r.zone, "Bosque de Elwynn")
    eq("mapID", r.mapID, 1429)
    eq("inInstance false", r.inInstance, false)
    eq("dano total", r.damageDone.total, 12345)
    eq("dps = total / duracion (1 decimal)", r.damageDone.dps, 361)
    eq("hechizos de dano ordenados (mayor primero)", r.damageDone.spells[1].spellID, 589)
    eq("nombre de hechizo", r.damageDone.spells[1].name, "Dolor abrasador")
    eq("hechizos de dano: 2", #r.damageDone.spells, 2)
    eq("curacion total", r.healingDone.total, 800)
    eq("dano recibido total", r.damageTaken.total, 4000)
    eq("origen de dano recibido agregado por nombre", r.damageTaken.sources[1].name, "Lobo")
    eq("origen: cantidad agregada", r.damageTaken.sources[1].amount, 3000)
    eq("origen: spellID 0", r.damageTaken.sources[1].spellID, 0)
    eq("segundo origen", r.damageTaken.sources[2].name, "Jefe")
    eq("objetivo: nombre", r.targets[1].name, "Lobo")
    eq("objetivo: npcID desde el GUID", r.targets[1].npcID, 1100)
    eq("objetivo: nivel", r.targets[1].level, 3)
    eq("objetivo: clasificacion", r.targets[1].classification, "normal")
    eq("objetivos sin duplicar (inicio y fin)", #r.targets, 1)
    eq("sin bloque de muerte", r.death, nil)
    eq("salud al final", r.resourcesAtEnd.healthPct, 35)
    eq("poder al final", r.resourcesAtEnd.powerPct, 10)
    eq("resumen de estado", ns.CombatStatusLines and ns.CombatStatusLines()[1], "Combates guardados: 1 de 10")
end

-- ---------------------------------------------------------------------------
outln("Combat: ring buffer de 10 combates, el mas nuevo primero")
newWorld()
for i = 1, 12 do
    setFight(5, { damageDone = { total = 100 * i, spells = {} } })
    startCombat(100 + i * 10)
    endCombat(105 + i * 10)
end
local list = combatsOf()
eq("se conservan 10", #list, 10)
eq("el mas nuevo primero (id 12)", list[1].id, 12)
eq("el mas antiguo es el id 3", list[10].id, 3)
eq("ids consecutivos", list[1].id - list[10].id, 9)
eq("dano del mas nuevo", list[1].damageDone.total, 1200)

-- ---------------------------------------------------------------------------
outln("Combat: combates triviales (< 3 s y sin dano) no se guardan")
newWorld()
setFight(2, {})
startCombat(100); endCombat(102)
eq("2 s sin dano: no se guarda", #combatsOf(), 0)
setFight(2, { damageDone = { total = 50, spells = { { 585, 50 } } } })
startCombat(200); endCombat(202)
eq("2 s con dano: se guarda", #combatsOf(), 1)
setFight(4, {})
startCombat(300); endCombat(304)
eq("4 s sin dano: se guarda", #combatsOf(), 2)
check("un combate sin dano no tiene bloque damageDone", combatsOf()[1].damageDone == nil)

-- ---------------------------------------------------------------------------
outln("Combat: muerte (registro provisional y actualizacion al salir de combate)")
newWorld()
W.state.target = TARGET_LOBO
setFight(30, { damageDone = { total = 900, spells = { { 585, 900 } } },
               damageTaken = { total = 4000, spells = { { 9999, 3000, "Lobo" }, { 9999, 1000, "Jefe" } } } })
startCombat(100)
W.state.clock = 110
ns.Combat.OnEvent("PLAYER_DEAD") -- muerte dentro de combate: se lee la sesion con retardo
W.runTimers()
eq("tras la muerte hay un registro provisional", #combatsOf(), 1)
local d = combatsOf()[1]
check("registro de muerte marcado", d and d.died == true)
eq("killer: origen de mayor dano recibido", d.death.killer, "Lobo")
eq("lastHits: primero el de mayor dano", d.death.lastHits[1].amount, 3000)
eq("lastHits: origen", d.death.lastHits[1].source, "Lobo")
eq("lastHits: nombre del hechizo", d.death.lastHits[1].spellName, "Explosión")
eq("lastHits: como mucho 5", #d.death.lastHits, 2)
endCombat(130)
eq("al salir de combate se actualiza el mismo registro (no se duplica)", #combatsOf(), 1)
eq("mismo id tras actualizar", combatsOf()[1].id, 1)
eq("duracion final hasta salir de combate", combatsOf()[1].duration, 30)
eq("sigue marcado como muerte", combatsOf()[1].died, true)

-- ---------------------------------------------------------------------------
outln("Combat: valores secretos ignorados sin error")
newWorld()
W.state.target = TARGET_LOBO
setFight(20, { damageDone = { total = 500, spells = { { 585, 500 } } },
               damageTaken = { total = 300, spells = { { 9999, 300, "Lobo" } } } })
W.DM.secretInCombat = true
local errBefore = ns.errorCount or 0
startCombat(100)
W.state.clock = 105
ns.Combat.OnEvent("PLAYER_DEAD") -- en combate las lecturas son secretas
W.runTimers()
eq("sin errores de Lua al leer secretos", ns.errorCount or 0, errBefore)
local s = combatsOf()[1]
check("registro provisional guardado", s ~= nil)
if s then
    check("total de dano secreto -> sin bloque damageDone", s.damageDone == nil)
    check("dano recibido secreto -> sin bloque damageTaken", s.damageTaken == nil)
    check("salud secreta -> sin resourcesAtEnd", s.resourcesAtEnd == nil)
    eq("muerte sin datos: lastHits vacio", #s.death.lastHits, 0)
    eq("muerte sin datos: sin killer", s.death.killer, nil)
end
W.DM.secretInCombat = false
endCombat(120)
local f = combatsOf()[1]
eq("al salir de combate (lecturas no secretas) se completa", f and f.damageDone and f.damageDone.total, 500)
eq("registro actualizado, no duplicado", #combatsOf(), 1)

-- ---------------------------------------------------------------------------
outln("Combat: objetivos vistos fuera de combate")
newWorld()
W.state.target = { name = "Lobo", guid = "Creature-0-1-0-0-1100-0000000000", level = 3, classification = "normal", canAttack = true }
ns.Combat.OnEvent("PLAYER_TARGET_CHANGED")
W.state.target = { name = "Jefe", guid = "Creature-0-1-0-0-2200-0000000000", level = 12, classification = "elite", canAttack = true }
ns.Combat.OnEvent("PLAYER_TARGET_CHANGED")
W.state.target = { name = "Amigo", guid = "Player-1-0000BBBB", level = 9, canAttack = false }
ns.Combat.OnEvent("PLAYER_TARGET_CHANGED")
W.state.target = nil
setFight(10, { damageDone = { total = 100, spells = {} } })
startCombat(100)
endCombat(110)
local tg = combatsOf()[1].targets
eq("objetivos recientes: 2 (un aliado no cuenta)", #tg, 2)
eq("primer objetivo visto", tg[1].name, "Lobo")
eq("segundo objetivo: elite", tg[2].classification, "elite")

-- ---------------------------------------------------------------------------
outln("Combat: sin C_DamageMeter no se registra nada")
newWorld()
W.DM.available = false
setFight(10, { damageDone = { total = 100, spells = {} } })
startCombat(100); endCombat(110)
eq("sin API de sesiones: nada guardado", #combatsOf(), 0)
eq("sin API de sesiones: ninguna lectura", #W.callsNamed("C_DamageMeter.GetCombatSessionFromType"), 0)

-- ===========================================================================
-- BARRAS
-- ===========================================================================
-- Disposicion de prueba: slot 1 = Castigo (585), slot 2 = objeto 6948, slot 3 = macro 1,
-- slot 5 = mascota (tipo no restaurable), slot 61 = hechizo 1234 en la multibarra inferior izquierda.
local function layout()
    BARS.actions[1] = { type = "spell", id = 585 }
    BARS.actions[2] = { type = "item", id = 6948 }
    BARS.actions[3] = { type = "macro", id = 1 }
    BARS.actions[5] = { type = "companion", id = 10 }
    BARS.actions[61] = { type = "spell", id = 1234 }
    BARS.spellNames = { [585] = "Castigo", [589] = "Dolor abrasador", [2050] = "Curación", [1234] = "Ataque" }
    BARS.items = { [6948] = "Piedra de hogar" }
    BARS.macros = { [1] = { name = "Tele", body = "/cast X" } }
    BARS.known = { [585] = true, [589] = true, [2050] = true, [1234] = true }
    BARS.bindings = { ACTIONBUTTON1 = "1", MULTIACTIONBAR1BUTTON1 = "SHIFT-1" }
    BARS.shown.MultiBarBottomLeft = true
    BARS.numGlobal, BARS.numChar = 1, 0
end

local function backups()
    local c = chars()[CHAR]
    return c and c.actionBarsBackup or {}
end

outln("Bars: exportacion de actionBars")
newWorld()
layout()
ns.Bars.ExportNow()
local ab = chars()[CHAR] and chars()[CHAR].actionBars
check("actionBars guardado", ab ~= nil)
if ab then
    eq("exportedAt", ab.exportedAt, 1700000000)
    eq("slot 1 es hechizo 585", ab.slots[1].kind, "spell")
    eq("slot 1 nombre", ab.slots[1].name, "Castigo")
    eq("slot 1 tecla", ab.slots[1].key, "1")
    eq("slot 1 con su numero de slot", ab.slots[1].slot, 1)
    eq("slot 2 objeto", ab.slots[2].kind, "item")
    eq("slot 2 nombre de objeto", ab.slots[2].name, "Piedra de hogar")
    eq("slot 2 sin tecla", ab.slots[2].key, nil)
    eq("slot 3 macro", ab.slots[3].kind, "macro")
    eq("slot 3 nombre de macro", ab.slots[3].name, "Tele")
    eq("slot 5 otro tipo", ab.slots[5].kind, "other")
    eq("slot 61 con tecla de multibarra", ab.slots[61].key, "SHIFT-1")
    eq("slot vacio no aparece", ab.slots[4], nil)
    eq("slots ocupados", count(ab.slots), 5)
    eq("barras visibles: principal y multibarra inferior izquierda", table.concat(ab.visibleBars, ","), "1,2")
end

-- Una macro que lanza un hechizo: GetActionInfo da el id del hechizo (subType "spell"), no el indice.
outln("Bars: macros cuyo slot devuelve el id del hechizo")
newWorld()
layout()
BARS.macros[121] = { name = "abrumar", body = "/cast Abrumar" }
BARS.macros[100] = { name = "Otra", body = "/cast Y" }
BARS.numGlobal, BARS.numChar = 1, 1
BARS.actions[10] = { type = "macro", id = 7384, subType = "spell", text = "abrumar" }
-- El id del hechizo (100) coincide con el indice de otra macro: no debe confundirse con ella.
BARS.actions[11] = { type = "macro", id = 100, subType = "spell", text = "Tele" }
BARS.actions[12] = { type = "macro", id = 6547, subType = "spell", text = "Borrada" }
ns.Bars.ExportNow()
ab = chars()[CHAR].actionBars
eq("macro de hechizo: nombre de la macro", ab.slots[10].name, "abrumar")
eq("macro de hechizo: indice de la macro, no el id del hechizo", ab.slots[10].id, 121)
eq("id que coincide con otro indice: nombre correcto", ab.slots[11].name, "Tele")
eq("id que coincide con otro indice: indice correcto", ab.slots[11].id, 1)
eq("macro no localizable: conserva el nombre", ab.slots[12].name, "Borrada")
eq("macro no localizable: sin indice", ab.slots[12].id, nil)
eq("macro normal sigue igual", ab.slots[3].id, 1)
-- Sin GetActionText: una macro de hechizo queda sin identificar en vez de con datos erroneos.
local savedText = _G.GetActionText
_G.GetActionText = nil
ns.Bars.ExportNow()
_G.GetActionText = savedText
ab = chars()[CHAR].actionBars
eq("sin GetActionText: macro de hechizo sin nombre", ab.slots[10].name, nil)
eq("sin GetActionText: macro de hechizo sin indice", ab.slots[10].id, nil)
eq("sin GetActionText: macro normal por indice", ab.slots[3].name, "Tele")

outln("Bars: exportacion con disparadores (login, ACTIONBAR_SLOT_CHANGED con throttle, combate, sync)")
newWorld()
layout()
ns.Bars.OnEvent("PLAYER_LOGIN")
eq("login: una exportacion diferida", #W.callsNamed("C_Timer.After"), 1)
W.runTimers()
check("login: se exporta", chars()[CHAR] and chars()[CHAR].actionBars ~= nil)
local before = #W.callsNamed("C_Timer.After")
for _ = 1, 5 do ns.Bars.OnEvent("ACTIONBAR_SLOT_CHANGED", 1) end
eq("ACTIONBAR_SLOT_CHANGED en rafaga: un solo temporizador", #W.callsNamed("C_Timer.After") - before, 1)
BARS.actions[7] = { type = "spell", id = 589 }
W.runTimers()
eq("exportacion tras el cambio", chars()[CHAR].actionBars.slots[7].id, 589)
-- En combate: no se exporta y queda pendiente hasta salir.
W.state.inCombat = true
BARS.actions[8] = { type = "spell", id = 2050 }
ns.Bars.OnEvent("ACTIONBAR_SLOT_CHANGED", 8)
W.runTimers()
eq("en combate no se exporta", chars()[CHAR].actionBars.slots[8], nil)
W.state.inCombat = false
ns.Bars.OnEvent("PLAYER_REGEN_ENABLED")
W.runTimers()
eq("al salir de combate se exporta lo pendiente", chars()[CHAR].actionBars.slots[8].id, 2050)
-- Sync: exportacion inmediata.
BARS.actions[9] = { type = "spell", id = 585 }
ns.ExportBarsNow()
eq("/compa sync exporta sin esperar", chars()[CHAR].actionBars.slots[9].id, 585)
-- API ausente: no sustituye un bloque bueno.
local saved = _G.GetActionInfo
_G.GetActionInfo = nil
ns.Bars.ExportNow()
_G.GetActionInfo = saved
eq("API ausente no sustituye el bloque bueno", chars()[CHAR].actionBars.slots[9].id, 585)

-- Comando de resumen.
outln("Bars: /compa barras (resumen)")
newWorld()
layout()
ns.Bars.Command("")
check("resumen: huecos ocupados", sayHas("5 huecos ocupados"))
check("resumen: barras visibles", sayHas("Barras visibles: 1, 2."))
clearSaid()
ns.Bars.Command("bogus")
check("subcomando desconocido: uso", sayHas("Uso: /compa barras"))

-- ---------------------------------------------------------------------------
outln("Bars: aplicar - combate, limites y formato")
newWorld()
layout()
W.state.inCombat = true
ns.Bars.Command("aplicar 3:589")
check("aplicar en combate: rechazado", sayHas("combate"))
eq("aplicar en combate: sin colocar", #W.callsNamed("PlaceAction"), 0)
eq("aplicar en combate: sin copia", #backups(), 0)
W.state.inCombat = false

clearSaid()
ns.Bars.Command("aplicar " .. string.rep("1:585,", 60))
check("aplicar: mas de 255 caracteres rechazado", sayHas("255 caracteres"))
eq("aplicar largo: sin colocar", #W.callsNamed("PlaceAction"), 0)

clearSaid()
local many = {}
for i = 1, 41 do many[#many + 1] = "1:585" end
ns.Bars.Command("aplicar " .. table.concat(many, ","))
check("aplicar: mas de 40 pares rechazado", sayHas("Máximo 40 pares"))
eq("aplicar 41 pares: sin colocar", #W.callsNamed("PlaceAction"), 0)

clearSaid()
ns.Bars.Command("aplicar")
check("aplicar sin argumentos: uso", sayHas("Uso: /compa barras aplicar"))

local savedPick, savedPick2 = _G.C_Spell.PickupSpell, _G.PickupSpell
_G.C_Spell.PickupSpell = nil
_G.PickupSpell = nil
clearSaid()
ns.Bars.Command("aplicar 3:589")
check("sin API de recoger hechizos: mensaje claro", sayHas("no está disponible en este cliente"))
eq("sin API: sin copia de seguridad", #backups(), 0)
_G.C_Spell.PickupSpell = savedPick
_G.PickupSpell = savedPick2

-- ---------------------------------------------------------------------------
outln("Bars: aplicar - pares validos, omitidos y copia de seguridad")
newWorld()
layout()
clearSaid()
ns.Bars.Command("aplicar 3:589,4:9999,x:1,121:589,3:585,6:585")
eq("colocados: slot 3 (589) y slot 6 (585)", BARS.actions[3] and BARS.actions[3].id, 589)
eq("colocado en slot 6", BARS.actions[6] and BARS.actions[6].id, 585)
eq("PlaceAction dos veces", #W.callsNamed("PlaceAction"), 2)
check("resumen de colocados", sayHas("colocados 2 de 2"))
check("omitido: formato", sayHas("formato no válido"))
check("omitido: hechizo no conocido", sayHas("4:9999 (hechizo no conocido)"))
check("omitido: slot fuera de rango", sayHas("slot fuera de 1-120"))
check("omitido: slot repetido", sayHas("slot repetido"))
eq("copia de seguridad guardada", #backups(), 1)
eq("copia: slot 3 estaba macro (ahora vacio?)", backups()[1].slots[3].kind, "macro")
eq("copia: slot 6 estaba vacio", backups()[1].slots[6].kind, "empty")
eq("tras aplicar se re-exporta", chars()[CHAR].actionBars.slots[3].id, 589)
eq("no toca otros slots", BARS.actions[1].id, 585)
check("no se usa cursor residual", W.BARS.cursor == nil)

-- Solo hechizos conocidos.
newWorld()
layout()
clearSaid()
ns.Bars.Command("aplicar 7:9999")
eq("hechizo desconocido: ninguna colocacion", #W.callsNamed("PlaceAction"), 0)
eq("hechizo desconocido: sin copia", #backups(), 0)
check("hechizo desconocido: informa", sayHas("hechizo no conocido"))

-- Coloca no realizado (la API no deja el hechizo en el slot): se informa, no se da por bueno.
newWorld()
layout()
BARS.noPlaceAction = true
clearSaid()
ns.Bars.Command("aplicar 7:589")
check("no colocado: se informa", sayHas("no se pudo colocar"))
eq("no colocado: cuenta 0", sayHas("colocados 0 de 1"), true)
BARS.noPlaceAction = false

-- Maximo 3 copias.
newWorld()
layout()
for i = 1, 4 do
    ns.Bars.Command("aplicar " .. i .. ":589")
end
eq("como mucho 3 copias", #backups(), 3)

-- ---------------------------------------------------------------------------
outln("Bars: deshacer")
newWorld()
layout()
ns.Bars.Command("aplicar 2:589,3:2050")
eq("aplicado: slot 2 ahora es 589", BARS.actions[2].id, 589)
eq("aplicado: slot 3 ahora es 2050", BARS.actions[3].id, 2050)
clearSaid()
W.state.inCombat = true
ns.Bars.Command("deshacer")
check("deshacer en combate: rechazado", sayHas("combate"))
eq("deshacer en combate: no cambia nada", BARS.actions[2].id, 589)
W.state.inCombat = false

clearSaid()
ns.Bars.Command("deshacer")
check("deshacer: resumen", sayHas("restauradas: 2 de 2"))
eq("deshacer restaura el objeto del slot 2", BARS.actions[2] and BARS.actions[2].type, "item")
eq("deshacer restaura objeto 6948", BARS.actions[2] and BARS.actions[2].id, 6948)
eq("deshacer restaura la macro del slot 3", BARS.actions[3] and BARS.actions[3].type, "macro")
eq("deshacer: copia retirada de la lista", #backups(), 0)
eq("deshacer: cursor limpio", W.BARS.cursor, nil)

clearSaid()
ns.Bars.Command("deshacer")
check("deshacer sin copias: mensaje", sayHas("No hay copias de seguridad"))

-- Deshacer de un slot vacio: lo que se coloco se quita del bar.
newWorld()
layout()
ns.Bars.Command("aplicar 7:589")
eq("antes de deshacer el slot 7 tiene hechizo", BARS.actions[7] and BARS.actions[7].id, 589)
ns.Bars.Command("deshacer")
eq("deshacer slot vacio: se quita", BARS.actions[7], nil)

-- Mascota (tipo no restaurable): se omite con motivo.
newWorld()
layout()
ns.Bars.Command("aplicar 5:589")
clearSaid()
ns.Bars.Command("deshacer")
check("tipo no restaurable: se informa", sayHas("tipo no restaurable"))

-- ---------------------------------------------------------------------------
outln("Bars: linea de estado (/compa estado)")
newWorld()
layout()
check("sin exportar: linea clara", string.find(ns.BarsStatusLine(), "sin exportar", 1, true) ~= nil)
ns.Bars.ExportNow()
check("exportadas: huecos y visibles", string.find(ns.BarsStatusLine(), "5 huecos ocupados", 1, true) ~= nil)

outln("Combat: linea de estado con el ultimo combate")
newWorld()
W.state.target = TARGET_LOBO
setFight(34.2, { damageDone = { total = 12345, spells = { { 585, 12345 } } }, healingDone = { total = 800, spells = {} } })
startCombat(100); endCombat(134.2)
local lines = ns.CombatStatusLines()
eq("guardados", lines[1], "Combates guardados: 1 de 10")
check("ultimo combate: zona y dano", string.find(lines[2], "Bosque de Elwynn", 1, true) ~= nil
    and string.find(lines[2], "daño 12345", 1, true) ~= nil)

-- ---------------------------------------------------------------------------
if failures > 0 then
    outln(string.format("FAILURES: %d", failures))
    os.exit(1)
end
outln("ALL TESTS PASSED")
