-- test_core.lua: pruebas offline de Core.lua (snapshots, fusion defensiva, logout, carga de pantalla)
-- y de la persistencia de los bloques de integracion y Questie frente a logout y exportaciones fallidas.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_core.lua
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

-- Marco capturado: Core registra sus eventos en un frame propio; el test los dispara a mano.
local frames = {}
_G.CreateFrame = function()
    local f = { events = {} }
    function f:RegisterEvent(name) self.events[name] = true end
    function f:SetScript(name, fn) if name == "OnEvent" then self.onEvent = fn end end
    frames[#frames + 1] = f
    return f
end

SlashCmdList = {}
CompaneroWoWDB = nil
W.installGameAPI()

-- Estado del mundo que ven IsLoggedIn / UnitExists (independiente de W.reset).
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
load("CompaneroWoW/Core.lua")
local coreFrame
for _, f in ipairs(frames) do
    if f.events.PLAYER_LOGOUT then coreFrame = f end
end
assert(coreFrame and coreFrame.onEvent, "Core.lua no registro sus eventos")
load("CompaneroWoW/Questie.lua")
load("CompaneroWoW/Integrations.lua")

local S = W.state
local KEY = "Tester-Ravencrest"
local function char() return CompaneroWoWDB.characters[KEY] end

-- Collectors controlados por el test. Cada llamada cuenta para comprobar que no se ejecutan.
local data
local collectorCalls = 0
local function counted(fn)
    return function(...)
        collectorCalls = collectorCalls + 1
        return fn(...)
    end
end
ns.CollectCharacter = counted(function() return { name = "Tester", level = 10, money = data.money } end)
ns.CollectLocation = counted(function() return { mapID = 1429 } end)
ns.CollectQuests = counted(function() return data.quests end)
ns.CollectInventory = counted(function() return { items = data.items, freeSlots = 4, totalSlots = 16 } end)
ns.CollectEquipment = counted(function() return data.equipment end)
ns.CollectGroup = counted(function() return { inGroup = false, inRaid = false, members = {} } end)
ns.CollectSpells = counted(function() return data.spells end)
ns.CollectTalents = counted(function() return { raw = {} } end)
ns.CollectProfessions = counted(function() return data.professions end)
ns.CollectReputations = counted(function() return data.reputations end)

local function fullData()
    return {
        money = 600000,
        professions = {
            { name = "Tailoring", skillLineID = 197, rank = 68, maxRank = 75, isSecondary = false },
            { name = "Enchanting", skillLineID = 333, rank = 47, maxRank = 75, isSecondary = false },
        },
        quests = { [1001] = { questID = 1001, title = "Prueba" } },
        items = { { bag = 0, slot = 1, itemID = 2589, count = 20 } },
        equipment = { [1] = { slotID = 1, itemID = 6833, link = "|Hitem:6833|h" } },
        spells = { { spellID = 3908, name = "Sastrería" } },
        reputations = { { factionID = 72, name = "Ventormenta", standing = 5 } },
    }
end

-- Lo que devuelven los collectors cuando las APIs aun no responden (carga o salida).
local function degradedData()
    return {
        money = 0, professions = {}, quests = {}, items = {},
        equipment = {}, spells = {}, reputations = {},
    }
end

-- Eventos de Core. Un error silenciado dentro del manejador se reporta como fallo del test.
local function fire(event)
    local before = ns.errorCount
    coreFrame.onEvent(coreFrame, event)
    if ns.errorCount ~= before then
        check("sin errores silenciados al disparar " .. event, false, tostring(ns.lastError))
    end
end

-- Estado de partida: dentro del mundo, con datos buenos guardados. Limpia pendientes de lecturas vacias.
local function setupInWorld()
    W.reset()
    W.uninstallAddons()
    world.loggedIn, world.exists = true, true
    S.inCombat = false
    S.now = S.now or 1700000000
    S.clock = (S.clock or 100) + 1000
    CompaneroWoWDB = nil
    data = fullData()
    fire("PLAYER_ENTERING_WORLD")
    ns.SnapshotAll()
    collectorCalls = 0
end

local function advanceClock(sec)
    S.clock = S.clock + sec
    S.now = S.now + sec
end

-- ---------------------------------------------------------------------------
outln("snapshot periodico:")
-- Primer PLAYER_ENTERING_WORLD: arranca el ticker (una sola vez por sesion).
setupInWorld()
local ticker
for _, t in ipairs(S.tickers) do
    if t.sec == 60 then ticker = t end
end
check("ticker de 60 s creado al entrar al mundo", ticker ~= nil)

data = fullData()
advanceClock(61)
collectorCalls = 0
W.tickAll()
check("el ticker toma snapshot dentro del mundo", collectorCalls > 0)

S.inCombat = true
advanceClock(61)
collectorCalls = 0
W.tickAll()
check("el ticker no toma snapshot en combate", collectorCalls == 0)
S.inCombat = false

fire("PLAYER_LEAVING_WORLD")
advanceClock(61)
collectorCalls = 0
W.tickAll()
check("el ticker no toma snapshot durante la carga", collectorCalls == 0)
fire("PLAYER_ENTERING_WORLD")

-- ---------------------------------------------------------------------------
outln("logout:")
setupInWorld()
local before = char()
local keptProf = before.professions
local keptMoney = before.character.money
local keptUpdatedAt = before.updatedAt
S.now = S.now + 30
data = degradedData() -- lo que devolverian las APIs al cerrar sesion
collectorCalls = 0
fire("PLAYER_LOGOUT")
eq("logout no ejecuta collectors", collectorCalls, 0)
eq("logout conserva professions", char().professions, keptProf)
eq("logout conserva money", char().character.money, keptMoney)
eq("logout conserva quests", count(char().quests), 1)
eq("logout conserva inventario", count(char().inventory.items), 1)
eq("logout conserva equipo", count(char().equipment), 1)
eq("logout conserva spells", count(char().spells), 1)
eq("logout conserva reputaciones", count(char().reputations), 1)
eq("logout actualiza loggedOutAt", char().loggedOutAt, ns.Now())
eq("logout no cambia updatedAt del ultimo snapshot bueno", char().updatedAt, keptUpdatedAt)

-- Una lectura posterior, sin estar en el mundo, tampoco destruye los datos (fusion defensiva).
ns.SnapshotAll()
eq("snapshot fuera del mundo conserva professions", count(char().professions), 2)
eq("snapshot fuera del mundo conserva money", char().character.money, keptMoney)

-- PLAYER_LEAVING_WORLD (pantalla de carga) solo marca la hora.
setupInWorld()
data = degradedData()
collectorCalls = 0
fire("PLAYER_LEAVING_WORLD")
eq("leaving world no ejecuta collectors", collectorCalls, 0)
check("leaving world actualiza loggedOutAt", char().loggedOutAt == ns.Now())
eq("leaving world conserva professions", count(char().professions), 2)

-- ---------------------------------------------------------------------------
outln("carga y estado del cliente:")
setupInWorld()
local good = char().professions
fire("PLAYER_LEAVING_WORLD") -- empieza la pantalla de carga
data = degradedData()
advanceClock(10)
collectorCalls = 0
fire("BAG_UPDATE_DELAYED")
fire("PLAYER_CAMPING")
eq("eventos de snapshot en carga no ejecutan collectors", collectorCalls, 0)
-- Una lectura directa durante la carga (vacia) no reemplaza nada.
ns.SnapshotAll()
eq("degradado en carga: professions conservadas", char().professions, good)
eq("degradado en carga: money conservado", char().character.money, 600000)
eq("degradado en carga: quests conservadas", count(char().quests), 1)
eq("degradado en carga: inventario conservado", count(char().inventory.items), 1)
eq("degradado en carga: equipo conservado", count(char().equipment), 1)
eq("degradado en carga: spells conservadas", count(char().spells), 1)
eq("degradado en carga: reputaciones conservadas", count(char().reputations), 1)

-- IsLoggedIn false / UnitExists false con el evento de entrar ya disparado: sigue sin fiarse.
setupInWorld()
world.loggedIn = false
data = degradedData()
ns.SnapshotAll()
eq("IsLoggedIn false: professions conservadas", count(char().professions), 2)
world.loggedIn = true
world.exists = false
ns.SnapshotAll()
eq("UnitExists false: professions conservadas", count(char().professions), 2)
world.exists = true

-- ---------------------------------------------------------------------------
outln("vaciado confirmado (dos lecturas dentro del mundo):")
setupInWorld()
data = degradedData()
ns.SnapshotAll()
eq("primera lectura vacia: professions conservadas", count(char().professions), 2)
eq("primera lectura vacia: money conservado", char().character.money, 600000)
ns.SnapshotAll()
eq("segunda lectura vacia: professions vaciadas", count(char().professions), 0)
eq("segunda lectura vacia: money a cero", char().character.money, 0)

-- Una lectura buena entre dos vacias reinicia la confirmacion: no basta con dos vacias separadas.
setupInWorld()
data = degradedData()
ns.SnapshotAll()
data = fullData()
ns.SnapshotAll()
data = degradedData()
ns.SnapshotAll()
eq("vacia, buena, vacia: professions conservadas", count(char().professions), 2)

-- ---------------------------------------------------------------------------
outln("cambios reales:")
setupInWorld()
data = fullData()
data.professions = { { name = "Alquimia", skillLineID = 171, rank = 10, maxRank = 75, isSecondary = false } }
ns.SnapshotAll()
eq("cambio no vacio aplicado en una lectura", char().professions[1].name, "Alquimia")
eq("cambio no vacio: nueva lista de un elemento", count(char().professions), 1)

setupInWorld()
data = fullData()
data.money = 0
ns.SnapshotAll()
eq("money a cero: primera lectura conservada", char().character.money, 600000)
ns.SnapshotAll()
eq("money a cero: confirmada en la segunda", char().character.money, 0)

setupInWorld()
data = fullData()
data.money = 1234
ns.SnapshotAll()
eq("money nuevo no cero aplicado en una lectura", char().character.money, 1234)

-- ---------------------------------------------------------------------------
outln("integraciones y Questie:")
setupInWorld()
local c = char()
c.training = { available = true, source = "WhatsTraining", availableCount = 2, spells = { { spellID = 1 } } }
c.dungeons = { available = true, list = { { key = "mina-prueba" } } }
c.profPlan = { available = true, source = "SkillUpForever", professions = { { name = "Sastrería" } } }
c.questie = { available = true, version = "12.6.4", quests = {} }
local saved = { c.training, c.dungeons, c.profPlan, c.questie }

fire("PLAYER_LOGOUT")
eq("logout conserva training", char().training, saved[1])
eq("logout conserva dungeons", char().dungeons, saved[2])
eq("logout conserva profPlan", char().profPlan, saved[3])
eq("logout conserva questie", char().questie, saved[4])

-- Exportar durante la carga no toca nada.
eq("exportacion de integraciones en carga no hace nada", ns.ExportIntegrationsNow(), nil)
eq("exportacion de Questie en carga no hace nada", ns.ExportQuestieNow(), nil)
eq("carga: training intacto", char().training, saved[1])
eq("carga: questie intacto", char().questie, saved[4])

-- Dentro del mundo, sin los addons: la exportacion no disponible no sustituye a la buena anterior.
fire("PLAYER_ENTERING_WORLD")
W.uninstallAddons()
ns.ExportIntegrationsNow()
ns.ExportQuestieNow()
eq("no disponible no sustituye training", char().training, saved[1])
eq("no disponible no sustituye dungeons", char().dungeons, saved[2])
eq("no disponible no sustituye profPlan", char().profPlan, saved[3])
eq("no disponible no sustituye questie", char().questie, saved[4])

-- Con WhatsTraining instalado la exportacion correcta si reemplaza el bloque.
W.installAddons()
ns.ExportIntegrationsNow()
check("exportacion correcta reemplaza training", char().training ~= saved[1]
    and char().training.available == true)
W.uninstallAddons()

-- ---------------------------------------------------------------------------
if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
