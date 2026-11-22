-- test_extras.lua: pruebas offline de Extras.lua y de las secciones nuevas del estado guardado (0.2.0):
-- bags, bank, reputations, currencies, recipes, guildRoster e info. Las APIs del juego estan simuladas en
-- wowstub.lua (installExtras); cada bloque se prueba con datos, con la API ausente y con la API rota.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_extras.lua
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

_G.print = function() end
_G.CreateFrame = function()
    local f = { events = {} }
    function f:RegisterEvent(name) self.events[name] = true end
    function f:SetScript(name, fn) if name == "OnEvent" then self.onEvent = fn end end
    return f
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
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Storage.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Collectors.lua")
load("CompaneroWoW/Extras.lua")

local E, St = ns.Extras, ns.Storage
local CHAR = "Tester-Ravencrest"
local X -- estado mutable del stub (W.EXTRAS)

-- Mundo nuevo: juego, datos guardados y APIs de Extras recien instaladas.
local function newWorld(noExtras)
    W.uninstallExtras()
    W.reset()
    E.ResetForTests()
    CompaneroWoWDB = nil
    W.state.inCombat = false
    ns.errorCount, ns.lastError = 0, nil
    X = (not noExtras) and W.installExtras() or nil
    ns.EnsureDB()
end
local function char() return CompaneroWoWDB.characters[CHAR] end
local function state() return char() and char().state or nil end
-- Pasa el tiempo (GetTime) y ejecuta los temporizadores pendientes.
local function pass(sec)
    W.state.clock = W.state.clock + sec
    W.state.now = W.state.now + sec
    W.runTimers()
end
local function fire(event, ...) E.OnEvent(event, ...) end
local function byName(list, name, key)
    for _, e in ipairs(list or {}) do
        if e[key or "name"] == name then return e end
    end
    return nil
end

-- ---------------------------------------------------------------------------
outln("(a) Bolsas, reputaciones y dinero: lo que ya se recogia llega al estado")
newWorld()
check("snapshot: se ejecuta", ns.SnapshotAll() == true)
local st = state()
check("estado: existe", type(st) == "table")
eq("dinero: sigue en el personaje guardado", char().character.money, 123456)
eq("descanso: XP de descanso en el personaje guardado", char().character.restedXP, 1500)
check("bolsas: van al estado", type(st.bags) == "table")
local bags = St.Expand(st.bags)
local lino, seda = byName(bags.items, "Tela de lino"), byName(bags.items, "Tela de seda")
check("bolsas: un objeto repartido en dos huecos se suma", lino ~= nil and lino.count == 27, lino and tostring(lino.count))
check("bolsas: id, nombre (del enlace), cantidad y calidad", seda ~= nil and seda.itemID == 4306 and seda.count == 12 and seda.quality == 1)
eq("bolsas: objetos distintos", bags.distinct, 4)
eq("bolsas: primero los de mas calidad", bags.items[1].name, "Gema de sombra")
eq("bolsas: huecos totales", bags.totalSlots, 24)
eq("bolsas: huecos libres", bags.freeSlots, 19)
check("bolsas: forma compacta (i, n, c)", st.bags.it[1].i == 1210 and st.bags.it[1].n == "Gema de sombra" and st.bags.it[1].c == 2)
check("bolsas: sin enlaces ni posiciones", not string.find(St.Serialize(st.bags), "Hitem", 1, true)
    and st.bags.it[1].bag == nil and st.bags.it[1].slot == nil and st.bags.it[1].l == nil)
