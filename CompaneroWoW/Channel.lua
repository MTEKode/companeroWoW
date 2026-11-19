-- Channel.lua: canal de datos en vivo sobre la franja superior (protocolo v3.1), bajo demanda.
-- Parte pura (testeable offline): CRC-16, capacidad, trama y planificador de trozos.
-- Parte de juego: el plan de envio de una activacion (tecla o /compa enviar). En reposo no hay
-- nada que serializar: ni carrusel, ni JSON.
--
-- Trama v3.1 (bytes, 1-based; todo big-endian salvo donde se indica):
--   [1] version=4  [2] seq  [3] N lo  [4] N hi            (N = bloques de la fila, u16 LE)
--   [5..36]  estado (32 bytes, mismo layout y CRC-8 que v2; byte 30 = shareRemainingSec)
--   [37] msgType  [38..39] msgId (LE)  [40..41] chunkIndex (LE)  [42..43] chunkCount (LE)
--   [44..45] dataLen (LE)
--   [46..45+dataLen] datos del trozo
--   [46+dataLen, 47+dataLen] CRC-16/CCITT-FALSE de los bytes 1..45+dataLen (big-endian)
-- Sin trozo pendiente: msgType = 0 y dataLen = 0. Los nibbles sobrantes valen 0.
-- Capacidad de datos por trama: floor(3*(N-7)/2) - 47 bytes (N = 860 -> 1232 bytes).
--
-- Activacion (C.Begin): ventana de SHARE_SECONDS. Al empezar una ventana nueva (desde reposo):
--  - cada item de la bandeja (Storage.lua) se encola como un mensaje propio, en orden de seq; su JSON
--    ({oseq, p, v}) se codifica al salir. Al salir entero, cuenta una ventana para el item.
--  - el plan recorre el estado actual (C.TYPES, en orden C.PRIORITY): cada tipo se serializa una vez
--    por activacion, troceado: C.Pump reparte el trabajo en tramas con PUMP_BUDGET_MS por trama.
-- Solo se encola lo que cambio: por tipo se guarda el hash del ultimo envio (S.sent, persistido en
-- el personaje como c.sent). Los tipos clave (KEY_TYPES) se reenvian si el ultimo envio tiene mas de
-- KEY_RESEND_SEC, o si no hay hora de ese envio (tras recargar).
-- C.Begin(now, { full = true }) (/compa sync) envia todo el estado sin saltarse nada.
-- Pregunta (msgType 15, C.SendQuestion): prioridad maxima, reenviada cada QUESTION_RESEND_SEC.
-- C.Update devuelve false al acabar la ventana: a los SHARE_SECONDS, o antes si la cola esta vacia
-- y han pasado al menos SHARE_MIN_SEC.
local ADDON_NAME, ns = ...
ns = ns or {}

local bit = bit
local band, bxor, lshift, rshift = bit.band, bit.bxor, bit.lshift, bit.rshift
local floor, min, max, ceil = math.floor, math.min, math.max, math.ceil
local byte, concat = string.byte, table.concat
local type, pairs, ipairs, tostring, next = type, pairs, ipairs, tostring, next
local cocreate, coresume, costatus = coroutine.create, coroutine.resume, coroutine.status
local tinsert, tremove = table.insert, table.remove

local C = {}
ns.Channel = C

C.VERSION = 4
C.STATUS_LEN = 32
C.CHUNK_HEADER_LEN = 9
C.OVERHEAD = 4 + 32 + 9 + 2 -- cabecera + estado + cabecera de trozo + CRC-16
C.CALIB_BLOCKS = 7
C.SHARE_SECONDS = 20        -- duracion maxima de una activacion (se alarga si se vuelve a pulsar)
C.SHARE_MIN_SEC = 3         -- minimo de una activacion: no acaba antes aunque la cola este vacia
C.KEY_RESEND_SEC = 600      -- tipos clave: se reenvian si pasan 10 min (o tras recargar)
C.QUESTION_TYPE = 15        -- pregunta del jugador (contrato in-game): msgType 15, prioridad maxima
C.QUESTION_RESEND_SEC = 2   -- la pregunta se reenvia cada 2 s durante la ventana
C.PUMP_BUDGET_MS = 1        -- presupuesto de serializacion por trama
C.HEAVY_TYPE = 10           -- tipo pesado: como mucho un trozo cada HEAVY_EVERY tramas
C.HEAVY_EVERY = 3
C.MAX_MESSAGE_BYTES = 98304 -- tope de cada mensaje JSON (mas grande -> no se envia)
C.TICK_SEC = 0.2            -- una trama cada 0.2 s (Bridge.lua REFRESH_SEC)

