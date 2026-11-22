-- Shot.lua: canal de capturas (addon -> app), el canal por defecto (/compa canal captura).
-- Contrato SCREENSHOT CHANNEL (identico en la app, T52):
--   1. Una activacion (tecla, /compa enviar, /compa sync o pregunta) serializa el plan de siempre
--      (Channel.lua: estado, pregunta, cambios) en TROZOS, igual que la franja v3.
--   2. Cada fila del PANEL DE DATOS es una trama v3.1 (magia, calibracion y nibbles, Bridge.lua
--      EncodeFrameColors) de version 5 ("trama de captura"), con el shotId de la captura:
--        [1] version=5   [2] seq (fila, mod 256)   [3..4] N (bloques de la fila, LE)
--        [5..6] shotId (LE)   [7..38] estado (32 bytes, CRC-8 en 38)
--        [39] msgType  [40..41] msgId  [42..43] chunkIndex  [44..45] chunkCount  [46..47] dataLen (LE)
--        [48..47+dataLen] datos  [48+dataLen..49+dataLen] CRC-16/CCITT-FALSE de 1..47+dataLen (BE)
--      Cabecera de la trama = 6 bytes (la v4 tiene 4); sobrecarga = 49 bytes.
--      Fila final ("end"): msgType 0, dataLen 0, chunkCount = numero de filas de datos de la captura.
--   3. Panel: bloques cuadrados de BLOCK_PX (2) px fisicos, esquina superior izquierda, por encima de
--      todo (FULLSCREEN_DIALOG), opaco. Una fila de bloques por trama, de arriba abajo, y el resto del
--      marco (minimo 256x64 px) en negro. W = N * bloque (256, 512 o 1024 px). Cada captura usa como
--      mucho MAX_BLOCKS (4096) bloques = texturas: N * filas <= 4096. Las texturas se crean una vez
--      (pool) y se reutilizan entre capturas. Si el cliente solo deja jpg, el bloque es de
--      JPG_BLOCK_PX (4) px y la calidad es 10.
--   4. Se guardan screenshotFormat y screenshotQuality, se fija el formato sin perdidas (png; si no,
--      tga; si no, jpg), se espera un frame, se llama a Screenshot() y, al recibir
--      SCREENSHOT_SUCCEEDED / SCREENSHOT_FAILED (o al cabo de TIMEOUT_SEC), se oculta el panel y se
--      restauran las CVar. Hasta MAX_SHOTS capturas por activacion, separadas GAP_SEC.
--   5. En combate solo se captura por una pregunta. Si Screenshot() no existe o falla, se avisa y la
--      activacion usa la franja (Share.lua).
-- Parte pura (testeable offline): Capacity, BuildRow, ParseRow, PanelFrames, Geometry.
-- Parte de juego: Start (plan, capturas, CVar, panel, eventos) y StatusLines.
local ADDON_NAME, ns = ...
ns = ns or {}

local floor, ceil, max, min = math.floor, math.ceil, math.max, math.min

local Shot = {}
ns.Shot = Shot

Shot.VERSION = 5              -- trama de captura. La franja v3.2 tambien lleva un 5, pero con otra
                              -- cabecera (4 bytes) y otros colores: cada lector solo valida la suya (CRC)
Shot.HEADER_LEN = 6           -- [version, seq, Nlo, Nhi, shotIdLo, shotIdHi]
Shot.STATUS_LEN = 32
Shot.CHUNK_HEADER_LEN = 9
Shot.OVERHEAD = Shot.HEADER_LEN + Shot.STATUS_LEN + Shot.CHUNK_HEADER_LEN + 2 -- 49
Shot.CALIB_BLOCKS = 7         -- bloques fijos de magia, calibracion y blanco (v3.1)
Shot.BLOCK_PX = 2             -- bloque del panel, en pixeles fisicos
Shot.JPG_BLOCK_PX = 4         -- si solo hay jpg (calidad 10)
Shot.MIN_W, Shot.MIN_H = 256, 64
Shot.MAX_BLOCKS = 4096        -- tope de bloques (texturas) por captura
Shot.WIDTHS = { 256, 512, 1024 }  -- anchos del panel en px (N = ancho / bloque)
Shot.MAX_SHOTS = 8            -- capturas por activacion
Shot.GAP_SEC = 0.3            -- entre capturas
Shot.TIMEOUT_SEC = 4          -- sin confirmacion del juego: se da por fallida
Shot.NOTICE_SEC = 1           -- la notice de captura vuelve tras este tiempo
Shot.NOTICE_FRAMES = { "ScreenshotStatus", "ActionStatus" }
Shot.QUALITY_JPG = 10

