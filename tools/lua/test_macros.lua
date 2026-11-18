-- test_macros.lua: pruebas offline de Macros.lua (exportacion de macros y /compa macro crear | parte |
-- borrar | deshacer | lista | barra), con las APIs de macros y de barras simuladas en wowstub.lua.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_macros.lua
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

-- Marcos capturados: Core, Bars y Macros registran sus eventos en frames propios.
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

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Bars.lua")
load("CompaneroWoW/Macros.lua")

local CHAR = "Tester-Ravencrest"
local MACROS, BARS

-- Reinicia el mundo: estado del juego, datos guardados, macros de cuenta y de personaje y barras.
local function newWorld()
    W.reset()
    ns.Bars.ResetForTests()
    ns.Macros.ResetForTests()
    CompaneroWoWDB = nil
    W.state.inCombat = false
    ns.errorCount = 0
    ns.lastError = nil
    clearSaid()
    BARS = W.installBars()
    MACROS = W.installMacros()
end

local function chars()
    return CompaneroWoWDB and CompaneroWoWDB.characters or {}
end
local function charRec()
    return chars()[CHAR] or {}
end
local function backups()
    return charRec().macroBackup or {}
end
local function macroNamed(name)
    for _, list in ipairs({ MACROS.global, MACROS.char }) do
        for _, m in ipairs(list) do
            if m.name == name then return m end
        end
    end
    return nil
