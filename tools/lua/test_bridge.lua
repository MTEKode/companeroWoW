-- test_bridge.lua: pruebas offline del protocolo de la franja de pixeles (v2).
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

local function near(name, got, want, tol)
    check(name, math.abs(got - want) <= tol, string.format("got %s, want %s +-%s", tostring(got), tostring(want), tostring(tol)))
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
    shareRemainingSec = 17,
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
eq("byte 30 = shareRemainingSec 17 (p[31])", p[31], 17)
eq("freeBagSlots ya no va en el estado", ns.BuildPayload({ freeBagSlots = 9 })[31], 0)
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
print("(c) EncodeBlocks v2")
local blocks = ns.EncodeBlocks(p, 7)
eq("29 bloques", #blocks, 29)
eq("bloque 1 magia R", blocks[1][1], 255)
eq("bloque 1 magia G", blocks[1][2], 0)
eq("bloque 1 magia B", blocks[1][3], 255)
eq("bloque 2 magia R", blocks[2][1], 0)
eq("bloque 2 magia G", blocks[2][2], 255)
eq("bloque 2 magia B", blocks[2][3], 255)
eq("bloque 3 calibracion R", blocks[3][1], 85)
eq("bloque 3 calibracion G", blocks[3][2], 170)
eq("bloque 3 calibracion B", blocks[3][3], 0)
eq("bloque 4 calibracion R", blocks[4][1], 170)
eq("bloque 4 calibracion G", blocks[4][2], 85)
eq("bloque 4 calibracion B", blocks[4][3], 85)
eq("bloque 5 calibracion R", blocks[5][1], 0)
eq("bloque 5 calibracion G", blocks[5][2], 255)
eq("bloque 5 calibracion B", blocks[5][3], 170)

-- Cadencia de calibracion: cada canal tiene los niveles 0, 85, 170 y 255 en bloques 1..5.
local levels = { { 255, 0, 85, 170, 0 }, { 0, 255, 170, 85, 255 }, { 255, 255, 0, 85, 170 } }
for c = 1, 3 do
    local seen = {}
    for b = 1, 5 do seen[blocks[b][c]] = true end
    check("canal " .. c .. " tiene 0, 85, 170 y 255", seen[0] and seen[85] and seen[170] and seen[255])
end

-- Cabecera: bytes 2, 7, 32 -> nibbles 0,2 | 0,7 | 2,0 ; bloque 6 = (0,2,0)*17 ; bloque 7 = (7,2,0)*17
eq("bloque 6 cabecera R (nibbles 0,2,0)", blocks[6][1], 0)
eq("bloque 6 cabecera G", blocks[6][2], 2 * 17)
eq("bloque 6 cabecera B", blocks[6][3], 0)
eq("bloque 7 cabecera R (nibble 7)", blocks[7][1], 7 * 17)
eq("bloque 7 cabecera G (nibble 2)", blocks[7][2], 2 * 17)
eq("bloque 7 cabecera B (nibble 0)", blocks[7][3], 0)
-- Ultimo byte del payload (CRC) = nibbles 69,70 ; bloque 29 = (nib70, relleno 0, relleno 0)
local crcLo = p[32] % 16
eq("bloque 29 R = nibble bajo del CRC", blocks[29][1], crcLo * 17)
eq("bloque 29 G relleno = 0", blocks[29][2], 0)
eq("bloque 29 B relleno = 0", blocks[29][3], 0)
local nibOK = true
for i = 1, 29 do
    for c = 1, 3 do
        local v = blocks[i][c]
        if type(v) ~= "number" or v < 0 or v > 255 or v % 17 ~= 0 then nibOK = false end
    end
end
check("todos los canales son multiplos de 17 en 0..255", nibOK)
eq("seq 256 envuelve a 0", ns.EncodeBlocks(p, 256)[7][1], 0)
eq("seq 7 en bloque 7 R", ns.EncodeBlocks(p, 7)[7][1], 7 * 17)

-- Decodificacion de referencia (solo para el test): nibbles de bloques 6..29 -> bytes.
local nib = {}
for b = 6, 29 do
    for c = 1, 3 do nib[#nib + 1] = blocks[b][c] / 17 end
end
local bytes = {}
for k = 1, 35 do bytes[k] = nib[2 * k - 1] * 16 + nib[2 * k] end
eq("flujo: version 2", bytes[1], 2)
eq("flujo: seq 7", bytes[2], 7)
eq("flujo: longitud 32", bytes[3], 32)
local roundtrip = true
for k = 1, 32 do if bytes[k + 3] ~= p[k] then roundtrip = false end end
check("flujo: payload recuperado byte a byte", roundtrip)

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
print("(f) ComputePixelSize: unidades de marco por pixel fisico")
-- Caso real: Windows 133 %, ventana de 1440 px fisicos, UIParent de 768 unidades.
-- 1 unidad = 1440 / 768 = 1.875 px, asi que un bloque de 4 px mide 4 * 768 / 1440 unidades.
near("1440 px fisicos, UIParent 768 -> 768/1440 unidades por pixel", ns.ComputePixelSize(1440, 768), 768 / 1440, 1e-9)
near("bloque 4 px = 4 * 768/1440 unidades", 4 * ns.ComputePixelSize(1440, 768), 4 * 768 / 1440, 1e-9)
-- Comprobacion fisica: bloque (unidades) * unidades por pixel fisico = 4 pixeles fisicos.
near("bloque 4 px * (1440/768) = 4 px fisicos", 4 * ns.ComputePixelSize(1440, 768) * (1440 / 768), 4, 1e-9)
near("1080 px fisicos, UIParent 768 -> 768/1080", ns.ComputePixelSize(1080, 768), 768 / 1080, 1e-9)
near("sin UIParent (nil) -> referencia 768", ns.ComputePixelSize(1440, nil), 768 / 1440, 1e-9)
near("UIParent 900 -> 900/1440", ns.ComputePixelSize(1440, 900), 900 / 1440, 1e-9)
eq("altura fisica 0 -> nil", ns.ComputePixelSize(0, 768), nil)
eq("altura fisica nil -> nil", ns.ComputePixelSize(nil, 768), nil)

-- ---------------------------------------------------------------------------
print("(g) GameWindowHeight: fuentes de la altura fisica y escala del bloque")
local function setPhysical(fn) _G.GetPhysicalScreenSize = fn end
local function setCVars(t)
    _G.GetCVar = function(name) return t[name] end
end
local function clearSources()
    _G.C_VideoOptions = nil
    _G.C_CVar = nil
    _G.GetCVar = nil
end

-- 1) C_VideoOptions.GetCurrentGameWindowSize gana sobre todo lo demas.
clearSources()
_G.C_VideoOptions = { GetCurrentGameWindowSize = function() return { x = 3440, y = 1440 } end }
setCVars({ gxWindow = "0", gxFullscreenResolution = "1920x1080" })
setPhysical(function() return 1920, 1080 end)
local h, src = ns.GameWindowHeight()
eq("C_VideoOptions: altura 1440", h, 1440)
eq("C_VideoOptions: fuente", src, "C_VideoOptions")

-- 2) Sin C_VideoOptions: CVar gxFullscreenResolution en pantalla completa o sin ventana.
clearSources()
setCVars({ gxWindow = "0", gxFullscreenResolution = "3440x1440", gxWindowedResolution = "2560x1080" })
h, src = ns.GameWindowHeight()
eq("CVar pantalla completa: altura 1440", h, 1440)
eq("CVar pantalla completa: fuente", src, "CVar gxFullscreenResolution")

