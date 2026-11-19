-- gen_outbox_fixture.lua: genera app/testdata/sv_outbox_lean.lua con el codificador REAL del addon
-- (Json.lua y Storage.lua de la rama feat/addon-lean, tal cual). Los registros pasan por Storage.Learn,
-- que los compacta (claves cortas) y los mete en la bandeja con seq; luego se vuelca CompaneroWoWDB con
-- Storage.Serialize, igual que lo escribe WoW en SavedVariables.
--
-- Uso (desde cualquier sitio; <addon> es una carpeta que contiene CompaneroWoW/Json.lua y Storage.lua):
--   luajit gen_outbox_fixture.lua <addon>/CompaneroWoW <salida.lua>
-- Para sacar los ficheros de la rama:
--   git show feat/addon-lean:CompaneroWoW/Storage.lua  (y Json.lua) a una carpeta temporal.
local addonDir, outPath = arg[1], arg[2]
assert(addonDir and outPath, "uso: luajit gen_outbox_fixture.lua <CompaneroWoW dir> <salida.lua>")

local ns = {}
-- Hash de texto: solo sirve para no repetir un registro igual seguido; cualquier funcion estable vale.
ns.HashText = function(s)
    local h = 0
    for i = 1, #s do h = (h * 31 + s:byte(i)) % 2147483647 end
    return tostring(h)
end
ns.Now = function() return 1795000000 end
ns.SafeCall = function(fn, ...) return pcall(fn, ...) end

local function load(name)
    local chunk, err = loadfile(addonDir .. "/" .. name)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("Json.lua")
load("Storage.lua")
local St = ns.Storage

CompaneroWoWDB = { settings = { strip = true } }
St.Sync()

-- Conocimiento (9): una mision y un PNJ, con recompensas de la ventana de mision.
St.Learn(St.TYPE_KNOWLEDGE, { "quests", 127 }, {
    questID = 127, title = "Mision de prueba", level = 3, giverNpcID = 900, giverName = "Guardia",
    giverMapID = 1429, giverX = 0.50123, giverY = 0.5, turnInNpcID = 901, turnInName = "Capitan",
    turnInMapID = 1429, turnInX = 0.4, turnInY = 0.4, lastSeen = 1795000000,
    rewards = { money = 500, xp = 300, items = { { itemID = 6948, link = "|Hitem:6948|h[Piedra de hogar]|h|r", count = 1, choice = true } } },
})
St.Learn(St.TYPE_KNOWLEDGE, { "npcs", 900 }, {
    npcID = 900, name = "Guardia", mapID = 1429, x = 0.5, y = 0.5, isQuestGiver = true, isVendor = true,
    firstSeen = 1794000000, lastSeen = 1795000000,
})

-- Combate (7): la ruta es {"combats", id}; el registro lleva spells, dano recibido y muerte.
St.Learn(St.TYPE_COMBAT, { "combats", 1 }, {
    id = 1, startedAt = 1795000100, duration = 12.5, zone = "Bosque de Elwynn", died = false,
    targets = { { name = "Lobo" } },
    damageDone = { total = 300, dps = 24, spells = { { name = "Golpe heroico", spellID = 78, amount = 300, hits = 3 } } },
    healingDone = { total = 0, spells = {} },
    damageTaken = { total = 80, sources = { { name = "Lobo", amount = 80 } } },
    resourcesAtEnd = { healthPct = 40 },
})
St.Learn(St.TYPE_COMBAT, { "combats", 2 }, {
    id = 2, startedAt = 1795000200, duration = 30, zone = "Bosque de Elwynn", died = true,
    targets = { { name = "Jabali" } },
    damageDone = { total = 100, dps = 3.3, spells = {} },
    healingDone = { total = 0, spells = {} },
    damageTaken = { total = 500, sources = { { name = "Jabali", amount = 500 } } },
    death = { killer = "Jabali", lastHits = { { source = "Jabali", spellName = "Mordisco", amount = 120 } } },
})

-- Mundo (16): vendedor, ruta de vuelo medida, nodo conocido por un personaje, consumible.
St.Learn(St.TYPE_WORLD, { "vendors", 901 }, {
    npcID = 901, name = "Vendedor de viaje", mapID = 1426, x = 0.2, y = 0.2,
    items = { { itemID = 4604, name = "Barra de tiza", price = 10, stack = 5 } },
})
St.Learn(St.TYPE_WORLD, { "flight", "Alliance", "nodes", 10 }, { nodeID = 10, name = "Ventormenta", mapID = 1453, x = 0.5, y = 0.5 })
St.Learn(St.TYPE_WORLD, { "flight", "Alliance", "nodes", 11 }, { nodeID = 11, name = "Pueblo Lobo", mapID = 1429, x = 0.6, y = 0.3 })
St.Learn(St.TYPE_WORLD, { "flight", "Alliance", "costs", "10>11" }, { copper = 120, hops = 1, seconds = 45 })
St.Learn(St.TYPE_WORLD, { "flight", "known", "Fixture-Realm" }, { 10, 11 })

-- Hoja (12): la ruta lleva el dia (dias desde la epoca Unix): 20000 = 2024-10-04.
St.Learn(St.TYPE_SHEET, { "sheet", 20000 }, { stats = { stamina = 40, strength = 12 } })

-- Un registro repetido sin cambios no entra otra vez en la bandeja.
local again = St.Learn(St.TYPE_KNOWLEDGE, { "quests", 127 }, {
    questID = 127, title = "Mision de prueba", level = 3, giverNpcID = 900, giverName = "Guardia",
    giverMapID = 1429, giverX = 0.50123, giverY = 0.5, turnInNpcID = 901, turnInName = "Capitan",
    turnInMapID = 1429, turnInX = 0.4, turnInY = 0.4, lastSeen = 1795000999,
    rewards = { money = 500, xp = 300, items = { { itemID = 6948, link = "|Hitem:6948|h[Piedra de hogar]|h|r", count = 1, choice = true } } },
})
assert(again == nil, "a repeated record was queued again")

-- Personaje minimo (lo que queda en SavedVariables con el contrato lean).
CompaneroWoWDB.characters = {
    ["Fixture-Realm"] = { updatedAt = 1795000500, character = { name = "Fixture", realm = "Realm", level = 13, xp = 2100, xpMax = 4500, money = 26000 } },
}

local out = assert(io.open(outPath, "w"))
out:write("-- Generado por app/testdata/gen_outbox_fixture.lua con Json.lua y Storage.lua de feat/addon-lean.\n")
out:write("CompaneroWoWDB = ", St.Serialize(CompaneroWoWDB), "\n")
out:close()
print("items en la bandeja: " .. #CompaneroWoWDB.outbox.items .. ", seq " .. CompaneroWoWDB.outbox.seq)
