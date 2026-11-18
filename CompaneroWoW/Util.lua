-- Util.lua: helpers puros y envoltorios seguros para la API de WoW.
-- Todo acceso a la API pasa por ns.Call / ns.Get (existencia + pcall) y todo
-- valor devuelto pasa por ns.Clean (valores "secret" de Midnight).
local ADDON_NAME, ns = ...
ns = ns or {}

local type, pcall, tonumber, tostring = type, pcall, tonumber, tostring
local floor = math.floor
local bit = bit -- disponible en WoW y en LuaJIT (require "bit")

-- Diagnostico: errores que pcall ha silenciado (se exportan como health en CompaneroWoWDB).
ns.errorCount = ns.errorCount or 0
ns.lastError = ns.lastError or nil
local MAX_ERROR_TEXT = 200

-- Cuenta un error silenciado y guarda su texto (recortado).
local function recordError(err)
    ns.errorCount = (ns.errorCount or 0) + 1
    local text = tostring(err)
    if #text > MAX_ERROR_TEXT then text = string.sub(text, 1, MAX_ERROR_TEXT) end
    ns.lastError = text
end

ns.RecordError = recordError

-- Hash de 32 bits (determinista) de un texto, con su longitud. Para detectar cambios.
function ns.HashText(s)
    local h = 5381
    for i = 1, #s do
        h = (h * 33 + string.byte(s, i)) % 4294967296
    end
    return h .. ":" .. #s
end

-- Devuelve los resultados de fn si pcall tuvo exito; nada si hubo error (contado).
local function passResults(ok, ...)
    if ok then return ... end
    recordError((...))
    return nil
end

-- pcall que cuenta los errores silenciados. Mismo contrato: true + resultados, o false + error.
local function pcallCounted(ok, ...)
    if ok then return true, ... end
    recordError((...))
    return false, (...)
end
function ns.Pcall(fn, ...)
    return pcallCounted(pcall(fn, ...))
end

-- Llamada protegida. Devuelve nil ante cualquier error.
function ns.SafeCall(fn, ...)
    if type(fn) ~= "function" then return nil end
    return passResults(pcall(fn, ...))
end

-- Linea para /compa estado con los errores silenciados.
function ns.HealthStatusLine()
    local last = ns.lastError and (" (último: " .. ns.lastError .. ")") or ""
    return string.format("Errores capturados: %d%s", ns.errorCount or 0, last)
end

-- true si el global issecretvalue existe y devuelve true para v.
function ns.IsSecret(v)
    local probe = _G.issecretvalue
    if type(probe) ~= "function" then return false end
    local ok, res = pcall(probe, v)
    return ok and res == true
end

-- Devuelve v salvo que sea secreto (en ese caso nil).
function ns.Clean(v)
    if ns.IsSecret(v) then return nil end
    return v
end

-- Bloque de integracion o de Questie (available = true/false). Una exportacion no disponible
-- (addon ausente, cargando, error) nunca sustituye a un bloque que si estaba disponible: solo lo
-- reemplaza una exportacion correcta posterior.
function ns.PreferAvailable(old, new)
    local oldOk = type(old) == "table" and old.available == true
    local newOk = type(new) == "table" and new.available == true
    if oldOk and not newOk then return old end
    return new
end

-- Numero limpio o nil.
function ns.Num(v)
    v = ns.Clean(v)
    if type(v) == "number" then return v end
    return nil
end

-- Cadena limpia o nil.
function ns.Str(v)
    v = ns.Clean(v)
    if type(v) == "string" then return v end
    return nil
end

-- Busca una funcion global ("UnitName") o de tabla ("C_Map.GetMapInfo"). nil si no existe.
function ns.Get(path)
    local cur = _G
    for part in string.gmatch(path, "[^%.]+") do
        if type(cur) ~= "table" then return nil end
        cur = cur[part]
    end
    if type(cur) == "function" then return cur end
    return nil
end

