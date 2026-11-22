-- gen_state_fixture.lua: escribe un fichero de SavedVariables (CompaneroWoWDB) como el que guarda el addon
-- 0.2.0, con las secciones nuevas del estado (bags, bank, reputations, currencies, recipes, guildRoster,
-- info) leidas de las APIs simuladas de wowstub.lua. El servidor lo usa como dato de prueba
-- (companero-bot/test/fixtures/extras.lua): asi sus pruebas leen lo que el addon escribe de verdad.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/gen_state_fixture.lua <fichero de salida>
package.path = "tools/lua/?.lua;" .. package.path
local W = require("wowstub")

local out = arg and arg[1]
if not out then
    io.stderr:write("uso: luajit tools/lua/gen_state_fixture.lua <fichero de salida>\n")
    os.exit(2)
end

_G.print = function() end
SlashCmdList = {}
CompaneroWoWDB = nil
W.installGameAPI()
local X = W.installExtras()
_G.UnitClass = function() return "Guerrero", "WARRIOR" end
_G.UnitRace = function() return "Humano", "Human" end
_G.UnitXP = function() return 5200 end
_G.UnitXPMax = function() return 44000 end
W.state.level = 30

local ns = {}
for _, file in ipairs({ "Util", "Json", "Storage", "Core", "Collectors", "Extras" }) do
    local chunk, err = loadfile("CompaneroWoW/" .. file .. ".lua")
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end

X.craft = { name = "Encantamiento", rank = 80, max = 150, list = {
    { "Vara rúnica de cobre", "", "optimal" }, { "Encantar brazal: salud menor", "", "trivial" } } }
ns.EnsureDB()
local E = ns.Extras
E.OnEvent("BANKFRAME_OPENED")
E.OnEvent("TRADE_SKILL_SHOW")
E.OnEvent("CRAFT_SHOW")
E.OnEvent("TIME_PLAYED_MSG", 360000, 7200)
W.state.clock = W.state.clock + 60
W.runTimers()
ns.SnapshotAll()
-- La hoja la escribe Sheet.lua: aqui solo lo que el servidor lee de ella para estas secciones.
local c = CompaneroWoWDB.characters["Tester-Ravencrest"]
c.sheet = { level = 30, class = "WARRIOR", talents = { unspent = 2 },
            weaponSkills = { { name = "Espadas", rank = 140, max = 150 }, { name = "Mazas", rank = 12, max = 150 } } }
ns.Storage.SaveState(c)

local f = assert(io.open(out, "wb"))
f:write("CompaneroWoWDB = ", ns.Storage.Serialize(CompaneroWoWDB), "\n")
f:close()
io.write("escrito ", out, " (", #ns.Storage.Serialize(CompaneroWoWDB), " bytes)\n")
