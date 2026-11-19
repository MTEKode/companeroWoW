-- gen_shot_fixture.lua: genera app/testdata/shot_panel_blocks.json para el decodificador de la app
-- (T52, canal de capturas). Un payload conocido (estado valido + 200 bytes de datos en dos trozos)
-- se compone con Shot.lua (filas de version 5) y se pinta como colores de bloque (v3.1, Bridge.lua).
-- El JSON trae: parametros del panel, las tramas (bytes) y los colores de cada bloque por fila, para
-- que el decodificador compruebe que lee lo mismo que el addon.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/gen_shot_fixture.lua
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
load("CompaneroWoW/Shot.lua")

local OUT = "app/testdata/shot_panel_blocks.json"
local SHOT_ID = 4321
local DATA_LEN = 200
local FIRST_CHUNK = 132 -- = capacidad de una fila de 128 bloques

-- Estado de ejemplo (valido: lleva su CRC-8 en el byte 32).
local status = ns.BuildPayload({
    mapID = 1404, x = 0.4, y = 0.6, level = 60, xpPct = 25, money = 12345,
    trackedQuestID = 0, targetNpcID = 0, targetLevel = 61, healthPct = 90, powerPct = 70,
    inCombat = false, dead = false, resting = true, mounted = false, inGroup = false,
    targetHostile = false, hasTarget = false, inInstance = false,
    lastEventCode = 0, eventSeq = 0, eventArg = 0, shareRemainingSec = 20,
})

-- Datos conocidos: byte k = (37k + 11) mod 256.
local data = {}
for k = 1, DATA_LEN do data[k] = (k * 37 + 11) % 256 end
local function slice(from, to)
    local out = {}
    for k = from, to do out[#out + 1] = data[k] end
    return out
end
local chunks = {
    { type = 2, id = 7, index = 0, count = 2, data = slice(1, FIRST_CHUNK) },
    { type = 2, id = 7, index = 1, count = 2, data = slice(FIRST_CHUNK + 1, DATA_LEN) },
}

local geo = ns.Shot.Geometry({ DATA_LEN }, ns.Shot.BLOCK_PX)
assert(geo.W == 256 and geo.N == 128 and geo.rows == 2 and geo.shots == 1, "geometria inesperada")
local frames, why = ns.Shot.PanelFrames(status, chunks, geo.N, SHOT_ID)
assert(frames, why)
local W, H = ns.Shot.PanelSize(geo.N, geo.rows, geo.blockPx)

local function arr(t)
    return "[" .. table.concat(t, ",") .. "]"
end

local frameJSON, rowJSON = {}, {}
for r, frame in ipairs(frames) do
    frameJSON[r] = arr(frame)
    local colors = ns.EncodeFrameColors(frame, geo.N, {})
    local blocks = {}
    for b = 1, geo.N do
        local c = colors[b]
        blocks[b] = arr({ math.floor(c / 65536), math.floor(c / 256) % 256, c % 256 })
    end
    rowJSON[r] = arr(blocks)
end

local json = table.concat({
    "{\n",
    '"description": "Panel de datos de captura (contrato SCREENSHOT CHANNEL, version 5). Generado por tools/lua/gen_shot_fixture.lua; no editar a mano.",\n',
    '"version": 5,\n',
    '"blockPx": ' .. geo.blockPx .. ',\n',
    '"W": ' .. W .. ',\n',
    '"H": ' .. H .. ',\n',
    '"N": ' .. geo.N .. ',\n',
    '"shotId": ' .. SHOT_ID .. ',\n',
    '"dataRows": ' .. geo.rows .. ',\n',
    '"data": ' .. arr(data) .. ',\n',
    '"frames": [\n' .. table.concat(frameJSON, ",\n") .. '\n],\n',
    '"blocks": [\n' .. table.concat(rowJSON, ",\n") .. '\n]\n',
    "}\n",
}, "")

local fh = assert(io.open(OUT, "wb"))
fh:write(json)
fh:close()
io.write(string.format("fixture escrito: %s (%d filas, %d bloques por fila, %dx%d px)\n",
    OUT, #frames, geo.N, W, H))
