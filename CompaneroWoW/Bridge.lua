-- Bridge.lua: franja de pixeles (addon -> app).
-- Protocolo v3 (activo): una fila de bloques cuadrados de BLOCK_PX px fisicos a lo ancho de
-- la pantalla: N = min(floor(anchoDisponible / bloque) - 1, 1024) bloques, medidos en unidades
-- del propio marco (no en pixeles fisicos estimados). Bloques 1..5 = magia y calibracion
-- (igual que v2); bloques 6..N = flujo de nibbles (3 por bloque). El contenido del flujo lo
-- construye Channel.lua (cabecera, estado, trozo de mensaje y CRC-16).
-- Protocolo v2 (solo codificador de referencia para tests; el addon ya no lo emite).
-- BuildPayload, EncodeBlocks, EncodeFrameColors, StripBlocks y ComputePixelSize son funciones
-- puras (testeables offline). GameWindowSize consulta la API y se prueba con stubs.
-- Los arrays son 1-based: byte k (0-based del contrato) = array[k + 1].
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Clean, Num = ns.Call, ns.Clean, ns.Num
local floor = math.floor
local min, max = math.min, math.max

local PAYLOAD_LEN = 32
local PROTOCOL_V2 = 2
local PROTOCOL_V3 = 3
local BLOCK_COUNT_V2 = 29       -- v2: 5 de magia/calibracion + 24 de datos
local BLOCK_PX = 4              -- lado nominal de cada bloque, en pixeles fisicos
local MAX_BLOCKS = 1024         -- tope de bloques de la fila (v3)
local DEFAULT_BLOCKS = 256      -- ancho desconocido: fila de 1024 px fisicos
local CALIB_BLOCKS = 5          -- bloques 1..5 fijos (magia y calibracion)
local REFRESH_SEC = 0.2
local REFERENCE_UNITS = 768     -- fallback: 1 pixel fisico = 768 / altura fisica unidades de UI
local NIBBLE_STEP = 17          -- nibble n se codifica como n * 17 (0..255)

-- Colores de los bloques 1..5 (v2 y v3): magia, magia, calibracion x3.
local CAL_COLORS = {
    { 255, 0, 255 }, { 0, 255, 255 },
    { 5 * NIBBLE_STEP, 10 * NIBBLE_STEP, 0 },
    { 10 * NIBBLE_STEP, 5 * NIBBLE_STEP, 5 * NIBBLE_STEP },
    { 0, 15 * NIBBLE_STEP, 10 * NIBBLE_STEP },
}

ns.bridge = ns.bridge or {}
local B = ns.bridge
B.seq = B.seq or 0            -- uint8, sube cuando cambia el contenido de la trama
B.bytes = B.bytes or nil      -- ultima trama (array de bytes, 1-based)
B.payload = B.payload or nil  -- ultimo estado (32 bytes) pintado
B.acc = B.acc or 0            -- acumulador del throttle de OnUpdate
B.pixel = B.pixel or (REFERENCE_UNITS / 1080)
B.blocks = B.blocks or 0      -- N bloques de la fila (v3)
B.tex = B.tex or {}
B.colors = B.colors or {}     -- colores calculados en este refresco (codigo r*65536+g*256+b)
B.painted = B.painted or {}   -- colores ya pintados
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
-- Estado de la trama v3 y v2 (identico): ver docs/README "Protocolo".
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
    ns.AppendU8(p, s.shareRemainingSec)            -- 30: segundos de la ventana de envio (0 en reposo)
    p[PAYLOAD_LEN] = ns.CRC8(p, 1, PAYLOAD_LEN - 1) -- 31 = CRC sobre bytes 0..30
    return p
end

-- Numero de bloques de la fila v3: los que caben en availUnits (ancho disponible, en unidades
-- del marco) con bloques de blockUnits unidades, menos 1 de margen; tope MAX_BLOCKS.
-- Garantia: N * blockUnits <= availUnits - blockUnits, aunque el tamano fisico estimado este mal.
-- Ancho desconocido -> DEFAULT_BLOCKS. Si no caben CALIB_BLOCKS + 1 bloques -> 0 (sin franja).
function ns.StripBlocks(availUnits, blockUnits)
    local a, b = tonumber(availUnits), tonumber(blockUnits)
    if not a or a <= 0 or not b or b <= 0 then return DEFAULT_BLOCKS end
    local n = min(floor(a / b) - 1, MAX_BLOCKS)
    if n <= CALIB_BLOCKS then return 0 end
    return n
end

-- Codigo de color (r*65536 + g*256 + b) de un bloque, y su inverso para pintar.
local function colorCode(r, g, b)
    return r * 65536 + g * 256 + b