end
local function countChar() return #MACROS.char end
local function countGlobal() return #MACROS.global end
local function addMacro(scope, name, body, icon)
    local list = scope == "cuenta" and MACROS.global or MACROS.char
    list[#list + 1] = { name = name, icon = icon or 134400, body = body }
end

-- ---------------------------------------------------------------------------
outln("Macros: exportacion (cuenta y personaje)")
newWorld()
addMacro("cuenta", "Global A", "/cast Abrumar")
addMacro("personaje", "Mi macro", "#showtooltip Golpe heroico\n/cast Golpe heroico", 135000)
ns.Macros.ExportNow()
local block = chars()[CHAR].macros
eq("exporta: cuenta usadas", block and block.account.used, 1)
eq("exporta: cuenta maximo", block and block.account.max, 120)
eq("exporta: personaje usadas", block and block.character.used, 1)
eq("exporta: personaje maximo", block and block.character.max, 18)
eq("exporta: nombre de personaje", block and block.character.list[1].name, "Mi macro")
eq("exporta: cuerpo con saltos", block and block.character.list[1].body, "#showtooltip Golpe heroico\n/cast Golpe heroico")
eq("exporta: icono numerico", block and block.character.list[1].icon, 135000)
eq("exporta: indice de personaje tras cuenta", block and block.character.list[1].index, 121)
check("estado: linea de macros", string.find(ns.MacrosStatusLine(), "personaje 1 de 18", 1, true) ~= nil)

-- En combate no se exporta, queda pendiente y se exporta al salir.
newWorld()
W.state.inCombat = true
addMacro("personaje", "Luego", "/cast Abrumar")
eq("combate: no exporta", ns.Macros.ExportNow(), false)
eq("combate: sin bloque", charRec().macros, nil)
W.state.inCombat = false
ns.Macros.OnEvent("PLAYER_REGEN_ENABLED")
W.runTimers()
eq("al salir de combate exporta lo pendiente", charRec().macros and charRec().macros.character.used, 1)

-- Sin API de macros: bloque no disponible; no sustituye a uno bueno.
newWorld()
addMacro("personaje", "Buena", "/cast Abrumar")
ns.Macros.ExportNow()
local savedNum = _G.GetNumMacros
_G.GetNumMacros = nil
ns.Macros.ExportNow()
_G.GetNumMacros = savedNum
eq("API ausente no sustituye el bloque bueno", charRec().macros.character.used, 1)

-- ---------------------------------------------------------------------------
outln("Macros: crear")
newWorld()
clearSaid()
ns.Macros.Command("crear Golpe|INV_SWORD_04|/cast [stance:2] Abrumar\\n/cast Golpe heroico")
local m = macroNamed("Golpe")
check("crea la macro de personaje", m ~= nil and countChar() == 1)
eq("crea: cuerpo con saltos desde \\n", m and m.body, "/cast [stance:2] Abrumar\n/cast Golpe heroico")
eq("crea: icono por nombre", m and m.icon, "INV_SWORD_04")
eq("crea: llamada con perCharacter=true", W.callsNamed("CreateMacro")[1].args[4], true)
eq("crea: copia de seguridad accion=crear", backups()[1] and backups()[1].accion, "crear")
check("crea: mensaje en castellano", sayHas("creada"))
eq("crea: exporta", charRec().macros and charRec().macros.character.used, 1)

clearSaid()
ns.Macros.Command("crear Sin icono|/cast Abrumar;;/cast Golpe heroico")
local m2 = macroNamed("Sin icono")
eq("crea sin icono: icono por defecto", m2 and m2.icon, "INV_MISC_QUESTIONMARK")
eq("crea sin icono: separador ;;", m2 and m2.body, "/cast Abrumar\n/cast Golpe heroico")

clearSaid()
ns.Macros.Command("crear Numerica|135100|/cast Abrumar")
eq("icono numerico como fileID", macroNamed("Numerica") and macroNamed("Numerica").icon, 135100)

-- Actualizar (mismo nombre): EditMacro con copia de seguridad.
clearSaid()
ns.Macros.Command("crear Golpe|INV_SWORD_04|/cast Golpe heroico")
eq("actualiza: no crea otra", countChar(), 3)
eq("actualiza: cuerpo nuevo", macroNamed("Golpe").body, "/cast Golpe heroico")
eq("actualiza: llamada EditMacro", #W.callsNamed("EditMacro"), 1)
eq("actualiza: copia accion=editar", backups()[1].accion, "editar")
eq("actualiza: copia con cuerpo anterior", backups()[1].previous.body, "/cast [stance:2] Abrumar\n/cast Golpe heroico")
check("actualiza: mensaje", sayHas("actualizada"))

-- Validaciones.
newWorld()
clearSaid()
ns.Macros.Command("crear " .. string.rep("N", 17) .. "|INV_MISC_QUESTIONMARK|/cast Abrumar")
check("nombre de mas de 16 caracteres: rechazado", sayHas("máximo 16"))
eq("nombre largo: no crea", countChar(), 0)
clearSaid()
ns.Macros.Command("crear Larga|INV_MISC_QUESTIONMARK|" .. string.rep("/cast Abrumar\\n", 20))
check("cuerpo de mas de 255 caracteres: rechazado", sayHas("máximo 255"))
eq("cuerpo largo: no crea", countChar(), 0)
clearSaid()
ns.Macros.Command("crear Vacia|INV_MISC_QUESTIONMARK|")
check("cuerpo vacio: rechazado", sayHas("no tiene cuerpo"))
clearSaid()
ns.Macros.Command("crear")
check("crear sin argumentos: uso", sayHas("Uso: /compa macro crear"))
clearSaid()
ns.Macros.Command("crear  |INV|/cast Abrumar")
check("nombre vacio: rechazado", sayHas("Falta el nombre"))

-- Limite de personaje: 18 llenas.
newWorld()
for i = 1, 18 do addMacro("personaje", "M" .. i, "/cast Abrumar") end
clearSaid()
ns.Macros.Command("crear Una mas|INV_MISC_QUESTIONMARK|/cast Abrumar")
check("macros llenas: rechazado con cuenta", sayHas("18 de 18"))
eq("llenas: no crea", countChar(), 18)
eq("llenas: sin llamada a CreateMacro", #W.callsNamed("CreateMacro"), 0)

-- Partes: cadena de /compa macro parte que se junta y crea en la ultima.
newWorld()
clearSaid()
ns.Macros.Command("parte 1/2 Trozo|INV_MISC_QUESTIONMARK|/cast Abrumar\\n")
check("parte 1 recibida", sayHas("Parte 1 de 2 recibida"))
eq("parte 1: no crea todavia", countChar(), 0)
ns.Macros.Command("parte 2/2 /cast Golpe heroico")
local pm = macroNamed("Trozo")
eq("partes: crea al llegar la ultima", pm and pm.body, "/cast Abrumar\n/cast Golpe heroico")
eq("partes: una sola creacion", #W.callsNamed("CreateMacro"), 1)

newWorld()
clearSaid()
ns.Macros.Command("parte 2/2 /cast Abrumar")
check("parte fuera de orden: rechazada", sayHas("Falta una parte anterior"))
eq("fuera de orden: no crea", countChar(), 0)
ns.Macros.Command("parte 1/3 Ab|INV_X|/cast")
ns.Macros.Command("parte 3/3 fin")
check("parte saltada: rechazada", sayHas("Falta una parte anterior"))
ns.Macros.Command("parte 1/3 uno|INV_X|/cast ")
ns.Macros.Command("parte 2/3 Abrumar")
ns.Macros.Command("parte 3/3 ")
eq("tres partes: cuerpo unido", macroNamed("uno") and macroNamed("uno").body, "/cast Abrumar")
eq("tres partes: crea solo al final", #W.callsNamed("CreateMacro"), 1)
clearSaid()
ns.Macros.Command("parte 9/3 x")
check("parte fuera de rango: rechazada", sayHas("fuera de rango"))

-- Parte en combate: rechazada.
newWorld()
W.state.inCombat = true
ns.Macros.Command("parte 1/2 a")
W.state.inCombat = false
eq("parte en combate: sin trozos", countChar(), 0)

-- ---------------------------------------------------------------------------
outln("Macros: borrar, deshacer y lista")
newWorld()
addMacro("personaje", "Borrame", "/cast Abrumar", "INV_MISC_QUESTIONMARK")
ns.Macros.Command("borrar Borrame")
eq("borrar: quitada", macroNamed("Borrame"), nil)
eq("borrar: copia accion=borrar", backups()[1].accion, "borrar")
clearSaid()
ns.Macros.Command("deshacer")
local back = macroNamed("Borrame")
check("deshacer: recrea la borrada", back ~= nil)
eq("deshacer: cuerpo intacto", back and back.body, "/cast Abrumar")
eq("deshacer: cuenta copias restantes en 0", #backups(), 0)

clearSaid()
ns.Macros.Command("borrar No existe")
check("borrar: no encontrada", sayHas("No encuentro ninguna macro"))
eq("borrar no encontrada: sin copia", #backups(), 0)
clearSaid()
ns.Macros.Command("borrar")
check("borrar sin nombre: uso", sayHas("Uso: /compa macro borrar"))

-- Deshacer una creacion: retira la macro.
newWorld()
ns.Macros.Command("crear Nueva|INV_MISC_QUESTIONMARK|/cast Abrumar")
ns.Macros.Command("deshacer")
eq("deshacer creacion: la retira", macroNamed("Nueva"), nil)

-- Deshacer una actualizacion: vuelve al cuerpo anterior.
newWorld()
addMacro("personaje", "Ed", "/cast Abrumar", "INV_MISC_QUESTIONMARK")
ns.Macros.Command("crear Ed|INV_MISC_QUESTIONMARK|/cast Golpe heroico")
ns.Macros.Command("deshacer")
eq("deshacer edicion: cuerpo anterior", macroNamed("Ed").body, "/cast Abrumar")

-- Sin copias.
newWorld()
clearSaid()
ns.Macros.Command("deshacer")
check("sin copias: aviso", sayHas("No hay copias de seguridad de macros"))

-- Como mucho 3 copias.
newWorld()
for i = 1, 4 do ns.Macros.Command("crear C" .. i .. "|INV_MISC_QUESTIONMARK|/cast Abrumar") end
eq("como mucho 3 copias", #backups(), 3)

-- Lista.
newWorld()
addMacro("cuenta", "Global", "/cast Abrumar")
addMacro("personaje", "Local", "/cast Abrumar")
clearSaid()
ns.Macros.Command("lista")
check("lista: contadores", sayHas("cuenta 1 de 120, personaje 1 de 18"))
check("lista: nombres", sayHas("Global, Local"))
clearSaid()
ns.Macros.Command("")
check("sin subcomando: uso", sayHas("Uso: /compa macro"))
clearSaid()
ns.Macros.Command("bogus")
check("subcomando desconocido: uso", sayHas("Uso: /compa macro"))

-- ---------------------------------------------------------------------------
outln("Macros: combate y barras")
newWorld()
addMacro("personaje", "Combate", "/cast Abrumar", "INV_MISC_QUESTIONMARK")
W.state.inCombat = true
clearSaid()
ns.Macros.Command("crear X|INV_MISC_QUESTIONMARK|/cast Abrumar")
check("crear en combate: rechazado", sayHas("combate"))
ns.Macros.Command("borrar Combate")
ns.Macros.Command("deshacer")
ns.Macros.Command("barra Combate 3")
eq("combate: ninguna llamada de escritura", #W.callsNamed("CreateMacro") + #W.callsNamed("DeleteMacro")
    + #W.callsNamed("EditMacro") + #W.callsNamed("PlaceAction"), 0)
eq("combate: la macro sigue", macroNamed("Combate") ~= nil, true)
W.state.inCombat = false

newWorld()
addMacro("personaje", "Barra", "/cast Abrumar", "INV_MISC_QUESTIONMARK")
clearSaid()
ns.Macros.Command("barra Barra 3")
eq("barra: la macro queda en la ranura 3", BARS.actions[3] and BARS.actions[3].type, "macro")
eq("barra: guardada la copia del slot", ns.Bars.SlotKindOf(3), "macro")
check("barra: mensaje", sayHas("colocada en la ranura 3"))
local bk = W.callsNamed("PlaceAction")
eq("barra: una colocacion", #bk, 1)
eq("barra: sin cursor residual", W.BARS.cursor == nil, true)
local bars = charRec().actionBarsBackup
eq("barra: copia de barras guardada", bars and bars[1] and bars[1].slots[3].kind, "empty")
ns.Bars.Command("deshacer")
eq("barra: /compa barras deshacer quita la macro del slot", ns.Bars.SlotKindOf(3), nil)

clearSaid()
ns.Macros.Command("barra Barra 121")
check("barra: slot fuera de rango", sayHas("entre 1 y 120"))
clearSaid()
ns.Macros.Command("barra No existe 2")
check("barra: macro no encontrada", sayHas("No encuentro ninguna macro"))
clearSaid()
ns.Macros.Command("barra Barra")
check("barra: uso", sayHas("Uso: /compa macro barra"))

-- Sin cliente de barras: no se toca nada.
newWorld()
addMacro("personaje", "Sin barras", "/cast Abrumar", "INV_MISC_QUESTIONMARK")
local savedPlace = _G.PlaceAction
_G.PlaceAction = nil
clearSaid()
ns.Macros.Command("barra Sin barras 4")
_G.PlaceAction = savedPlace
check("sin PlaceAction: aviso claro", sayHas("no están disponibles en este cliente"))
eq("sin PlaceAction: sin copia de barras", charRec().actionBarsBackup, nil)

-- Sin API de macros: aviso y ninguna llamada.
newWorld()
MACROS = nil
W.uninstallMacros()
clearSaid()
ns.Macros.Command("crear Y|INV_MISC_QUESTIONMARK|/cast Abrumar")
check("sin API de macros: aviso", sayHas("no están disponibles"))

-- ---------------------------------------------------------------------------
if failures > 0 then
    outln(string.format("FAILURES: %d", failures))
    os.exit(1)
end
outln("ALL TESTS PASSED")