-- 3) Ventana (gxWindow 1): gxWindowedResolution.
clearSources()
setCVars({ gxWindow = "1", gxFullscreenResolution = "3440x1440", gxWindowedResolution = "2560x1080" })
h, src = ns.GameWindowHeight()
eq("CVar ventana: altura 1080", h, 1080)
eq("CVar ventana: fuente", src, "CVar gxWindowedResolution")

-- 4) Sin CVar: GetPhysicalScreenSize.
clearSources()
setPhysical(function() return 3440, 1440 end)
h, src = ns.GameWindowHeight()
eq("GetPhysicalScreenSize: altura 1440", h, 1440)
eq("GetPhysicalScreenSize: fuente", src, "GetPhysicalScreenSize")

-- 5) Nada disponible.
setPhysical(function() return nil end)
h, src = ns.GameWindowHeight()
eq("sin fuente: altura nil", h, nil)
eq("sin fuente: fuente ninguna", src, "ninguna")

-- Escala del bloque con UIParent 768 y la altura de C_VideoOptions: 4 px fisicos.
clearSources()
_G.C_VideoOptions = { GetCurrentGameWindowSize = function() return { x = 3440, y = 1440 } end }
_G.UIParent = { GetHeight = function() return 768 end }
ns.InitBridge()
local B = ns.bridge
near("InitBridge: bloque 4 px fisicos (4 * (1440/768) * unidades por pixel)", 4 * B.pixel * (B.physH / B.uiH), 4, 1e-9)
eq("InitBridge: fuente registrada", B.physSource, "C_VideoOptions")
local lines = table.concat(ns.BridgeDebugLines(), "\n")
check("estado: muestra la fuente", string.find(lines, "Fuente del tamano: C_VideoOptions", 1, true) ~= nil, lines)
check("estado: muestra la ventana fisica", string.find(lines, "Ventana fisica: 3440x1440 px", 1, true) ~= nil, lines)
check("estado: muestra el pitch esperado", string.find(lines, "Pitch esperado: 4.00 px", 1, true) ~= nil, lines)