-- ---------------------------------------------------------------------------
-- Funciones puras de protocolo
-- ---------------------------------------------------------------------------

local function u16(v)
    return v % 256, floor(v / 256) % 256
end

-- Bytes de datos que caben en una fila de N bloques (0 si no cabe ninguno).
function Shot.Capacity(N)
    local bytes = floor(3 * (N - Shot.CALIB_BLOCKS) / 2)
    return max(0, bytes - Shot.OVERHEAD)
end

-- Compone una trama de captura (array de bytes 1-based), o nil si el trozo no cabe en N bloques.
--   status: 32 bytes del estado (1-based, CRC-8 en 32). seq: 0..255. N: bloques de la fila.
--   shotId: 0..65535. chunk: nil, o { type, id, index (0-based), count, data = bytes }.
function Shot.BuildRow(status, seq, N, shotId, chunk)
    local dlen, msgType, id, index, count = 0, 0, 0, 0, 0
    local data
    if chunk then
        data = chunk.data or {}
        dlen = #data
        msgType, id, index, count = chunk.type, chunk.id, chunk.index, chunk.count
    end
    if dlen > Shot.Capacity(N) then return nil end
    local f = { Shot.VERSION, seq % 256 }
    f[3], f[4] = u16(N)
    f[5], f[6] = u16(shotId)
    for k = 1, Shot.STATUS_LEN do f[6 + k] = status[k] or 0 end -- 7..38
    f[39] = msgType
    f[40], f[41] = u16(id)
    f[42], f[43] = u16(index)
    f[44], f[45] = u16(count)
    f[46], f[47] = u16(dlen)
    for k = 1, dlen do f[47 + k] = data[k] end
    local last = 47 + dlen
    local crc = ns.Channel.CRC16(f, 1, last)
    f[last + 1] = floor(crc / 256)
    f[last + 2] = crc % 256
    return f
end

-- Fila final de la captura: msgType 0, dataLen 0, chunkCount = NUMERO DE FILAS DE DATOS (dataRows).
-- Contrato para la app (T52): chunkCount de la fila final = filas de datos, no filas totales.
function Shot.BuildEnd(status, seq, N, shotId, dataRows)
    return Shot.BuildRow(status, seq, N, shotId, { type = 0, id = 0, index = 0, count = dataRows, data = {} })
end

-- Lee una trama de captura (array de bytes). Devuelve la tabla o nil + motivo.
function Shot.ParseRow(f)
    if type(f) ~= "table" or #f < Shot.OVERHEAD or f[1] ~= Shot.VERSION then return nil, "version" end
    local dlen = f[46] + 256 * f[47]
    local last = 47 + dlen
    if #f < last + 2 then return nil, "short" end
    local stored = f[last + 1] * 256 + f[last + 2]
    if ns.Channel.CRC16(f, 1, last) ~= stored then return nil, "crc" end
    local data, status = {}, {}
    for k = 1, dlen do data[k] = f[47 + k] end
    for k = 1, Shot.STATUS_LEN do status[k] = f[6 + k] end
    return {
        N = f[3] + 256 * f[4],
        seq = f[2],
        shotId = f[5] + 256 * f[6],
        status = status,
        type = f[39],
        id = f[40] + 256 * f[41],
        index = f[42] + 256 * f[43],
        count = f[44] + 256 * f[45],
        data = data,
    }
end

