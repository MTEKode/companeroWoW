-- Markup.lua: lee la respuesta de Compañero que el jugador pega (Ctrl+V) en el marco. Parte pura,
-- sin API de WoW (testeable offline). Contrato in-game (identico en la app):
--   [CWOW1 #<questionId> <provider/model>]
--   <lineas del cuerpo>
--   [/CWOW1]
-- Cuerpo: **negrita**, lineas que empiezan por "- " (viñetas) y lineas de accion que se vuelven
-- botones: @marca <mapID> <x> <y> <texto>, @barras <slot:spellID,...>, @macro <nombre>|<icono>|<cuerpo>,
-- y @fuente <texto> (linea gris). Cada "|" literal del cuerpo viene escrito como "||".
-- Maximo MAX_CHARS caracteres del cuerpo (lo que pase de ahi se corta).
-- Resultado de M.Parse: { id, provider, blocks = {...}, body, truncated } o nil + motivo (castellano).
-- Bloques: { kind = "para"|"bullet"|"source", segs = { {text, bold} } }, { kind = "space" },
--          { kind = "action", action = "marca", mapID, x, y (0..100), text }
--          { kind = "action", action = "barras", args = "slot:spellID,..." }
--          { kind = "action", action = "macro", name, args = "nombre|icono|cuerpo" }
local ADDON_NAME, ns = ...
ns = ns or {}

local M = {}
ns.Markup = M

M.MAX_CHARS = 4000
M.NOT_ANSWER = "Eso no es una respuesta de Compañero."
M.INCOMPLETE = "La respuesta está incompleta (falta [/CWOW1])."
M.EMPTY = "La respuesta no tiene contenido."

local byte, sub, gsub, find = string.byte, string.sub, string.gsub, string.find
local ESC_PIPE = "\1" -- marcador interno de "||" mientras se trocea una linea

local function trim(s)
    return (string.match(s, "^%s*(.-)%s*$"))
end

-- Trozos separados por una cadena literal (sin patrones).
local function splitPlain(s, sep)
    local out, from = {}, 1
    while true do
        local i, j = find(s, sep, from, true)
        if not i then
            out[#out + 1] = sub(s, from)
            return out
        end
        out[#out + 1] = sub(s, from, i - 1)
        from = j + 1
    end
end

-- "||" -> "|" (escape de WoW para el pipe literal).
local function unescape(s)
    return (gsub(s, "||", "|"))
end

-- Numero de caracteres UTF-8 de s (los bytes de continuacion no cuentan).
function M.CharLen(s)
    local _, cont = gsub(s, "[\128-\191]", "")
    return #s - cont
end

-- Recorta s a n caracteres UTF-8 (sin cortar un caracter por la mitad).
local function capChars(s, n)
    local i, count = 1, 0
    while i <= #s and count < n do
        local b = byte(s, i)
        local w = (b >= 0xF0 and 4) or (b >= 0xE0 and 3) or (b >= 0xC0 and 2) or 1
        i = i + w
        count = count + 1
    end
    if i > #s then return s end
    return sub(s, 1, i - 1)
end
M.CapChars = capChars

-- Segmentos de texto con **negrita** (una ** sin cerrar es literal). "||" ya desescapado.
local function inline(text)
    local pieces = splitPlain(text, "**")
    local n = #pieces
    if n % 2 == 0 then
        pieces[n - 1] = pieces[n - 1] .. "**" .. pieces[n]
        pieces[n] = nil
        n = n - 1
    end
    local segs = {}
    for i, p in ipairs(pieces) do
        if p ~= "" then segs[#segs + 1] = { text = unescape(p), bold = (i % 2 == 0) } end
    end
    return segs
end
M.Inline = inline

-- Trocea una linea de accion por "|" (los "||" son pipes literales).
local function pipeFields(s)
    local marked = gsub(s, "||", ESC_PIPE)
    local fields = splitPlain(marked, "|")
    for i, f in ipairs(fields) do fields[i] = (gsub(f, ESC_PIPE, "|")) end
    return fields
end

local function parseAction(cmd, args)
    if cmd == "marca" then
        local mapTok, xTok, yTok, text = string.match(args, "^(%d+)%s+([%d%.]+)%s+([%d%.]+)%s*(.*)$")
        local mapID, x, y = tonumber(mapTok), tonumber(xTok), tonumber(yTok)
        if mapID and mapID > 0 and x and y and x >= 0 and x <= 100 and y >= 0 and y <= 100 then
            return { kind = "action", action = "marca", mapID = mapID, x = x, y = y, text = unescape(text or "") }
        end
    elseif cmd == "barras" then
        local spec = unescape(trim(args))
        if spec ~= "" then return { kind = "action", action = "barras", args = spec } end
    elseif cmd == "macro" then
        local fields = pipeFields(args)
        local name = trim(fields[1] or "")
        if name == "" or #fields < 2 then return nil end
        -- Mismo formato que /compa macro crear: nombre|icono|cuerpo, o nombre|cuerpo (sin icono).
        local rebuilt
        if #fields >= 3 then
            rebuilt = name .. "|" .. trim(fields[2]) .. "|" .. table.concat(fields, "|", 3)
        else
            rebuilt = name .. "|" .. fields[2]
        end
        return { kind = "action", action = "macro", name = name, args = rebuilt }
    elseif cmd == "fuente" then
        return { kind = "source", segs = inline(args) }
    end
    return nil
end

-- Una linea del cuerpo -> bloque.
local function parseLine(line)
    line = string.gsub(line, "%s+$", "")
    if line == "" then return { kind = "space" } end
    local cmd, args = string.match(line, "^@(%a+)%s*(.*)$")
    if cmd then
        local block = parseAction(string.lower(cmd), args or "")
        if block then return block end
    end
    local bullet = string.match(line, "^%- (.*)$")
    if bullet then return { kind = "bullet", segs = inline(bullet) } end
    return { kind = "para", segs = inline(line) }
end
M.ParseLine = parseLine

-- Pega completa -> respuesta, o nil y el motivo.
function M.Parse(text)
    if type(text) ~= "string" then return nil, M.NOT_ANSWER end
    local s = gsub(gsub(text, "\r\n", "\n"), "\r", "\n")
    local lines = splitPlain(s, "\n")
    local first, last
    for i = 1, #lines do
        if trim(lines[i]) ~= "" then first = i break end
    end
    for i = #lines, 1, -1 do
        if trim(lines[i]) ~= "" then last = i break end
    end
    if not first then return nil, M.NOT_ANSWER end
    local idText, provider = string.match(trim(lines[first]), "^%[CWOW1 #(%d+)%s*([^%]]*)%]$")
    local id = tonumber(idText)
    if not id or id < 1 or id > 4294967295 then return nil, M.NOT_ANSWER end
    if last == first or trim(lines[last]) ~= "[/CWOW1]" then return nil, M.INCOMPLETE end

    local parts = {}
    for i = first + 1, last - 1 do parts[#parts + 1] = lines[i] end
    local body = table.concat(parts, "\n")
    local truncated = false
    if M.CharLen(body) > M.MAX_CHARS then
        body = capChars(body, M.MAX_CHARS)
        truncated = true
    end
    if trim(body) == "" then return nil, M.EMPTY end

    local blocks = {}
    for _, line in ipairs(splitPlain(body, "\n")) do
        blocks[#blocks + 1] = parseLine(line)
    end
    provider = trim(provider or "")
    return {
        id = id,
        provider = provider ~= "" and provider or nil,
        blocks = blocks,
        body = body,
        truncated = truncated,
    }
end

return M
