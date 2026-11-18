-- Channel.lua: canal de datos en vivo sobre la franja superior (protocolo v3).
-- Parte pura (testeable offline): CRC-16, capacidad, trama, planificador de trozos y carrusel.
-- Parte de juego: tipos de mensaje (leen la copia en memoria de CompaneroWoWDB: lo mismo que se
-- guardaria en SavedVariables, pero al dia), deteccion de cambios por hash y eventos.
--
-- Trama v3 (bytes, 1-based; todo big-endian salvo donde se indica):
--   [1] version=3  [2] seq  [3] N lo  [4] N hi            (N = bloques de la fila, u16 LE)
--   [5..36]  estado (32 bytes, mismo layout y CRC-8 que v2)
--   [37] msgType  [38..39] msgId (LE)  [40..41] chunkIndex (LE)  [42..43] chunkCount (LE)
--   [44..45] dataLen (LE)
--   [46..45+dataLen] datos del trozo
--   [46+dataLen, 47+dataLen] CRC-16/CCITT-FALSE de los bytes 1..45+dataLen (big-endian)
-- Sin trozo pendiente: msgType = 0 y dataLen = 0. Los nibbles sobrantes valen 0.
-- Capacidad de datos por trama: floor(3*(N-5)/2) - 47 bytes (N = 860 -> 1235 bytes).
local ADDON_NAME, ns = ...
ns = ns or {}

local bit = bit
local band, bxor, lshift, rshift = bit.band, bit.bxor, bit.lshift, bit.rshift
local floor, min, max, ceil = math.floor, math.min, math.max, math.ceil
local byte, concat = string.byte, table.concat
local type, pairs, ipairs, tostring = type, pairs, ipairs, tostring

local C = {}
ns.Channel = C

C.VERSION = 3
C.STATUS_LEN = 32
C.CHUNK_HEADER_LEN = 9
C.OVERHEAD = 4 + 32 + 9 + 2 -- cabecera + estado + cabecera de trozo + CRC-16
C.CALIB_BLOCKS = 5
C.CAROUSEL_SEC = 15         -- cada mensaje se reenvia como mucho cada 15 s (contrato ON-DEMAND)
C.HEAVY_CAROUSEL_SEC = 60   -- el tipo pesado (10) puede quedarse en 60 s
C.HEAVY_TYPE = 10           -- tipo pesado: como mucho un trozo cada HEAVY_EVERY tramas
C.HEAVY_EVERY = 3
C.MAX_MESSAGE_BYTES = 98304 -- tope de cada mensaje JSON (mas grande -> no se envia)

-- Prioridad de envio de los tipos (de mayor a menor).
-- Contrato ON-DEMAND: status (siempre, en cada trama), personaje, hoja, misiones, equipo, sesion,
-- bolsas, profesiones, y el resto. Se mantiene siempre (no hay "reactivacion" que el addon pueda ver).
C.PRIORITY = { 1, 12, 2, 4, 14, 3, 5, 7, 8, 6, 9, 11, 10 }
local RANK = {}
for i, t in ipairs(C.PRIORITY) do RANK[t] = i end
C.RANK = RANK

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
-- Un trabajo: { type, id, str (JSON), cs (bytes por trozo), count, next (1-based) }.

local Sched = {}
Sched.__index = Sched

function C.NewScheduler()
    return setmetatable({
        jobs = {},        -- trabajos pendientes, en orden de turno
        nextMsgId = 0,    -- msgId 1..65535 (envuelve)
        carouselAt = nil, -- ultimo ciclo de carrusel (segundos); nil = nunca
        lastSent = {},    -- [tipo] = segundos en que se encolo por ultima vez en el carrusel
    }, Sched)
end

local function newMsgId(s)
    s.nextMsgId = s.nextMsgId % 65535 + 1
    return s.nextMsgId
end