-- Filas de datos que necesitan las longitudes lens (bytes por mensaje) con cap bytes por fila.
-- Cada mensaje se trocea solo: ceil(len / cap) filas (al menos 1).
function Shot.DataRows(lens, cap)
    if not cap or cap <= 0 then return nil end
    local rows = 0
    for _, n in ipairs(lens) do rows = rows + max(1, ceil(n / cap)) end
    return rows
end

-- Geometria del panel para las longitudes lens (bytes por mensaje) y bloques de blockPx px.
-- Para cada ancho de WIDTHS: filas de datos necesarias (cap bytes por fila), capturas (cada una con
-- como mucho floor(MAX_BLOCKS / N) - 1 filas de datos, mas la final) y bloques totales. Gana el de
-- menos capturas y, a igualdad, el de menos bloques (menos texturas). Sin datos: 256x64 px.
-- Devuelve { W, N, cap, rows (filas de datos), rowsPerShot, shots, blocks (texturas de la captura
-- mayor), blockPx }.
function Shot.Geometry(lens, blockPx)
    blockPx = blockPx or Shot.BLOCK_PX
    local best
    for _, W in ipairs(Shot.WIDTHS) do
        local N = floor(W / blockPx)
        local cap = Shot.Capacity(N)
        local maxData = floor(Shot.MAX_BLOCKS / N) - 1 -- una fila es la final
        if cap > 0 and maxData >= 1 then
            local rows = Shot.DataRows(lens, cap) or 0
            local shots = max(1, ceil(rows / maxData))
            local total = N * (rows + shots)
            if not best or shots < best.shots or (shots == best.shots and total < best.total) then
                best = {
                    W = W, N = N, cap = cap, rows = rows, rowsPerShot = maxData,
                    shots = shots, total = total,
                    blocks = N * (min(rows, maxData) + 1),
                }
            end
        end
    end
    best.blockPx = blockPx
    return best
end

-- Tamano en pixeles fisicos del marco con dataRows filas de datos (+ la final): ancho N bloques y
-- alto de las filas, con minimo MIN_H (el marco negro; los bloques solo cubren las filas).
function Shot.PanelSize(N, dataRows, blockPx)
    blockPx = blockPx or Shot.BLOCK_PX
    local h = max(Shot.MIN_H, (dataRows + 1) * blockPx)
    return N * blockPx, h
end