-- Limpieza de stubs para no afectar a otras pruebas.
clearSources()
_G.UIParent = nil
setPhysical(function() return 1920, 1080 end)

-- ---------------------------------------------------------------------------
print("(h) Fuente de la altura: CVar antes que C_VideoOptions si este va escalado por DPI")
clearSources()
_G.C_VideoOptions = { GetCurrentGameWindowSize = function() return { x = 2580, y = 1080 } end }
setCVars({ gxWindow = "0", gxMaximize = "0", gxFullscreenResolution = "3440x1440", gxWindowedResolution = "2560x1080" })
setPhysical(function() return 2580, 1080 end)
h, src = ns.GameWindowHeight()
eq("video escalado (1080) + CVar real (1440): altura 1440", h, 1440)
eq("video escalado: fuente CVar con aviso", src, "CVar gxFullscreenResolution (C_VideoOptions escalado)")
clearSources()
_G.C_VideoOptions = { GetCurrentGameWindowSize = function() return { x = 3440, y = 1440 } end }
setCVars({ gxWindow = "0", gxFullscreenResolution = "3440x1440" })
h, src = ns.GameWindowHeight()
eq("video real y CVar iguales: fuente C_VideoOptions", src, "C_VideoOptions")
clearSources()
setCVars({ gxWindow = "1", gxMaximize = "1", gxFullscreenResolution = "3440x1440", gxWindowedResolution = "2560x1080" })
h, src = ns.GameWindowHeight()
eq("ventana maximizada: gxFullscreenResolution (1440)", h, 1440)
eq("ventana maximizada: fuente", src, "CVar gxFullscreenResolution")
clearSources()
setPhysical(function() return 2580, 1080 end)
h, src = ns.GameWindowHeight()
eq("sin C_VideoOptions ni CVar: GetPhysicalScreenSize (ultimo recurso)", src, "GetPhysicalScreenSize")

-- ---------------------------------------------------------------------------
print("(i) Ancho de la franja en pantalla: N en unidades del marco (modelo de pantalla propio)")
-- Modelo independiente del addon: pantalla de SCREEN_W x TRUE_H px reales; UIParent de UI_H
-- unidades (su ancho sale de la proporcion) con escala efectiva uiS. El marco tiene escala fs:
-- sus unidades miden fs * TRUE_H / (UI_H * uiS) px de pantalla cada una.
local SCREEN_W, TRUE_H, UI_H = 3440, 1440, 768
local origCreateFrame = _G.CreateFrame

local function fakeTexture()
    local t = {}
    t.SetSize = function(self, w, h) self.w, self.h = w, h end
    t.SetPoint = function() end
    t.SetColorTexture = function() end
    t.Show = function() end
    t.Hide = function() end
    t.SetSnapToPixelGrid = function() end
    t.SetTexelSnappingBias = function() end
    return t
end

