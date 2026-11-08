-- Bridge.lua: franja de pixeles (addon -> app), protocolo v2 (robusto a escalado y desplazamiento de color).
-- BuildPayload, EncodeBlocks y ComputePixelSize son funciones puras (testeables offline).
-- GameWindowHeight consulta la API (C_VideoOptions, CVar, GetPhysicalScreenSize) y se prueba con stubs.
-- Los arrays son 1-based: byte k (0-based del contrato) = payload[k + 1].
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Clean, Num = ns.Call, ns.Clean, ns.Num
local floor = math.floor

local PAYLOAD_LEN = 32
local PROTOCOL_VERSION = 2
local BLOCK_COUNT = 29          -- 5 de magia/calibracion + 24 de datos
local BLOCK_PX = 4              -- lado nominal de cada bloque, en pixeles fisicos
local REFRESH_SEC = 0.2
local REFERENCE_UNITS = 768     -- fallback: 1 pixel fisico = 768 / altura fisica unidades de UI
local NIBBLE_STEP = 17          -- nibble n se codifica como n * 17 (0..255)

ns.bridge = ns.bridge or {}
local B = ns.bridge
B.seq = B.seq or 0            -- uint8, sube cuando cambia el payload
B.payload = B.payload or nil  -- ultimo payload pintado
B.acc = B.acc or 0            -- acumulador del throttle de OnUpdate
B.pixel = B.pixel or (REFERENCE_UNITS / 1080)
B.tex = B.tex or {}
B.lastEventCode = B.lastEventCode or 0
B.eventSeq = B.eventSeq or 0
B.eventArg = B.eventArg or 0

-- ---------------------------------------------------------------------------
-- Funciones puras de protocolo
-- ---------------------------------------------------------------------------

-- Coordenada 0..1 -> uint16 round(x * 65535). Desconocida -> 0.
local function coordU16(v)
    if type(v) ~= "number" or v ~= v then return 0 end
    if v < 0 then v = 0 elseif v > 1 then v = 1 end
    return floor(v * 65535 + 0.5)
end

-- UnitLevel -1 (calavera) -> 255. Desconocido -> 0.
local function targetLevelByte(v)
    if v == -1 then return 255 end
    return ns.ClampInt(v, 0, 255, 0)
end

-- Porcentaje 0..100; secreto o no disponible -> 255.
local function percentByte(v)
    return ns.ClampInt(v, 0, 100, 255)
end

local function flagsOf(s)
    local f = 0
    if s.inCombat then f = f + 1 end
    if s.dead then f = f + 2 end
    if s.resting then f = f + 4 end
    if s.mounted then f = f + 8 end
    if s.inGroup then f = f + 16 end
    if s.targetHostile then f = f + 32 end
    if s.hasTarget then f = f + 64 end
    if s.inInstance then f = f + 128 end
    return f
end

-- state (tabla) -> array de 32 bytes (1-based) con CRC-8 en la posicion 32.
function ns.BuildPayload(s)
    s = s or {}
    local p = {}
    ns.AppendU16(p, s.mapID)                       -- 0-1
    ns.AppendU16(p, coordU16(s.x))                 -- 2-3
    ns.AppendU16(p, coordU16(s.y))                 -- 4-5
    ns.AppendU8(p, s.level)                        -- 6
    ns.AppendU8(p, ns.ClampInt(s.xpPct, 0, 100, 0)) -- 7
    ns.AppendU32(p, s.money)                       -- 8-11
    ns.AppendU32(p, s.trackedQuestID)              -- 12-15
    ns.AppendU32(p, s.targetNpcID)                 -- 16-19
    ns.AppendU8(p, targetLevelByte(s.targetLevel)) -- 20
    ns.AppendU8(p, percentByte(s.healthPct))       -- 21
    ns.AppendU8(p, percentByte(s.powerPct))        -- 22
    ns.AppendU8(p, flagsOf(s))                     -- 23
    ns.AppendU8(p, s.lastEventCode)                -- 24
    ns.AppendU8(p, s.eventSeq)                     -- 25
    ns.AppendU32(p, s.eventArg)                    -- 26-29
    ns.AppendU8(p, s.freeBagSlots)                 -- 30
    p[PAYLOAD_LEN] = ns.CRC8(p, 1, PAYLOAD_LEN - 1) -- 31 = CRC sobre bytes 0..30
    return p
end