end

-- Nibble k (0-based) de la trama: byte k/2 (1-based j = floor(k/2)+1), nibble alto si k es par.
local function nibbleAt(frame, nbytes, k)
    local j = floor(k / 2) + 1
    if j > nbytes then return 0 end
    local byte = frame[j]
    if k % 2 == 0 then return floor(byte / 16) end
    return byte % 16
end

-- Trama v3 (bytes, 1-based) -> colores de los N bloques (codigo r*65536+g*256+b).
-- Escribe en out (reutilizable) y lo devuelve. Bloques 1..5 fijos; 6..N = nibbles.
-- Nibbles mas alla de la trama valen 0.
function ns.EncodeFrameColors(frame, N, out)
    out = out or {}
    for i = 1, CALIB_BLOCKS do
        local c = CAL_COLORS[i]
        out[i] = colorCode(c[1], c[2], c[3])
    end
    local nbytes = #frame
    for b = CALIB_BLOCKS + 1, N do
        local k = (b - CALIB_BLOCKS - 1) * 3 -- nibble del canal R (0-based)
        local r = nibbleAt(frame, nbytes, k) * NIBBLE_STEP
        local g = nibbleAt(frame, nbytes, k + 1) * NIBBLE_STEP
        local bl = nibbleAt(frame, nbytes, k + 2) * NIBBLE_STEP
        out[b] = colorCode(r, g, bl)
    end
    for i = N + 1, #out do out[i] = nil end
    return out
end

-- payload (32 bytes) + seq -> 29 bloques {r, g, b} (protocolo v2, solo referencia).
-- Bloques (1-based): 1 = magia (255,0,255); 2 = magia (0,255,255);
-- 3 = calibracion (85,170,0); 4 = calibracion (170,85,85); 5 = calibracion (0,255,170);
-- 6..29 = datos: 72 nibbles (3 por bloque, orden R,G,B).
-- Flujo de bytes: [version=2, seq, 32] ++ payload[32] = 35 bytes = 70 nibbles,
-- cada byte con el nibble alto primero, y 2 nibbles de relleno a 0.
-- Cada nibble n se codifica como n * 17.
function ns.EncodeBlocks(payload, seq)
    local sq = floor(tonumber(seq) or 0) % 256
    local stream = { PROTOCOL_V2, sq, PAYLOAD_LEN }
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
    for i = 1, CALIB_BLOCKS do
        local c = CAL_COLORS[i]
        blocks[i] = { c[1], c[2], c[3] }
    end
    for b = 6, BLOCK_COUNT_V2 do
        local triple = {}
        for c = 1, 3 do
            triple[c] = nib[(b - 6) * 3 + c] * NIBBLE_STEP
        end
        blocks[b] = triple
    end
    return blocks
end

-- Unidades del marco por pixel fisico. uiHeight: altura de UIParent en sus unidades (768 en
-- un cliente normal). physH: altura real de la ventana de juego en pixeles fisicos.
-- uiScale / frameScale: escala efectiva de UIParent y del marco de la franja (1 si se ignoran).
-- Un pixel fisico mide (uiHeight / physH) unidades de UIParent, y una unidad de UIParent son
-- uiScale / frameScale unidades del marco. El marco de la franja no hereda la escala de
-- UIParent (SetIgnoreParentScale), asi que el cociente de escalas no es 1 en general.
-- Ej.: 1440 px, UIParent 768 y escala 0.948 con marco a 1: 1 px = 0.5056 unidades (bloque 4 px = 2.02).
function ns.ComputePixelSize(physH, uiHeight, uiScale, frameScale)
    physH = tonumber(physH)
    if not physH or physH <= 0 then return nil end
    uiHeight = tonumber(uiHeight)
    if not uiHeight or uiHeight <= 0 then uiHeight = REFERENCE_UNITS end
    local su = tonumber(uiScale)
    if not su or su <= 0 then su = 1 end
    local sf = tonumber(frameScale)
    if not sf or sf <= 0 then sf = 1 end
    return uiHeight / physH * su / sf
end

-- Lee un CVar como cadena limpia o nil.
local function cvarText(name)
    return ns.Str(ns.Call("C_CVar.GetCVar", name)) or ns.Str(ns.Call("GetCVar", name))
end

-- Ancho y alto en pixeles de una resolucion "3440x1440". nil si no tiene ese formato.
local function resolutionSize(text)
    if not text then return nil, nil end
    local w, h = string.match(text, "^(%d+)x(%d+)$")
    return tonumber(w), tonumber(h)
end

