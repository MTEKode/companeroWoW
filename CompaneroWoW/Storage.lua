-- Storage.lua: datos guardados (SavedVariables, esquema 2.1), la BANDEJA (outbox) y el ESTADO ACTUAL
-- del personaje. Contrato LEAN STORAGE (identico en el addon y en la app; la app SQLite es el sistema
-- de registro).
--
-- SavedVariables (CompaneroWoWDB) tras la migracion:
--   schema = 2, schemaMinor = 1, settings, maps, mark
--   outbox = { seq = <ultimo seq>, win = <ultima ventana de envio>,
--              items = { { seq, msgType, at, shares, win, payload = { oseq, p, v } }, ... } }
--              Solo historial aprendido: combates (7), conocimiento (9) y mundo (16).
--   characters[clave] = los campos de Storage.PERSIST_CHAR (personaje core, marcas de tiempo,
--              marco, preguntas y respuestas, sesion, copias de seguridad de barras y macros,
--              contador de combates, hashes del ultimo envio) y state (ver mas abajo).
-- Memoria de juego (nunca se guarda): el resto de campos del personaje (bloques de estado actual,
-- recalculados en juego y enviados en cada ventana), y knowledge/world, que se reconstruyen desde la
-- bandeja al cargar. Los campos de memoria se enrutan con metatablas: el codigo de los modulos no cambia.
--
-- Estado actual: characters[clave].state = { sheet, equipment, gear, proficiencies, professions,
-- training, profPlan, quests, location, completedQuests, dungeons, gearCandidates, questWindow,
-- questieStatus, at }.
-- Es la ULTIMA copia de cada bloque, sobrescrita (no es un historial ni va a la bandeja) y en formato
-- largo (las claves son las del juego, como en el canal). questWindow y gearCandidates van en forma
-- compacta (STATE_QUESTS, STATE_CANDIDATES); questieStatus es el resumen de Questie, no su exportacion.
-- Se escribe con Storage.SaveState tras cada cambio de bloque. Tope por personaje: STATE_MAX_BYTES; si
-- el fichero pasa de FILE_TARGET_BYTES, los demas personajes se recortan a su nucleo (hoja basica).
--
-- Bandeja: cada registro aprendido (conocimiento 9, mundo 16, combates 7) se anade con Storage.Learn
-- como item con seq creciente. Payload (lo que viaja en el canal): { oseq, p, v }
--   p = ruta del registro (array), v = registro en forma compacta (Storage.KEYS).
-- Un item NO se quita al salir en una ventana (no hay confirmacion de que la app lo haya recibido): se
-- quita al cumplir OUTBOX_MAX_AGE (7 dias desde su hora de aprendizaje, at), o al superar MAX_SV_BYTES
-- (tamano estimado del item serializado como SavedVariables, no del JSON) o MAX_ITEMS items (el mas
-- antiguo primero). Formato exacto del canal para la app: docs/outbox-format.md.
--
-- Codificacion compacta: claves largas -> cortas (Storage.KEYS); x/y y coordenadas de giver/turnIn con
-- 3 decimales; lastSeen/lastOffered no se guardan (son volatiles y no cambian el registro).
local ADDON_NAME, ns = ...
ns = ns or {}

local type, pairs, ipairs, tostring, tonumber = type, pairs, ipairs, tostring, tonumber
local floor, sort = math.floor, table.sort
local concat, format = table.concat, string.format
local getmetatable, setmetatable, rawget, rawset = getmetatable, setmetatable, rawget, rawset

local Storage = {}
ns.Storage = Storage

Storage.SCHEMA = 2
Storage.SCHEMA_MINOR = 1          -- 2.1: characters[clave].state (estado actual, ver mas abajo)
Storage.MAX_ITEMS = 2000          -- tope secundario: items de la bandeja
Storage.MAX_SV_BYTES = 60 * 1024  -- tope principal: bytes estimados de la bandeja serializada (SavedVariables)
Storage.OUTBOX_MAX_AGE = 7 * 24 * 3600 -- un item de la bandeja se quita al cumplir 7 dias (at)
Storage.ANSWER_MAX_BYTES = 2048   -- texto de cada respuesta guardada (Ask.lua)
Storage.STATE_MAX_BYTES = 96 * 1024   -- tope por personaje del bloque state (serializado; antes 60 KB, sin la ventana de misiones)
Storage.STATE_QUESTS = 120             -- misiones de la ventana que van al estado (accesibles primero, luego las mas cercanas al nivel)
Storage.STATE_CANDIDATES = 80          -- candidatos de equipo que van al estado (los de mayor nivel de objeto)
Storage.FILE_TARGET_BYTES = 150 * 1024 -- tope orientativo del fichero entero (por encima se recortan los demas)