local function fakeFrame(fs)
    local f = {}
    f.SetSize = function(self, w, h) self.w, self.h = w, h end
    f.GetEffectiveScale = function() return fs end
    f.CreateTexture = function() return fakeTexture() end
    setmetatable(f, { __index = function() return function(self) return self end end })
    return f
end

-- Monta la franja con esa pantalla. Devuelve N, ancho en px de pantalla, pitch en px y fuente.
local function runScreen(o)
    clearSources()
    B.frame, B.tex, B.blocks, B.painted = nil, {}, 0, {}
    _G.UIParent = {
        GetWidth = function() return SCREEN_W * UI_H / TRUE_H end,
        GetHeight = function() return UI_H end,
        GetEffectiveScale = function() return o.uiS end,
    }
    _G.CreateFrame = function() return fakeFrame(o.fs) end
    if o.video then _G.C_VideoOptions = { GetCurrentGameWindowSize = function() return o.video end } end
    if o.cvars then setCVars(o.cvars) end
    setPhysical(function() return o.physical[1], o.physical[2] end)
    ns.InitBridge()
    local pxPerUnit = o.fs * TRUE_H / (UI_H * o.uiS)
    return {
        n = B.blocks,
        stripPx = B.frame.w * pxPerUnit,
        pitch = B.tex[1].w * pxPerUnit,
        source = B.physSource,
        physH = B.physH,
        lines = table.concat(ns.BridgeDebugLines(), "\n"),
    }
end

local DPI_PHYS = { 2580, 1080 }   -- GetPhysicalScreenSize con Windows al 133 %
local DPI_VIDEO = { x = 2580, y = 1080 }
local REAL_VIDEO = { x = 3440, y = 1440 }
local CVARS = { gxWindow = "0", gxMaximize = "0", gxFullscreenResolution = "3440x1440", gxWindowedResolution = "2560x1080" }

for _, us in ipairs({ 0.948, 1.0, 1.406, 1.875 }) do
    for _, fs in ipairs({ 1.0, 0.5 }) do
        local tag = string.format("uiS %.3f, marco %.1f", us, fs)
        local r = runScreen({ uiS = us, fs = fs, video = DPI_VIDEO, cvars = CVARS, physical = DPI_PHYS })
        check(tag .. ": franja dentro de la pantalla (" .. string.format("%.1f", r.stripPx) .. " px)",
            r.stripPx <= SCREEN_W + 1e-6, string.format("%.3f > %d", r.stripPx, SCREEN_W))
        check(tag .. ": N >= 600 (" .. r.n .. ")", r.n >= 600)
        near(tag .. ": bloque de 4 px fisicos", r.pitch, 4, 1e-6)
        eq(tag .. ": altura fisica desde la CVar", r.physH, 1440)
    end
end

-- Escala del bloque en unidades: 4 * 768 * uiS / (1440 * fs) (la correccion del 1.055 medido).
local r948 = runScreen({ uiS = 0.948, fs = 1.0, video = DPI_VIDEO, cvars = CVARS, physical = DPI_PHYS })
near("uiS 0.948: bloque 2.0210 unidades", ns.bridge.block, 4 * 768 * 0.948 / 1440, 1e-9)
eq("uiS 0.948: N = 859", r948.n, 859)
check("estado: fuente, ventana, UIParent, marco, bloque, N y pitch en una linea cada una",
    select(2, string.gsub(r948.lines, "\n", "")) == 6 and string.find(r948.lines, "Pitch esperado: 4.00 px", 1, true) ~= nil, r948.lines)

-- Sin CVar ni C_VideoOptions: la altura DPI (1080) da bloques de 5.33 px, pero la franja
-- sigue dentro de la pantalla: el ajuste no depende de la altura fisica.
local rDpi = runScreen({ uiS = 0.948, fs = 1.0, physical = DPI_PHYS })
check("solo GetPhysicalScreenSize (1080): franja dentro de la pantalla (" .. string.format("%.1f", rDpi.stripPx) .. " px)",
    rDpi.stripPx <= SCREEN_W + 1e-6, string.format("%.3f", rDpi.stripPx))