-- Tamano de la ventana segun las CVar, respetando gxWindow y gxMaximize: con ventana sin
-- maximizar, gxWindowedResolution; si no, gxFullscreenResolution (maximizada = el monitor).
-- Si falta, prueba la otra. Devuelve ancho, alto y el nombre de la CVar (o nil).
local function cvarWindowSize()
    local windowed = cvarText("gxWindow") == "1"
    local maximized = cvarText("gxMaximize") == "1"
    local first, second = "gxFullscreenResolution", "gxWindowedResolution"
    if windowed and not maximized then first, second = second, first end
    local cw, ch = resolutionSize(cvarText(first))
    if ch and ch > 0 then return cw, ch, "CVar " .. first end
    cw, ch = resolutionSize(cvarText(second))
    if ch and ch > 0 then return cw, ch, "CVar " .. second end
    return nil, nil, nil
end

-- Tamano de C_VideoOptions.GetCurrentGameWindowSize (vector x,y). nil si no hay.
local function videoOptionsSize()
    local v = ns.Call("C_VideoOptions.GetCurrentGameWindowSize")
    local w, h
    if type(v) == "table" then
        w, h = ns.Num(v.x), ns.Num(v.y)
    end
    if (not h or not w) and v ~= nil then
        local x, y = ns.Method(v, "GetXY")
        w = w or ns.Num(x)
        h = h or ns.Num(y)
    end
    if w and h and w > 0 and h > 0 then return w, h end
    return nil, nil
end

-- Escalas de DPI de Windows habituales (125 %, 133 %, 150 %).
local DPI_RATIOS = { 1.25, 4 / 3, 1.5 }

-- true si bigH / smallH es una de esas escalas (1 % de tolerancia): smallH es la ventana
-- escalada por DPI (p. ej. 1080) y bigH la real (1440).
local function isDpiScaled(smallH, bigH)
    if not smallH or not bigH or smallH <= 0 then return false end
    local r = bigH / smallH
    for _, k in ipairs(DPI_RATIOS) do
        if math.abs(r - k) <= k * 0.01 then return true end
    end
    return false
end

-- Tamano fisico real de la ventana de juego y de donde sale. Orden:
-- 1) CVar (gxWindowedResolution / gxFullscreenResolution, ver cvarWindowSize). Si
--    C_VideoOptions.GetCurrentGameWindowSize devuelve la altura escalada por DPI (la CVar es
--    esa altura x 1.25/1.333/1.5), gana la CVar;
-- 2) C_VideoOptions.GetCurrentGameWindowSize;
-- 3) GetPhysicalScreenSize (con DPI de Windows puede venir escalado: 1080 en un 1440). Ultimo recurso.
-- Devuelve ancho, alto (o nil) y el nombre de la fuente.
function ns.GameWindowSize()
    local vw, vh = videoOptionsSize()
    local cw, ch, csource = cvarWindowSize()
    if vh and ch and isDpiScaled(vh, ch) then
        return cw, ch, csource .. " (C_VideoOptions escalado)"
    end
    if vh then return vw, vh, "C_VideoOptions" end
    if ch then return cw, ch, csource end

    local pw, ph = ns.Call("GetPhysicalScreenSize")
    local w, h = ns.Num(pw), ns.Num(ph)
    if h and h > 0 then return w, h, "GetPhysicalScreenSize" end
    return nil, nil, "ninguna"
end

-- Altura fisica (compatibilidad con v2 y pruebas): altura y fuente.
function ns.GameWindowHeight()
    local _, h, source = ns.GameWindowSize()
    if h and h > 0 then return h, source end
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

-- Lee el estado del jugador para construir el payload. shareLeft: segundos de la ventana (byte 30).
-- Las bolsas libres ya no van aqui: viajan en el mensaje del personaje (tipo 1, Channel.lua).
function ns.ReadLiveState(shareLeft)
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
    s.shareRemainingSec = ns.ClampInt(shareLeft, 0, 255, 0)
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

