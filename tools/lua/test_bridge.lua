-- test_bridge.lua: pruebas offline del protocolo de la franja de pixeles.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_bridge.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

local ns = {}

local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end

load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Collectors.lua") -- ReadLiveState usa GetPlayerPosition/XPPercent/CountFreeBagSlots
load("CompaneroWoW/Bridge.lua")

local failures = 0
local function check(name, cond, detail)
    if cond then
        print("  ok    " .. name)
    else
        failures = failures + 1
        print("  FAIL  " .. name .. (detail and (" -> " .. detail) or ""))
    end
end

local function eq(name, got, want)
    check(name, got == want, string.format("got %s, want %s", tostring(got), tostring(want)))
end

-- CRC-8 de referencia, solo aritmetica (independiente de bit.*).
local function xor8(a, b)
    local r, p = 0, 1
    for _ = 1, 8 do
        if (a % 2) ~= (b % 2) then r = r + p end
        a = math.floor(a / 2)
        b = math.floor(b / 2)
        p = p * 2
    end
    return r
end

local function crc8Reference(bytes, n)
    local crc = 0
    for i = 1, n do
        crc = xor8(crc, bytes[i])
        for _ = 1, 8 do
            local top = crc >= 128
            crc = (crc * 2) % 256
            if top then crc = xor8(crc, 7) end
        end
    end
    return crc
end

-- ---------------------------------------------------------------------------
print("(a) BuildPayload con un estado conocido")
local state = {
    mapID = 1429,
    x = 0.5,
    y = 0.25,
    level = 70,
    xpPct = 50,
    money = 123456789,
    trackedQuestID = 12345,
    targetNpcID = 0x01020304,
    targetLevel = 60,
    healthPct = 100,
    powerPct = 80,
    inCombat = true,
    resting = true,
    inGroup = true,
    targetHostile = true,
    hasTarget = true,
    lastEventCode = 1,
    eventSeq = 5,
    eventArg = 71,
    freeBagSlots = 10,
}
local p = ns.BuildPayload(state)
eq("payload tiene 32 bytes", #p, 32)
eq("mapID 1429 hi", p[1], 0x05)
eq("mapID 1429 lo", p[2], 0x95)
eq("x=0.5 -> 32768 hi", p[3], 0x80)
eq("x=0.5 -> 32768 lo", p[4], 0x00)
eq("y=0.25 -> 16384 hi", p[5], 0x40)
eq("y=0.25 -> 16384 lo", p[6], 0x00)
eq("level 70", p[7], 70)
eq("xpPct 50", p[8], 50)
eq("money 123456789 b0", p[9], 0x07)
eq("money 123456789 b1", p[10], 0x5B)
eq("money 123456789 b2", p[11], 0xCD)
eq("money 123456789 b3", p[12], 0x15)
eq("trackedQuest 12345 b0", p[13], 0x00)
eq("trackedQuest 12345 b1", p[14], 0x00)
eq("trackedQuest 12345 b2", p[15], 0x30)
eq("trackedQuest 12345 b3", p[16], 0x39)
eq("targetNpc 0x01020304 b0", p[17], 0x01)
eq("targetNpc 0x01020304 b3", p[20], 0x04)
eq("targetLevel 60", p[21], 60)
eq("healthPct 100", p[22], 100)
eq("powerPct 80", p[23], 80)
eq("flags inCombat|resting|inGroup|targetHostile|hasTarget = 117", p[24], 117)
eq("lastEventCode 1", p[25], 1)
eq("eventSeq 5", p[26], 5)
eq("eventArg 71 b3", p[30], 71)
eq("freeBagSlots 10", p[31], 10)
eq("crc8 coincide con referencia", p[32], crc8Reference(p, 31))

eq("UnitLevel -1 -> targetLevel 255", ns.BuildPayload({ targetLevel = -1 })[21], 255)
eq("healthPct ausente -> 255", ns.BuildPayload({})[22], 255)
eq("money clamp a 2^32-1", ns.BuildPayload({ money = 99999999999 })[9], 255)

-- ---------------------------------------------------------------------------
print("(b) CRC-8 de \"123456789\" = 0xF4")
local digits = { string.byte("123456789", 1, -1) }
eq("ns.CRC8 de 123456789", ns.CRC8(digits), 0xF4)
eq("referencia de 123456789", crc8Reference(digits, #digits), 0xF4)

-- ---------------------------------------------------------------------------
print("(c) EncodeBlocks")
local blocks = ns.EncodeBlocks(p, 7)
eq("14 bloques", #blocks, 14)
eq("bloque 1 magia R", blocks[1][1], 255)
eq("bloque 1 magia G", blocks[1][2], 0)
eq("bloque 1 magia B", blocks[1][3], 255)
eq("bloque 2 magia R", blocks[2][1], 0)
eq("bloque 2 magia G", blocks[2][2], 255)
eq("bloque 2 magia B", blocks[2][3], 255)
eq("bloque 3 = {1, seq, 32} R", blocks[3][1], 1)
eq("bloque 3 = {1, seq, 32} seq", blocks[3][2], 7)
eq("bloque 3 = {1, seq, 32} len", blocks[3][3], 32)
eq("bloque 4 = bytes 0..2 R", blocks[4][1], p[1])
eq("bloque 4 = bytes 0..2 G", blocks[4][2], p[2])
eq("bloque 4 = bytes 0..2 B", blocks[4][3], p[3])
eq("bloque 14 = bytes 30,31 + padding", blocks[14][1], p[31])
eq("bloque 14 = byte 31 (crc)", blocks[14][2], p[32])
eq("bloque 14 padding = 0", blocks[14][3], 0)
eq("seq 256 envuelve a 0", ns.EncodeBlocks(p, 256)[3][2], 0)
local allBytes = true
for i = 1, 14 do
    for c = 1, 3 do
        local v = blocks[i][c]
        if type(v) ~= "number" or v < 0 or v > 255 then allBytes = false end
    end
end
check("todos los canales en 0..255", allBytes)

-- ---------------------------------------------------------------------------
print("(d) healthPct = 255 cuando issecretvalue devuelve true")
UnitHealth = function() return 50 end
UnitHealthMax = function() return 100 end
issecretvalue = function() return true end
eq("salud secreta -> 255", ns.BuildPayload(ns.ReadLiveState())[22], 255)
issecretvalue = function() return false end
eq("salud visible -> 50", ns.BuildPayload(ns.ReadLiveState())[22], 50)
issecretvalue = nil
UnitHealth, UnitHealthMax = nil, nil

-- ---------------------------------------------------------------------------
print("(e) utilidades: GUID, Clean, endianness")
local kind, npcID = ns.ParseGUID("Creature-0-1-0-3-12345-000000ABCD")
eq("GUID Creature tipo", kind, "Creature")
eq("GUID Creature npcID", npcID, 12345)
eq("GUID jugador no da npcID", select(2, ns.ParseGUID("Player-3391-0A1B2C3D")), nil)
issecretvalue = function() return true end
eq("Clean descarta secretos", ns.Clean(42), nil)
issecretvalue = nil
eq("Clean deja pasar valores normales", ns.Clean(42), 42)
local out = {}
ns.AppendU32(out, 0x01020304)
eq("AppendU32 big-endian", table.concat(out, ","), "1,2,3,4")

-- ---------------------------------------------------------------------------
if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
os.exit(0)