-- Orden de envio (de mayor a menor prioridad en el planificador). Los tipos de la bandeja (7, 9, 16)
-- y la hoja de cambios (12) ocupan su lugar en el orden; los que no tienen TYPES no entran en el plan.
-- Personaje, hoja, misiones, equipo, sesion, bolsas, profesiones, macros, barras, combate,
-- misiones completadas, conocimiento (bandeja), metadatos, el pesado (10) y, al final, el mundo (bandeja).
-- La pregunta (15) no esta aqui: va por delante de todo (RANK[15] = 0, ver SendQuestion).
C.PRIORITY = { 1, 12, 2, 4, 14, 3, 5, 13, 8, 7, 6, 9, 11, 10, 16 }
local RANK = {}
for i, t in ipairs(C.PRIORITY) do RANK[t] = i end
-- La pregunta no pasa por el plan: va al frente de la cola (rango 0) en cuanto se encola.
RANK[C.QUESTION_TYPE] = 0
C.RANK = RANK
-- Tipos clave: se reenvian cada KEY_RESEND_SEC aunque no hayan cambiado.
-- character (1), quests (2), equipment (4), sheet (12).
C.KEY_TYPES = { [1] = true, [2] = true, [4] = true, [12] = true }

-- ---------------------------------------------------------------------------
-- CRC-16/CCITT-FALSE (poly 0x1021, init 0xFFFF, sin reflexion, sin xorout)
-- ---------------------------------------------------------------------------
local CRC_TABLE = {}
for i = 0, 255 do
    local c = i * 256
    for _ = 1, 8 do
        local top = c >= 32768
        c = (c * 2) % 65536
        if top then c = bxor(c, 0x1021) end
    end
    CRC_TABLE[i] = c
end

-- CRC-16 de bytes[first..last] (1-based). Vector de prueba: "123456789" -> 0x29B1.
function C.CRC16(bytes, first, last)
    local crc = 0xFFFF
    for i = first, last do
        local idx = bxor(rshift(crc, 8), band(bytes[i], 0xFF))
        crc = bxor(band(lshift(crc, 8), 0xFFFF), CRC_TABLE[band(idx, 0xFF)])
    end
    return crc
end

-- ---------------------------------------------------------------------------
-- Trama
-- ---------------------------------------------------------------------------

-- Bytes de datos que caben en una trama con N bloques (0 si no cabe ninguno).
function C.Capacity(N)
    local bytes = floor(3 * (N - C.CALIB_BLOCKS) / 2)
    return max(0, bytes - C.OVERHEAD)
end

local function u16(v)
    return v % 256, floor(v / 256) % 256
end

-- Compone la trama v3 (array de bytes 1-based).
--   status: 32 bytes del estado (array 1-based con CRC-8 en 32).
--   seq: 0..255. N: bloques de la fila.
--   chunk: nil, o { type, id, index (0-based), count, data = array de bytes }.
-- Devuelve la trama, o nil si el trozo no cabe.
function C.BuildFrame(status, seq, N, chunk)
    local dlen, msgType, id, index, count = 0, 0, 0, 0, 0
    local data
    if chunk then
        data = chunk.data
        dlen = #data
        msgType, id, index, count = chunk.type, chunk.id, chunk.index, chunk.count
    end
    if dlen > C.Capacity(N) then return nil end
    local f = { C.VERSION, seq % 256 }
    f[3], f[4] = u16(N)
    for k = 1, C.STATUS_LEN do f[4 + k] = status[k] or 0 end
    f[37] = msgType
    f[38], f[39] = u16(id)
    f[40], f[41] = u16(index)
    f[42], f[43] = u16(count)
    f[44], f[45] = u16(dlen)
    for k = 1, dlen do f[45 + k] = data[k] end
    local last = 45 + dlen
    local crc = C.CRC16(f, 1, last)
    f[last + 1] = floor(crc / 256)
    f[last + 2] = crc % 256
    return f