-- Campos de cada personaje que se quedan en SavedVariables. Cualquier otro campo es de memoria.
Storage.PERSIST_CHAR = {
    character = true, updatedAt = true, loggedOutAt = true, ui = true, ask = true,
    session = true, sessionBase = true, actionBarsBackup = true, macroBackup = true,
    combatSeq = true, sent = true, state = true,
}
-- Claves de primer nivel de CompaneroWoWDB que viven solo en memoria (se reconstruyen de la bandeja).
Storage.LIVE_TOP = { knowledge = true, world = true }

-- Tipos de mensaje de la bandeja.
Storage.TYPE_COMBAT = 7
Storage.TYPE_KNOWLEDGE = 9
Storage.TYPE_SHEET = 12
Storage.TYPE_WORLD = 16

-- ---------------------------------------------------------------------------
-- Codificacion compacta
-- ---------------------------------------------------------------------------
-- Clave larga -> corta. Los codigos cortos no deben coincidir con una clave larga que aparezca sin
-- traducir en un registro (por eso no se usan id, t, at, p, v, k).
Storage.KEYS = {
    name = "n", mapID = "m", x = "x", y = "y", reaction = "r",
    isQuestGiver = "qg", isVendor = "vd", isTrainer = "tr", availableCount = "ac", services = "sv",
    firstSeen = "fs", npcID = "pn", questID = "qd", title = "ti", level = "lv",
    objectivesText = "ot", descriptionText = "dt", rewards = "rw", money = "mo", xp = "xp",
    items = "it", itemID = "i", link = "l", count = "c", choice = "ch",
    giverNpcID = "gn", giverName = "gna", giverMapID = "gm", giverX = "gx", giverY = "gy",
    turnInNpcID = "tn", turnInName = "tna", turnInMapID = "tm", turnInX = "tx", turnInY = "ty",
    offeredAt = "oa", offeredSource = "os", nodeID = "nd", known = "kn",
    copper = "cu", hops = "ho", seconds = "se", status = "st", slot = "sl",
}
-- Claves cuyo valor numerico se redondea a 3 decimales.
Storage.ROUND = { x = true, y = true, giverX = true, giverY = true, turnInX = true, turnInY = true }
-- Claves volatiles: no se guardan (no cambian el registro).
Storage.DROP = { lastSeen = true, lastOffered = true }

local SHORT, LONG = {}, {}
for long, short in pairs(Storage.KEYS) do
    SHORT[long] = short
    LONG[short] = long
end

local function isArray(t)
    local count, maxi = 0, 0
    for k in pairs(t) do
        if type(k) ~= "number" or k < 1 or floor(k) ~= k then return false end
        count = count + 1
        if k > maxi then maxi = k end
    end
    return count == maxi
end

-- Escape de claves: una clave que no tiene codigo corto pero que coincide con uno (o empieza por ESC)
-- se escribe con ESC delante. Asi Expand(Compact(x)) = x para cualquier clave, sin ambiguedad.
local ESC = "~"
local function shortKey(k)
    if type(k) ~= "string" then return k end
    if SHORT[k] then return SHORT[k] end
    if LONG[k] or string.sub(k, 1, 1) == ESC then return ESC .. k end
    return k
end
local function longKey(k)
    if type(k) ~= "string" then return k end
    if string.sub(k, 1, 1) == ESC then return string.sub(k, 2) end
    if LONG[k] then return LONG[k] end
    return k
end

-- Copia de v con las claves traducidas (toCompact: largas -> cortas; si no, al reves).
-- Solo al compactar se redondean los numeros de las claves de Storage.ROUND y se quitan las volatiles.
local function convert(v, toCompact, depth)
    if type(v) ~= "table" then
        return v
    end
    if depth > 12 then return nil end
    local keyFn = toCompact and shortKey or longKey
    local out = {}
    if isArray(v) then
        for i = 1, #v do out[i] = convert(v[i], toCompact, depth + 1) end
        return out
    end
    for k, val in pairs(v) do
        local vt = type(val)
        if vt ~= "function" and vt ~= "userdata" and vt ~= "thread"
            and not (toCompact and type(k) == "string" and Storage.DROP[k]) then
            local nv = val
            if toCompact and type(k) == "string" and Storage.ROUND[k] and type(val) == "number" then
                nv = floor(val * 1000 + 0.5) / 1000
            else
                nv = convert(val, toCompact, depth + 1)
            end
            out[keyFn(k)] = nv
        end
    end
    return out
end

-- Registro en forma compacta (copia; el original no cambia).
function Storage.Compact(v)
    return convert(v, true, 0)
end

-- Inversa de Compact: devuelve claves largas (lo volatil ya no esta).
function Storage.Expand(v)
    return convert(v, false, 0)
end