-- payload (32 bytes) + seq -> 29 bloques {r, g, b} (protocolo v2).
-- Bloques (1-based): 1 = magia (255,0,255); 2 = magia (0,255,255);
-- 3 = calibracion (85,170,0); 4 = calibracion (170,85,85); 5 = calibracion (0,255,170);
-- 6..29 = datos: 72 nibbles (3 por bloque, orden R,G,B).
-- Los bloques 1..5 dan a cada canal los niveles 0, 85, 170 y 255.
-- Flujo de bytes: [version=2, seq, 32] ++ payload[32] = 35 bytes = 70 nibbles,
-- cada byte con el nibble alto primero, y 2 nibbles de relleno a 0.
-- Cada nibble n se codifica como n * 17.
function ns.EncodeBlocks(payload, seq)
    local sq = floor(tonumber(seq) or 0) % 256
    local stream = { PROTOCOL_VERSION, sq, PAYLOAD_LEN }
    for k = 1, PAYLOAD_LEN do
        stream[3 + k] = payload[k] or 0
    end
    local nib = {}
    for k = 1, #stream do
        nib[2 * k - 1] = floor(stream[k] / 16)
        nib[2 * k] = stream[k] % 16
    end
    nib[71] = 0 -- relleno
    nib[72] = 0 -- relleno

    local blocks = {}
    blocks[1] = { 255, 0, 255 }
    blocks[2] = { 0, 255, 255 }
    blocks[3] = { 5 * NIBBLE_STEP, 10 * NIBBLE_STEP, 0 }
    blocks[4] = { 10 * NIBBLE_STEP, 5 * NIBBLE_STEP, 5 * NIBBLE_STEP }
    blocks[5] = { 0, 15 * NIBBLE_STEP, 10 * NIBBLE_STEP }
    for b = 6, BLOCK_COUNT do
        local triple = {}
        for c = 1, 3 do
            triple[c] = nib[(b - 6) * 3 + c] * NIBBLE_STEP
        end
        blocks[b] = triple
    end
    return blocks
end

-- Unidades de marco por pixel fisico. uiHeight: altura de UIParent en unidades (768 en
-- un cliente normal). physH: altura real de la ventana de juego en pixeles fisicos.
-- La franja usa el marco sin escala propia, cuyas unidades son las de UIParent: un
-- bloque de BLOCK_PX pixeles fisicos mide BLOCK_PX * uiHeight / physH unidades.
-- Con 1440 px fisicos y UIParent de 768 unidades: 1 unidad = 1.875 px, bloque = 4 px.
function ns.ComputePixelSize(physH, uiHeight)
    physH = tonumber(physH)
    if not physH or physH <= 0 then return nil end
    uiHeight = tonumber(uiHeight)
    if not uiHeight or uiHeight <= 0 then uiHeight = REFERENCE_UNITS end
    return uiHeight / physH
end

-- Lee un CVar como cadena limpia o nil.
local function cvarText(name)
    return ns.Str(ns.Call("C_CVar.GetCVar", name)) or ns.Str(ns.Call("GetCVar", name))
end

-- Altura en pixeles de una resolucion "3440x1440". nil si no tiene ese formato.
local function resolutionHeight(text)
    if not text then return nil end
    local _, h = string.match(text, "^(%d+)x(%d+)$")
    return tonumber(h)
end

-- Altura fisica real de la ventana de juego y de donde sale. Orden:
-- 1) C_VideoOptions.GetCurrentGameWindowSize (vector x,y);
-- 2) CVar gxFullscreenResolution o gxWindowedResolution ("3440x1440"), segun gxWindow
--    (1 = ventana: la ventana, si no el monitor);
-- 3) GetPhysicalScreenSize (con DPI de Windows puede venir escalado: 1080 en un 1440).
-- Devuelve la altura (o nil) y el nombre de la fuente.
function ns.GameWindowHeight()
    local v = ns.Call("C_VideoOptions.GetCurrentGameWindowSize")
    local h
    if type(v) == "table" then
        h = ns.Num(v.y)
    end
    if not h and v ~= nil then
        local _, y = ns.Method(v, "GetXY")
        h = ns.Num(y)
    end
    if h and h > 0 then return h, "C_VideoOptions" end

    local windowed = cvarText("gxWindow") == "1"
    local first, second = "gxFullscreenResolution", "gxWindowedResolution"
    if windowed then first, second = second, first end
    h = resolutionHeight(cvarText(first))
    if h and h > 0 then return h, "CVar " .. first end
    h = resolutionHeight(cvarText(second))
    if h and h > 0 then return h, "CVar " .. second end

    local _, physH = ns.Call("GetPhysicalScreenSize")
    h = ns.Num(physH)
    if h and h > 0 then return h, "GetPhysicalScreenSize" end
    return nil, "ninguna"