-- Lineas de diagnostico para /compa estado (una linea por dato): fuente del tamano, ventana
-- fisica, UIParent en unidades, escala del marco, bloque en unidades, N y pitch esperado.
function ns.BridgeDebugLines()
    local function num(v, fmt) return v and string.format(fmt, v) or "?" end
    local pitch = B.pixel and B.pixel > 0 and B.block and B.block / B.pixel or nil
    local stripPx = pitch and (B.blocks or 0) * pitch
    local lines = {
        "Fuente del tamano: " .. (B.physSource or "ninguna"),
        string.format("Ventana fisica: %sx%s px", num(B.physW, "%d"), num(B.physH, "%d")),
        string.format("UIParent: %sx%s unidades (escala efectiva %s)",
            num(B.uiW, "%.1f"), num(B.uiH, "%.0f"), num(B.uiS, "%.4f")),
        string.format("Marco de la franja: escala efectiva %s; ancho disponible %s unidades",
            num(B.frS, "%.4f"), num(B.avail, "%.1f")),
        string.format("Bloque: %s unidades (%d px fisicos nominales)", num(B.block, "%.4f"), BLOCK_PX),
        string.format("Franja v%d: N = %d bloques (tope %d)", PROTOCOL_V3, B.blocks or 0, MAX_BLOCKS),
        string.format("Pitch esperado: %s px; franja de %s px%s", num(pitch, "%.2f"), num(stripPx, "%.0f"),
            B.physW and string.format(" de %d", B.physW) or ""),
    }
    return lines
end

