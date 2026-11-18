-- Json.lua: codificador JSON minimo y puro (sin llamadas a la API de WoW) para el canal en vivo.
-- ns.Json.Encode(valor) -> cadena JSON, o nil + motivo ("depth" | "size" | "type").
-- ns.Json.EncodeAsync(valor): igual, pero cede el control (coroutine.yield) cada ASYNC_NODES nodos.
--   Solo se llama dentro de una coroutine; quien la reanuda reparte la codificacion entre tramas.
-- Reglas:
--  - nil y funciones/userdata se omiten en objetos y se escriben null en arrays.
--  - tabla con solo claves enteras 1..n (sin huecos) -> array; cualquier otra -> objeto.
--  - tabla vacia -> {} (objeto), salvo las marcadas con Json.Array() -> [].
--  - claves de objeto: cadenas, o numeros convertidos a cadena; se ordenan (salida determinista,
--    necesaria para que el hash de cambios sea estable).
--  - numeros enteros sin decimales; NaN e infinitos -> null; decimales con 10 cifras significativas.
--  - cadenas: escapes JSON; caracteres de control como \u00XX; UTF-8 se deja tal cual.
--  - limites: profundidad 8 y tamano maximo MAX_BYTES; si se superan, se devuelve nil (nunca un JSON cortado).
local ADDON_NAME, ns = ...
ns = ns or {}

local type, pairs, tostring, tonumber = type, pairs, tostring, tonumber
local getmetatable, setmetatable, next = getmetatable, setmetatable, next
local floor, huge = math.floor, math.huge
local concat, sort = table.concat, table.sort
local format, gsub, byte = string.format, string.gsub, string.byte
local yield = coroutine.yield

local Json = {}
ns.Json = Json

Json.MAX_DEPTH = 8
Json.MAX_BYTES = 65536
Json.ASYNC_NODES = 48 -- nodos entre dos cesiones en EncodeAsync (unas decenas de us en WoW)

-- Marca de "lista vacia": una tabla vacia con esta metatabla se escribe [] (no {}).
local ARRAY_MT = {}
function Json.Array(t)
    return setmetatable(t or {}, ARRAY_MT)
end

local ESC = {
    ['"'] = '\\"', ['\\'] = '\\\\', ['\b'] = '\\b', ['\f'] = '\\f',
    ['\n'] = '\\n', ['\r'] = '\\r', ['\t'] = '\\t',
}

-- Cadena entre comillas con escapes.
local function quote(s)
    return '"' .. gsub(s, '[%c"\\]', function(c)
        return ESC[c] or format("\\u%04x", byte(c))
    end) .. '"'
end
Json.Quote = quote

local function number(v)
    if v ~= v or v == huge or v == -huge then return "null" end
    if v == floor(v) and v > -2 ^ 53 and v < 2 ^ 53 then
        return format("%.0f", v)
    end
    return format("%.10g", v)
end

-- true si t es un array denso 1..n (sin otras claves y no vacio). Devuelve n o nil.
local function arrayLength(t)
    local count, maxi = 0, 0
    for k in pairs(t) do
        if type(k) ~= "number" or k < 1 or floor(k) ~= k then return nil end
        count = count + 1
        if k > maxi then maxi = k end
    end
    if count > 0 and count == maxi then return count end
    return nil
end

-- Ordena las claves de un objeto como cadenas. Devuelve lista de pares {clave_texto, valor}.
local function sortedEntries(t)
    local keys, texts = {}, {}
    for k, v in pairs(t) do
        local vt = type(v)
        if vt ~= "function" and vt ~= "userdata" and vt ~= "thread" and v ~= nil then
            local kt
            if type(k) == "string" then
                kt = k
            elseif type(k) == "number" then
                kt = number(k)
            end
            if kt then
                keys[#keys + 1] = { text = kt, value = v }
            end
        end
    end
    sort(keys, function(a, b) return a.text < b.text end)
    return keys
end

-- Escribe el valor en buf. Devuelve true, o false + motivo. async: ceder cada ASYNC_NODES nodos.
local nodes = 0
local function write(buf, v, depth, async)
    local t = type(v)
    if t == "nil" or t == "function" or t == "userdata" or t == "thread" then
        buf[#buf + 1] = "null"
        return true
    elseif t == "boolean" then
        buf[#buf + 1] = v and "true" or "false"
        return true
    elseif t == "number" then
        buf[#buf + 1] = number(v)
        return true
    elseif t == "string" then
        buf[#buf + 1] = quote(v)
        return true
    elseif t == "table" then
        if depth >= Json.MAX_DEPTH then return false, "depth" end
        if async then
            nodes = nodes + 1
            if nodes >= Json.ASYNC_NODES then
                nodes = 0
                yield()
            end
        end
        if next(v) == nil then
            buf[#buf + 1] = getmetatable(v) == ARRAY_MT and "[]" or "{}"
            return true
        end
        local n = arrayLength(v)
        if n then
            buf[#buf + 1] = "["
            for i = 1, n do
                if i > 1 then buf[#buf + 1] = "," end
                local ok, why = write(buf, v[i], depth + 1, async)
                if not ok then return false, why end
            end
            buf[#buf + 1] = "]"
            return true
        end
        buf[#buf + 1] = "{"
        local first = true
        for _, e in ipairs(sortedEntries(v)) do
            if not first then buf[#buf + 1] = "," end
            first = false
            buf[#buf + 1] = quote(e.text)
            buf[#buf + 1] = ":"
            local ok, why = write(buf, e.value, depth + 1, async)
            if not ok then return false, why end
        end
        buf[#buf + 1] = "}"
        return true
    end
    return false, "type"
end

-- Codifica v. Devuelve la cadena JSON, o nil y el motivo ("depth", "size" o "type").
-- El tamano se comprueba al final (una tabla gigante se rechaza entera, no a medias).
function Json.Encode(v)
    local buf = {}
    local ok, why = write(buf, v, 0, false)
    if not ok then return nil, why end
    local s = concat(buf)
    if #s > Json.MAX_BYTES then return nil, "size" end
    return s
end

-- Igual que Encode, pero cede el control cada ASYNC_NODES nodos. Debe llamarse dentro de una
-- coroutine. Mismo resultado que Encode.
function Json.EncodeAsync(v)
    local buf = {}
    nodes = 0
    local ok, why = write(buf, v, 0, true)
    if not ok then return nil, why end
    local s = concat(buf)
    if #s > Json.MAX_BYTES then return nil, "size" end
    return s
end