end

-- Lee una trama v3 (array de bytes). Devuelve tabla con los campos o nil + motivo.
-- No verifica el estado (CRC-8 de la posicion 32); eso lo hace el decodificador de la app.
function C.ParseFrame(f)
    if #f < C.OVERHEAD or f[1] ~= C.VERSION then return nil, "version" end
    local N = f[3] + 256 * f[4]
    local dlen = f[44] + 256 * f[45]
    local last = 45 + dlen
    if #f < last + 2 then return nil, "short" end
    local stored = f[last + 1] * 256 + f[last + 2]
    if C.CRC16(f, 1, last) ~= stored then return nil, "crc" end
    local data = {}
    for k = 1, dlen do data[k] = f[45 + k] end
    local status = {}
    for k = 1, C.STATUS_LEN do status[k] = f[4 + k] end
    return {
        N = N, seq = f[2], status = status,
        type = f[37], id = f[38] + 256 * f[39],
        index = f[40] + 256 * f[41], count = f[42] + 256 * f[43],
        data = data,
    }
end

-- ---------------------------------------------------------------------------
-- Planificador (puro)
-- ---------------------------------------------------------------------------
-- Cada tipo de estado tiene como mucho un trabajo activo; encolar de nuevo el mismo tipo reemplaza el
-- trabajo (el mensaje nuevo gana y el receptor descarta el parcial). Los items de la bandeja (job.seq)
-- son mensajes propios: no se reemplazan entre si ni a un tipo de estado.
-- Un trabajo: { type, id, str (JSON), make (codifica str al salir), cs (bytes por trozo), count,
--   next (1-based), heavy, optional, seq, onDone }.
-- optional: volcado que solo sale si hay tiempo (no cuenta para acabar antes).

local Sched = {}
Sched.__index = Sched

function C.NewScheduler()
    return setmetatable({
        jobs = {},        -- trabajos pendientes, en orden de turno
        nextMsgId = 0,    -- msgId 1..65535 (envuelve)
    }, Sched)
end

local function newMsgId(s)
    s.nextMsgId = s.nextMsgId % 65535 + 1
    return s.nextMsgId
end

-- Encola el mensaje msgType con el texto JSON str. Reemplaza un trabajo pendiente del mismo tipo
-- (salvo los items de la bandeja). heavy: carril lento (un trozo de cada HEAVY_EVERY tramas).
function Sched:enqueue(msgType, str, heavy, optional)
    for i, job in ipairs(self.jobs) do
        if job.type == msgType and not job.seq then
            tremove(self.jobs, i)
            break
        end
    end
    self.jobs[#self.jobs + 1] = {
        type = msgType, id = newMsgId(self), str = str, cs = 0, count = 0, next = 1,
        heavy = (heavy or msgType == C.HEAVY_TYPE) and true or false,
        optional = optional and true or false,
    }
end

-- Encola un item de la bandeja: mensaje propio. make() devuelve su JSON al salir (nil: se descarta).
-- onDone() se llama al salir el ultimo trozo.
function Sched:enqueueItem(msgType, seq, make, onDone)
    self.jobs[#self.jobs + 1] = {
        type = msgType, id = newMsgId(self), str = nil, make = make, cs = 0, count = 0, next = 1,
        heavy = false, optional = false, seq = seq, onDone = onDone,
    }
end