-- Encola el mensaje msgType con el texto JSON str. Reemplaza un trabajo pendiente del mismo tipo.
function Sched:enqueue(msgType, str)
    for i, job in ipairs(self.jobs) do
        if job.type == msgType then
            table.remove(self.jobs, i)
            break
        end
    end
    self.jobs[#self.jobs + 1] = { type = msgType, id = newMsgId(self), str = str, cs = 0, count = 0, next = 1 }
end

function Sched:pending()
    return #self.jobs
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
        local heavyOff = job.type == C.HEAVY_TYPE and frameNo % C.HEAVY_EVERY ~= 0
        if r < bestRank and not heavyOff then
            bestI, bestRank = i, r
        end
    end
    if not bestI then return nil end
    local job = table.remove(self.jobs, bestI)
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

-- Periodo de reenvio de un tipo en el carrusel: 15 s; el pesado, 60 s.
local function carouselPeriod(t)
    local def = C.TYPES[t]
    return (def and def.heavy) and C.HEAVY_CAROUSEL_SEC or C.CAROUSEL_SEC
end

-- true si hay un trabajo pendiente del tipo t.
function Sched:has(msgType)
    for _, job in ipairs(self.jobs) do
        if job.type == msgType then return true end
    end
    return false
end

-- true si al tipo t le toca reenviarse (nunca enviado, o hace al menos su periodo).
function Sched:carouselDue(t, now)
    local last = self.lastSent[t]
    return last == nil or now - last >= carouselPeriod(t) - 1e-6
end

-- Turno de ciclo: si ha pasado CAROUSEL_SEC desde el ultimo ciclo, lo abre. La fase es fija
-- (carouselAt + CAROUSEL_SEC), asi los ciclos caen cada 15 s exactos y el pesado cada 4 ciclos (60 s);
-- solo si se ha retrasado mas de un periodo empieza ahora. true si hay ciclo nuevo.
local function carouselTurn(sched, now)
    if sched.carouselAt == nil then
        sched.carouselAt = now
        return true
    end
    local elapsed = now - sched.carouselAt
    if elapsed < C.CAROUSEL_SEC - 1e-6 then return false end
    if elapsed < 2 * C.CAROUSEL_SEC then
        sched.carouselAt = sched.carouselAt + C.CAROUSEL_SEC
    else
        sched.carouselAt = now
    end
    return true
end

-- Carrusel: cada CAROUSEL_SEC reencola cada tipo que provider(tipo) devuelve (texto JSON completo)
-- y que no tenga ya un trabajo pendiente (un mensaje a medio enviar no se reinicia). provider puede
-- devolver nil (no reenviar). Devuelve true si ha empezado un ciclo.
function Sched:carousel(now, provider)
    if not carouselTurn(self, now) then return false end
    for _, t in ipairs(C.PRIORITY) do
        if not self:has(t) and self:carouselDue(t, self.carouselAt) then
            local str = provider(t)
            if str then
                self:enqueue(t, str)
                self.lastSent[t] = self.carouselAt
            end
        end
    end
    return true
end

-- Fuerza el siguiente carrusel con todos los tipos (lo pide /compa sync).
function Sched:forceCarousel()
    self.carouselAt = nil
    self.lastSent = {}
end

-- ---------------------------------------------------------------------------
-- Tipos de mensaje (lado de juego)
-- ---------------------------------------------------------------------------
-- build(c, db) devuelve la tabla a enviar, o nil si no hay nada. c = CompaneroWoWDB.characters[key].
-- events: eventos que marcan el tipo como cambiado. period: revision periodica (s).
-- minGap: separacion minima entre dos calculos (s). delta: el tipo tiene modo delta propio.

local function sectionOf(c, key)
    return c and c[key]
end

C.TYPES = {
    [1] = {
        name = "character", period = 15, minGap = 1,
        events = { "PLAYER_LEVEL_UP", "UPDATE_FACTION", "GROUP_ROSTER_UPDATE", "PLAYER_SPECIALIZATION_CHANGED", "TRAIT_CONFIG_UPDATED" },
        build = function(c)
            return { character = c.character, reputations = c.reputations, talents = c.talents }
        end,
    },
    [2] = {
        name = "quests", period = 15, minGap = 1,
        events = { "QUEST_LOG_UPDATE", "QUEST_ACCEPTED", "QUEST_REMOVED", "QUEST_TURNED_IN", "ZONE_CHANGED_NEW_AREA" },
        build = function(c)
            -- native: misiones del juego y sus fuentes (Native.lua), en el mismo bloque que las misiones
            return { quests = c.quests, location = c.location, native = c.native }
        end,
    },
    [3] = {
        name = "bags", period = 15, minGap = 1,
        events = { "BAG_UPDATE_DELAYED" },
        build = function(c)
            return { inventory = c.inventory }
        end,
    },
    [4] = {
        name = "equipment", period = 15, minGap = 1,
        events = { "PLAYER_EQUIPMENT_CHANGED" },
        build = function(c)
            return { equipment = c.equipment, gear = c.gear, proficiencies = c.proficiencies }
        end,
    },
    [5] = {
        name = "professions", period = 15, minGap = 1,
        events = { "SKILL_LINES_CHANGED" },
        build = function(c)
            return { professions = c.professions, training = c.training, profPlan = c.profPlan }
        end,
    },
    [6] = {
        name = "completedQuests", period = 30, minGap = 1, delta = true,
        events = { "QUEST_TURNED_IN", "QUEST_LOG_UPDATE" },
        build = function(c)
            return c.completedQuests
        end,
    },
    [7] = {
        name = "combat", period = 15, minGap = 1,
        events = { "PLAYER_REGEN_ENABLED" },
        build = function(c)
            local list = c.combats
            if type(list) ~= "table" or type(list[1]) ~= "table" then return nil end
            return { combat = list[1] }
        end,
    },
    [8] = {
        name = "actionBars", period = 30, minGap = 1,
        events = { "ACTIONBAR_SLOT_CHANGED", "PLAYER_REGEN_ENABLED", "ACTIONBAR_PAGE_CHANGED" },
        build = function(c)
            return { actionBars = c.actionBars }
        end,
    },
    [9] = {
        name = "knowledge", period = 30, minGap = 2, delta = true,
        events = {},
        build = function(_, db)
            local k = db and db.knowledge
            if type(k) ~= "table" then return nil end
            return { quests = k.quests, npcs = k.npcs }
        end,
    },
    [10] = {
        name = "heavy", period = 30, minGap = 5, heavy = true,
        events = { "ZONE_CHANGED_NEW_AREA", "PLAYER_REGEN_ENABLED" },
        build = function(c)
            return { questie = c.questie, questWindow = c.questWindow, gearCandidates = c.gearCandidates, dungeons = c.dungeons }
        end,
    },
    [12] = {
        -- hoja de personaje (Sheet.lua). manual: no lo recalcula el planificador (ni consume turnos);
        -- Sheet.lua lo envia con C.Send(12) al escribir characters[key].sheet. El carrusel si lo reenvia.
        name = "sheet", manual = true, period = 60, minGap = 1,
        events = {},
        build = function(c)
            local s = c.sheet
            if type(s) ~= "table" then return nil end
            return s
        end,
    },
    [14] = {
        -- sesion del personaje (Session.lua): historial de la sesion en curso (contrato ON-DEMAND).
        -- minGap largo: la sesion cambia con cada XP y no conviene reiniciar el envio a cada cambio.
        name = "session", period = 15, minGap = 5,
        events = { "PLAYER_LEVEL_UP", "PLAYER_XP_UPDATE", "PLAYER_MONEY", "QUEST_TURNED_IN", "QUEST_ACCEPTED",
                   "PARTY_KILL", "PLAYER_DEAD", "ZONE_CHANGED_NEW_AREA" },
        build = function(c)
            if not (ns.Session and ns.Session.Public) then return nil end
            return ns.Session.Public(c.session)
        end,
    },
    [11] = {
        name = "meta", period = 60, minGap = 5,
        events = { "PLAYER_ENTERING_WORLD" },
        build = function(c)
            local health = { luaErrors = ns.errorCount or 0, lastError = ns.lastError }
            return { version = ns.VERSION, schema = ns.SCHEMA, updatedAt = c.updatedAt, health = health, channel = C.VERSION }
        end,
    },
}

-- Hash de 32 bits (FNV-like, determinista) de un texto, con su longitud.
local function hashText(s)
    local h = 5381
    for i = 1, #s do
        h = (h * 33 + byte(s, i)) % 4294967296
    end
    return h .. ":" .. #s
end
C.HashText = hashText

-- ---------------------------------------------------------------------------
-- Estado del canal (glue)
-- ---------------------------------------------------------------------------
local S = {
    sched = nil,
    frame = 0,
    hash = {},        -- [tipo] = hash del ultimo JSON enviado (o calculado)
    lastBuilt = {},   -- [tipo] = segundos del ultimo calculo
    lastCheck = {},   -- [tipo] = segundos de la ultima revision periodica
    dirty = {},       -- [tipo] = true si un evento pide revisar
    completedSet = nil,  -- [questID] = true del ultimo envio de completedQuests
    knowHash = { quests = {}, npcs = {} },
    sentCount = 0,    -- mensajes encolados (estadistica para /compa estado)
    lastSentAt = nil,
    errors = 0,
}
C.state = S

local function ensureScheduler()
    if not S.sched then S.sched = C.NewScheduler() end
    return S.sched
end

local function markAll()
    for t, def in pairs(C.TYPES) do
        if not def.manual then S.dirty[t] = true end
    end
end

local function markTypes(list)
    for _, t in ipairs(list) do S.dirty[t] = true end
end

-- Eventos -> tipos a revisar. PLAYER_REGEN_ENABLED revisa todo al salir de combate.
local EVENT_TYPES = {}
for t, def in pairs(C.TYPES) do
    for _, ev in ipairs(def.events) do
        EVENT_TYPES[ev] = EVENT_TYPES[ev] or {}
        EVENT_TYPES[ev][#EVENT_TYPES[ev] + 1] = t
    end
end
C.EVENT_TYPES = EVENT_TYPES

-- Clave del personaje actual o nil.
local function currentChar()
    local key = ns.CharKey and ns.CharKey()
    if not key or type(CompaneroWoWDB) ~= "table" then return nil end
    local c = CompaneroWoWDB.characters and CompaneroWoWDB.characters[key]
    if type(c) ~= "table" then return nil end
    return c
end

-- JSON completo de un tipo (carrusel). nil si no hay datos o no se puede codificar.
local function fullJSON(t)
    local def = C.TYPES[t]
    local c = currentChar()
    if not def or (not c and t ~= 9) then return nil end
    local value = def.build(c, CompaneroWoWDB)
    if value == nil then return nil end
    if t == 6 and type(value) == "table" and next(value) == nil then return "[]" end
    local json, why = ns.Json.Encode(value)
    if not json then
        S.errors = S.errors + 1
        S.lastError = why
        return nil
    end
    return json
end

-- Calcula el tipo t y encola si cambio. Mensajes "delta" (6 y 9) encolan solo lo nuevo.
local function refreshType(t, now)
    local def = C.TYPES[t]
    local c = currentChar()
    if not c and t ~= 9 then return end
    S.lastBuilt[t] = now
    if t == 6 then
        local list = c.completedQuests
        if type(list) ~= "table" then return end
        local json = (next(list) == nil) and "[]" or ns.Json.Encode(list) -- lista vacia: []
        if not json then return end
        local h = hashText(json)
        if h == S.hash[t] then return end
        S.hash[t] = h
        local newSet, add = {}, {}
        for _, id in ipairs(list) do
            newSet[id] = true
            if not (S.completedSet and S.completedSet[id]) then add[#add + 1] = id end
        end
        -- Delta {add} solo si nada desaparece; si algo sale de la lista, se envia la lista completa.
        local removed = false
        if S.completedSet then
            for id in pairs(S.completedSet) do
                if not newSet[id] then removed = true break end
            end
        end
        local firstSend = S.completedSet == nil
        S.completedSet = newSet
        if firstSend or removed then
            ensureScheduler():enqueue(6, json)
        elseif #add > 0 then
            ensureScheduler():enqueue(6, ns.Json.Encode({ add = add }))
        end
        return
    end
    if t == 9 then
        local k = CompaneroWoWDB and CompaneroWoWDB.knowledge
        if type(k) ~= "table" then return end
        local delta = { quests = {}, npcs = {} }
        local any = false
        for _, kind in ipairs({ "quests", "npcs" }) do
            local map = k[kind]
            if type(map) == "table" then
                local prev = S.knowHash[kind]
                for id, v in pairs(map) do
                    local json = ns.Json.Encode(v) or ""
                    local h = hashText(json)
                    if prev[id] ~= h then
                        prev[id] = h
                        delta[kind][id] = v
                        any = true
                    end
                end
            end
        end
        if any then
            local json = ns.Json.Encode(delta)
            if json then ensureScheduler():enqueue(9, json) end
        end
        return
    end
    local value = def.build(c, CompaneroWoWDB)
    if value == nil then return end
    local json = ns.Json.Encode(value)
    if not json then
        S.errors = S.errors + 1
        return
    end
    local h = hashText(json)
    if h == S.hash[t] then return end
    S.hash[t] = h
    ensureScheduler():enqueue(t, json)
    S.sentCount = S.sentCount + 1
end

-- Revisa como mucho un tipo por tick: el primero marcado (prioridad) o el que toca por periodo.
-- Nunca calcula en combate (los trozos ya encolados siguen saliendo).
local function refreshOne(now)
    local due
    for _, t in ipairs(C.PRIORITY) do
        local def = C.TYPES[t]
        local gap = now - (S.lastBuilt[t] or -math.huge)
        if not def.manual and gap >= def.minGap then
            if S.dirty[t] or now - (S.lastCheck[t] or -math.huge) >= def.period then
                due = t
                break
            end
        end
    end
    if not due then return end
    S.dirty[due] = nil
    S.lastCheck[due] = now
    refreshType(due, now)
end

-- Carrusel repartido: cada CAROUSEL_SEC se prepara la lista de tipos que toca reenviar (sin trabajo
-- pendiente y con su periodo cumplido), en orden de prioridad; cada tick serializa y encola uno solo,
-- para no pagar todo el JSON de golpe. Los tipos de mayor prioridad van primero en la lista.
local function carouselStep(now, sched)
    if not (S.carouselTodo and #S.carouselTodo > 0) then
        if not carouselTurn(sched, now) then return end
        S.carouselTodo = {}
        local c = currentChar()
        for _, t in ipairs(C.PRIORITY) do
            local def = C.TYPES[t]
            if not sched:has(t) and sched:carouselDue(t, sched.carouselAt) then
                -- Tipos manuales (la hoja) solo si tienen datos: no gastan turnos del carrusel en vacio.
                if not def.manual or (c and def.build(c, CompaneroWoWDB) ~= nil) then
                    S.carouselTodo[#S.carouselTodo + 1] = t
                    -- Se marca al empezar el ciclo (no al serializar): la cadencia de 15 s es entre ciclos.
                    sched.lastSent[t] = sched.carouselAt
                end
            end
        end
    end
    -- Un tipo por tick (el primero, en el mismo tick que abre el ciclo).
    local t = table.remove(S.carouselTodo, 1)
    if t and not sched:has(t) then -- si mientras tanto llego un cambio, ese trabajo ya lleva lo nuevo
        local json = fullJSON(t)
        if json then sched:enqueue(t, json) end
    end
end

-- Llamar en cada refresco de la franja (cada 0.2 s). N = bloques de la fila.
-- Devuelve el trozo a enviar en esta trama (o nil). Fuera de combate se revisan los cambios;
-- el carrusel corre tambien en combate (un tipo por tick), para cumplir el periodo de 15 s.
function C.Tick(now, N)
    local sched = ensureScheduler()
    S.frame = S.frame + 1
    local inCombat = ns.InCombat and ns.InCombat()
    if not inCombat then refreshOne(now) end
    carouselStep(now, sched)
    return sched:nextChunk(C.Capacity(N), S.frame)
end

-- Encola ahora el tipo manual t (lo usa Sheet.lua para el tipo 12) con su JSON actual.
-- false si no hay datos o no se puede codificar.
function C.Send(t)
    local json = fullJSON(t)
    if not json then return false end
    ensureScheduler():enqueue(t, json)
    S.sentCount = S.sentCount + 1
    return true
end

-- Pide reenviar todo (lo llama /compa sync, antes de recargar).
function C.ForceCarousel()
    markAll()
    S.carouselTodo = nil
    ensureScheduler():forceCarousel()
end

-- Lineas para /compa estado.
function C.StatusLines()
    local sched = S.sched
    local queued = sched and sched:pending() or 0
    local lines = { string.format("Canal en vivo: %d mensaje(s) en cola, %d mensaje(s) calculados", queued, S.sentCount) }
    if S.errors > 0 then
        lines[#lines + 1] = string.format("Canal en vivo: %d error(es) al codificar", S.errors)
    end
    return lines
end

-- ---------------------------------------------------------------------------
-- Eventos del canal
-- ---------------------------------------------------------------------------
local frame = ns.NewFrame and ns.NewFrame("Frame")
if frame then
    local events = {}
    for ev in pairs(EVENT_TYPES) do events[#events + 1] = ev end
    for _, ev in ipairs(events) do
        ns.Method(frame, "RegisterEvent", ev)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event)
        if event == "PLAYER_REGEN_ENABLED" then
            markAll()
            return
        end
        markTypes(EVENT_TYPES[event] or {})
    end)
end

-- Internos expuestos solo para tools/lua/test_channel.lua.
C.testing = { refreshType = refreshType, refreshOne = refreshOne, fullJSON = fullJSON }

-- Tope de cada mensaje JSON (Json.lua usa 64 KB por defecto).
if ns.Json then ns.Json.MAX_BYTES = C.MAX_MESSAGE_BYTES end

-- Marca todo al arrancar (primer refresco completo).
markAll()