end

-- ---------------------------------------------------------------------------
-- Estado en vivo
-- ---------------------------------------------------------------------------

-- Porcentaje de dos APIs (cur/max) de una unidad. nil si secreto o no disponible.
local function percentOf(curName, maxName, unit)
    local cur = Num(Call(curName, unit))
    local max = Num(Call(maxName, unit))
    if cur == nil or max == nil or max <= 0 then return nil end
    return ns.ClampInt(floor(cur / max * 100), 0, 100, 0)
end

-- Lee el estado del jugador para construir el payload.
function ns.ReadLiveState()
    local s = {}
    s.mapID, s.x, s.y = ns.GetPlayerPosition()
    s.level = Num(Call("UnitLevel", "player"))
    s.xpPct = ns.XPPercent()
    s.money = Num(Call("GetMoney"))
    s.trackedQuestID = Num(Call("C_SuperTrack.GetSuperTrackedQuestID"))
    s.hasTarget = Clean(Call("UnitExists", "target")) == true
    if s.hasTarget then
        local kind, npcID = ns.ParseGUID(Call("UnitGUID", "target"))
        if kind == "Creature" or kind == "Vehicle" then s.targetNpcID = npcID end
        s.targetLevel = Num(Call("UnitLevel", "target"))
        s.targetHostile = Clean(Call("UnitCanAttack", "player", "target")) == true
    end
    s.inCombat = Clean(Call("UnitAffectingCombat", "player")) == true
    s.dead = Clean(Call("UnitIsDeadOrGhost", "player")) == true
    s.resting = Clean(Call("IsResting")) == true
    s.mounted = Clean(Call("IsMounted")) == true
    s.inGroup = Clean(Call("IsInGroup")) == true
    s.inInstance = Clean(Call("IsInInstance")) == true
    s.healthPct = percentOf("UnitHealth", "UnitHealthMax", "player")
    s.powerPct = percentOf("UnitPower", "UnitPowerMax", "player")
    s.freeBagSlots = ns.CountFreeBagSlots()
    s.lastEventCode = B.lastEventCode
    s.eventSeq = B.eventSeq
    s.eventArg = B.eventArg
    return s
end

-- Registra un evento nuevo (lo llama Events.lua).
function ns.RecordEvent(code, arg)
    B.lastEventCode = ns.ClampInt(code, 0, 255, 0)
    B.eventSeq = (B.eventSeq + 1) % 256
    B.eventArg = ns.ClampInt(arg, 0, 4294967295, 0)
end

-- ---------------------------------------------------------------------------
-- Dibujo
-- ---------------------------------------------------------------------------

function ns.IsStripOn()
    local db = CompaneroWoWDB
    return not (type(db) == "table" and type(db.settings) == "table" and db.settings.strip == false)
end

-- Lineas de diagnostico para /compa estado: altura fisica usada y su fuente, y el pixel.
function ns.BridgeDebugLines()
    local h = B.physH
    local hText = h and string.format("%d px (%s)", h, B.physSource or "?") or "desconocida"
    return {
        string.format("Altura fisica de la ventana: %s", hText),
        string.format("UIParent: %s unidades de altura", B.uiH and string.format("%.0f", B.uiH) or "?"),
        string.format("Pixel fisico: %.4f unidades de marco; bloque de %dx%d px = %.3f unidades",
            B.pixel or 0, BLOCK_PX, BLOCK_PX, BLOCK_PX * (B.pixel or 0)),
    }
end

-- Recalcula el tamano del bloque segun la altura fisica de la ventana y UIParent.
local function recomputeScale()
    local h, source = ns.GameWindowHeight()
    local uiH
    local ui = _G.UIParent
    if ui then uiH = ns.Num(ns.Method(ui, "GetHeight")) end
    local px = ns.ComputePixelSize(h, uiH)
    if not px or px <= 0 then px = REFERENCE_UNITS / 1080 end
    B.pixel = px
    B.physH, B.physSource, B.uiH = h, source, uiH
    local block = BLOCK_PX * px
    if B.frame then
        ns.Method(B.frame, "SetSize", BLOCK_COUNT * block, block)
    end
    for i = 1, BLOCK_COUNT do
        local t = B.tex[i]
        if t then
            ns.Method(t, "SetSize", block, block)
            ns.Method(t, "SetPoint", "TOPLEFT", B.frame, "TOPLEFT", (i - 1) * block, 0)
        end
    end