-- Filas de una captura: una trama por trozo (en orden) y la fila final. Puro: sin API del juego.
-- chunks: lista de trozos de Channel (type, id, index, count, data), uno por fila.
-- La fila final lleva chunkCount = NUMERO DE FILAS DE DATOS de la captura (no el numero de filas
-- totales): la app lo usa para saber cuantas filas esperar. msgType 0, dataLen 0.
function Shot.PanelFrames(status, chunks, N, shotId)
    local frames = {}
    for i, ch in ipairs(chunks) do
        local f = Shot.BuildRow(status, (i - 1) % 256, N, shotId, ch)
        if not f then return nil, "trozo mayor que la fila" end
        frames[i] = f
    end
    frames[#chunks + 1] = Shot.BuildEnd(status, #chunks % 256, N, shotId, #chunks)
    return frames
end

-- ---------------------------------------------------------------------------
-- Estado de juego
-- ---------------------------------------------------------------------------

local function say(msg)
    if ns.Say then ns.Say(msg) end
end

local function nowSec()
    local t = ns.Num(ns.Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function later(sec, fn)
    ns.Call("C_Timer.After", sec, fn)
end

-- Estadisticas para /compa estado.
Shot.last = nil        -- { id, at, rows, bytes, format } de la ultima captura confirmada
Shot.sent = 0          -- capturas confirmadas
Shot.errors = 0        -- capturas fallidas
Shot.lastError = nil
Shot.blocked = false   -- Screenshot() ha fallado en este cliente: se usa la franja

-- G: panel (frame + texturas) y su pintado.
local G = { frame = nil, bg = nil, tex = {}, painted = {}, px = {}, py = {}, pb = {}, shown = 0 }
local run = nil        -- captura en curso (nil = reposo)
local evFrame = nil
local colorBuf = {}

-- Id de captura: uint16 creciente, guardado en settings (persistente).
local function nextShotId()
    local db = CompaneroWoWDB
    local last = 0
    if type(db) == "table" and type(db.settings) == "table" then
        last = ns.Num(db.settings.shotId) or 0
    end
    local id = floor(last) % 65535 + 1
    if type(db) == "table" and type(db.settings) == "table" then db.settings.shotId = id end
    return id
end

-- CVar (lectura y escritura con comprobacion). Las llamadas van protegidas.
local function cvarRead(name)
    local get = ns.Get("C_CVar.GetCVar") or ns.Get("GetCVar")
    if not get then return nil end
    local ok, v = pcall(get, name)
    if not ok then return nil end
    return ns.Str(v)
end

local function cvarWrite(name, value)
    local set = ns.Get("C_CVar.SetCVar") or ns.Get("SetCVar")
    if not set then return false end
    if not pcall(set, name, value) then return false end
    return cvarRead(name) == tostring(value)
end

-- Elige el formato sin perdidas que acepte el cliente: png, tga o, en ultimo caso, jpg (calidad 10).
-- Devuelve formato y tamano de bloque en px, o nil si ninguno.
function Shot.PickFormat()
    if cvarWrite("screenshotFormat", "png") then return "png", Shot.BLOCK_PX end
    if cvarWrite("screenshotFormat", "tga") then return "tga", Shot.BLOCK_PX end
    if cvarWrite("screenshotFormat", "jpg") then
        cvarWrite("screenshotQuality", tostring(Shot.QUALITY_JPG))
        return "jpg", Shot.JPG_BLOCK_PX
    end
    return nil
end

local function restoreCVars(saved)
    if not saved then return end
    for _, name in ipairs({ "screenshotFormat", "screenshotQuality" }) do
        if saved[name] ~= nil then cvarWrite(name, saved[name]) end
    end
end

-- Tamano de un pixel fisico en unidades del marco del panel (escala 1).
local function pixelUnits()
    local _, h = ns.GameWindowSize()
    local ui = _G.UIParent
    local uiH = ui and ns.Num(ns.Method(ui, "GetHeight"))
    local uiS = ui and ns.Num(ns.Method(ui, "GetEffectiveScale")) or 1
    if not uiS or uiS <= 0 then uiS = 1 end
    return ns.ComputePixelSize(h or 1080, uiH, uiS, 1)
end

-- Panel: frame a escala 1, por encima de todo, esquina superior izquierda, fondo negro.
local function ensureFrame()
    if G.frame then return G.frame end
    local f = ns.NewFrame("Frame", "CompaneroWoWShot", _G.UIParent)
    if not f then return nil end
    G.frame = f
    ns.Method(f, "SetFrameStrata", "FULLSCREEN_DIALOG")
    ns.Method(f, "SetFrameLevel", 1000)
    ns.Method(f, "SetIgnoreParentScale", true)
    ns.Method(f, "SetIgnoreParentAlpha", true)
    ns.Method(f, "SetScale", 1)
    ns.Method(f, "SetAlpha", 1)
    if _G.UIParent then
        ns.Method(f, "SetPoint", "TOPLEFT", _G.UIParent, "TOPLEFT", 0, 0)
    else
        ns.Method(f, "SetPoint", "TOPLEFT", 0, 0)
    end
    G.bg = ns.Method(f, "CreateTexture", nil, "BACKGROUND")
    if G.bg then
        ns.Method(G.bg, "SetColorTexture", 0, 0, 0, 1)
        ns.Method(G.bg, "SetAllPoints", f)
    end
    ns.Method(f, "Hide")
    return f
end

-- Pinta las filas (tramas) en el panel: un bloque por posicion, solo lo que cambia.
local function paintPanel(frames, geo)
    local f = ensureFrame()
    if not f then return false end
    local pxU = pixelUnits()
    if not pxU or pxU <= 0 then return false end
    local bp, N = geo.blockPx, geo.N
    local W, H = Shot.PanelSize(N, #frames - 1, bp)
    ns.Method(f, "SetSize", W * pxU, H * pxU)
    local bu = bp * pxU
    local idx = 0
    for r, frame in ipairs(frames) do
        local colors = ns.EncodeFrameColors(frame, N, colorBuf)
        for b = 1, N do
            idx = idx + 1
            local t = G.tex[idx]
            if not t then
                t = ns.Method(f, "CreateTexture", nil, "OVERLAY")
                if t then
                    ns.Method(t, "SetSnapToPixelGrid", false)
                    ns.Method(t, "SetTexelSnappingBias", 0)
                end
                G.tex[idx] = t
                G.painted[idx] = nil
            end
            if t then
                local x, y = (b - 1) * bu, -(r - 1) * bu
                if G.pb[idx] ~= bu or G.px[idx] ~= x or G.py[idx] ~= y then
                    ns.Method(t, "SetSize", bu, bu)
                    ns.Method(t, "SetPoint", "TOPLEFT", f, "TOPLEFT", x, y)
                    G.pb[idx], G.px[idx], G.py[idx] = bu, x, y
                end
                local c = colors[b]
                if G.painted[idx] ~= c then
                    local rr, gg, bb = floor(c / 65536), floor(c / 256) % 256, c % 256
                    ns.Method(t, "SetColorTexture", rr / 255, gg / 255, bb / 255, 1)
                    G.painted[idx] = c
                end
            end
        end
    end
    -- Texturas visibles: las de este panel; las sobrantes se ocultan.
    for i = idx + 1, G.shown do
        if G.tex[i] then ns.Method(G.tex[i], "Hide") end
    end
    for i = G.shown + 1, idx do
        if G.tex[i] then ns.Method(G.tex[i], "Show") end
    end
    G.shown = idx
    return true
end

local function hidePanel()
    if G.frame then ns.Method(G.frame, "Hide") end
end

-- Oculta la franja si estuviera visible (el panel y la franja usan la esquina superior izquierda).
local function hideStrip()
    local b = ns.bridge
    if b and b.frame then ns.Method(b.frame, "Hide") end
end

-- Notices de captura: se ocultan durante la captura y vuelven tras NOTICE_SEC.
local function hideNotices(r)
    r.notices = {}
    for _, name in ipairs(Shot.NOTICE_FRAMES) do
        local f = _G[name]
        if type(f) == "table" then
            local a = ns.Num(ns.Method(f, "GetAlpha"))
            if a then
                r.notices[#r.notices + 1] = { f = f, alpha = a }
                ns.Method(f, "SetAlpha", 0)
            end
        end
    end
end

local function restoreNotices(list)
    for _, n in ipairs(list or {}) do ns.Method(n.f, "SetAlpha", n.alpha) end
end

-- Serializa todo el plan de la activacion (sin extraer trozos).
local function drainPlan(now)
    local C = ns.Channel
    local guard = 0
    while not C.PlanDone() and guard < 100000 do
        C.Pump(now, 50)
        guard = guard + 1
    end
end

-- Saca hasta maxRows trozos de la cola (con capacidad cap) y los anade a out. Espera a los pesados.
-- out se rellena aunque falle a mitad: asi un aborto puede devolver lo ya sacado a la cola.
local function takeRows(cap, maxRows, out)
    local C = ns.Channel
    local frameNo, guard = 0, 0
    local limit = maxRows * (C.HEAVY_EVERY or 3) + 1000
    while #out < maxRows and guard < limit do
        frameNo = frameNo + 1
        guard = guard + 1
        local ch = C.NextChunkCapacity(cap, frameNo)
        if ch then
            out[#out + 1] = ch
        elseif C.Pending() == 0 then
            break
        end
    end
    return out
end

-- Aborta la captura en curso (error inesperado): deja el estado limpio para la siguiente activacion.
local function abortRun(reason)
    local r = run
    if not r then return end
    run = nil
    r.waiting = nil
    hidePanel()
    local list = r.chunks
    r.chunks = nil
    ns.Channel.RequeueChunks(list)
    ns.Channel.Stop()
    restoreCVars(r.saved)
    later(Shot.NOTICE_SEC, function() restoreNotices(r.notices) end)
    Shot.errors = Shot.errors + 1
    Shot.lastError = reason
    say("La captura falló (" .. tostring(reason) .. "). Sus datos vuelven a la cola y se envían en el siguiente envío.")
end

-- Ejecuta fn protegida: si falla, aborta la captura en curso y no deja nada a medias.
local function guard(fn, ...)
    local ok, err = pcall(fn, ...)
    if not ok then
        if ns.RecordError then ns.RecordError(err) end
        abortRun(err)
    end
end

local function finish(ok, err)
    local r = run
    if not r then return end
    r.waiting = nil
    hidePanel()
    local notices = r.notices
    later(Shot.NOTICE_SEC, function() restoreNotices(notices) end)
    local C = ns.Channel
    local list = r.chunks
    r.chunks = nil
    if ok then
        C.ConfirmChunks(list)
        Shot.sent = Shot.sent + 1
        Shot.last = { id = r.shotId, at = ns.Now(), rows = r.rows, bytes = r.bytes, format = r.fmt }
        if ns.UI and ns.UI.OnShotSent then ns.SafeCall(ns.UI.OnShotSent, r.shotId) end
        if r.shotNo < Shot.MAX_SHOTS and C.Pending() > 0 then
            later(Shot.GAP_SEC, function()
                if run == r and not r.waiting then guard(Shot.NextShot) end
            end)
            return
        end
    else
        C.RequeueChunks(list)
        Shot.errors = Shot.errors + 1
        Shot.lastError = err
        say(err .. ". Sus datos vuelven a la cola y se envían en el siguiente envío.")
    end
    -- Fin de la activacion: restaura las CVar y deja lo que no quepo para la siguiente.
    run = nil
    C.Stop()
    restoreCVars(r.saved)
    local left = C.Pending()
    if ok then
        say(string.format("Datos enviados en %d captura(s).", r.shotNo))
    end
    if left > 0 then
        say(string.format("Quedan %d mensaje(s) para la siguiente captura (límite de %d por envío).", left, Shot.MAX_SHOTS))
    end
end

-- Llamado por el evento SCREENSHOT_SUCCEEDED / SCREENSHOT_FAILED (y ignorado sin captura en curso).
function Shot.OnResult(ok)
    local r = run
    if not r or not r.waiting then return end
    if ok then
        guard(finish, true)
    else
        guard(finish, false, "El juego no pudo guardar la captura (SCREENSHOT_FAILED)")
    end
end

-- Dispara Screenshot() un frame despues de pintar el panel. El token descarta eventos viejos.
local function fire(token)
    local r = run
    if not r or r.waiting ~= token then return end
    local fn = ns.Get("Screenshot")
    if not fn then
        Shot.blocked = true
        finish(false, "Screenshot() no existe en este cliente")
        return
    end
    local ok = pcall(fn)
    if not ok then
        Shot.blocked = true
        guard(finish, false, "Screenshot() no está permitido para los complementos en este cliente")
        return
    end
    later(Shot.TIMEOUT_SEC, function()
        if run and run.waiting == token then guard(finish, false, "El juego no confirmó la captura a tiempo") end
    end)
end

local function ensureEvents()
    if evFrame then return end
    evFrame = ns.NewFrame("Frame", "CompaneroWoWShotEvents", nil)
    if not evFrame then return end
    ns.Method(evFrame, "RegisterEvent", "SCREENSHOT_SUCCEEDED")
    ns.Method(evFrame, "RegisterEvent", "SCREENSHOT_FAILED")
    ns.Method(evFrame, "SetScript", "OnEvent", function(_, event)
        ns.SafeCall(Shot.OnResult, event == "SCREENSHOT_SUCCEEDED")
    end)
end

-- Una captura: toma sus trozos, compone las filas, pinta el panel y programa Screenshot().
function Shot.NextShot()
    local r = run
    if not r then return end
    r.shotNo = r.shotNo + 1
    local C = ns.Channel
    local now = nowSec()
    r.chunks = r.chunks or {}
    local chunks = takeRows(r.geo.cap, r.geo.rowsPerShot, r.chunks)
    local status = ns.BuildPayload(ns.ReadLiveState(C.RemainingSec(now)))
    local shotId = nextShotId()
    local frames, why = Shot.PanelFrames(status, chunks, r.geo.N, shotId)
    if not frames or not paintPanel(frames, r.geo) then
        finish(false, "No se pudo pintar el panel de datos" .. (why and (": " .. why) or ""))
        return
    end
    local bytes = 0
    for _, ch in ipairs(chunks) do bytes = bytes + #ch.data end
    r.shotId, r.rows, r.bytes = shotId, #chunks, bytes
    r.token = (r.token or 0) + 1
    r.waiting = r.token
    if r.shotNo == 1 then hideNotices(r) end
    ns.Method(G.frame, "Show")
    local token = r.token
    later(0, function() guard(fire, token) end)
end

-- Entrada de Share.lua. opts: { full = bool, question = bool }. Devuelve:
--   "started"                     captura en marcha (ver run)
--   "combat"                      en combate y sin pregunta: nada que enviar
--   "busy"                        ya hay una captura en curso
--   "unavailable", motivo         no hay capturas (sin Screenshot, bloqueado, sin formato, error interno):
--                                 usar la franja
--   "failed"                      el panel no se pudo pintar (ya avisado)
local function startBody(now, opts)
    opts = opts or {}
    if run then return "busy" end
    local question = opts.question and true or false
    if ns.InCombat() and not question then return "combat" end
    if Shot.blocked then return "unavailable", "Screenshot() está bloqueado en este cliente" end
    if not ns.Get("Screenshot") then return "unavailable", "Screenshot() no existe en este cliente" end
    local C = ns.Channel
    if not C then return "unavailable", "no hay canal" end
    local saved = { screenshotFormat = cvarRead("screenshotFormat"), screenshotQuality = cvarRead("screenshotQuality") }
    local fmt, blockPx = Shot.PickFormat()
    if not fmt then
        restoreCVars(saved)
        return "unavailable", "el cliente no acepta png, tga ni jpg en screenshotFormat"
    end
    C.Begin(now, opts)
    drainPlan(now)
    local geo = Shot.Geometry(C.PendingLengths(), blockPx)
    run = { geo = geo, saved = saved, fmt = fmt, shotNo = 0, rows = 0, bytes = 0, token = 0 }
    hideStrip()
    ensureEvents()
    Shot.NextShot()
    if run == nil then return "failed" end -- NextShot ya dijo por que
    return "started"
end

function Shot.Start(now, opts)
    local ok, res, why = pcall(startBody, now, opts)
    if ok then return res, why end
    if ns.RecordError then ns.RecordError(res) end
    if run then abortRun(res) end
    ns.Channel.Stop()
    return "unavailable", "error interno: " .. tostring(res)
end

-- Captura en curso (para Share.lua y pruebas).
function Shot.Busy()
    return run ~= nil
end

-- Lineas para /compa estado.
function Shot.StatusLines()
    local lines = {}
    local last = Shot.last
    if last then
        lines[#lines + 1] = string.format("Captura: última #%d hace %d s; %d fila(s) de datos, %d bytes; formato %s",
            last.id, max(0, floor(ns.Now() - last.at)), last.rows, last.bytes, last.format)
    else
        lines[#lines + 1] = "Captura: ninguna en esta sesión"
    end
    local err = Shot.lastError and (" (último: " .. Shot.lastError .. ")") or ""
    lines[#lines + 1] = string.format("Capturas: %d confirmada(s), %d con error%s", Shot.sent, Shot.errors, err)
    if Shot.blocked then
        lines[#lines + 1] = "Captura: Screenshot() bloqueado; se usa la franja de píxeles"
    end
    return lines
end

-- Pruebas: acceso a internos del panel.
Shot.testing = {
    panel = function() return G.frame, G.tex, G.shown end,
    current = function() return run end,
    reset = function()
        run = nil
        G = { frame = nil, bg = nil, tex = {}, painted = {}, px = {}, py = {}, pb = {}, shown = 0 }
        Shot.last, Shot.sent, Shot.errors, Shot.lastError, Shot.blocked = nil, 0, 0, nil, false
    end,
    fire = fire,
}

return Shot