-- Quita todos los items de la bandeja de la cola (al vaciar la bandeja). Devuelve cuantos quito.
function Sched:dropItems()
    local keep, n = {}, 0
    for _, job in ipairs(self.jobs) do
        if job.seq then
            n = n + 1
        else
            keep[#keep + 1] = job
        end
    end
    self.jobs = keep
    return n
end

function Sched:pending()
    return #self.jobs
end

-- Codifica los items que aun no tienen texto (make) y quita los que no se pueden codificar.
function Sched:encodeAll()
    local keep = {}
    for _, job in ipairs(self.jobs) do
        if job.str == nil and job.make then job.str = job.make() end
        if job.str ~= nil then keep[#keep + 1] = job end
    end
    self.jobs = keep
end

-- Longitud en bytes de cada trabajo pendiente (tras codificar). Sin tocar el turno.
function Sched:lengths()
    self:encodeAll()
    local out = {}
    for i, job in ipairs(self.jobs) do out[i] = #job.str end
    return out
end

-- Trabajos pendientes que no son optativos (el plan no ha acabado hasta que salgan).
function Sched:requiredPending()
    local n = 0
    for _, job in ipairs(self.jobs) do
        if not job.optional then n = n + 1 end
    end
    return n
end

-- Numero de trozos de un texto de n bytes con cs bytes por trozo (al menos 1).
local function chunkCount(n, cs)
    return max(1, ceil(n / cs))
end

-- Indice del trabajo con mejor rango que puede salir en esta trama (nil si ninguno).
function Sched:pick(frameNo)
    local bestI, bestRank = nil, math.huge
    for i, job in ipairs(self.jobs) do
        local r = RANK[job.type] or 99
        local heavyOff = job.heavy and frameNo % C.HEAVY_EVERY ~= 0
        if r < bestRank and not heavyOff then
            bestI, bestRank = i, r
        end
    end
    return bestI
end

-- Saca un trozo del trabajo i. Al salir el ultimo, llama a onDone.
function Sched:serve(i, job, cs)
    tremove(self.jobs, i)
    if job.cs ~= cs then
        -- Capacidad distinta (p. ej. cambio de ancho): el trozado se rehace con un msgId nuevo.
        job.cs = cs
        job.count = chunkCount(#job.str, cs)
        job.id = newMsgId(self)
        job.next = 1
    end
    local from = (job.next - 1) * cs + 1
    local to = min(job.next * cs, #job.str)
    local data = { byte(job.str, from, to) } -- vacio si el texto no tiene bytes
    local chunk = { type = job.type, id = job.id, index = job.next - 1, count = job.count, data = data }
    job.next = job.next + 1
    if job.next <= job.count then
        if job.seq then
            tinsert(self.jobs, 1, job) -- un item de la bandeja termina antes que el siguiente (mas antiguo primero)
        else
            self.jobs[#self.jobs + 1] = job -- al final: turno rotatorio entre mensajes
        end
    elseif job.onDone then
        job.onDone()
    end
    return chunk
end

-- Trozo siguiente segun la prioridad y el turno. cs = capacidad de datos de la trama.
-- Devuelve el trozo { type, id, index (0-based), count, data } o nil si no hay nada que enviar
-- en esta trama. frameNo sirve para limitar los tipos pesados.
function Sched:nextChunk(cs, frameNo)
    if cs <= 0 then return nil end
    while #self.jobs > 0 do
        local bestI = self:pick(frameNo)
        if not bestI then return nil end
        local job = self.jobs[bestI]
        if job.str == nil and job.make then job.str = job.make() end
        if job.str ~= nil then return self:serve(bestI, job, cs) end
        tremove(self.jobs, bestI) -- item que no se puede codificar: se descarta
    end
    return nil
end

-- true si hay un trabajo pendiente del tipo t.
function Sched:has(msgType)
    for _, job in ipairs(self.jobs) do
        if job.type == msgType then return true end
    end
    return false
end

-- ---------------------------------------------------------------------------
-- Tipos de mensaje de estado actual (lado de juego)
-- ---------------------------------------------------------------------------
-- build(c) devuelve la tabla a enviar, o nil si no hay nada. c = CompaneroWoWDB.characters[key]
-- (sus campos de memoria se leen como campos normales). Cada tipo se serializa una vez por
-- activacion, en el momento de su turno del plan.
-- Los tipos 7, 9 y 16 (combates, conocimiento, mundo) viajan por la bandeja (Storage.lua).

C.TYPES = {
    [1] = {
        name = "character",
        build = function(c)
            local free = ns.CountFreeBagSlots and ns.CountFreeBagSlots() or nil -- antes en el byte 30 de la franja
            return { character = c.character, reputations = c.reputations, talents = c.talents, freeBagSlots = free }
        end,
    },
    [2] = {
        name = "quests",
        build = function(c)
            -- native: misiones del juego y sus fuentes (Native.lua), en el mismo bloque que las misiones
            return { quests = c.quests, location = c.location, native = c.native }
        end,
    },
    [3] = {
        name = "bags",
        build = function(c)
            return { inventory = c.inventory }
        end,
    },
    [4] = {
        name = "equipment",
        build = function(c)
            return { equipment = c.equipment, gear = c.gear, proficiencies = c.proficiencies }
        end,
    },
    [5] = {
        name = "professions",
        build = function(c)
            return { professions = c.professions, training = c.training, profPlan = c.profPlan }
        end,
    },
    [6] = {
        name = "completedQuests",
        build = function(c)
            return c.completedQuests -- lista de ids; vacia -> []
        end,
    },
    [8] = {
        name = "actionBars",
        build = function(c)
            return { actionBars = c.actionBars }
        end,
    },
    [10] = {
        name = "heavy", heavy = true,
        build = function(c)
            return { questie = c.questie, questWindow = c.questWindow, gearCandidates = c.gearCandidates, dungeons = c.dungeons }
        end,
    },
    [11] = {
        name = "meta",
        build = function(c)
            local health = { luaErrors = ns.errorCount or 0, lastError = ns.lastError }
            return { version = ns.VERSION, schema = ns.SCHEMA, updatedAt = c.updatedAt, health = health, channel = C.VERSION }
        end,
    },
    [12] = {
        -- hoja de personaje (Sheet.lua). Sheet.ForShare recorta los hechizos hasta que quepa.
        name = "sheet",
        build = function(c)
            local s = c.sheet
            if type(s) ~= "table" then return nil end
            if ns.Sheet and ns.Sheet.ForShare then return ns.Sheet.ForShare(s) end
            return s
        end,
    },
    [13] = {
        -- macros de cuenta y de personaje (Macros.lua).
        name = "macros",
        build = function(c)
            local m = c.macros
            if type(m) ~= "table" then return nil end
            return m
        end,
    },
    [14] = {
        -- sesion del personaje (Session.lua): historial de la sesion en curso.
        name = "session",
        build = function(c)
            if not (ns.Session and ns.Session.Public) then return nil end
            return ns.Session.Public(c.session)
        end,
    },
}

-- ---------------------------------------------------------------------------
-- Estado del canal
-- ---------------------------------------------------------------------------
local S = {
    sched = nil,          -- planificador (sobrevive a la activacion: los trozos pendientes salen en la siguiente)
    share = nil,          -- activacion en curso (nil = reposo)
    cur = nil,            -- unidad de serializacion en curso { t, co }
    win = nil,            -- numero de la ventana de envio actual (Storage.NewWindow)
    queued = {},          -- [seq] = true: item de la bandeja ya encolado y sin salir
    sent = {},            -- [msgType] = hash del ultimo mensaje de estado enviado (solo se reenvia si cambia)
    sentAt = {},          -- [msgType] = GetTime del ultimo envio de ese tipo (nil tras recargar)
    skipped = 0,          -- mensajes que no se encolaron por no haber cambiado (estadistica)
    question = nil,       -- ultima pregunta { json, nextAt }: se reenvia durante la ventana
    sentCount = 0,        -- mensajes serializados (estadistica para /compa estado)
    errors = 0,
    lastError = nil,
}
C.state = S

local function ensureScheduler()
    if not S.sched then S.sched = C.NewScheduler() end
    return S.sched
end

-- Milisegundos de reloj (debugprofilestop en WoW; os.clock en pruebas).
local function clockMs()
    local t = ns.Num(ns.Call("debugprofilestop"))
    if t then return t end
    return os.clock() * 1000
end

local function recordFailure(err)
    S.errors = S.errors + 1
    S.lastError = tostring(err)
end

-- Clave del personaje actual o nil.
local function currentChar()
    local key = ns.CharKey and ns.CharKey()
    if not key or type(CompaneroWoWDB) ~= "table" then return nil end
    local c = CompaneroWoWDB.characters and CompaneroWoWDB.characters[key]
    if type(c) ~= "table" then return nil end
    return c
end

-- ---------------------------------------------------------------------------
-- Bandeja (items de Storage.lua): cada item sale como mensaje propio
-- ---------------------------------------------------------------------------

-- Encola un item de la bandeja (si no esta ya encolado). Su JSON ({oseq, p, v}) se codifica al salir.
local function enqueueItem(item)
    if S.queued[item.seq] then return end
    S.queued[item.seq] = true
    local seq = item.seq
    ensureScheduler():enqueueItem(item.msgType, seq,
        function() return ns.Json.Encode(item.payload) end,
        function()
            S.queued[seq] = nil
            if ns.Storage then ns.Storage.MarkShared(seq, S.win) end
        end)
end

-- Item nuevo durante una activacion: entra en la cola de la ventana en curso.
function C.OnAppend(item)
    if S.share and item and type(item.payload) == "table" then enqueueItem(item) end
end

-- Al vaciar la bandeja (/compa limpiar si): se quitan sus mensajes de la cola.
function C.ClearOutbox()
    if S.sched then S.sched:dropItems() end
    S.queued = {}
end

if ns.Storage then
    ns.Storage.OnAppend = C.OnAppend
    ns.Storage.OnClear = C.ClearOutbox
end

-- Restaura los hashes de envio guardados en el personaje (tras recargar).
local function restoreSent()
    local c = currentChar()
    if not c or type(c.sent) ~= "table" then return end
    for t, h in pairs(c.sent) do
        if S.sent[t] == nil then S.sent[t] = h end
    end
end

-- Ventana nueva: se abre el numero de ventana, se descartan los items que no salieron en la ventana
-- anterior y se encolan todos los de la bandeja (el mas antiguo primero). Un item cuenta una ventana
-- solo cuando sale entero: lo que no cabe en la ventana no se cuenta.
local function startWindow()
    if not ns.Storage then return end
    S.win = ns.Storage.NewWindow()
    if S.sched then S.sched:dropItems() end
    S.queued = {}
    for _, item in ipairs(ns.Storage.Items()) do enqueueItem(item) end
end

-- ---------------------------------------------------------------------------
-- Unidades de serializacion (una por tipo; se reanudan trama a trama)
-- ---------------------------------------------------------------------------

-- Cuerpo de la unidad: construye la tabla del tipo, cede, y la codifica cediendo cada pocos nodos.
-- Devuelve el JSON, o nil (sin datos) o nil + motivo (no se pudo codificar). Se ejecuta en una coroutine.
local function unitBody(t)
    local def = C.TYPES[t]
    local c = currentChar()
    if not def or not def.build or not c then return nil end
    local value = def.build(c, CompaneroWoWDB)
    if value == nil then return nil end
    if t == 6 and type(value) == "table" and next(value) == nil then return "[]" end
    coroutine.yield()
    return ns.Json.EncodeAsync(value)
end

local function startUnit(t)
    S.cur = { t = t, co = cocreate(function() return unitBody(t) end) }
end

-- Un tipo ya enviado con el mismo hash se omite. Completo (/compa sync): nunca se omite. Tipo clave:
-- se reenvia si el ultimo envio tiene KEY_RESEND_SEC o mas, o si no hay hora de ese envio.
local function unchanged(t, h, now, full)
    if full or S.sent[t] ~= h then return false end
    if C.KEY_TYPES[t] then
        local at = S.sentAt[t]
        if at == nil or now - at >= C.KEY_RESEND_SEC then return false end
    end
    return true
end

local function resumeUnit()
    local u = S.cur
    local ok, json, why = coresume(u.co)
    if costatus(u.co) ~= "dead" then return "yield" end
    S.cur = nil
    if not ok then
        ns.RecordError(json)
        recordFailure(json)
    elseif json then
        local sh = S.share
        local now = sh and sh.now or 0
        local h = ns.HashText(json)
        if unchanged(u.t, h, now, sh and sh.full) then
            S.skipped = S.skipped + 1
        else
            ensureScheduler():enqueue(u.t, json)
            S.sent[u.t], S.sentAt[u.t] = h, now
            local c = currentChar()
            if c then
                if type(c.sent) ~= "table" then c.sent = {} end
                c.sent[u.t] = h
            end
            S.sentCount = S.sentCount + 1
        end
    elseif why then
        recordFailure(why)
    end
    return "done"
end

-- Serializa un tipo de una vez (para pruebas y para /compa, fuera del plan). nil si no hay datos.
local function fullJSON(t)
    local co = cocreate(function() return unitBody(t) end)
    while true do
        local ok, json, why = coresume(co)
        if not ok then
            ns.RecordError(json)
            recordFailure(json)
            return nil
        end
        if costatus(co) == "dead" then
            if not json and why then recordFailure(why) end
            return json
        end
    end
end

-- ---------------------------------------------------------------------------
-- Activacion: plan, presupuesto por trama y ventana
-- ---------------------------------------------------------------------------

-- Un paso del plan (una reanudacion de unidad). Devuelve progreso (bool).
local function planStep(sh)
    local t = sh.plan[sh.pos]
    if not S.cur then
        startUnit(t)
    elseif resumeUnit() == "done" then
        sh.pos = sh.pos + 1
    end
    return true
end

-- Empieza una activacion (o la reinicia si ya hay una: pulsar otra vez alarga la ventana y
-- vuelve a serializar cada tipo; solo encola lo que cambio). now: segundos (GetTime).
-- opts.full = true: envia todo (/compa sync). Devuelve true si era una reactivacion.
function C.Begin(now, opts)
    local again = S.share ~= nil
    S.share = {
        startAt = now, endAt = now + C.SHARE_SECONDS, now = now,
        full = (opts and opts.full) and true or false,
        plan = {}, pos = 1, planDone = false, planDoneAt = nil,
    }
    for _, t in ipairs(C.PRIORITY) do
        if C.TYPES[t] then S.share.plan[#S.share.plan + 1] = t end
    end
    S.cur = nil
    if not again then
        restoreSent()
        startWindow()
    end
    -- Si hay una pregunta, sale de nuevo al principio de la ventana.
    if S.question then
        ensureScheduler():enqueue(C.QUESTION_TYPE, S.question.json)
        S.question.nextAt = now + C.QUESTION_RESEND_SEC
    end
    return again
end

-- Pregunta del jugador: se encola ya (prioridad maxima) y se reenvia cada QUESTION_RESEND_SEC
-- mientras dure la ventana, hasta que llegue una pregunta nueva. json: el mensaje 15 ya codificado.
function C.SendQuestion(json, now)
    if type(json) ~= "string" then return false end
    S.question = { json = json, nextAt = (now or 0) + C.QUESTION_RESEND_SEC }
    ensureScheduler():enqueue(C.QUESTION_TYPE, json)
    return true
end

-- Ultima pregunta enviada (JSON) o nil.
function C.LastQuestion()
    return S.question and S.question.json or nil
end

-- Vuelve al reposo. Los trozos ya encolados salen en la siguiente activacion (no se pierden).
function C.Stop()
    S.share = nil
    S.cur = nil
end

-- true si hay una activacion en curso.
function C.IsSharing()
    return S.share ~= nil
end

-- Segundos que quedan de la ventana (entero hacia arriba, 0..255). 0 en reposo.
function C.RemainingSec(now)
    local sh = S.share
    if not sh then return 0 end
    local r = ceil(sh.endAt - (now or sh.now))
    return max(0, min(255, r))
end

-- Llamar en cada trama durante la activacion. Devuelve false si la ventana ha acabado (y para).
-- Acaba a los SHARE_SECONDS, o antes cuando no queda nada por enviar (plan, cola de la bandeja y
-- pregunta) y han pasado al menos SHARE_MIN_SEC.
function C.Update(now)
    local sh = S.share
    if not sh then return false end
    sh.now = now
    if now >= sh.endAt - 1e-9 then
        C.Stop()
        return false
    end
    local q = S.question
    if q and now >= q.nextAt - 1e-9 then
        ensureScheduler():enqueue(C.QUESTION_TYPE, q.json)
        q.nextAt = now + C.QUESTION_RESEND_SEC
    end
    local idle = sh.planDone and not S.cur and (S.sched == nil or S.sched:requiredPending() == 0)
    if idle and now - sh.startAt >= C.SHARE_MIN_SEC - 1e-9 then
        C.Stop()
        return false
    end
    return true
end

-- Reparte la serializacion del plan en la trama actual: como mucho budgetMs (por defecto
-- PUMP_BUDGET_MS). Siempre hace al menos un paso si hay trabajo. Sin activacion, no hace nada.
function C.Pump(now, budgetMs)
    local sh = S.share
    if not sh then return false end
    sh.now = now
    local deadline = clockMs() + (budgetMs or C.PUMP_BUDGET_MS)
    while true do
        local progressed = false
        if sh.pos <= #sh.plan then
            progressed = planStep(sh)
        elseif not sh.planDone then
            sh.planDone = true
            sh.planDoneAt = now
        elseif S.cur then -- la unidad en curso acaba de salir del plan
            resumeUnit()
            progressed = true
        end
        if not progressed or clockMs() >= deadline then break end
    end
    return true
end

-- Encola ahora el tipo t (la hoja de personaje, al escribirla) si hay activacion: va al frente del
-- plan, tras la unidad en curso. En reposo no serializa nada y devuelve false.
function C.Send(t)
    local sh = S.share
    if not sh then return false end
    if sh.plan[sh.pos] == t and S.cur then return true end
    for i = #sh.plan, sh.pos, -1 do
        if sh.plan[i] == t then tremove(sh.plan, i) end
    end
    local at = sh.pos + (S.cur and 1 or 0)
    if at > #sh.plan + 1 then at = #sh.plan + 1 end
    tinsert(sh.plan, at, t)
    return true
end

-- Siguiente trozo para la trama (N bloques). nil si no hay activacion o no hay nada que enviar.
function C.NextChunk(N, frameNo)
    if not S.share or not S.sched then return nil end
    return S.sched:nextChunk(C.Capacity(N), frameNo)
end

-- Capturas (Shot.lua): ¿ya se ha serializado todo el plan de la activacion? (sin trozos en curso)
function C.PlanDone()
    local sh = S.share
    return sh ~= nil and sh.planDone and S.cur == nil
end

-- Capturas: longitudes (bytes) de lo pendiente en la cola (items codificados al consultar).
function C.PendingLengths()
    if not S.sched then return {} end
    return S.sched:lengths()
end

-- Capturas: trabajos pendientes en la cola (0 = nada que enviar).
function C.Pending()
    return S.sched and S.sched:pending() or 0
end

-- Capturas: siguiente trozo con capacidad cs (bytes por trama, la del formato de captura).
function C.NextChunkCapacity(cs, frameNo)
    if not S.share or not S.sched then return nil end
    return S.sched:nextChunk(cs, frameNo)
end

-- Quita la pregunta pendiente (si la activacion no pudo empezar).
function C.DropQuestion()
    S.question = nil
    if S.sched then
        for i = #S.sched.jobs, 1, -1 do
            if S.sched.jobs[i].type == C.QUESTION_TYPE and not S.sched.jobs[i].seq then
                tremove(S.sched.jobs, i)
            end
        end
    end
end

-- Lineas para /compa estado.
function C.StatusLines()
    local queued = S.sched and S.sched:pending() or 0
    local lines = { string.format("Canal en vivo: %d mensaje(s) en cola, %d mensaje(s) serializados, %d sin cambios",
        queued, S.sentCount, S.skipped) }
    if S.errors > 0 then
        lines[#lines + 1] = string.format("Canal en vivo: %d error(es) al codificar", S.errors)
    end
    return lines
end

-- Compatibilidad: la hoja de personaje y otros tipos "manuales" se llaman con Send.
C.HashText = ns.HashText

-- Internos expuestos solo para tools/lua/test_channel.lua.
C.testing = {
    fullJSON = fullJSON,
    -- Reinicia el estado del canal (pruebas).
    reset = function()
        S.share, S.cur, S.sched, S.win = nil, nil, nil, nil
        S.queued = {}
        S.sent, S.sentAt, S.skipped, S.question = {}, {}, 0, nil
        S.sentCount, S.errors, S.lastError = 0, 0, nil
    end,
    state = S,
}

-- Tope de cada mensaje JSON (Json.lua usa 64 KB por defecto).
if ns.Json then ns.Json.MAX_BYTES = C.MAX_MESSAGE_BYTES end