-- Texto tipo SavedVariables (Lua) de un valor, para estimar el tamano de lo guardado.
function Storage.Serialize(value)
    local buf, n = {}, 0
    local function put(s)
        n = n + 1
        buf[n] = s
    end
    local function ser(x, depth)
        local t = type(x)
        if t == "table" then
            if depth > 20 then return end
            put("{")
            local first = true
            for k, val in pairs(x) do
                local vt = type(val)
                if vt ~= "function" and vt ~= "userdata" and vt ~= "thread" then
                    if not first then put(",") end
                    first = false
                    put("[")
                    ser(k, depth + 1)
                    put("]=")
                    ser(val, depth + 1)
                end
            end
            put("}")
        elseif t == "string" then
            put(format("%q", x))
        elseif t == "number" then
            put(format("%.14g", x))
        elseif t == "boolean" then
            put(x and "true" or "false")
        else
            put("nil")
        end
    end
    ser(value, 0)
    return concat(buf)
end

-- ---------------------------------------------------------------------------
-- Estado en memoria (no se guarda) y enrutado de campos
-- ---------------------------------------------------------------------------
local S = {
    db = nil,          -- CompaneroWoWDB con el que se inicializo el estado
    epoch = 0,         -- sube cada vez que cambia la base (los modulos reconstruyen su memoria)
    byseq = {},        -- [seq] = item de la bandeja
    sizes = {},        -- [seq] = bytes del payload
    total = 0,         -- bytes totales del payload de la bandeja
    lastHash = {},     -- [msgType|ruta] = hash del ultimo registro guardado (evita duplicados)
}

local top = {}                               -- memoria de knowledge y world
local live = setmetatable({}, { __mode = "k" }) -- [tabla de personaje] = campos de memoria

local CHAR_MT = {
    __index = function(t, k)
        local m = live[t]
        if m == nil then return nil end
        return m[k]
    end,
    __newindex = function(t, k, v)
        if Storage.PERSIST_CHAR[k] then
            rawset(t, k, v)
            return
        end
        local m = live[t]
        if not m then
            m = {}
            live[t] = m
        end
        m[k] = v
    end,
}

local TOP_MT = {
    __index = function(_, k) return top[k] end,
    __newindex = function(t, k, v)
        if Storage.LIVE_TOP[k] then
            top[k] = v
        else
            rawset(t, k, v)
        end
    end,
}

local CHARS_MT = {
    __newindex = function(t, k, v)
        rawset(t, k, Storage.AttachChar(v))
    end,
}

