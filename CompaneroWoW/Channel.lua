-- Channel.lua: canal de datos en vivo sobre la franja superior (protocolo v3), bajo demanda.
-- Parte pura (testeable offline): CRC-16, capacidad, trama y planificador de trozos.
-- Parte de juego: el plan de envio de una activacion (tecla o /compa enviar). En reposo no hay
-- nada que serializar: ni carrusel, ni revision de conocimiento, ni JSON.
--
-- Trama v3 (bytes, 1-based; todo big-endian salvo donde se indica):
--   [1] version=3  [2] seq  [3] N lo  [4] N hi            (N = bloques de la fila, u16 LE)
--   [5..36]  estado (32 bytes, mismo layout y CRC-8 que v2; byte 30 = shareRemainingSec)
--   [37] msgType  [38..39] msgId (LE)  [40..41] chunkIndex (LE)  [42..43] chunkCount (LE)
--   [44..45] dataLen (LE)
--   [46..45+dataLen] datos del trozo
--   [46+dataLen, 47+dataLen] CRC-16/CCITT-FALSE de los bytes 1..45+dataLen (big-endian)
-- Sin trozo pendiente: msgType = 0 y dataLen = 0. Los nibbles sobrantes valen 0.
-- Capacidad de datos por trama: floor(3*(N-5)/2) - 47 bytes (N = 860 -> 1235 bytes).
--
-- Activacion (C.Begin): ventana de SHARE_SECONDS. El plan recorre C.PRIORITY en orden; cada tipo se
-- serializa una vez por activacion, de forma troceada: C.Pump reparte el trabajo en tramas con un
-- presupuesto de PUMP_BUDGET_MS por trama. Los trozos salen por el planificador (C.NextChunk).
-- Solo se encola lo que cambio: por tipo se guarda el hash del ultimo envio (S.sent). Los tipos clave
-- (KEY_TYPES) y el volcado de conocimiento se reenvian si el ultimo envio tiene mas de KEY_RESEND_SEC.
-- C.Begin(now, { full = true }) (/compa sync) envia todo sin saltarse nada.
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

C.VERSION = 3
C.STATUS_LEN = 32
C.CHUNK_HEADER_LEN = 9
C.OVERHEAD = 4 + 32 + 9 + 2 -- cabecera + estado + cabecera de trozo + CRC-16
C.CALIB_BLOCKS = 5
C.SHARE_SECONDS = 20        -- duracion maxima de una activacion (se alarga si se vuelve a pulsar)
C.SHARE_MIN_SEC = 3         -- minimo de una activacion: no acaba antes aunque la cola este vacia
C.KEY_RESEND_SEC = 600      -- tipos clave y volcado de conocimiento: se reenvian si pasan 10 min
C.QUESTION_TYPE = 15        -- pregunta del jugador (contrato in-game): msgType 15, prioridad maxima
C.QUESTION_RESEND_SEC = 2   -- la pregunta se reenvia cada 2 s durante la ventana
C.PUMP_BUDGET_MS = 1        -- presupuesto de serializacion por trama
C.HEAVY_TYPE = 10           -- tipo pesado: como mucho un trozo cada HEAVY_EVERY tramas
C.HEAVY_EVERY = 3
C.MAX_MESSAGE_BYTES = 98304 -- tope de cada mensaje JSON (mas grande -> no se envia)
C.TICK_SEC = 0.2            -- una trama cada 0.2 s (Bridge.lua REFRESH_SEC)
C.SCAN_PER_STEP = 16        -- registros de knowledge que se codifican por paso de revision
C.KNOW_SLICE_BYTES = 16384  -- tamano objetivo de cada mensaje de knowledge (tipo 9)

-- Orden de envio de una activacion (de mayor a menor prioridad en el planificador).
-- Personaje, hoja, misiones, equipo, sesion, bolsas, profesiones, macros, barras, combate,
-- misiones completadas, conocimiento (deltas y volcado), metadatos, el pesado (10) y, al final,
-- el mundo (16: solo los bloques cambiados, World.lua).
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
-- Cada mensaje (tipo) tiene como mucho un trabajo activo; encolar de nuevo el mismo tipo
-- reemplaza el trabajo (el mensaje nuevo gana y el receptor descarta el parcial).
-- Un trabajo: { type, id, str (JSON), cs (bytes por trozo), count, next (1-based), heavy, optional }.
-- optional: volcado de conocimiento que solo sale si hay tiempo (no cuenta para acabar antes).

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

-- Encola el mensaje msgType con el texto JSON str. Reemplaza un trabajo pendiente del mismo tipo.
-- heavy: el mensaje va por el carril lento (un trozo de cada HEAVY_EVERY tramas). El tipo pesado siempre.
function Sched:enqueue(msgType, str, heavy, optional)
    for i, job in ipairs(self.jobs) do
        if job.type == msgType then
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