near("solo GetPhysicalScreenSize: pitch 5.33 px (sin CVar, la altura es la de DPI)", rDpi.pitch, 4 * 1440 / 1080, 1e-6)
check("solo GetPhysicalScreenSize: N >= 600 (" .. rDpi.n .. ")", rDpi.n >= 600)

-- Video real: bloques de 4 px.
local rReal = runScreen({ uiS = 0.948, fs = 1.0, video = REAL_VIDEO, cvars = CVARS, physical = DPI_PHYS })
near("video real 3440x1440: pitch 4 px", rReal.pitch, 4, 1e-6)
eq("video real: fuente C_VideoOptions", rReal.source, "C_VideoOptions")

-- Anchura de la pantalla 3440: el hueco dejado es el margen de 1 bloque (<= 4 px fisicos, en el caso 4 px).
check("margen: la franja deja al menos un bloque libre", rReal.stripPx <= SCREEN_W - 4 + 1e-6, string.format("%.3f", rReal.stripPx))

-- Limpieza.
_G.CreateFrame = origCreateFrame
B.frame, B.tex, B.blocks, B.painted = nil, {}, 0, {}
clearSources()
_G.UIParent = nil
setPhysical(function() return 1920, 1080 end)

-- ---------------------------------------------------------------------------
print("(j) Colores de la franja v3.2: suelo 22, paso 6, calibracion 22/52/82/112")
local function rgbOf(c) return math.floor(c / 65536), math.floor(c / 256) % 256, c % 256 end
-- Trama de prueba con los 16 nibbles: bytes 0x01, 0x23, ..., 0xEF.
local allNibbles = { 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF }
local sc = ns.EncodeStripColors(allNibbles, 20, {})
eq("franja: 20 bloques", #sc, 20)
local wantFixed = {
    { 255, 0, 255 }, { 0, 255, 255 },
    { 22, 52, 82 }, { 52, 82, 112 }, { 82, 112, 22 }, { 112, 22, 52 },
    { 255, 255, 255 },
}
local fixedOK, calMin = true, 255
for i = 1, 7 do
    local r, g, b = rgbOf(sc[i])
    if r ~= wantFixed[i][1] or g ~= wantFixed[i][2] or b ~= wantFixed[i][3] then fixedOK = false end
    if i >= 3 and i <= 6 then calMin = math.min(calMin, r, g, b) end
end
check("franja: magia y blanco como en v3.1, calibracion 22/52/82/112 en cuadrado latino", fixedOK)
eq("franja: ningun nivel de calibracion por debajo del suelo", calMin, 22)
-- Los 16 nibbles, en orden, salen como 22 + 6 * n.
local levelsOK = true
for n = 0, 15 do
    local blockIdx = 8 + math.floor(n / 3)
    local r, g, b = rgbOf(sc[blockIdx])
    local got = ({ r, g, b })[n % 3 + 1]
    if got ~= 22 + 6 * n then levelsOK = false end
end
check("franja: nibble n -> 22 + 6 * n", levelsOK)
do
    local r, g, b = rgbOf(sc[14]) -- nibbles 18, 19 y 20: mas alla de la trama
    check("franja: nibbles sobrantes = nibble 0 = nivel 22 (nunca negro)", r == 22 and g == 22 and b == 22)
end
-- Las dos rectas medidas en el juego (visto = ganancia * nivel + desplazamiento; por debajo de 0, negro):
-- con v3.1 la de Contraste 60 / Brillo 45 junta los nibbles 0 y 1; con v3.2 todos quedan separados.
local function minGap(level, gain, offset)
    local worst, lowest = math.huge, nil
    for n = 0, 15 do
        local v = math.max(0, math.min(255, gain * level(n) + offset))
        if n == 0 then lowest = v end
        if n > 0 then
            local prev = math.max(0, math.min(255, gain * level(n - 1) + offset))
            worst = math.min(worst, v - prev)
        end
    end
    return worst, lowest
end
local function v32(n) return 22 + 6 * n end
local function v31(n) return math.floor(n * 110 / 15 + 0.5) end
local gapA, lowA = minGap(v32, 1.75, -7.75)
check("franja v3.2 con 1.75 v - 7.75: niveles vecinos a 10.5 y el mas bajo en 30.75",
    math.abs(gapA - 10.5) < 1e-9 and math.abs(lowA - 30.75) < 1e-9, gapA .. " / " .. lowA)
local gapB = minGap(v32, 1.70, 11.3)
check("franja v3.2 con 1.70 v + 11.3: niveles vecinos a 10.2", math.abs(gapB - 10.2) < 1e-9, tostring(gapB))
local gapOld = minGap(v31, 1.75, -7.75)
check("v3.1 con 1.75 v - 7.75: los nibbles 0 y 1 quedan a menos de 5 (el fallo que arregla la v3.2)", gapOld < 5, tostring(gapOld))
local _, topSeen = minGap(function() return 112 end, 2.2, 0)
check("franja v3.2: el techo con ganancia x2.2 no satura", topSeen < 250, tostring(topSeen))

-- EncodeFrameColors sigue siendo v3.1 (panel de capturas): sin cambios.
local oc = ns.EncodeFrameColors(allNibbles, 20, {})
do
    local r, g, b = rgbOf(oc[3])
    check("v3.1 (capturas): calibracion 0/37/73 intacta", r == 0 and g == 37 and b == 73)
    local r8, g8, b8 = rgbOf(oc[8])
    check("v3.1 (capturas): nibbles 0, 1, 2 -> 0, 7, 15", r8 == 0 and g8 == 7 and b8 == 15)
    local _, _, b12 = rgbOf(oc[12])
    local r13 = rgbOf(oc[13])
    check("v3.1 (capturas): nibbles 14 y 15 -> 103 y 110", b12 == 103 and r13 == 110)
end
check("out reutilizable: recorta lo que sobra", #ns.EncodeStripColors(allNibbles, 10, sc) == 10)

-- BridgeRefresh pinta la franja con los colores v3.2 (y la trama lleva la version 5).
do
    load("CompaneroWoW/Json.lua")
    load("CompaneroWoW/Channel.lua")
    local painted = {}
    local function recTexture(i)
        local t = fakeTexture()
        t.SetColorTexture = function(_, r, g, b) painted[i] = { r, g, b } end
        return t
    end
    local count = 0
    local f = fakeFrame(1)
    f.CreateTexture = function() count = count + 1 return recTexture(count) end
    _G.CreateFrame = function() return f end
    _G.UIParent = { GetWidth = function() return 256 end, GetHeight = function() return 768 end,
        GetEffectiveScale = function() return 1 end }
    setPhysical(function() return 256, 768 end)
    local B = ns.bridge
    B.frame, B.tex, B.blocks, B.painted, B.bytes = nil, {}, 0, {}, nil
    CompaneroWoWDB = { settings = {} }
    ns.InitBridge()
    ns.BridgeRefresh()
    local n = B.blocks
    check("refresco: hay franja", n > 7, tostring(n))
    eq("refresco: la trama lleva la version 5", B.bytes and B.bytes[1], 5)
    local function px(i, ch) return painted[i] and math.floor(painted[i][ch] * 255 + 0.5) end
    check("refresco: bloque 3 = calibracion (22, 52, 82)", px(3, 1) == 22 and px(3, 2) == 52 and px(3, 3) == 82)
    check("refresco: bloque 7 = blanco", px(7, 1) == 255 and px(7, 2) == 255 and px(7, 3) == 255)
    -- Bloque 8: nibbles de la version (0 y 5) y nibble alto de seq.
    check("refresco: bloque 8 empieza por la version 5 (22, 52)", px(8, 1) == 22 and px(8, 2) == 22 + 6 * 5)
    local lowOK = true
    for i = 8, n do
        for ch = 1, 3 do
            local v = px(i, ch)
            if not v or v < 22 or v > 112 or (v - 22) % 6 ~= 0 then lowOK = false end
        end
    end
    check("refresco: todos los bloques de datos en 22..112, en pasos de 6", lowOK)
    _G.CreateFrame = origCreateFrame
    B.frame, B.tex, B.blocks, B.painted, B.bytes = nil, {}, 0, {}, nil
    _G.UIParent = nil
    CompaneroWoWDB = nil
    setPhysical(function() return 1920, 1080 end)
end

-- ---------------------------------------------------------------------------
if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
os.exit(0)