-- Crea o ajusta las texturas hasta n (las sobrantes se ocultan). Resetea lo pintado.
local function ensureTextures(n)
    local f = B.frame
    for i = 1, max(n, #B.tex) do
        local t = B.tex[i]
        if i <= n then
            if not t and f then
                t = ns.Method(f, "CreateTexture", nil, "OVERLAY")
                if t then
                    ns.Method(t, "SetSnapToPixelGrid", false)
                    ns.Method(t, "SetTexelSnappingBias", 0)
                    ns.Method(t, "SetColorTexture", 0, 0, 0, 1)
                end
                B.tex[i] = t
            end
            if t then ns.Method(t, "Show") end
        elseif t then
            ns.Method(t, "Hide")
        end
    end
    B.painted = {} -- texturas nuevas u ocultas: el siguiente refresco pinta todo
end

-- Recalcula el bloque y N. El ancho disponible se mide en unidades del propio marco:
-- ancho de UIParent * escala de UIParent / escala del marco. N bloques de ese tamano caben
-- siempre en pantalla aunque la altura fisica estimada este mal; la estimacion solo fija el
-- pitch (4 px fisicos cuando la altura fisica es la real).
local function recomputeScale()
    local w, h, source = ns.GameWindowSize()
    local ui = _G.UIParent
    local uiW, uiH, uiS
    if ui then
        uiW = ns.Num(ns.Method(ui, "GetWidth"))
        uiH = ns.Num(ns.Method(ui, "GetHeight"))
        uiS = ns.Num(ns.Method(ui, "GetEffectiveScale"))
    end
    local frS = B.frame and ns.Num(ns.Method(B.frame, "GetEffectiveScale"))
    if not uiS or uiS <= 0 then uiS = 1 end
    if not frS or frS <= 0 then frS = 1 end
    -- Sin altura fisica: referencia de 1080 px (como antes).
    local px = ns.ComputePixelSize(h or 1080, uiH, uiS, frS)
    if not px or px <= 0 then px = REFERENCE_UNITS / 1080 end
    local block = BLOCK_PX * px
    local avail
    if uiW and uiW > 0 then
        avail = uiW * uiS / frS
    elseif w and w > 0 then
        avail = w * px -- sin UIParent: ancho fisico convertido a unidades del marco
    end
    local n = ns.StripBlocks(avail, block)
    B.pixel = px
    B.physW, B.physH, B.physSource = w, h, source
    B.uiW, B.uiH, B.uiS, B.frS = uiW, uiH, uiS, frS
    B.avail, B.block = avail, block
    if n ~= B.blocks or not B.frame then
        B.blocks = n
        ensureTextures(n)
    end
    if B.frame then
        ns.Method(B.frame, "SetSize", n * block, block)
    end
    for i = 1, n do
        local t = B.tex[i]
        if t then
            ns.Method(t, "SetSize", block, block)
            ns.Method(t, "SetPoint", "TOPLEFT", B.frame, "TOPLEFT", (i - 1) * block, 0)
        end
    end
end

local function sameBytes(a, b)
    if not a or not b or #a ~= #b then return false end
    for i = 1, #a do
        if a[i] ~= b[i] then return false end
    end
    return true
end

-- Pinta solo los bloques cuyo color cambio desde el ultimo pintado.
local function paint(colors, n)
    local painted = B.painted
    for i = 1, n do
        local c = colors[i]
        if c ~= painted[i] then
            local t = B.tex[i]
            if t then
                local r = floor(c / 65536)
                local g = floor(c / 256) % 256
                local bl = c % 256
                ns.Method(t, "SetColorTexture", r / 255, g / 255, bl / 255, 1)
            end
            painted[i] = c
        end
    end
end

local function nowSec()
    local t = ns.Num(ns.Call("GetTime"))
    if t then return t end
    return ns.Now()
end

-- Un refresco (cada REFRESH_SEC, solo durante una activacion): recalcula el estado, toma el
-- siguiente trozo del canal, compone la trama, sube seq solo si cambia el contenido y pinta lo que cambio.
function ns.BridgeRefresh()
    if not B.frame or not ns.IsStripOn() then return end
    local n = B.blocks
    if n <= CALIB_BLOCKS then return end
    local C = ns.Channel
    local now = nowSec()
    B.frameNo = (B.frameNo or 0) + 1
    local payload = ns.BuildPayload(ns.ReadLiveState(C and C.RemainingSec(now) or 0))
    B.payload = payload
    local chunk = C and C.NextChunk(n, B.frameNo) or nil
    local frame = C.BuildFrame(payload, B.seq, n, chunk)
    if not sameBytes(frame, B.bytes) then
        B.seq = (B.seq + 1) % 256
        frame = ns.Channel.BuildFrame(payload, B.seq, n, chunk)
        B.bytes = frame
    end
    B.colors = ns.EncodeFrameColors(frame, n, B.colors)
    paint(B.colors, n)
end

-- ---------------------------------------------------------------------------
-- Reposo y activacion
-- ---------------------------------------------------------------------------
-- Reposo (por defecto): la franja esta oculta y el frame no tiene OnUpdate: no hay nada que
-- pintar ni que serializar. Activacion (tecla o /compa enviar): ns.StripStart registra OnUpdate y
-- muestra la franja; ns.StripRest lo quita todo. Solo OnUpdate mientras dura la ventana.

-- Trama de la ventana: reparte la serializacion (Channel.Pump, presupuesto por trama) y refresca la
-- franja cada REFRESH_SEC. Al acabar la ventana, vuelve al reposo.
local function onShareUpdate(_, elapsed)
    local C = ns.Channel
    if not C then return end
    local now = nowSec()
    if not C.Update(now) then
        ns.StripRest()
        return
    end
    C.Pump(now, C.PUMP_BUDGET_MS)
    B.acc = B.acc + (tonumber(elapsed) or 0)
    if B.acc < REFRESH_SEC then return end
    B.acc = 0
    ns.SafeCall(ns.BridgeRefresh)
end

-- Entra en la ventana de envio: muestra la franja y registra OnUpdate. false si no hay franja.
function ns.StripStart()
    if not B.frame then return false end
    B.acc = 0
    B.frameNo = 0
    ns.Method(B.frame, "SetScript", "OnUpdate", onShareUpdate)
    ns.Method(B.frame, "Show")
    return true
end

-- Vuelve al reposo: oculta la franja, quita OnUpdate y para el canal.
function ns.StripRest()
    if ns.Channel and ns.Channel.Stop then ns.Channel.Stop() end
    if B.frame then
        ns.Method(B.frame, "SetScript", "OnUpdate", nil)
        ns.Method(B.frame, "Hide")
    end
    B.acc = 0
end

-- Guarda la preferencia (settings.strip): "off" = no se comparte nunca; "on" = se comparte con la tecla.
-- Al apagarla, si hay una ventana en curso se corta.
function ns.SetStripEnabled(on)
    on = on and true or false
    if type(CompaneroWoWDB) == "table" then
        if type(CompaneroWoWDB.settings) ~= "table" then CompaneroWoWDB.settings = {} end
        CompaneroWoWDB.settings.strip = on
    end
    if not on then ns.StripRest() end
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
    B.painted = {} -- texturas nuevas: el siguiente refresco debe pintar
    ns.Method(f, "SetFrameStrata", "TOOLTIP")
    ns.Method(f, "SetFrameLevel", 1000)
    ns.Method(f, "SetIgnoreParentScale", true)
    ns.Method(f, "SetIgnoreParentAlpha", true)
    if _G.UIParent then
        ns.Method(f, "SetPoint", "TOPLEFT", _G.UIParent, "TOPLEFT", 0, 0)
    else
        ns.Method(f, "SetPoint", "TOPLEFT", 0, 0)
    end
    recomputeScale()
    ns.Method(f, "RegisterEvent", "UI_SCALE_CHANGED")
    ns.Method(f, "RegisterEvent", "DISPLAY_SIZE_CHANGED")
    ns.Method(f, "RegisterEvent", "PLAYER_ENTERING_WORLD")
    ns.Method(f, "SetScript", "OnEvent", function()
        ns.SafeCall(recomputeScale)
    end)
    -- Reposo: oculta y sin OnUpdate (se registra solo durante una activacion, ns.StripStart).
    ns.Method(f, "SetScript", "OnUpdate", nil)
    ns.Method(f, "Hide")
    return f
end