-- Llama a la API si existe, protegida. Devuelve sus resultados o nada.
function ns.Call(path, ...)
    local fn = ns.Get(path)
    if fn == nil then return nil end
    return ns.SafeCall(fn, ...)
end

-- Llama a un metodo de un objeto (frame, textura) si existe, protegido.
function ns.Method(obj, name, ...)
    if obj == nil then return nil end
    local m = ns.SafeCall(function() return obj[name] end)
    if type(m) ~= "function" then return nil end
    return ns.SafeCall(m, obj, ...)
end

-- Crea un frame si la API existe. Devuelve el frame o nil.
function ns.NewFrame(kind, name, parent)
    return ns.Call("CreateFrame", kind, name, parent)
end

-- Tiempo actual en segundos (time() de WoW; os.time como respaldo).
function ns.Now()
    local t = ns.Num(ns.Call("time"))
    if t then return t end
    if type(os) == "table" and type(os.time) == "function" then return os.time() end
    return 0
end

function ns.PlayerName()
    return ns.Str(ns.Call("UnitName", "player"))
end

-- Reino normalizado sin espacios (coincide entre llamadas).
function ns.RealmName()
    local r = ns.Str(ns.Call("GetNormalizedRealmName")) or ns.Str(ns.Call("GetRealmName"))
    if r then r = (string.gsub(r, "%s+", "")) end
    return r
end

-- Clave de personaje "Nombre-Reino". nil si no hay nombre.
function ns.CharKey()
    local name = ns.PlayerName()
    if not name then return nil end
    local realm = ns.RealmName()
    if not realm or realm == "" then return name end
    return name .. "-" .. realm
end

-- Parsea un GUID de WoW. Devuelve tipo ("Creature", "Player", ...) y el
-- campo 6 numerico (npcID para Creature/Vehicle). Secretos -> nil, nil.
function ns.ParseGUID(guid)
    guid = ns.Str(guid)
    if not guid then return nil, nil end
    local parts = {}
    for p in string.gmatch(guid, "[^-]+") do parts[#parts + 1] = p end
    return parts[1], tonumber(parts[6])
end

-- Entero acotado. Valores no numericos o NaN devuelven default.
function ns.ClampInt(v, lo, hi, default)
    if type(v) ~= "number" or v ~= v then return default end
    v = floor(v)
    if v < lo then return lo end
    if v > hi then return hi end
    return v
end

-- Anade un byte (0..255, nil -> 0) al array out.
function ns.AppendU8(out, v)
    out[#out + 1] = ns.ClampInt(v, 0, 255, 0)
end

-- Anade uint16 big-endian (nil -> 0).
function ns.AppendU16(out, v)
    v = ns.ClampInt(v, 0, 65535, 0)
    out[#out + 1] = bit.band(bit.rshift(v, 8), 0xFF)
    out[#out + 1] = bit.band(v, 0xFF)
end

-- Anade uint32 big-endian (nil -> 0, clamp a 2^32-1).
function ns.AppendU32(out, v)
    v = ns.ClampInt(v, 0, 4294967295, 0)
    out[#out + 1] = bit.band(bit.rshift(v, 24), 0xFF)
    out[#out + 1] = bit.band(bit.rshift(v, 16), 0xFF)
    out[#out + 1] = bit.band(bit.rshift(v, 8), 0xFF)
    out[#out + 1] = bit.band(v, 0xFF)
end

-- CRC-8: poly 0x07, init 0x00, MSB primero, sin reflexion ni xorout.
-- bytes: array de enteros 0..255; calcula sobre bytes[first..last].
function ns.CRC8(bytes, first, last)
    first = first or 1
    last = last or #bytes
    local crc = 0
    for i = first, last do
        crc = bit.bxor(crc, bit.band(bytes[i], 0xFF))
        for _ = 1, 8 do
            if bit.band(crc, 0x80) ~= 0 then
                crc = bit.band(bit.bxor(bit.lshift(crc, 1), 0x07), 0xFF)
            else
                crc = bit.band(bit.lshift(crc, 1), 0xFF)
            end
        end
    end
    return crc
end
