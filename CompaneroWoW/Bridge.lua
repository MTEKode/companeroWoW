-- Bridge.lua: franja de pixeles (addon -> app), protocolo v1 segun el CONTRATO COMPARTIDO.
-- BuildPayload y EncodeBlocks son funciones puras (testeables offline).
-- Los arrays son 1-based: byte k (0-based del contrato) = payload[k + 1].
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Clean, Num = ns.Call, ns.Clean, ns.Num
local floor = math.floor

local PAYLOAD_LEN = 32
local BLOCK_COUNT = 14
local REFRESH_SEC = 0.2
local REFERENCE_UNITS = 768 -- 1 pixel fisico = 768 / altura fisica unidades de UI

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

-- payload (32 bytes) + seq -> 14 bloques {r, g, b}.
-- Bloque 1 y 2 = magia; bloque 3 = (version, seq, longitud); bloques 4..14 = 33 bytes
-- (payload + 1 de relleno a 0).
function ns.EncodeBlocks(payload, seq)
    local blocks = {}
    blocks[1] = { 255, 0, 255 }
    blocks[2] = { 0, 255, 255 }
    local sq = floor(tonumber(seq) or 0) % 256
    blocks[3] = { 1, sq, PAYLOAD_LEN }
    local k = 0 -- indice 0-based dentro de los 33 bytes
    for b = 4, BLOCK_COUNT do
        local triple = {}
        for c = 1, 3 do
            local v = 0
            if k < PAYLOAD_LEN then v = payload[k + 1] or 0 end
            triple[c] = v
            k = k + 1
        end
        blocks[b] = triple
    end
    return blocks
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

local function samePayload(a, b)
    if not a or not b then return false end
    for i = 1, PAYLOAD_LEN do
        if a[i] ~= b[i] then return false end
    end
    return true
end

-- Recalcula el tamano del bloque segun la altura fisica de pantalla.
local function recomputeScale()
    local _, physH = ns.Call("GetPhysicalScreenSize")
    physH = Num(physH)
    local px = REFERENCE_UNITS / 1080
    if physH and physH > 0 then px = REFERENCE_UNITS / physH end
    B.pixel = px
    local f = B.frame
    if not f then return end
    ns.Method(f, "SetSize", 28 * px, 2 * px) -- 14 bloques x 2 px de ancho, 2 px de alto
    for i = 1, BLOCK_COUNT do
        local t = B.tex[i]
        if t then
            ns.Method(t, "SetSize", 2 * px, 2 * px)
            ns.Method(t, "SetPoint", "TOPLEFT", f, "TOPLEFT", (i - 1) * 2 * px, 0)
        end
    end
end

local function paint(blocks)
    for i = 1, BLOCK_COUNT do
        local c, t = blocks[i], B.tex[i]
        if c and t then
            ns.Method(t, "SetColorTexture", c[1] / 255, c[2] / 255, c[3] / 255, 1)
        end
    end
end

-- Un refresco: recalcula payload, sube seq solo si cambia, y pinta.
function ns.BridgeRefresh()
    if not B.frame or not ns.IsStripOn() then return end
    local payload = ns.BuildPayload(ns.ReadLiveState())
    if not samePayload(payload, B.payload) then
        B.seq = (B.seq + 1) % 256
        B.payload = payload
    end
    paint(ns.EncodeBlocks(B.payload, B.seq))
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
    if not f then return nil end
    B.frame = f
    ns.Method(f, "SetFrameStrata", "TOOLTIP")
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