function Sched:pending()
    return #self.jobs
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

-- Trozo siguiente segun la prioridad y el turno. cs = capacidad de datos de la trama.
-- Devuelve el trozo { type, id, index (0-based), count, data } o nil si no hay nada que enviar
-- en esta trama. frameNo sirve para limitar los tipos pesados.
function Sched:nextChunk(cs, frameNo)
    if cs <= 0 or #self.jobs == 0 then return nil end
    local bestI, bestRank = nil, math.huge
    for i, job in ipairs(self.jobs) do
        local r = RANK[job.type] or 99
        local heavyOff = job.heavy and frameNo % C.HEAVY_EVERY ~= 0
        if r < bestRank and not heavyOff then
            bestI, bestRank = i, r
        end
    end
    if not bestI then return nil end
    local job = tremove(self.jobs, bestI)
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
        self.jobs[#self.jobs + 1] = job -- al final: turno rotatorio entre mensajes
    end
    return chunk
end

-- true si hay un trabajo pendiente del tipo t.
function Sched:has(msgType)
    for _, job in ipairs(self.jobs) do
        if job.type == msgType then return true end
    end
    return false
end

-- ---------------------------------------------------------------------------
-- Tipos de mensaje (lado de juego)
-- ---------------------------------------------------------------------------
-- build(c, db) devuelve la tabla a enviar, o nil si no hay nada. c = CompaneroWoWDB.characters[key].
-- Cada tipo se serializa una vez por activacion, en el momento de su turno del plan.

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
    [7] = {
        name = "combat",
        build = function(c)
            -- combat: el ultimo combate; combats: los ultimos 10 (Combat.lua guarda como mucho 10)
            local list = c.combats
            if type(list) ~= "table" or type(list[1]) ~= "table" then return nil end
            return { combat = list[1], combats = list }
        end,
    },
    [8] = {
        name = "actionBars",
        build = function(c)
            return { actionBars = c.actionBars }
        end,
    },
    [9] = {
        -- conocimiento: revision de cambios y volcado por tandas (ver "Conocimiento" abajo).
        -- Cada mensaje del tipo 9 es {"npcs":{id:registro},"quests":{id:registro}}; la app fusiona por id.
        name = "knowledge",
        pipeline = true,
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
    [16] = {
        -- mundo (World.lua): solo los bloques cambiados desde el ultimo envio. Devuelve el JSON ya
        -- compuesto (raw): no pasa por el codificador. nil si no hay nada que enviar.
        name = "world", raw = true, account = true, -- cuenta, no de personaje
        build = function()
            local W = ns.World
            if not W or not W.ShareMessage then return nil end
            return W.ShareMessage()
        end,
    },
}

-- ---------------------------------------------------------------------------
-- Estado del canal
-- ---------------------------------------------------------------------------
local function newQueue()
    return { items = {}, head = 1, set = { quests = {}, npcs = {} } }
end

local S = {
    sched = nil,          -- planificador (sobrevive a la activacion: los trozos pendientes salen en la siguiente)
    share = nil,          -- activacion en curso (nil = reposo)
    cur = nil,            -- unidad de serializacion en curso { t, co }
    cache = { quests = {}, npcs = {} }, -- conocimiento: [tipo][id] = JSON del registro en el ultimo envio
    know = { scan = nil, out = newQueue(), snap = nil, snapDone = false, dumpAt = nil },
    sent = {},            -- [msgType] = hash del ultimo mensaje encolado (solo se reenvia si cambia)
    sentAt = {},          -- [msgType] = GetTime del ultimo encolado de ese tipo
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
-- Unidades de serializacion (una por tipo; se reanudan trama a trama)
-- ---------------------------------------------------------------------------

-- Cuerpo de la unidad: construye la tabla del tipo, cede, y la codifica cediendo cada pocos nodos.
-- Devuelve el JSON, o nil (sin datos) o nil + motivo (no se pudo codificar). Se ejecuta en una coroutine.
local function unitBody(t)
    local def = C.TYPES[t]
    local c = currentChar()
    if not def or not def.build or (not c and def.account ~= true) then return nil end
    -- Mundo: no se construye otro mensaje mientras sale el anterior (enqueue lo reemplazaria y
    -- los bloques marcados como enviados se perderian). Lo que queda se envia en la siguiente.
    if def.raw and ensureScheduler():has(t) then return nil end
    local value = def.build(c, CompaneroWoWDB)
    if value == nil then return nil end
    if def.raw then return value end -- ya es JSON (mundo)
    if t == 6 and type(value) == "table" and next(value) == nil then return "[]" end
    coroutine.yield()
    return ns.Json.EncodeAsync(value)
end

local function startUnit(t)
    S.cur = { t = t, co = cocreate(function() return unitBody(t) end) }
end

-- Reanuda la unidad en curso. "yield" si sigue; "done" si acabo (encola su JSON si lo hay).
-- Un tipo ya enviado con el mismo hash se omite. Completo (/compa sync): nunca se omite. Tipo clave:
-- se reenvia si el ultimo envio tiene KEY_RESEND_SEC o mas.
local function unchanged(t, h, now, full)
    if full or S.sent[t] ~= h then return false end
    if C.KEY_TYPES[t] and now - (S.sentAt[t] or now) >= C.KEY_RESEND_SEC then return false end
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
-- Conocimiento (tipo 9): revision de cambios y volcado en tandas
-- ---------------------------------------------------------------------------
-- Una activacion revisa los registros (scanStep, SCAN_PER_STEP por paso): encola solo los nuevos o
-- cambiados (deltas) y los envia en mensajes de como mucho KNOW_SLICE_BYTES. Despues, si queda tiempo
-- en la ventana, sale el volcado completo desde la cache (optativo). El protocolo no tiene borrados:
-- un registro que sale de knowledge se quita de la cache y la app conserva su ultima copia.
local KNOW_KINDS = { "quests", "npcs" }

local function knowTables()
    local k = CompaneroWoWDB and CompaneroWoWDB.knowledge
    if type(k) ~= "table" then return nil end
    return k
end

-- Encola un delta (kind, id). Una sola vez hasta que sale: al enviar se lee la cache (lo mas nuevo).
local function queueDelta(kind, id)
    local out = S.know.out
    if out.set[kind][id] then return end
    out.set[kind][id] = true
    out.items[#out.items + 1] = { kind = kind, id = id }
end

-- Cierra la revision: quita de la cache lo que ya no esta en knowledge.
local function finishScan(k)
    for _, kind in ipairs(KNOW_KINDS) do
        local map = type(k[kind]) == "table" and k[kind] or {}
        for id in pairs(S.cache[kind]) do
            if map[id] == nil then S.cache[kind][id] = nil end
        end
    end
    S.know.scan = nil
end

-- Un paso de revision: como mucho SCAN_PER_STEP registros codificados.
local function scanStep()
    local scan = S.know.scan
    if not scan then return end
    local k = knowTables()
    if not k then
        S.know.scan = nil
        return
    end
    local budget = C.SCAN_PER_STEP
    while budget > 0 do
        local kind = KNOW_KINDS[scan.ki]
        if not kind then
            finishScan(k)
            return
        end
        local map = type(k[kind]) == "table" and k[kind] or {}
        if scan.keys == nil then
            scan.keys = {}
            for id in pairs(map) do scan.keys[#scan.keys + 1] = id end
            scan.pos = 0
        end
        if scan.pos >= #scan.keys then
            scan.ki, scan.keys = scan.ki + 1, nil
        else
            scan.pos = scan.pos + 1
            budget = budget - 1
            local id = scan.keys[scan.pos]
            local rec = map[id]
            if type(rec) == "table" then
                local json = ns.Json.Encode(rec)
                if json and json ~= S.cache[kind][id] then
                    S.cache[kind][id] = json
                    queueDelta(kind, id)
                end
            end
        end
    end
end

-- Saca de la cola los registros que caben en un mensaje y devuelve su JSON, o nil si la cola esta vacia.
-- Lee la cache al sacar cada registro (el valor mas nuevo). Los que ya no estan en la cache se descartan.
local function takeSlice(q)
    local parts = { npcs = {}, quests = {} }
    local size, any = 2, false
    while q.head <= #q.items do
        local it = q.items[q.head]
        local json = S.cache[it.kind][it.id]
        if json then
            local cost = #json + #tostring(it.id) + 6
            if any and size + cost > C.KNOW_SLICE_BYTES then break end
            local list = parts[it.kind]
            list[#list + 1] = ns.Json.Quote(tostring(it.id)) .. ":" .. json
            size = size + cost
            any = true
        end
        q.head = q.head + 1
        if q.set then q.set[it.kind][it.id] = nil end
    end
    if q.head > #q.items then
        q.items, q.head = {}, 1
    end
    if not any then return nil end
    return '{"npcs":{' .. concat(parts.npcs, ",") .. '},"quests":{' .. concat(parts.quests, ",") .. '}}'
end

-- Paso del tipo 9 del plan. Devuelve progreso (bool) y si el tipo ha acabado.
local function knowledgeStep(sh)
    if sh.know == "scan" then
        if not knowTables() then
            sh.know = "deltas"
            return true, false
        end
        if not S.know.scan then S.know.scan = { ki = 1, keys = nil, pos = 0 } end
        scanStep()
        if not S.know.scan then sh.know = "deltas" end
        return true, false
    end
    -- deltas: un mensaje cada vez, cuando el tipo 9 esta libre en el planificador
    if ensureScheduler():has(9) then return false, false end
    local msg = takeSlice(S.know.out)
    if msg then
        ensureScheduler():enqueue(9, msg)
        return true, false
    end
    return true, true
end

-- Volcado completo optativo (fuera del plan, si queda ventana y el tipo 9 esta libre).
-- Solo en /compa sync o si el ultimo volcado tiene KEY_RESEND_SEC o mas.
local function dumpDue(sh)
    if sh.full then return true end
    return S.know.dumpAt == nil or sh.now - S.know.dumpAt >= C.KEY_RESEND_SEC
end

local function snapshotStep()
    local know = S.know
    if know.snapDone then return false end
    if S.sched and S.sched:has(9) then return false end
    if not know.snap then
        if not dumpDue(S.share) then
            know.snapDone = true
            return false
        end
        know.dumpAt = S.share.now
        local items = {}
        for _, kind in ipairs(KNOW_KINDS) do
            for id in pairs(S.cache[kind]) do items[#items + 1] = { kind = kind, id = id } end
        end
        know.snap = { items = items, head = 1 }
        return true
    end
    local msg = takeSlice(know.snap)
    if msg then
        ensureScheduler():enqueue(9, msg, true, true)
        return true
    end
    know.snapDone = true
    return false
end

-- ---------------------------------------------------------------------------
-- Activacion: plan, presupuesto por trama y ventana
-- ---------------------------------------------------------------------------

-- Un paso del plan (una reanudacion de unidad, o un tramo de revision). Devuelve progreso (bool).
local function planStep(sh)
    local t = sh.plan[sh.pos]
    if C.TYPES[t] and C.TYPES[t].pipeline then
        sh.know = sh.know or "scan"
        local progressed, finished = knowledgeStep(sh)
        if finished then
            sh.pos = sh.pos + 1
            sh.know = nil
        end
        return progressed
    end
    if not S.cur then
        startUnit(t)
    elseif resumeUnit() == "done" then
        sh.pos = sh.pos + 1
    end
    return true
end

-- Sobrante del mundo: si tras el plan quedan bloques sin enviar (presupuesto por mensaje) y no hay
-- otro mensaje 16 en cola, serializa otro trozo. Es trabajo obligatorio: la ventana no acaba hasta
-- que sale, como mucho hasta SHARE_SECONDS.
local function worldExtraStep()
    local W = ns.World
    if not (W and W.ChannelPending and W.ChannelPending()) then return false end
    if S.cur or ensureScheduler():has(16) then return false end
    startUnit(16)
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
    for i, t in ipairs(C.PRIORITY) do S.share.plan[i] = t end
    S.cur = nil
    S.know.scan = nil
    S.know.snap = nil
    S.know.snapDone = false
    -- Si hay una pregunta, sale de nuevo al principio de la ventana.
    if S.question then
        ensureScheduler():enqueue(C.QUESTION_TYPE, S.question.json)
        S.question.nextAt = now + C.QUESTION_RESEND_SEC
    end
    -- /compa sync: el mundo se reenvia entero (World.lua olvida lo enviado).
    if S.share.full and ns.World and ns.World.ForceFull then ns.World.ForceFull() end
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
    S.know.scan = nil
    S.know.snap = nil
    S.know.snapDone = false
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
-- Acaba a los SHARE_SECONDS, o antes cuando no queda nada por enviar (plan, cola, volcado y
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
    local dumping = S.know.snap ~= nil and not S.know.snapDone
    local idle = sh.planDone and not S.cur and (S.sched == nil or S.sched:requiredPending() == 0)
        and S.know.out.head > #S.know.out.items and not dumping
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
        local progressed
        if sh.pos <= #sh.plan then
            progressed = planStep(sh)
        else
            if not sh.planDone then
                sh.planDone = true
                sh.planDoneAt = now
            end
            if S.cur then -- sobrante del mundo en curso (el plan ya acabo)
                resumeUnit()
                progressed = true
            else
                progressed = snapshotStep() or worldExtraStep()
            end
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
    -- Revision de knowledge completa de una vez (la de juego va en pasos dentro del plan).
    scanAll = function() while S.know.scan do scanStep() end end,
    -- Reinicia el estado del canal (pruebas).
    reset = function()
        S.share, S.cur, S.sched = nil, nil, nil
        S.cache = { quests = {}, npcs = {} }
        S.know = { scan = nil, out = newQueue(), snap = nil, snapDone = false, dumpAt = nil }
        S.sent, S.sentAt, S.skipped, S.question = {}, {}, 0, nil
        S.sentCount, S.errors, S.lastError = 0, 0, nil
    end,
    state = S,
}

-- Tope de cada mensaje JSON (Json.lua usa 64 KB por defecto).
if ns.Json then ns.Json.MAX_BYTES = C.MAX_MESSAGE_BYTES end