eq("reputaciones: sin cabeceras", #st.reputations, 2)
local vt = byName(st.reputations, "Ventormenta")
check("reputaciones: nivel y progreso dentro del nivel", vt ~= nil and vt.standing == 6 and vt.current == 3000 and vt.max == 12000 and vt.factionID == 72)
check("snapshot: Extras tambien se lee (info)", type(st.info) == "table" and st.info.hearth == "Ventormenta")
eq("sin errores silenciados", ns.errorCount, 0)

-- Tope de objetos distintos.
newWorld()
for i = 1, 16 do X.containers[0].items[i] = { 1000 + i, "Objeto " .. i, 1, 0 } end
local saved = St.STATE_BAG_ITEMS
St.STATE_BAG_ITEMS = 5
ns.SnapshotAll()
eq("bolsas: tope de objetos distintos", #state().bags.it, 5)
eq("bolsas: el total de distintos se conserva", state().bags.distinct, 17)
St.STATE_BAG_ITEMS = saved

-- ---------------------------------------------------------------------------
outln("(b) Banco: solo con el banco abierto")
newWorld()
eq("banco cerrado: sin lectura", E.CollectBank(), nil)
ns.ExportExtrasNow()
eq("banco cerrado: no hay seccion", state() and state().bank, nil)
fire("BANKFRAME_OPENED")
check("banco abierto: va al estado al momento", state() ~= nil and type(state().bank) == "table")
local bank = St.Expand(state().bank)
local bseda = byName(bank.items, "Tela de seda")
check("banco: objeto del banco principal", bseda ~= nil and bseda.count == 40 and bseda.itemID == 4306)
check("banco: objeto de una bolsa del banco", byName(bank.items, "Aguamarina") ~= nil and byName(bank.items, "Aguamarina").count == 3)
eq("banco: huecos totales (principal + bolsa)", bank.totalSlots, 34)
eq("banco: huecos libres", bank.freeSlots, 31)
check("banco: hora de la lectura", type(bank.at) == "number")
check("banco: no incluye las bolsas del personaje", byName(bank.items, "Tela de lino") == nil)
X.containers[-1].items[3] = { 3577, "Barra de oro", 5, 2 }
fire("PLAYERBANKSLOTS_CHANGED")
pass(3)
check("banco: un cambio con el banco abierto se relee", byName(St.Expand(state().bank).items, "Barra de oro") ~= nil)
fire("BANKFRAME_CLOSED")
X.containers[-1].items = {}
fire("BAG_UPDATE_DELAYED")
pass(3)
ns.ExportExtrasNow()
check("banco cerrado: se conserva la ultima lectura", byName(St.Expand(state().bank).items, "Barra de oro") ~= nil)
eq("banco cerrado: la lectura es nil", E.CollectBank(), nil)
-- En combate no se lee.
newWorld()
W.state.inCombat = true
fire("BANKFRAME_OPENED")
pass(3)
eq("combate: el banco no se lee", state() and state().bank, nil)
W.state.inCombat = false
fire("PLAYER_REGEN_ENABLED")
pass(3)
check("combate: se lee al salir de combate", state() ~= nil and state().bank ~= nil)
eq("banco: sin errores silenciados", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
outln("(c) Recetas: se leen con la ventana de la profesion abierta y no se olvidan")
newWorld()
fire("TRADE_SKILL_SHOW")
eq("recetas: no se leen antes de la espera", state() and state().recipes, nil)
pass(3)
local rec = state().recipes
eq("recetas: una profesion", #rec, 1)
eq("recetas: nombre de la profesion", rec[1].name, "Sastrería")
eq("recetas: nivel", rec[1].rank, 150)
eq("recetas: maximo", rec[1].max, 225)
eq("recetas: sin cabeceras", #rec[1].list, 2)
eq("recetas: ordenadas por nombre", rec[1].list[1], "Bolsa de seda")
check("recetas: solo nombres", type(rec[1].list[2]) == "string")
-- Ventana filtrada: la lectura trae menos recetas.
X.trade.list = { { "Bolsa de seda", "optimal" } }
fire("TRADE_SKILL_UPDATE")
pass(3)
eq("recetas: una ventana filtrada no borra las conocidas", #state().recipes[1].list, 2)
X.trade.list = { { "Bolsa de seda", "optimal" }, { "Toga de seda", "optimal" } }
X.trade.rank = 151
fire("TRADE_SKILL_UPDATE")
pass(3)
eq("recetas: la nueva se anade", #state().recipes[1].list, 3)
eq("recetas: el nivel se actualiza", state().recipes[1].rank, 151)
-- Ventana de craft (encantamiento) y ventana de craft que no es profesion (adiestrar mascota).
X.craft = { name = "Encantamiento", rank = 80, max = 150, list = { { "Encantar brazal: salud menor", "", "optimal" }, { "Vara", "", "header" } } }
fire("CRAFT_SHOW")
pass(3)
eq("recetas: segunda profesion (craft)", #state().recipes, 2)
local ench = byName(state().recipes, "Encantamiento")
check("recetas: craft sin cabeceras", ench ~= nil and #ench.list == 1 and ench.rank == 80)
X.craft = { name = nil, list = { { "Mordedura 2", "", "optimal" } } }
fire("CRAFT_UPDATE")
pass(3)
eq("recetas: una ventana de craft sin profesion no se guarda", #state().recipes, 2)
-- Sin ventana abierta.
X.trade = nil
fire("TRADE_SKILL_UPDATE")
pass(3)
eq("recetas: sin ventana no cambia nada", #byName(state().recipes, "Sastrería").list, 3)
-- Tras una recarga (la memoria se pierde, el estado queda): leer una profesion conserva las demas.
E.ResetForTests()
char().recipes = nil
X.craft = { name = "Encantamiento", rank = 81, max = 150, list = { { "Encantar capa: proteccion menor", "", "optimal" } } }
fire("CRAFT_SHOW")
pass(3)
eq("recarga: siguen las dos profesiones", #state().recipes, 2)
eq("recarga: la otra profesion conserva sus recetas", #byName(state().recipes, "Sastrería").list, 3)
eq("recarga: la leida suma la nueva", #byName(state().recipes, "Encantamiento").list, 2)
-- Tope por profesion y de profesiones.
local many = {}
for i = 1, 400 do many[i] = string.format("Receta %03d", i) end
local merged = E.MergeRecipes(nil, "Herrería", 300, 300, many, 5)
eq("recetas: tope por profesion", #merged[1].list, E.RECIPES_MAX)
local profs = {}
for i = 1, E.PROFESSIONS_MAX do profs[i] = { name = "P" .. i, at = i, list = { "x" } } end
local capped = E.MergeRecipes(profs, "Nueva", 1, 75, { "y" }, 100)
eq("recetas: tope de profesiones", #capped, E.PROFESSIONS_MAX)
check("recetas: sale la mas antigua", byName(capped, "P1") == nil and byName(capped, "Nueva") ~= nil)
eq("recetas: la lista anterior no cambia", #profs, E.PROFESSIONS_MAX)
-- API moderna (C_TradeSkillUI) cuando no existe la clasica.
newWorld()
_G.GetNumTradeSkills, _G.GetTradeSkillLine, _G.GetTradeSkillInfo = nil, nil, nil
_G.C_TradeSkillUI = {
    GetBaseProfessionInfo = function() return { professionName = "Alquimia", skillLevel = 60, maxSkillLevel = 75 } end,
    GetAllRecipeIDs = function() return { 11, 12, 13 } end,
    GetRecipeInfo = function(id)
        return ({ [11] = { name = "Poción menor", learned = true }, [12] = { name = "No aprendida", learned = false } })[id]
    end,
}
fire("TRADE_SKILL_LIST_UPDATE")
pass(3)
check("recetas (API moderna): solo las aprendidas", state().recipes[1].name == "Alquimia" and #state().recipes[1].list == 1
    and state().recipes[1].list[1] == "Poción menor" and state().recipes[1].rank == 60)
eq("recetas: sin errores silenciados", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
outln("(d) Monedas")
newWorld()
ns.ExportExtrasNow()
local cur = state().currencies
eq("monedas: sin cabeceras", #cur, 2)
check("monedas: nombre y cantidad", cur[1].name == "Favor del mercader" and cur[1].count == 14)
X.currencies[2][3] = 20
fire("CURRENCY_DISPLAY_UPDATE")
pass(11)
eq("monedas: se actualizan con su evento", state().currencies[1].count, 20)
for i = 1, 80 do X.currencies[#X.currencies + 1] = { "Moneda " .. i, false, i } end
ns.ExportExtrasNow()
eq("monedas: tope", #state().currencies, E.CURRENCIES_MAX)
-- API moderna.
newWorld()
_G.GetCurrencyListSize, _G.GetCurrencyListInfo = nil, nil
_G.C_CurrencyInfo = {
    GetCurrencyListSize = function() return 2 end,
    GetCurrencyListInfo = function(i)
        return ({ { name = "Varios", isHeader = true }, { name = "Ficha de torneo", quantity = 7, currencyID = 241 } })[i]
    end,
}
ns.ExportExtrasNow()
check("monedas (API moderna): nombre, cantidad e id", #state().currencies == 1 and state().currencies[1].name == "Ficha de torneo"
    and state().currencies[1].count == 7 and state().currencies[1].id == 241)

-- ---------------------------------------------------------------------------
outln("(e) Hermandad: lista basica, sin notas")
newWorld()
ns.ExportExtrasNow()
local roster = St.Expand(state().guildRoster)
eq("hermandad: nombre", roster.guild, "Legio VII")
eq("hermandad: total", roster.total, 3)
eq("hermandad: conectados", roster.online, 2)
eq("hermandad: conectados primero, por nivel", roster.members[1].name, "Tester")
eq("hermandad: el nombre va sin reino", roster.members[1].name, "Tester")
eq("hermandad: segundo conectado", roster.members[2].name, "Aela")
eq("hermandad: los desconectados al final", roster.members[3].name, "Brutus")
check("hermandad: nivel, clase y rango", roster.members[3].level == 42 and roster.members[3].class == "Mago" and roster.members[3].rank == "Miembro")
check("hermandad: zona solo de los conectados", roster.members[1].zone == "Elwynn" and roster.members[3].zone == nil)
check("hermandad: forma compacta (n, lv)", state().guildRoster.members[1].n == "Tester" and state().guildRoster.members[1].lv == 30)
local text = St.Serialize(CompaneroWoWDB)
check("privacidad: ninguna nota publica en lo guardado", not string.find(text, "nota publica", 1, true) and not string.find(text, "alter de Pepe", 1, true))
check("privacidad: ninguna nota de oficial en lo guardado", not string.find(text, "nota de oficial", 1, true) and not string.find(text, "no fiable", 1, true))
local memText = ns.Json.Encode(char().guildRoster) or ""
check("privacidad: tampoco en memoria", not string.find(memText, "nota", 1, true) and not string.find(memText, "no fiable", 1, true))
-- Intervalo: la lista no se relee en cada evento.
X.guild.members[2][4] = 43
fire("GUILD_ROSTER_UPDATE")
pass(3)
eq("hermandad: dentro del intervalo no se relee", St.Expand(state().guildRoster).members[3].level, 42)
pass(E.INTERVAL.guildRoster)
eq("hermandad: pasado el intervalo, si", St.Expand(state().guildRoster).members[3].level, 43)
-- Tope de miembros.
for i = 1, 300 do X.guild.members[#X.guild.members + 1] = { "Miembro" .. i, "Miembro", 4, i % 60 + 1, "Pícaro", "Zona", "n", "o", false } end
ns.ExportExtrasNow()
eq("hermandad: tope de miembros", #state().guildRoster.members, E.ROSTER_MAX)
eq("hermandad: el total real se conserva", state().guildRoster.total, 303)
check("presupuesto: el estado cabe con la lista llena", #St.Serialize(state()) <= St.STATE_MAX_BYTES)
-- Sin hermandad.
newWorld()
X.guild.members = {}
ns.ExportExtrasNow()
eq("sin hermandad: no hay seccion", state().guildRoster, nil)

-- ---------------------------------------------------------------------------
outln("(f) Info: hogar, rango, JcJ, mascota, bloqueos y tiempo jugado")
newWorld()
ns.ExportExtrasNow()
local info = state().info
eq("info: piedra de hogar", info.hearth, "Ventormenta")
check("info: rango en la hermandad", info.guildRank == "Oficial" and info.guildRankIndex == 2)
check("info: JcJ", info.pvp.kills == 120 and info.pvp.dishonorable == 1 and info.pvp.rankName == "Cabo" and info.pvp.rank == 3 and info.pvp.honor == 450)
check("info: mascota", info.pet.name == "Colmillo" and info.pet.level == 29 and info.pet.family == "Lobo")
check("info: habilidades de la mascota, sin las ordenes", #info.pet.abilities == 2 and info.pet.abilities[1] == "Mordedura")
eq("info: bloqueos vigentes", #info.lockouts, 1)
check("info: bloqueo con nombre y hora de reinicio", info.lockouts[1].name == "Monasterio Escarlata"
    and info.lockouts[1].resetAt % 600 == 0 and math.abs(info.lockouts[1].resetAt - (W.state.now + 7200)) <= 300)
eq("info: sin /played no hay tiempo jugado", info.played, nil)
fire("TIME_PLAYED_MSG", 360000, 7200)
pass(31)
check("info: tiempo jugado (solo si el juego lo anuncia)", state().info.played.total == 360000 and state().info.played.level == 7200)
check("info: el addon no pide el tiempo jugado", #W.callsNamed("RequestTimePlayed") == 0)
-- La mascota guardada y el tiempo jugado se conservan cuando ya no se pueden leer.
X.pet = nil
X.hearth = "Forjaz"
fire("HEARTHSTONE_BOUND")
pass(31)
eq("info: el hogar cambia", state().info.hearth, "Forjaz")
check("info: la mascota guardada se conserva", state().info.pet ~= nil and state().info.pet.name == "Colmillo")
E.ResetForTests()
char().info = nil
ns.ExportExtrasNow()
check("recarga: el tiempo jugado sigue en el estado", state().info.played ~= nil and state().info.played.total == 360000)
X.lockouts = {}
ns.ExportExtrasNow()
eq("info: sin bloqueos la lista queda vacia", #state().info.lockouts, 0)
fire("UNIT_PET", "target")
check("info: UNIT_PET de otra unidad no pide nada", #W.state.timers == 0)
eq("info: sin errores silenciados", ns.errorCount, 0)
-- Valores secretos: se descartan.
newWorld()
_G.issecretvalue = function(v) return v == "Ventormenta" or v == 123456 end
ns.SnapshotAll()
eq("secretos: el hogar secreto no se guarda", state().info.hearth, nil)
eq("secretos: el dinero secreto no se guarda", char().character.money, nil)
_G.issecretvalue = nil

-- ---------------------------------------------------------------------------
outln("(g) API ausente: cada funcion que falta deja su bloque sin dato, sin errores")
newWorld(true)
eq("ausente: banco", E.CollectBank(), nil)
eq("ausente: profesion", (E.ReadTradeSkill()), nil)
eq("ausente: craft", (E.ReadCraft()), nil)
eq("ausente: monedas", E.CollectCurrencies(), nil)
eq("ausente: hermandad", E.CollectRoster(), nil)
check("ausente: info vacia", next(E.CollectInfo()) == nil)
for _, ev in ipairs({ "BANKFRAME_OPENED", "PLAYERBANKSLOTS_CHANGED", "TRADE_SKILL_SHOW", "CRAFT_SHOW", "CURRENCY_DISPLAY_UPDATE",
                      "GUILD_ROSTER_UPDATE", "PLAYER_ENTERING_WORLD", "UNIT_PET", "UPDATE_INSTANCE_INFO", "TIME_PLAYED_MSG",
                      "PLAYER_REGEN_ENABLED", "BANKFRAME_CLOSED", "EVENTO_DESCONOCIDO" }) do
    local ok = pcall(fire, ev)
    check("ausente: " .. ev .. " no lanza error", ok)
end
pass(200)
check("ausente: exportar todo no lanza error", pcall(ns.ExportExtrasNow))
check("ausente: el snapshot no lanza error", pcall(ns.SnapshotAll))
local empty = state() or {}
check("ausente: ninguna seccion nueva", empty.bank == nil and empty.recipes == nil and empty.currencies == nil
    and empty.guildRoster == nil and empty.info == nil and empty.reputations == nil)
eq("ausente: sin errores silenciados", ns.errorCount, 0)
check("ausente: la linea de estado no lanza error", pcall(ns.ExtrasStatusLine))
-- Una a una: con el resto presente, cada funcion ausente no rompe nada ni borra lo guardado.
local NAMES = {
    "GetContainerNumSlots", "GetContainerNumFreeSlots", "GetContainerItemInfo", "GetNumFactions", "GetFactionInfo", "GetMoney",
    "GetXPExhaustion", "GetTradeSkillLine", "GetNumTradeSkills", "GetTradeSkillInfo", "GetCraftDisplaySkillLine", "GetNumCrafts",
    "GetCraftInfo", "GetCurrencyListSize", "GetCurrencyListInfo", "GetNumGuildMembers", "GetGuildRosterInfo", "GetGuildInfo",
    "GetBindLocation", "GetPVPLifetimeStats", "UnitPVPRank", "GetPVPRankInfo", "GetHonorCurrency", "UnitExists",
    "UnitCreatureFamily", "GetPetActionInfo", "GetNumSavedInstances", "GetSavedInstanceInfo",
}
-- Deja el mundo con todo leido y guardado (banco, profesion y craft abiertos).
local function fullWorld()
    newWorld()
    X.craft = { name = "Encantamiento", rank = 80, max = 150, list = { { "Encantar brazal", "", "optimal" } } }
    fire("BANKFRAME_OPENED")
    fire("TRADE_SKILL_SHOW")
    fire("CRAFT_SHOW")
    pass(3)
    ns.SnapshotAll()
    ns.errorCount = 0
end
local function allSections(s)
    return s.bags ~= nil and s.bank ~= nil and s.reputations ~= nil and s.currencies ~= nil and s.recipes ~= nil
        and #s.recipes == 2 and s.guildRoster ~= nil and s.info ~= nil and s.info.hearth ~= nil and s.info.pet ~= nil
end
fullWorld()
check("completo: todas las secciones nuevas estan", allSections(state()))
local missingOk, missingKept, missingErrors = true, true, 0
for _, name in ipairs(NAMES) do
    fullWorld()
    _G[name] = nil
    for _, ev in ipairs({ "PLAYERBANKSLOTS_CHANGED", "TRADE_SKILL_UPDATE", "CRAFT_UPDATE", "CURRENCY_DISPLAY_UPDATE", "GUILD_ROSTER_UPDATE", "UNIT_PET" }) do
        if not pcall(fire, ev) then missingOk = false end
    end
    pass(200)
    if not pcall(ns.ExportExtrasNow) or not pcall(ns.SnapshotAll) then missingOk = false end
    if not allSections(state()) then
        missingKept = false
        outln("    falta una seccion sin " .. name)
    end
    missingErrors = missingErrors + ns.errorCount
end
check("ausente una a una: nunca un error de Lua", missingOk)
check("ausente una a una: lo guardado se conserva", missingKept)
eq("ausente una a una: sin errores silenciados", missingErrors, 0)

-- ---------------------------------------------------------------------------
outln("(h) API rota: una funcion que lanza error no rompe nada ni borra lo guardado")
local brokenOk, brokenKept, counted = true, true, 0
for _, name in ipairs(NAMES) do
    fullWorld()
    X.fail[name] = true
    for _, ev in ipairs({ "PLAYERBANKSLOTS_CHANGED", "TRADE_SKILL_UPDATE", "CRAFT_UPDATE", "CURRENCY_DISPLAY_UPDATE", "GUILD_ROSTER_UPDATE", "UNIT_PET" }) do
        if not pcall(fire, ev) then brokenOk = false end
    end
    pass(200)
    if not pcall(ns.ExportExtrasNow) or not pcall(ns.SnapshotAll) then brokenOk = false end
    if not allSections(state()) then
        brokenKept = false
        outln("    falta una seccion con " .. name .. " roto")
    end
    if ns.errorCount > 0 then counted = counted + 1 end
end
check("rota: nunca un error de Lua", brokenOk)
check("rota: lo guardado se conserva", brokenKept)
check("rota: los errores quedan contados (salud)", counted > 0, tostring(counted))
-- Formas inesperadas: tablas donde se esperan numeros, textos enormes, cuentas absurdas.
newWorld()
_G.GetNumGuildMembers = function() return 1e9 end
_G.GetGuildRosterInfo = function(i) if i <= 3 then return string.rep("x", 500), {}, "z", "muchos", 5, {}, nil, nil, 1 end end
_G.GetNumSavedInstances = function() return "tres" end
_G.GetCurrencyListSize = function() return -5 end
_G.GetBindLocation = function() return {} end
_G.GetPVPLifetimeStats = function() return "x" end
_G.GetNumTradeSkills = function() return 0 / 0 end
check("formas raras: no lanza error", pcall(ns.ExportExtrasNow) and pcall(fire, "TRADE_SKILL_SHOW"))
pass(3)
local odd = St.Expand(state().guildRoster)
check("formas raras: nombres acotados y campos invalidos fuera", #odd.members == 3)
check("formas raras: nombre recortado", #odd.members[1].name <= E.NAME_MAX and odd.members[1].level == nil and odd.members[1].rank == nil)
check("formas raras: info sin los campos invalidos", state().info.hearth == nil and state().info.pvp == nil and state().info.lockouts == nil)

-- ---------------------------------------------------------------------------
outln("(i) Compatibilidad del estado")
newWorld()
-- Un estado de la version anterior (sin secciones nuevas) se conserva y se completa.
CompaneroWoWDB.characters[CHAR] = { state = { at = 5, location = { zone = "Elwynn" }, quests = { [7] = { questID = 7, title = "Vieja" } } } }
ns.EnsureDB()
ns.ExportExtrasNow()
check("estado anterior: sus secciones siguen", state().location.zone == "Elwynn" and state().quests[7].title == "Vieja")
check("estado anterior: se anaden las nuevas", state().info ~= nil and state().currencies ~= nil)
-- Las secciones que ya existian mantienen su orden relativo de recorte, y la hoja sigue siendo la ultima.
-- (Macros y barras pasan delante: se recortan antes que las misiones y el equipo.)
local OLD = { "macros", "actionBars", "training", "profPlan", "gear", "proficiencies", "quests", "professions", "location",
              "equipment", "completedQuests", "dungeons", "gearCandidates", "questWindow", "questieStatus", "sheet" }
local pos = {}
for i, name in ipairs(St.STATE_SECTIONS) do pos[name] = i end
local ordered = true
for i = 2, #OLD do
    if not (pos[OLD[i - 1]] and pos[OLD[i]] and pos[OLD[i - 1]] < pos[OLD[i]]) then ordered = false end
end
check("secciones: las anteriores conservan su orden", ordered)
eq("secciones: la hoja es la ultima", St.STATE_SECTIONS[#St.STATE_SECTIONS], "sheet")
local firstOld = pos.macros
check("secciones: las nuevas se recortan antes que las anteriores", pos.guildRoster < firstOld and pos.recipes < firstOld
    and pos.bank < firstOld and pos.bags < firstOld and pos.currencies < firstOld and pos.reputations < firstOld and pos.info < firstOld)
-- Presupuesto: con todo lleno, lo nuevo cede antes que la hoja.
fullWorld()
char().sheet = { level = 30, spells = {} }
for i = 1, 60 do char().sheet.spells[i] = { spellID = i, name = "Hechizo " .. i } end
local hugeSaved = St.STATE_MAX_BYTES
St.STATE_MAX_BYTES = 3000
St.SaveState(char())
check("presupuesto: el estado cabe en el tope", #St.Serialize(state()) <= St.STATE_MAX_BYTES)
check("presupuesto: la hermandad y las recetas ceden antes que la hoja", state().guildRoster == nil and state().recipes == nil and state().sheet ~= nil)
St.STATE_MAX_BYTES = hugeSaved
-- Sin cambios no se reescribe nada.
fullWorld()
X.lockouts = {} -- su hora de reinicio se calcula desde "ahora" en el stub
ns.ExportExtrasNow()
local at = state().at
pass(500)
ns.ExportExtrasNow()
ns.SnapshotAll()
eq("sin cambios: el estado no se reescribe", state().at, at)
eq("version del addon", ns.VERSION, "0.2.1")
local toc = io.open("CompaneroWoW/CompaneroWoW.toc"):read("*a")
check("toc: version y fichero nuevo", string.find(toc, "## Version: 0.2.1", 1, true) ~= nil and string.find(toc, "\nExtras.lua", 1, true) ~= nil)
check("linea de estado", string.find(ns.ExtrasStatusLine(), "Extras: bolsas", 1, true) ~= nil)

-- ---------------------------------------------------------------------------
outln("(j) Revision: formas antiguas de la API, lecturas vacias y datos caducados")
-- Honor: la API puede devolver un segundo valor; solo cuenta el primero.
newWorld()
X.pvp.honorExtra = "esta semana"
ns.ExportExtrasNow()
local hi = (state() or {}).info or {}
check("honor: un segundo valor que no es numero no pierde el bloque info", hi.hearth == "Ventormenta" and hi.pet ~= nil and hi.lockouts ~= nil)
check("honor: se lee el primer valor", hi.pvp ~= nil and hi.pvp.honor == 450)
eq("honor: sin errores silenciados", ns.errorCount, 0)
newWorld()
X.pvp.honorExtra = 0
ns.ExportExtrasNow()
eq("honor: un segundo valor 0 no lo deja en 0", ((state().info or {}).pvp or {}).honor, 450)
-- Booleanos al estilo antiguo (1 / nil).
newWorld()
X.oneNil = true
ns.ExportExtrasNow()
local r1 = St.Expand(state().guildRoster)
eq("1/nil: conectados de la hermandad", r1.online, 2)
check("1/nil: conectados primero y con zona", r1.members[1].name == "Tester" and r1.members[1].zone == "Elwynn" and r1.members[3].name == "Brutus")
eq("1/nil: monedas sin cabeceras", #state().currencies, 2)
check("1/nil: la mascota existe", state().info.pet ~= nil and state().info.pet.name == "Colmillo")
newWorld()
_G.GetCurrencyListSize, _G.GetCurrencyListInfo = nil, nil
_G.GetNumTradeSkills, _G.GetTradeSkillLine, _G.GetTradeSkillInfo = nil, nil, nil
_G.C_CurrencyInfo = {
    GetCurrencyListSize = function() return 2 end,
    GetCurrencyListInfo = function(i) return ({ { name = "Varios", isHeader = 1 }, { name = "Ficha", quantity = 7 } })[i] end,
}
_G.C_TradeSkillUI = {
    GetBaseProfessionInfo = function() return { professionName = "Alquimia", skillLevel = 60, maxSkillLevel = 75 } end,
    GetAllRecipeIDs = function() return { 11, 12 } end,
    GetRecipeInfo = function(id) return ({ [11] = { name = "Poción menor", learned = 1 }, [12] = { name = "No aprendida" } })[id] end,
}
ns.ExportExtrasNow()
check("1/nil (API moderna): cabecera de monedas fuera", #state().currencies == 1 and state().currencies[1].name == "Ficha")
check("1/nil (API moderna): receta aprendida", #state().recipes == 1 and #state().recipes[1].list == 1)
-- Banco: una lectura vacia nada mas abrirlo no borra el banco guardado.
newWorld()
fire("BANKFRAME_OPENED")
pass(3)
fire("BANKFRAME_CLOSED")
local function bankCount() return #(St.Expand(state().bank).items or {}) end
eq("banco: guardado con objetos", bankCount(), 3)
local mainItems, bagItems = X.containers[-1].items, X.containers[5].items
X.containers[-1].items, X.containers[5].items = {}, {}
fire("BANKFRAME_OPENED")
eq("banco: la lectura vacia al abrir no borra lo guardado", bankCount(), 3)
pass(2)
eq("banco: tampoco la de dos segundos despues", bankCount(), 3)
mainItems[3] = { 3577, "Barra de oro", 5, 2 }
X.containers[-1].items, X.containers[5].items = mainItems, bagItems
pass(2)
check("banco: cuando llegan los objetos se leen", bankCount() == 4 and byName(St.Expand(state().bank).items, "Barra de oro") ~= nil)
fire("BANKFRAME_CLOSED")
-- Tras una recarga (sin copia en memoria) el guardado en el estado tambien se respeta.
E.ResetForTests()
char().bank = nil
X.containers[-1].items, X.containers[5].items = {}, {}
fire("BANKFRAME_OPENED")
pass(2)
eq("banco (recarga): la lectura vacia al abrir no borra lo guardado", bankCount(), 4)
pass(2)
pass(2)
eq("banco: un banco que sigue vacio pasada la espera si se guarda", bankCount(), 0)
eq("banco: sin errores silenciados", ns.errorCount, 0)
-- Hermandad y mascota que ya no existen.
newWorld()
local savedUnitClass = _G.UnitClass
ns.ExportExtrasNow()
check("caducado: de partida hay rango, lista y mascota", state().info.guildRank == "Oficial" and #state().guildRoster.members == 3 and state().info.pet ~= nil)
X.guild.name, X.guild.rank, X.guild.rankIndex, X.guild.members = nil, nil, nil, {}
ns.ExportExtrasNow()
check("sin IsInGuild: el rango y la lista se conservan", state().info.guildRank == "Oficial" and #state().guildRoster.members == 3)
_G.IsInGuild = function() error("roto") end
ns.ExportExtrasNow()
check("IsInGuild roto: el rango y la lista se conservan", state().info.guildRank == "Oficial" and #state().guildRoster.members == 3)
_G.IsInGuild = function() return true end
ns.ExportExtrasNow()
check("en hermandad sin datos cargados: se conservan", state().info.guildRank == "Oficial" and #state().guildRoster.members == 3)
_G.IsInGuild = function() return false end
_G.GetGuildInfo = nil
ns.ExportExtrasNow()
check("sin GetGuildInfo: se conservan", state().info.guildRank == "Oficial" and #state().guildRoster.members == 3)
_G.GetGuildInfo = function() return nil end
ns.ExportExtrasNow()
check("sin hermandad: el rango se borra", state().info.guildRank == nil and state().info.guildRankIndex == nil)
local gone = St.Expand(state().guildRoster)
check("sin hermandad: la lista queda vacia", #(gone.members or {}) == 0 and gone.total == 0 and gone.guild == nil)
eq("sin hermandad: el resto de info sigue", state().info.hearth, "Ventormenta")
check("sin hermandad: la linea de estado no falla", pcall(ns.ExtrasStatusLine))
X.pet = nil
_G.UnitClass = function() return "Cazador", "HUNTER" end
ns.ExportExtrasNow()
check("mascota: la de un cazador se conserva sin invocar", state().info.pet ~= nil and state().info.pet.name == "Colmillo")
_G.UnitClass = function() return "Guerrero", "WARRIOR" end
_G.UnitExists = nil
ns.ExportExtrasNow()
check("mascota: sin UnitExists se conserva", state().info.pet ~= nil)
_G.UnitExists = function(unit) return unit ~= "pet" end
ns.ExportExtrasNow()
eq("mascota: la de una clase sin mascota se borra", state().info.pet, nil)
_G.UnitClass = savedUnitClass
-- Sin hermandad desde siempre: no se crea una lista vacia.
newWorld()
X.guild.name, X.guild.rank, X.guild.rankIndex, X.guild.members = nil, nil, nil, {}
_G.IsInGuild = function() return false end
ns.ExportExtrasNow()
eq("sin hermandad desde siempre: no hay seccion", state().guildRoster, nil)
-- Temporizador roto: los eventos siguen leyendo.
newWorld()
local savedAfter = _G.C_Timer.After
_G.C_Timer.After = function() error("temporizador roto") end
check("temporizador roto: el evento no lanza error", pcall(fire, "HEARTHSTONE_BOUND"))
check("temporizador roto: se lee al momento", state() ~= nil and state().info ~= nil and state().info.hearth == "Ventormenta")
_G.C_Timer.After = savedAfter
X.hearth = "Forjaz"
fire("HEARTHSTONE_BOUND")
pass(31)
eq("temporizador roto: al volver, los eventos siguen programando lecturas", state() and state().info and state().info.hearth, "Forjaz")
-- Profesion fantasma: un nombre sin ventana abierta (lista vacia) no crea profesion.
newWorld()
X.trade = nil
_G.GetTradeSkillLine = function() return "Sastrería", 0, 0 end
fire("TRADE_SKILL_UPDATE")
pass(3)
eq("recetas: nombre sin ventana (0 recetas) no crea profesion", (state() or {}).recipes, nil)
X.craft = { name = "Encantamiento", rank = 1, max = 75, list = {} }
fire("CRAFT_UPDATE")
pass(3)
eq("recetas: craft con nombre y sin lista no crea profesion", (state() or {}).recipes, nil)
-- GetContainerItemInfo sin itemID: el id sale del enlace.
newWorld()
X.noItemID = true
ns.SnapshotAll()
local nb = St.Expand(state().bags)
check("sin itemID: las bolsas no quedan vacias", nb.distinct == 4 and byName(nb.items, "Tela de seda") ~= nil and byName(nb.items, "Tela de seda").itemID == 4306)
fire("BANKFRAME_OPENED")
check("sin itemID: el banco tampoco", state().bank ~= nil and byName(St.Expand(state().bank).items, "Aguamarina") ~= nil
    and byName(St.Expand(state().bank).items, "Aguamarina").itemID == 7909)
eq("sin itemID: sin errores silenciados", ns.errorCount, 0)

W.uninstallExtras()
if failures > 0 then
    outln(string.format("FAILURES: %d", failures))
    os.exit(1)
end
outln("ALL TESTS PASSED")