-- Mueve a memoria los campos de un personaje que no son persistentes y activa el enrutado.
function Storage.AttachChar(c)
    if type(c) ~= "table" or getmetatable(c) == CHAR_MT then return c end
    local m = live[c] or {}
    live[c] = m
    local moved = {}
    for k in pairs(c) do
        if not Storage.PERSIST_CHAR[k] then moved[#moved + 1] = k end
    end
    for _, k in ipairs(moved) do
        m[k] = rawget(c, k)
        rawset(c, k, nil)
    end
    setmetatable(c, CHAR_MT)
    return c
end

-- Campos de memoria de la base y de los personajes (idempotente).
local function attachDB(db)
    for k in pairs(Storage.LIVE_TOP) do
        local v = rawget(db, k)
        if v ~= nil then
            top[k] = v
            rawset(db, k, nil)
        end
    end
    if top.knowledge == nil then top.knowledge = { quests = {}, npcs = {} } end
    if top.world == nil then top.world = {} end
    if getmetatable(db) ~= TOP_MT then setmetatable(db, TOP_MT) end
    local chars = rawget(db, "characters")
    if type(chars) == "table" then
        for key, c in pairs(chars) do chars[key] = Storage.AttachChar(c) end
        if getmetatable(chars) ~= CHARS_MT then setmetatable(chars, CHARS_MT) end
    end
end

-- ---------------------------------------------------------------------------
-- Bandeja: indice, alta, topes y baja
-- ---------------------------------------------------------------------------

local function flat(path)
    local parts = {}
    for i = 1, #path do parts[i] = tostring(path[i]) end
    return concat(parts, "|")
end

local function outboxOf(db)
    if type(db.outbox) ~= "table" then db.outbox = {} end
    local box = db.outbox
    if type(box.seq) ~= "number" then box.seq = 0 end
    if type(box.items) ~= "table" then box.items = {} end
    return box
end

-- Un item caduca al cumplir OUTBOX_MAX_AGE desde su hora de aprendizaje (at). Sin hora (0): no caduca.
local function expired(item, now)
    local at = tonumber(item.at) or 0
    return at > 0 and now - at > Storage.OUTBOX_MAX_AGE
end

-- Quita el item en la posicion i de la lista (y de los indices).
local function removeAt(box, i)
    local item = table.remove(box.items, i)
    if not item then return end
    local size = S.sizes[item.seq] or 0
    S.total = S.total - size
    S.sizes[item.seq] = nil
    S.byseq[item.seq] = nil
end

-- Tamano estimado de un item en SavedVariables: su texto serializado completo (no el JSON del canal).
local function itemSize(item)
    return #Storage.Serialize(item)
end

-- Recalcula los indices de la bandeja de la base db (una vez por base, y tras cargarla).
local function indexOutbox(db)
    local box = outboxOf(db)
    S.byseq, S.sizes, S.total, S.lastHash = {}, {}, 0, {}
    local kept = {}
    local now = ns.Now()
    for _, item in ipairs(box.items) do
        local pl = type(item) == "table" and item.payload or nil
        if type(pl) == "table" and type(pl.p) == "table" and type(item.seq) == "number" and not expired(item, now) then
            local pjson = ns.Json.Encode(pl)
            local vjson = ns.Json.Encode(pl.v)
            if pjson and vjson then
                kept[#kept + 1] = item
                S.byseq[item.seq] = item
                S.sizes[item.seq] = itemSize(item)
                S.total = S.total + S.sizes[item.seq]
                S.lastHash[tostring(item.msgType) .. "|" .. flat(pl.p)] = ns.HashText(vjson)
            end
        end
    end
    box.items = kept
    while #box.items > Storage.MAX_ITEMS or S.total > Storage.MAX_SV_BYTES do
        removeAt(box, 1)
    end
end

-- Quita los items caducados y, despues, los mas antiguos mientras se superen los topes.
local function enforceCaps(box)
    local now = ns.Now()
    for i = #box.items, 1, -1 do
        if expired(box.items[i], now) then removeAt(box, i) end
    end
    while #box.items > Storage.MAX_ITEMS or S.total > Storage.MAX_SV_BYTES do
        if #box.items == 0 then break end
        removeAt(box, 1)
    end
end

-- Anade un item con la forma compacta v (ya comprobada). Devuelve su seq o nil.
local function append(db, msgType, path, v, hash, lastKey, at)
    local box = outboxOf(db)
    local seq = box.seq + 1
    local payload = { oseq = seq, p = {}, v = v }
    for i = 1, #path do payload.p[i] = path[i] end
    local pjson = ns.Json.Encode(payload)
    if not pjson then return nil end
    box.seq = seq
    local item = { seq = seq, msgType = msgType, at = at, shares = 0, payload = payload }
    box.items[#box.items + 1] = item
    S.byseq[seq] = item
    local isize = itemSize(item)
    S.sizes[seq] = isize
    S.total = S.total + isize
    S.lastHash[lastKey] = hash
    enforceCaps(box)
    if Storage.OnAppend and S.byseq[seq] then ns.SafeCall(Storage.OnAppend, item) end
    return seq
end

-- Base actual (CompaneroWoWDB) con el estado al dia. nil si no hay base.
local function current()
    if type(CompaneroWoWDB) ~= "table" then return nil end
    if CompaneroWoWDB ~= S.db then Storage.Sync() end
    if S.db ~= CompaneroWoWDB then return nil end
    return S.db
end

-- Inicializa el estado para la base actual (si cambio): indices, migracion y enrutado.
-- Devuelve la epoca (cambia cuando la base cambia; los modulos la usan para reconstruir su memoria).
function Storage.Sync()
    local db = CompaneroWoWDB
    if type(db) ~= "table" then return S.epoch end
    if S.db == db then return S.epoch end
    S.db = db
    indexOutbox(db)
    if db.schema ~= Storage.SCHEMA then Storage.Migrate(db) end
    db.schema = Storage.SCHEMA
    if (tonumber(db.schemaMinor) or 0) < Storage.SCHEMA_MINOR then
        Storage.MigrateState(db)
        db.schemaMinor = Storage.SCHEMA_MINOR
    end
    attachDB(db)
    S.epoch = S.epoch + 1
    return S.epoch
end

-- Enrutado de campos (EnsureDB lo llama en cada uso: barato, idempotente).
function Storage.Attach()
    local epoch = Storage.Sync()
    if type(CompaneroWoWDB) == "table" then attachDB(CompaneroWoWDB) end
    return epoch
end

-- Anade un registro a la bandeja si cambio desde el ultimo guardado de su ruta.
-- msgType: tipo de mensaje; path: ruta del registro (array); value: registro en claves largas.
-- Devuelve el seq del item nuevo, o nil (sin cambios, sin base o no codificable).
function Storage.Learn(msgType, path, value, at)
    local db = current()
    if not db or type(path) ~= "table" or value == nil then return nil end
    local v = Storage.Compact(value)
    local vjson = ns.Json.Encode(v)
    if not vjson then return nil end
    local hash = ns.HashText(vjson)
    local lastKey = tostring(msgType) .. "|" .. flat(path)
    if S.lastHash[lastKey] == hash then return nil end
    return append(db, msgType, path, v, hash, lastKey, at or ns.Now())
end

-- La ventana win ha enviado el item seq: cuenta una ventana para el item. El item NO se quita (solo
-- caduca o cae por los topes): no hay confirmacion de que la app lo haya recibido.
-- Devuelve true si se conto en esta ventana (false si ya estaba contado o no existe).
function Storage.MarkShared(seq, win)
    local db = current()
    local item = db and S.byseq[seq] or nil
    if not item or win == nil or item.win == win then return false end
    item.win = win
    item.shares = (item.shares or 0) + 1
    local oldSize = S.sizes[seq] or 0
    S.sizes[seq] = itemSize(item)
    S.total = S.total - oldSize + S.sizes[seq]
    return true
end

-- Numero de la ventana de envio nueva (se guarda en la base).
function Storage.NewWindow()
    local db = current()
    if not db then return nil end
    local box = outboxOf(db)
    box.win = (type(box.win) == "number" and box.win or 0) + 1
    return box.win
end

-- Items de la bandeja, del mas antiguo al mas nuevo (solo lectura).
function Storage.Items()
    local db = current()
    if not db then return {} end
    return outboxOf(db).items
end

-- Numero de items y bytes estimados (serializados) de la bandeja.
function Storage.Stats()
    local items = Storage.Items()
    return #items, S.total
end

-- Vacia la bandeja (el seq sigue creciendo). Devuelve cuantos items habia.
function Storage.Clear()
    local db = current()
    if not db then return 0 end
    local box = outboxOf(db)
    local n = #box.items
    box.items = {}
    S.byseq, S.sizes, S.total, S.lastHash = {}, {}, 0, {}
    if Storage.OnClear then ns.SafeCall(Storage.OnClear) end
    return n
end

-- Recorre la bandeja en orden: fn(msgType, ruta, registro en claves largas).
function Storage.Replay(fn)
    for _, item in ipairs(Storage.Items()) do
        fn(item.msgType, item.payload.p, Storage.Expand(item.payload.v))
    end
end

-- Estado para /compa estado: tamano estimado de lo guardado y de la bandeja.
function Storage.StatusLine()
    local n, bytes = Storage.Stats()
    local saved = CompaneroWoWDB and #Storage.Serialize(CompaneroWoWDB) or 0
    return string.format("Datos guardados: %.1f KB, bandeja: %d elementos (%.1f KB)",
        saved / 1024, n, bytes / 1024)
end

-- ---------------------------------------------------------------------------
-- Migracion desde el esquema 1
-- ---------------------------------------------------------------------------
-- Convierte knowledge, world y combates guardados en el esquema 1 en items de la bandeja (los mas
-- antiguos primero, para que los topes descarten lo viejo). Los bloques de estado actual no se pasan:
-- se recalculan en juego. Quita de SavedVariables los bloques grandes.
local function collectMigrated(db, out)
    local k = rawget(db, "knowledge")
    if type(k) == "table" then
        for _, kind in ipairs({ "quests", "npcs" }) do
            local map = type(k[kind]) == "table" and k[kind] or {}
            for id, rec in pairs(map) do
                if type(rec) == "table" then
                    out[#out + 1] = { at = tonumber(rec.lastSeen) or tonumber(rec.firstSeen) or 0,
                        msgType = Storage.TYPE_KNOWLEDGE, path = { kind, id }, value = rec }
                end
            end
        end
    end
    local w = rawget(db, "world")
    if type(w) == "table" then
        local flight = type(w.flight) == "table" and w.flight or {}
        for faction, f in pairs(flight) do
            if faction ~= "known" and type(f) == "table" then
                for id, rec in pairs(f.nodes or {}) do
                    out[#out + 1] = { at = tonumber(rec.lastSeen) or 0, msgType = Storage.TYPE_WORLD,
                        path = { "flight", faction, "nodes", id }, value = rec }
                end
                for key, rec in pairs(f.costs or {}) do
                    out[#out + 1] = { at = 0, msgType = Storage.TYPE_WORLD,
                        path = { "flight", faction, "costs", key }, value = rec }
                end
            end
        end
        if type(flight.known) == "table" then
            for ck, list in pairs(flight.known) do
                out[#out + 1] = { at = 0, msgType = Storage.TYPE_WORLD,
                    path = { "flight", "known", ck }, value = list }
            end
        end
        for _, sec in ipairs({ "vendors", "trainers", "questChains", "consumables" }) do
            for id, rec in pairs(type(w[sec]) == "table" and w[sec] or {}) do
                out[#out + 1] = { at = tonumber(type(rec) == "table" and rec.lastSeen or nil) or 0,
                    msgType = Storage.TYPE_WORLD, path = { sec, id }, value = rec }
            end
        end
    end
    local chars = rawget(db, "characters")
    if type(chars) == "table" then
        for key, c in pairs(chars) do
            local list = type(c) == "table" and rawget(c, "combats") or nil
            if type(list) == "table" then
                for i = #list, 1, -1 do -- la lista guarda el mas nuevo primero
                    local rec = list[i]
                    if type(rec) == "table" and rec.id ~= nil then
                        out[#out + 1] = { at = tonumber(rec.t) or 0, msgType = Storage.TYPE_COMBAT,
                            path = { "combats", rec.id }, value = rec, owner = key }
                    end
                end
            end
        end
    end
end

-- Pasa a la bandeja lo guardado en el esquema 1 y quita los bloques grandes de SavedVariables.
function Storage.Migrate(db)
    local migrated = {}
    collectMigrated(db, migrated)
    for i, m in ipairs(migrated) do m.order = i end
    sort(migrated, function(a, b)
        if a.at ~= b.at then return a.at < b.at end
        return a.order < b.order
    end)
    for _, m in ipairs(migrated) do
        local v = Storage.Compact(m.value)
        local vjson = ns.Json.Encode(v)
        if vjson then
            local lastKey = tostring(m.msgType) .. "|" .. flat(m.path)
            local hash = ns.HashText(vjson)
            if S.lastHash[lastKey] ~= hash then append(db, m.msgType, m.path, v, hash, lastKey, m.at) end
        end
    end
    rawset(db, "knowledge", nil)
    rawset(db, "world", nil)
    local chars = rawget(db, "characters")
    if type(chars) == "table" then
        for _, c in pairs(chars) do
            if type(c) == "table" then rawset(c, "combats", nil) end
        end
    end
    db.schema = Storage.SCHEMA
    return #migrated
end

-- ---------------------------------------------------------------------------
-- Estado actual (characters[clave].state, esquema 2.1)
-- ---------------------------------------------------------------------------
-- Secciones del estado, en orden de recorte: si el personaje no cabe en STATE_MAX_BYTES se quita primero
-- la seccion de la izquierda. La hoja (sheet) es la ultima; si aun no cabe, se recortan sus hechizos.
Storage.STATE_SECTIONS = {
    "training", "profPlan", "gear", "proficiencies", "quests", "professions", "location", "equipment",
    "completedQuests", "dungeons", "gearCandidates", "questWindow", "questieStatus", "sheet",
}
-- Campos de la hoja que se quedan en el nucleo de un personaje recortado (FILE_TARGET_BYTES).
local STATE_CORE = { level = true, class = true, race = true, total = true, talents = true }

-- Copia profunda de un valor (sin funciones ni userdata); nil si pasa de la profundidad.
local function copyValue(v, depth)
    local t = type(v)
    if t == "table" then
        if depth > 12 then return nil end
        local out = {}
        for k, x in pairs(v) do
            local cx = copyValue(x, depth + 1)
            if cx ~= nil then out[k] = cx end
        end
        return out
    end
    if t == "function" or t == "userdata" or t == "thread" then return nil end
    return v
end

-- Igualdad profunda de dos valores (sin codificar).
local function sameValue(a, b, depth)
    if a == b then return true end
    if type(a) ~= "table" or type(b) ~= "table" or depth > 12 then return false end
    for k, v in pairs(a) do
        if not sameValue(v, b[k], depth + 1) then return false end
    end
    for k in pairs(b) do
        if a[k] == nil then return false end
    end
    return true
end

-- Dos bloques de estado son iguales si lo son todas sus secciones (la hora "at" no cuenta).
local function sameState(old, new)
    if type(old) ~= "table" then return false end
    for k, v in pairs(old) do
        if k ~= "at" and not sameValue(v, new[k], 0) then return false end
    end
    for k in pairs(new) do
        if old[k] == nil then return false end
    end
    return true
end

-- La hoja sin exportedAt y sin textos de tooltip de sus hechizos: los numeros (coste, alcance, lanzamiento,
-- enfriamiento) se quedan. nil si no hay hoja.
local function sheetState(sheet)
    if type(sheet) ~= "table" then return nil end
    local out = {}
    for k, v in pairs(sheet) do
        if k ~= "exportedAt" and k ~= "spells" then out[k] = v end
    end
    if type(sheet.spells) == "table" then
        local spells = {}
        for i, sp in ipairs(sheet.spells) do
            local e = {}
            if type(sp) == "table" then
                for k, v in pairs(sp) do
                    if k ~= "tooltip" then e[k] = v end
                end
            end
            spells[i] = e
        end
        out.spells = spells
    end
    return copyValue(out, 0)
end

-- Redondeo a d decimales (nil si no es numero).
local function roundTo(v, d)
    v = tonumber(v)
    if not v then return nil end
    local m = 10 ^ d
    return floor(v * m + 0.5) / m
end

-- Punto de un PNJ u objeto (inicio o fin de una mision): tipo (solo si no es PNJ), id, nombre, mapa y
-- coordenadas a 3 decimales (x e y van en fraccion 0-1).
local function pointState(p)
    if type(p) ~= "table" then return nil end
    return copyValue({
        kind = p.kind ~= "npc" and p.kind or nil, id = tonumber(p.id), name = p.name, mapID = tonumber(p.mapID),
        x = roundTo(p.x, 3), y = roundTo(p.y, 3),
    }, 0)
end

-- Una mision de la ventana en forma compacta: sus ids de inicio, fin y recompensa, y lo que le falta.
local function questState(q)
    local out = {
        questID = tonumber(q.questID), name = q.name, level = tonumber(q.level), requiredLevel = tonumber(q.requiredLevel),
        zone = q.zone, source = q.source, accessibleNow = q.accessibleNow == true, inLog = (q.inLog == true) or nil,
        starter = pointState(q.starter), finisher = pointState(q.finisher),
    }
    if type(q.missingPrereqs) == "table" and #q.missingPrereqs > 0 then out.missingPrereqs = q.missingPrereqs end
    if type(q.rewards) == "table" then
        local r = {}
        if type(q.rewards.choice) == "table" and #q.rewards.choice > 0 then r.choice = q.rewards.choice end
        if type(q.rewards.fixed) == "table" and #q.rewards.fixed > 0 then r.fixed = q.rewards.fixed end
        if next(r) ~= nil then out.rewards = r end
    end
    return copyValue(out, 0)
end

-- questWindow del estado: las STATE_QUESTS misiones mas relevantes (accesibles ahora primero, luego las mas
-- cercanas a su nivel; a igualdad, la de menor id). nil si no hay bloque.
local function questWindowState(block)
    if type(block) ~= "table" then return nil end
    if block.available ~= true then return { available = false, reason = block.reason } end
    local level = tonumber(block.level) or 1
    local list = {}
    if type(block.quests) == "table" then
        for key, q in pairs(block.quests) do
            local id = type(q) == "table" and (tonumber(q.questID) or tonumber(key)) or nil
            if id then
                list[#list + 1] = { id = id, q = q, dist = math.abs((tonumber(q.level) or level) - level) }
            end
        end
    end
    sort(list, function(a, b)
        local aa, ba = a.q.accessibleNow == true, b.q.accessibleNow == true
        if aa ~= ba then return aa end
        if a.dist ~= b.dist then return a.dist < b.dist end
        return a.id < b.id
    end)
    local quests = {}
    for i = 1, math.min(#list, Storage.STATE_QUESTS) do
        quests[list[i].id] = questState(list[i].q)
    end
    return Storage.Compact(copyValue({
        available = true, exportedAt = block.exportedAt, level = block.level,
        minLevel = block.minLevel, maxLevel = block.maxLevel, sources = block.sources, quests = quests,
    }, 0))
end

-- gearCandidates del estado: los STATE_CANDIDATES de mayor nivel de objeto (cada item tal cual lo exporta
-- el addon: nombre, nivel, stats y origen).
local function candidatesState(block)
    if type(block) ~= "table" then return nil end
    if block.available ~= true then return { available = false, reason = block.reason } end
    local list = {}
    if type(block.items) == "table" then
        for key, it in pairs(block.items) do
            local id = type(it) == "table" and (tonumber(it.itemID) or tonumber(key)) or nil
            if id then list[#list + 1] = { id = id, it = it } end
        end
    end
    sort(list, function(a, b)
        local la, lb = tonumber(a.it.itemLevel) or 0, tonumber(b.it.itemLevel) or 0
        if la ~= lb then return la > lb end
        return a.id < b.id
    end)
    local items = {}
    for i = 1, math.min(#list, Storage.STATE_CANDIDATES) do items[list[i].id] = list[i].it end
    return Storage.Compact(copyValue({
        available = true, exportedAt = block.exportedAt, pending = block.pending, sources = block.sources, items = items,
    }, 0))
end

-- questieStatus: el resumen de la exportacion de Questie (si esta instalado, su version y cuantas misiones y
-- PNJ exporto). Sin Questie: available = false (el dato dice que NO esta instalado). nil si no se ha exportado.
local function questieState(data)
    if type(data) ~= "table" then return nil end
    -- Sin motivo ni datos: fallo de la exportacion, no "no instalado". Ese caso no dice nada de Questie.
    if data.available ~= true and data.reason == nil then return nil end
    local out = {
        available = data.available == true, version = data.version, exportedAt = data.exportedAt,
        reason = data.reason, sources = data.sources,
    }
    if out.available then
        local nq = 0
        if type(data.quests) == "table" then
            for _ in pairs(data.quests) do nq = nq + 1 end
        end
        out.quests = nq
        out.zoneQuests = (type(data.zoneQuests) == "table" and type(data.zoneQuests.list) == "table")
            and #data.zoneQuests.list or 0
        out.npcs = type(data.npcs) == "table" and #data.npcs or 0
    end
    return copyValue(out, 0)
end

-- Secciones que se guardan en forma de vista (no tal cual estan en memoria).
local STATE_VIEWS = {
    sheet = function(c) return sheetState(c.sheet) end,
    questWindow = function(c) return questWindowState(c.questWindow) end,
    gearCandidates = function(c) return candidatesState(c.gearCandidates) end,
    questieStatus = function(c) return questieState(c.questie) end,
}

-- Valor de una seccion a partir de los campos de memoria del personaje. nil si no hay dato (una lectura
-- vacia no sustituye al estado anterior).
local function sectionValue(c, name)
    local v
    local view = STATE_VIEWS[name]
    if view then
        v = view(c)
    else
        v = c[name]
    end
    -- completedQuests es texto (ids separados por comas, como lo exporta Gear.lua).
    if type(v) == "string" then return v ~= "" and v or nil end
    if type(v) ~= "table" or next(v) == nil then return nil end
    return copyValue(v, 0)
end

-- Recorta un bloque de estado hasta STATE_MAX_BYTES: quita secciones en orden (STATE_SECTIONS) y, al final,
-- los hechizos de la hoja desde el ultimo. Modifica state.
local function fitState(state)
    while #Storage.Serialize(state) > Storage.STATE_MAX_BYTES do
        local dropped = false
        for _, name in ipairs(Storage.STATE_SECTIONS) do
            if name ~= "sheet" and state[name] ~= nil then
                state[name] = nil
                dropped = true
                break
            end
        end
        if not dropped and type(state.sheet) == "table" and type(state.sheet.spells) == "table"
            and #state.sheet.spells > 0 then
            state.sheet.spells[#state.sheet.spells] = nil
            dropped = true
        end
        if not dropped then break end
    end
end

-- Nucleo de un bloque de estado (para un personaje recortado): solo los campos basicos de la hoja.
local function coreState(state)
    local out = { at = state.at }
    if type(state.sheet) == "table" then
        local core = {}
        for k in pairs(STATE_CORE) do
            if state.sheet[k] ~= nil then core[k] = copyValue(state.sheet[k], 0) end
        end
        out.sheet = core
    end
    return out
end

-- Si el fichero pasa de FILE_TARGET_BYTES, recorta el estado de los demas personajes a su nucleo.
local function trimOthers(keep)
    local db = current()
    if not db or Storage.SavedBytes() <= Storage.FILE_TARGET_BYTES then return end
    for _, other in pairs(rawget(db, "characters") or {}) do
        if other ~= keep and type(other) == "table" and type(other.state) == "table" then
            other.state = coreState(other.state)
        end
    end
end

-- Guarda el estado actual del personaje c (sus campos de memoria: sheet, equipment, gear, proficiencies,
-- professions, training, profPlan, quests, location). Cada seccion sin dato nuevo conserva la anterior.
-- Solo escribe si algo cambio. Devuelve true si escribio.
function Storage.SaveState(c)
    if type(c) ~= "table" or not current() then return false end
    local old = c.state
    local fresh = {}
    if type(old) == "table" then
        for k, v in pairs(old) do
            if k ~= "at" then fresh[k] = copyValue(v, 0) end
        end
    end
    for _, name in ipairs(Storage.STATE_SECTIONS) do
        local v = sectionValue(c, name)
        if v ~= nil then fresh[name] = v end
    end
    if next(fresh) == nil then return false end
    fitState(fresh)
    if sameState(old, fresh) then return false end
    fresh.at = ns.Now()
    c.state = fresh
    trimOthers(c)
    return true
end

-- Paso al esquema 2.1: la hoja mas reciente de la bandeja (tipo 12, que ya no se guarda ahi) pasa al
-- estado del personaje activo (el de mayor updatedAt/loggedOutAt). Los items tipo 12 salen de la bandeja.
local function activeCharacter(db)
    local best, rank = nil, -1
    for _, c in pairs(rawget(db, "characters") or {}) do
        if type(c) == "table" then
            local r = math.max(tonumber(rawget(c, "updatedAt")) or 0, tonumber(rawget(c, "loggedOutAt")) or 0)
            if r > rank then best, rank = c, r end
        end
    end
    return best
end

function Storage.MigrateState(db)
    local box = outboxOf(db)
    local newest, kept = nil, {}
    for _, item in ipairs(box.items) do
        if type(item) == "table" and item.msgType == Storage.TYPE_SHEET then
            if newest == nil or (tonumber(item.seq) or 0) > (tonumber(newest.seq) or 0) then newest = item end
        else
            kept[#kept + 1] = item
        end
    end
    box.items = kept
    local owner = activeCharacter(db)
    if newest and owner and type(newest.payload) == "table" and type(newest.payload.v) == "table" then
        local state = type(owner.state) == "table" and owner.state or {}
        state.sheet = sheetState(Storage.Expand(newest.payload.v))
        state.at = tonumber(newest.at) or ns.Now()
        owner.state = state
    end
    indexOutbox(db)
    return newest ~= nil
end

-- Tamano estimado (bytes) de lo que se guardaria en SavedVariables.
function Storage.SavedBytes()
    if type(CompaneroWoWDB) ~= "table" then return 0 end
    return #Storage.Serialize(CompaneroWoWDB)
end

return Storage