end

local function samePayload(a, b)
    if not a or not b then return false end
    for i = 1, PAYLOAD_LEN do
        if a[i] ~= b[i] then return false end
    end
    return true
end

-- Igual que EncodeBlocks: los bloques ya pintados (nil = nada pintado todavia).
local function sameBlocks(a, b)
    if not a or not b then return false end
    for i = 1, BLOCK_COUNT do
        local x, y = a[i], b[i]
        if not x or not y or x[1] ~= y[1] or x[2] ~= y[2] or x[3] ~= y[3] then return false end
    end
    return true
end

local function paint(blocks)
    for i = 1, BLOCK_COUNT do
        local c, t = blocks[i], B.tex[i]
        if c and t then
            ns.Method(t, "SetColorTexture", c[1] / 255, c[2] / 255, c[3] / 255, 1)
        end
    end
end

-- Un refresco: recalcula payload, sube seq solo si cambia, y repinta solo si cambian los bytes.
function ns.BridgeRefresh()
    if not B.frame or not ns.IsStripOn() then return end
    local payload = ns.BuildPayload(ns.ReadLiveState())
    if not samePayload(payload, B.payload) then
        B.seq = (B.seq + 1) % 256
        B.payload = payload
    end
    local blocks = ns.EncodeBlocks(B.payload, B.seq)
    if sameBlocks(blocks, B.painted) then return end
    paint(blocks)
    B.painted = blocks
end

-- Activa o desactiva la franja y guarda la preferencia (settings.strip).
function ns.SetStripEnabled(on)
    on = on and true or false
    if type(CompaneroWoWDB) == "table" then
        if type(CompaneroWoWDB.settings) ~= "table" then CompaneroWoWDB.settings = {} end
        CompaneroWoWDB.settings.strip = on
    end
    if B.frame then
        if on then
            ns.Method(B.frame, "Show")
            ns.SafeCall(ns.BridgeRefresh)
        else
            ns.Method(B.frame, "Hide")
        end
    end
end

-- Crea el frame de la franja (una sola vez). Llamado desde PLAYER_LOGIN.
function ns.InitBridge()
    if B.frame then return B.frame end
    local f = ns.NewFrame("Frame", "CompaneroWoWBridge", nil)
    if not f and _G.UIParent then
        f = ns.NewFrame("Frame", "CompaneroWoWBridge", _G.UIParent)
    end
    if not f then return nil end
    B.frame = f
    B.painted = nil -- texturas nuevas: el siguiente refresco debe pintar
    ns.Method(f, "SetFrameStrata", "TOOLTIP")
    ns.Method(f, "SetFrameLevel", 1000)
    ns.Method(f, "SetIgnoreParentScale", true)
    ns.Method(f, "SetIgnoreParentAlpha", true)
    if _G.UIParent then
        ns.Method(f, "SetPoint", "TOPLEFT", _G.UIParent, "TOPLEFT", 0, 0)
    else
        ns.Method(f, "SetPoint", "TOPLEFT", 0, 0)
    end
    for i = 1, BLOCK_COUNT do
        local t = ns.Method(f, "CreateTexture", nil, "OVERLAY")
        if t then
            ns.Method(t, "SetSnapToPixelGrid", false)
            ns.Method(t, "SetTexelSnappingBias", 0)
            ns.Method(t, "SetColorTexture", 0, 0, 0, 1)
        end
        B.tex[i] = t
    end
    recomputeScale()
    ns.Method(f, "RegisterEvent", "UI_SCALE_CHANGED")
    ns.Method(f, "RegisterEvent", "DISPLAY_SIZE_CHANGED")
    ns.Method(f, "RegisterEvent", "PLAYER_ENTERING_WORLD")
    ns.Method(f, "SetScript", "OnEvent", function()
        ns.SafeCall(recomputeScale)
    end)
    ns.Method(f, "SetScript", "OnUpdate", function(_, elapsed)
        B.acc = B.acc + (tonumber(elapsed) or 0)
        if B.acc < REFRESH_SEC then return end
        B.acc = 0
        ns.SafeCall(ns.BridgeRefresh)
    end)
    if ns.IsStripOn() then
        ns.Method(f, "Show")
    else
        ns.Method(f, "Hide")
    end
    return f
end
