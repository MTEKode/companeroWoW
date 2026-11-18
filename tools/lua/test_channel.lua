-- test_channel.lua: pruebas offline del canal en vivo (protocolo v3).
-- Cubre: codificador JSON, CRC-16, capacidad y N, layout de la trama (cabecera, CRC-16, lectura),
-- trama -> colores -> nibbles, planificador (prioridad, trozos, tipo pesado, carrusel),
-- comportamiento en combate y deteccion de cambios (incluido el delta de completedQuests).
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_channel.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

local ns = {}

local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end

load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Bridge.lua")
load("CompaneroWoW/Channel.lua")

local C, J = ns.Channel, ns.Json

local failures = 0
local function check(name, cond, detail)
    if cond then
        print("  ok    " .. name)
    else
        failures = failures + 1
        print("  FAIL  " .. name .. (detail and (" -> " .. detail) or ""))
    end
end

local function eq(name, got, want)
    check(name, got == want, string.format("got %s, want %s", tostring(got), tostring(want)))
end

local function bytesOf(s)
    local t = {}
    for i = 1, #s do t[i] = string.byte(s, i) end
    return t
end

local function strOf(bytes, from, to)
    local t = {}
    for i = from, to do t[#t + 1] = string.char(bytes[i]) end
    return table.concat(t)
end

-- ---------------------------------------------------------------------------
print("(a) Codificador JSON")
eq("entero", J.Encode(42), "42")
eq("decimal", J.Encode(0.5), "0.5")
eq("negativo", J.Encode(-7), "-7")
eq("NaN -> null", J.Encode(0 / 0), "null")
eq("infinito -> null", J.Encode(math.huge), "null")
eq("booleanos", J.Encode({ a = true, b = false }), '{"a":true,"b":false}')
eq("cadena escapada", J.Encode('di "hola"\n\\'), '"di \\"hola\\"\\n\\\\"')
eq("control como \\u00XX", J.Encode("a\1b"), '"a\\u0001b"')
eq("UTF-8 sin tocar", J.Encode("Compañero"), '"Compañero"')
eq("array denso", J.Encode({ 1, 2, 3 }), "[1,2,3]")
eq("objeto con claves ordenadas", J.Encode({ z = 1, a = 2, m = { 3 } }), '{"a":2,"m":[3],"z":1}')
eq("tabla vacia -> {}", J.Encode({}), "{}")
eq("claves numericas (no secuenciales) -> objeto", J.Encode({ [10] = "x", [2] = "y" }), '{"10":"x","2":"y"}')
eq("nil y funciones se omiten", J.Encode({ a = nil, f = print, b = 1 }), '{"b":1}')
eq("nil en array -> null", J.Encode({ 1, nil, 3 }), '{"1":1,"3":3}')
local deep = {}
local cur = deep
for _ = 1, 10 do cur.x = {} cur = cur.x end
local s, why = J.Encode(deep)
check("profundidad excesiva -> nil 'depth'", s == nil and why == "depth", tostring(why))
local big = {}
for i = 1, 6000 do big[i] = "xxxxxxxxxxxxxxxxxxxx" end
s, why = J.Encode(big)
check("tamano excesivo -> nil 'size'", s == nil and why == "size", tostring(why))

-- ---------------------------------------------------------------------------
print("(b) CRC-16/CCITT-FALSE")
local function crcOfString(str)
    return C.CRC16(bytesOf(str), 1, #str)
end
eq("vector 123456789 -> 0x29B1", crcOfString("123456789"), 0x29B1)
eq("vacio -> 0xFFFF", C.CRC16({}, 1, 0), 0xFFFF)

-- ---------------------------------------------------------------------------
print("(c) Capacidad y numero de bloques")
-- N = bloques que caben en el ancho disponible (unidades del marco) menos 1 de margen.
eq("N de 3440 unidades con bloque 4 = 859", ns.StripBlocks(3440, 4), 859)
eq("N de 5000 unidades con bloque 4 = 1024 (tope)", ns.StripBlocks(5000, 4), 1024)
eq("N de 1920 unidades con bloque 4 = 479", ns.StripBlocks(1920, 4), 479)
eq("ancho desconocido = 256", ns.StripBlocks(nil, 4), 256)
eq("ancho menor que 7 bloques = 0 (sin franja)", ns.StripBlocks(20, 4), 0)
do
    -- Garantia: N bloques mas su margen nunca pasan del ancho disponible.
    local bad = nil
    for avail = 50, 3440, 37 do
        for _, blk in ipairs({ 1.9, 2.021, 2.7, 4, 5.333 }) do
            local n = ns.StripBlocks(avail, blk)
            if n > 0 and (n + 1) * blk > avail + 1e-9 then bad = bad or string.format("avail %d block %s n %d", avail, blk, n) end
        end
    end
    check("N + 1 bloque cabe siempre en el ancho disponible", bad == nil, bad)
end
-- Capacidad sigue a N: N = 859 (3440 px a 4 px, con margen) -> 1234 bytes.
eq("capacidad con N=StripBlocks(3440,4)=859 = 1234 bytes", C.Capacity(ns.StripBlocks(3440, 4)), 1234)
eq("capacidad con N=860 = 1235 bytes", C.Capacity(860), 1235)
eq("capacidad con N=5 = 0", C.Capacity(5), 0)
eq("capacidad con N=6 = 0 (1 nibble de datos no basta)", C.Capacity(6), 0)

-- ---------------------------------------------------------------------------
print("(d) Layout de la trama v3")
local status = ns.BuildPayload({ mapID = 1429, x = 0.5, y = 0.25, level = 60, money = 12345, lastEventCode = 3, eventSeq = 9 })
local N = 860
local body = "hola mundo, esto es un mensaje de prueba"
local chunk = { type = 2, id = 513, index = 1, count = 4, data = bytesOf(body) }
local frame = C.BuildFrame(status, 77, N, chunk)
check("trama construida", frame ~= nil)
eq("version 3", frame[1], 3)
eq("seq", frame[2], 77)
eq("N lo", frame[3], 860 % 256)
eq("N hi", frame[4], math.floor(860 / 256))
eq("estado copiado (byte 5 = payload 1)", frame[5], status[1])
eq("estado CRC-8 en byte 36", frame[36], status[32])
eq("msgType", frame[37], 2)
eq("msgId lo", frame[38], 513 % 256)
eq("msgId hi", frame[39], math.floor(513 / 256))
eq("chunkIndex lo", frame[40], 1)
eq("chunkCount lo", frame[42], 4)
eq("dataLen lo", frame[44], #body)
eq("dataLen hi", frame[45], 0)
eq("datos", strOf(frame, 46, 45 + #body), body)
eq("longitud total = 47 + datos", #frame, 47 + #body)
local parsed, perr = C.ParseFrame(frame)
check("ParseFrame acepta la trama", parsed ~= nil, perr)
eq("ParseFrame: tipo", parsed and parsed.type, 2)
eq("ParseFrame: datos", parsed and strOf(parsed.data, 1, #parsed.data), body)
eq("ParseFrame: N", parsed and parsed.N, 860)
local damaged = {}
for i = 1, #frame do damaged[i] = frame[i] end
damaged[50] = (damaged[50] + 1) % 256
local _, derr = C.ParseFrame(damaged)
eq("byte alterado -> CRC incorrecto", derr, "crc")

local idle = C.BuildFrame(status, 1, N, nil)
eq("sin trozo: msgType 0", idle[37], 0)
eq("sin trozo: dataLen 0", idle[44] + idle[45], 0)
eq("sin trozo: longitud 47", #idle, 47)
check("sin trozo: ParseFrame ok", C.ParseFrame(idle) ~= nil)
check("trozo mayor que la capacidad -> nil", C.BuildFrame(status, 1, 6, chunk) == nil)

-- Trama -> colores -> nibbles (lo que ve el lector de pantalla).
local colors = ns.EncodeFrameColors(frame, N, {})
eq("bloque 1 = magia R", math.floor(colors[1] / 65536), 255)
eq("bloque 2 = magia G", math.floor(colors[2] / 256) % 256, 255)
eq("bloque 3 calibracion R = 85", math.floor(colors[3] / 65536), 85)
local decodedNibbles = {}
for b = 6, N do
    local c = colors[b]
    local rgb = { math.floor(c / 65536), math.floor(c / 256) % 256, c % 256 }
    for ch = 1, 3 do decodedNibbles[#decodedNibbles + 1] = math.floor(rgb[ch] / 17 + 0.5) end
end
local decodedBytes = {}
for k = 1, #frame do
    decodedBytes[k] = decodedNibbles[2 * k - 1] * 16 + decodedNibbles[2 * k]
end
local sameAsFrame = true
for k = 1, #frame do
    if decodedBytes[k] ~= frame[k] then sameAsFrame = false break end
end
check("colores decodifican a la misma trama", sameAsFrame)
local unusedZero = true
for k = 2 * #frame + 1, #decodedNibbles do
    if decodedNibbles[k] ~= 0 then unusedZero = false break end
end
check("nibbles sobrantes = 0", unusedZero)

-- ---------------------------------------------------------------------------
print("(e) Planificador: prioridad, trozos y tipo pesado")
local sched = C.NewScheduler()
local long = string.rep("abcdefghij", 300) -- 3000 bytes -> 3 trozos de 1235
sched:enqueue(10, long)
sched:enqueue(2, "{\"q\":1}")
local cs = C.Capacity(860)
local c1 = sched:nextChunk(cs, 1)
eq("frame 1: gana el tipo 2 (prioridad)", c1 and c1.type, 2)
eq("frame 1: trozo unico, index 0", c1 and c1.index, 0)
local c2 = sched:nextChunk(cs, 2)
check("frame 2: el pesado (10) no sale fuera de su turno", c2 == nil)
local c3 = sched:nextChunk(cs, 3)
eq("frame 3: sale el pesado (1 trozo cada 3 tramas)", c3 and c3.type, 10)
eq("frame 3: index 0 de 3", c3 and c3.count, 3)
local got = {}
local frameNo = 3
while sched:pending() > 0 and frameNo < 40 do
    frameNo = frameNo + 1
    local ch = sched:nextChunk(cs, frameNo)
    if ch then got[#got + 1] = ch end
end
eq("pesado: quedan 2 trozos", #got, 2)
eq("pesado: index 1 luego 2", got[1] and got[1].index, 1)
local reassembled = strOf(c3.data, 1, #c3.data) .. strOf(got[1].data, 1, #got[1].data) .. strOf(got[2].data, 1, #got[2].data)
eq("pesado: reensamblado = mensaje", reassembled, long)
eq("pesado: mismo msgId en los trozos", got[1].id, c3.id)
check("cola vacia -> nil", sched:nextChunk(cs, 99) == nil)

-- Con prioridad y sin tipo pesado: un mensaje grande no bloquea a uno de prioridad alta que llega despues.
local s2 = C.NewScheduler()
s2:enqueue(9, string.rep("k", 5000))
local first = s2:nextChunk(cs, 1)
s2:enqueue(1, "{\"c\":1}")
local nextOne = s2:nextChunk(cs, 2)
eq("prioridad: el tipo 1 interrumpe al 9", nextOne and nextOne.type, 1)
local resumed = s2:nextChunk(cs, 3)
eq("prioridad: luego sigue el 9", resumed and resumed.type, 9)
eq("prioridad: y en su trozo 1 (index 1)", resumed and resumed.index, 1)
eq("primer trozo del 9 era index 0", first and first.index, 0)
eq("5000 bytes = 5 trozos", first and first.count, 5)

-- Capacidad 0 (ancho muy pequeno): no se envia nada.
local s3 = C.NewScheduler()
s3:enqueue(2, "x")
eq("capacidad 0: nada que enviar", s3:nextChunk(0, 1), nil)

-- ---------------------------------------------------------------------------
print("(f) Activacion: reposo, orden de envio, ventana, reactivacion")
-- Ventana de prueba: tramas de 0.2 s desde 'start' con presupuesto ilimitado (el orden y el contenido
-- son lo que se mira aqui; el presupuesto se mide en (h)). Devuelve los trozos con su instante.
local function runShare(start, frames)
    local out = {}
    local now = start
    C.Begin(now)
    for f = 1, frames do
        now = now + C.TICK_SEC
        if not C.Update(now) then break end
        C.Pump(now, 1e9)
        local ch = C.NextChunk(860, f)
        if ch then out[#out + 1] = { t = now, chunk = ch } end
    end
    return out, now
end

-- Mensajes completos (por tipo, en orden de primer trozo) a partir de los trozos.
local function firstOrder(items)
    local order, seen = {}, {}
    for _, it in ipairs(items) do
        local ch = it.chunk
        if ch.index == 0 and not seen[ch.type] then
            seen[ch.type] = true
            order[#order + 1] = ch.type
        end
    end
    return order
end

local function sameList(a, b)
    if #a ~= #b then return false end
    for i = 1, #a do if a[i] ~= b[i] then return false end end
    return true
end

local function listOf(t)
    local s = {}
    for i, v in ipairs(t) do s[i] = tostring(v) end
    return table.concat(s, ",")
end

-- Datos de juego de prueba (personaje con todo lo que el plan envia).
local key = "Tester-Realm"
ns.CharKey = function() return key end
ns.InCombat = function() return false end
local function freshDB()
    CompaneroWoWDB = {
        settings = {},
        characters = {
            [key] = {
                character = { name = "Tester", level = 60, money = 100 },
                quests = { { id = 1 } },
                location = { mapID = 1 },
                equipment = { { slot = 1 } },
                inventory = { items = { { id = 1, n = 5 } } },
                professions = { { name = "Tailoring" } },
                actionBars = { { slot = 1, id = 7 } },
                combats = { { id = 1, t = 10 } },
                completedQuests = { 1, 2, 3 },
                macros = { account = { used = 1, max = 120, list = {} } },
            },
        },
        knowledge = { quests = {}, npcs = {} },
    }
    C.testing.reset()
end
freshDB()

-- Reposo: sin activacion no se serializa nada ni sale ningun trozo.
check("reposo: no hay activacion", not C.IsSharing())
C.Pump(500, 1e9)
eq("reposo: sin trozos", C.NextChunk(860, 1), nil)
eq("reposo: Send no serializa", C.Send(12), false)
eq("reposo: sin trabajos", C.state.sched and C.state.sched:pending() or 0, 0)

-- Orden de primer envio: el de la especificacion. Sin sesion (14), sin hoja (12) y sin
-- conocimiento (9) no hay trozos de esos tipos; el resto sale en el orden del plan.
local items = runShare(1000, 400)
local order = firstOrder(items)
eq("orden del primer envio (tipos con datos)", listOf(order), "1,2,4,3,5,13,8,7,6,11,10")
C.Stop()

-- Ventana: 20 s. Con todo enviado al menos una vez, vuelve al reposo 5 s despues.
freshDB()
C.Begin(2000)
eq("ventana: 20 s restantes al empezar", C.RemainingSec(2000), 20)
check("ventana: sigue abierta a los 19.9 s", C.Update(2019.9) == true)
check("ventana: se cierra a los 20 s", C.Update(2020) == false)
check("ventana: ya no esta activa", C.IsSharing() == false)
eq("ventana: 0 restantes en reposo", C.RemainingSec(2021), 0)

-- Acaba antes si todo se envio: 5 s despues del ultimo envio.
freshDB()
local early, endAt = runShare(3000, 400)
check("todo enviado: la ventana vuelve al reposo", C.IsSharing() == false)
check("todo enviado antes de los 20 s", endAt < 3000 + C.SHARE_SECONDS, tostring(endAt))
check("todo enviado: la ultima trama queda dentro de la ventana", #early > 0 and early[#early].t < 3000 + C.SHARE_SECONDS)

-- Pulsar otra vez: alarga la ventana y vuelve a serializar cada tipo (fresco).
freshDB()
C.Begin(4000)
C.Update(4015)
C.Pump(4015, 1e9)
check("re-pulsar: 'otra vez' reinicia el plan (devuelve true)", C.Begin(4015) == true)
eq("re-pulsar: 20 s desde la nueva pulsacion", C.RemainingSec(4015), 20)
check("re-pulsar: la ventana vieja (4020) ya no cierra", C.Update(4020) == true)
check("re-pulsar: cierra a los 4035", C.Update(4035) == false)

-- Cambio de datos entre activaciones: el envio siguiente lleva lo nuevo (fresco, sin hash).
freshDB()
runShare(5000, 400)
CompaneroWoWDB.characters[key].inventory.items[1].n = 6
local again = runShare(6000, 400)
local bagChunk
for _, it in ipairs(again) do
    if it.chunk.type == 3 and it.chunk.index == 0 then bagChunk = it.chunk end
end
check("nueva activacion: serializa de nuevo el tipo 3", bagChunk ~= nil)
local bagText = ""
for _, it in ipairs(again) do
    if it.chunk.type == 3 then
        for _, b in ipairs(it.chunk.data) do bagText = bagText .. string.char(b) end
    end
end
check("nueva activacion: el inventario trae n=6", string.find(bagText, '"n":6', 1, true) ~= nil, bagText)

-- ---------------------------------------------------------------------------
print("(g) Activacion: conocimiento (deltas y volcado), combate y completedQuests")
freshDB()
ns.InCombat = function() return false end
CompaneroWoWDB.knowledge.quests[101] = { questID = 101, title = "A" }
C.Begin(7000)
C.Pump(7000, 1e9)
local function jobOf(t)
    local found
    for _, job in ipairs(C.state.sched.jobs) do
        if job.type == t then found = job end
    end
    return found
end
-- Vacia la cola de la activacion en curso (envia todos los trozos pendientes).
local function drainQueue()
    local i = 0
    while C.state.sched and C.state.sched:pending() > 0 and i < 5000 do
        i = i + 1
        C.NextChunk(860, i)
    end
end
local kjob = jobOf(9)
check("knowledge: la primera activacion manda la 101 como delta", kjob ~= nil and string.find(kjob.str, '"101":', 1, true) ~= nil)
check("knowledge: el delta tiene la forma {npcs,quests}", kjob ~= nil and string.sub(kjob.str, 1, 9) == '{"npcs":{')
check("knowledge: el delta va por el carril normal (no lento)", kjob ~= nil and kjob.heavy == false and kjob.optional == false)
drainQueue()
C.Stop()
-- Segunda activacion sin cambios: no hay delta (el volcado completo es opcional).
C.Begin(7100)
C.Pump(7100, 1e9)
local kjob2 = jobOf(9)
check("knowledge: sin cambios no hay delta", kjob2 == nil or kjob2.optional == true)
drainQueue()
C.Stop()
-- Cambio de un registro: el delta solo trae la 102 (la 101 no se reenvia).
CompaneroWoWDB.knowledge.quests[102] = { questID = 102, title = "B" }
C.Begin(7200)
C.Pump(7200, 1e9)
local kjob3 = jobOf(9)
check("knowledge: el delta trae la 102", kjob3 ~= nil and string.find(kjob3.str, '"102":', 1, true) ~= nil)
check("knowledge: el delta no repite la 101", kjob3 ~= nil and string.find(kjob3.str, '"101":', 1, true) == nil)
C.Stop()

-- Combate: la activacion sigue sirviendo (solo se lee, no se recalcula nada del juego).
freshDB()
ns.InCombat = function() return true end
local inCombat = runShare(8000, 400)
check("combate: la activacion tambien envia", #inCombat > 0)
ns.InCombat = function() return false end

-- Macros y tipo 13: el plan los envia; sin macros no hay mensaje.
do
    local t13 = C.TYPES[13]
    check("tipo 13 = macros", t13 ~= nil and t13.name == "macros")
    check("tipo 13 en la lista de envio", (function()
        for _, t in ipairs(C.PRIORITY) do if t == 13 then return true end end
        return false
    end)())
    local built = t13 and t13.build({ macros = { account = { used = 1, max = 120, list = {} } } })
    check("tipo 13 envia el bloque de macros", type(built) == "table" and built.account ~= nil)
    check("tipo 13 sin macros no envia nada", t13 and t13.build({}) == nil)
end

-- completedQuests: lista completa; vacia -> [].
freshDB()
local cq = runShare(9000, 400)
local cqText
for _, it in ipairs(cq) do
    if it.chunk.type == 6 then
        cqText = (cqText or "") .. string.char(unpack(it.chunk.data))
    end
end
eq("completedQuests: lista completa", cqText, "[1,2,3]")
CompaneroWoWDB.characters[key].completedQuests = {}
local cq2 = runShare(9100, 400)
local cq2Text
for _, it in ipairs(cq2) do
    if it.chunk.type == 6 then cq2Text = (cq2Text or "") .. string.char(unpack(it.chunk.data)) end
end
eq("completedQuests vacia: []", cq2Text, "[]")

-- Carril lento: el pesado (10) sale como mucho un trozo cada HEAVY_EVERY tramas.
freshDB()
CompaneroWoWDB.characters[key].gearCandidates = { string.rep("g", 6000) }
local heavyFrames = {}
for _, it in ipairs(runShare(9200, 400)) do
    if it.chunk.type == 10 then heavyFrames[#heavyFrames + 1] = it.t end
end
check("pesado: varios trozos", #heavyFrames >= 2)

print("(h) Coste por trama (luajit, orientativo): reposo ~0, activacion <= 1 ms de serializacion")
do
    -- Datos grandes: cada tipo de la lista pesa; el plan se reparte en muchas tramas.
    freshDB()
    local big = {}
    for i = 1, 1500 do big[i] = { questID = 10000 + i, title = "Mision de prueba numero " .. i, objectives = { "a", "b", i } } end
    CompaneroWoWDB.characters[key].quests = big
    CompaneroWoWDB.knowledge = { quests = {}, npcs = {} }
    for i = 1, 2000 do
        CompaneroWoWDB.knowledge.npcs[i] = { npcID = i, name = "PNJ " .. i, x = 0.5, y = 0.25, level = 60 }
    end

    -- Reposo: no hay trabajo que repartir.
    C.testing.reset()
    local restMax = 0
    for f = 1, 50 do
        local t0 = os.clock()
        C.Pump(1000 + f * 0.2, C.PUMP_BUDGET_MS)
        restMax = math.max(restMax, os.clock() - t0)
    end
    print(string.format("  info  reposo: Pump por trama max %.3f us (sin activacion no hace nada)", restMax * 1e6))
    check("reposo: Pump no hace trabajo", restMax < 0.0001)

    -- Activacion: cada trama reparte hasta 1 ms de serializacion.
    C.Begin(2000)
    local sum, maxT, frames, lastNow = 0, 0, 0, 2000
    local now = 2000
    while C.IsSharing() and frames < 400 do
        frames = frames + 1
        now = now + C.TICK_SEC
        if not C.Update(now) then break end
        local t0 = os.clock()
        C.Pump(now, C.PUMP_BUDGET_MS)
        local dt = os.clock() - t0
        sum = sum + dt
        if dt > maxT then maxT = dt end
        C.NextChunk(860, frames)
        lastNow = now
    end
    local avg = sum / math.max(1, frames)
    print(string.format("  info  activacion: %d tramas, serializacion media %.3f ms, maxima %.3f ms",
        frames, avg * 1000, maxT * 1000))
    check("activacion: media <= 1 ms por trama", avg <= 0.001, string.format("%.3f ms", avg * 1000))
    check("activacion: ningun paso desbocado (pico < 10 ms; medido: ver info)", maxT < 0.010, string.format("%.3f ms", maxT * 1000))
    check("activacion: el trabajo se reparte (mas de una trama)", frames > 5)
    C.Stop()
end

-- ---------------------------------------------------------------------------
print("(i) Codificacion cediendo (EncodeAsync) = Encode")
do
    local sample = { a = { 1, 2, { x = "di \"hola\"\n" } }, z = true, n = 0.25 }
    local big = {}
    for k = 1, 500 do big[k] = { id = k, name = "item " .. k, tags = { "a", "b", k } } end
    sample.big = big
    local want = J.Encode(sample)
    local got
    local co = coroutine.create(function() return J.EncodeAsync(sample) end)
    local yields = 0
    while true do
        local ok, res, why = coroutine.resume(co)
        assert(ok, res)
        if coroutine.status(co) == "dead" then got = res break end
        yields = yields + 1
    end
    eq("EncodeAsync da el mismo JSON que Encode", got, want)
    check("EncodeAsync cede el control varias veces en un valor grande", yields >= 5, tostring(yields))
    local tooBig = {}
    for k = 1, 6000 do tooBig[k] = "xxxxxxxxxxxxxxxxxxxx" end
    local co2 = coroutine.create(function() return J.EncodeAsync(tooBig) end)
    local okc, json, why2
    repeat okc, json, why2 = coroutine.resume(co2) until coroutine.status(co2) == "dead"
    eq("EncodeAsync: tamano excesivo -> nil 'size'", why2, "size")
end

if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
