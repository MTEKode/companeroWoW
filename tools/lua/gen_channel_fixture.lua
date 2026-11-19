-- gen_channel_fixture.lua: genera app/testdata/channel_v31_fixture.json con el addon real (Channel.lua,
-- Bridge.lua, Json.lua) en luajit. Cada trama se guarda como colores de los N bloques (6 hex por bloque),
-- tal como se pintan en pantalla; el test de Go decodifica esas tramas y compara con los mensajes esperados.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/gen_channel_fixture.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Bridge.lua")
load("CompaneroWoW/Channel.lua")

local C, J = ns.Channel, ns.Json
local N = 860 -- 3440 px de ancho

-- Datos de prueba: un personaje con misiones (varios trozos), bolsas, banco de reputaciones y mas.
local key = "Fixture-Realm"
ns.CharKey = function() return key end
ns.InCombat = function() return false end

local quests = {}
for i = 1, 30 do
    quests[#quests + 1] = {
        questID = 1000 + i,
        title = "Mision de prueba numero " .. i,
        objectives = { { text = "Derrota 10 lobos del bosque", numFulfilled = i % 10, numRequired = 10 } },
    }
end
CompaneroWoWDB = {
    settings = {},
    characters = {
        [key] = {
            updatedAt = 1700000000,
            character = { name = "Fixture", realm = "Realm", level = 60, xp = 1234, xpMax = 5000, money = 98765 },
            reputations = { { factionID = 1, standing = 5, name = "Ventormentas" } },
            talents = { raw = { configID = 77, specID = 62 } },
            quests = quests,
            native = { available = true, sources = { juego = true }, quests = { { questID = 2001, name = "Mision nativa", mapID = 1429, source = "juego" } }, npcs = {} },
            location = { mapID = 1429, x = 0.45, y = 0.67 },
            inventory = { items = { { id = 6948, n = 1 }, { id = 2592, n = 20 } }, freeSlots = 12 },
            equipment = { { slot = 1, itemID = 12345 }, { slot = 16, itemID = 54321 } },
            gear = { ["1"] = { itemID = 12345 } },
            proficiencies = { "Espadas" },
            professions = { { skillLineID = 171, name = "Alquimia", rank = 150 } },
            completedQuests = { 1, 2, 3, 40, 41 },
            actionBars = { slots = { { slot = 1, kind = "spell", id = 585 } } },
            combats = { { id = 3, zone = "Bosque", damageDone = 1200 } },
        },
    },
    knowledge = {
        quests = { [1001] = { questID = 1001, title = "Mision de prueba numero 1" } },
        npcs = {},
    },
}

-- Estado del jugador (32 bytes con CRC-8) para todas las tramas.
local status = ns.BuildPayload({ mapID = 1429, x = 0.45, y = 0.67, level = 60, money = 98765,
    healthPct = 88, powerPct = 100, inCombat = false, hasTarget = true, lastEventCode = 3, eventSeq = 9, eventArg = 77 })

-- Pasa el tiempo hasta que todos los tipos estan calculados y encolados.
local now = 100
local frames = {}
local seq = 0
local hexOut = {}

local function colorsHex(colors)
    local parts = {}
    for b = 1, N do
        local c = colors[b]
        parts[#parts + 1] = string.format("%02x%02x%02x", math.floor(c / 65536), math.floor(c / 256) % 256, c % 256)
    end
    return table.concat(parts)
end

-- Como Bridge.lua: abre la ventana, serializa por trama (C.Pump) y toma el trozo de la trama (C.NextChunk).
C.Begin(now, { full = true })
local colorsBuf = {}
local sent = 0
local frameNo = 0
for i = 1, 400 do
    now = now + 0.2
    C.Pump(now, C.PUMP_BUDGET_MS)
    frameNo = frameNo + 1
    local chunk = C.NextChunk(N, frameNo)
    local sch = C.state.sched
    local pending = sch and sch:pending() or 0
    if chunk then
        seq = (seq + 1) % 256
        local frame = C.BuildFrame(status, seq, N, chunk)
        colorsBuf = ns.EncodeFrameColors(frame, N, colorsBuf)
        frames[#frames + 1] = colorsHex(colorsBuf)
        sent = sent + 1
    elseif i > 40 and pending == 0 then
        break -- todo calculado y enviado
    end
end

-- Mensajes esperados: el JSON completo de cada tipo con datos.
local expected = {}
for t = 1, 11 do
    local json = C.testing.fullJSON(t)
    if json then expected[#expected + 1] = { t = t, json = json } end
end

-- Escritura del fichero (JSON a mano: los textos ya estan codificados por Json).
local out = {}
out[#out + 1] = "{\n"
out[#out + 1] = '"generator": "tools/lua/gen_channel_fixture.lua",\n'
out[#out + 1] = string.format('"N": %d,\n', N)
out[#out + 1] = string.format('"status": "%s",\n', (function()
    local t = {}
    for i = 1, 32 do t[i] = string.format("%02x", status[i]) end
    return table.concat(t)
end)())
out[#out + 1] = '"frames": [\n'
for i, hex in ipairs(frames) do
    out[#out + 1] = '"' .. hex .. '"' .. (i < #frames and ",\n" or "\n")
end
out[#out + 1] = "],\n"
out[#out + 1] = '"messages": {\n'
for i, e in ipairs(expected) do
    out[#out + 1] = string.format('"%d": %s%s\n', e.t, e.json, i < #expected and "," or "")
end
out[#out + 1] = "}\n}\n"

local path = "app/testdata/channel_v31_fixture.json"
local f = assert(io.open(path, "wb"))
f:write(table.concat(out))
f:close()
print(string.format("escrito %s: %d tramas, %d trozos, %d mensajes", path, #frames, sent, #expected))
