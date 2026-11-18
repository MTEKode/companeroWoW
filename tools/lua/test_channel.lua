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
print("(f) Carrusel")
local sc = C.NewScheduler()
local provided = {}
local function provider(t)
    if t == 1 or t == 2 then return "{\"t\":" .. t .. "}" end
    return nil
end
check("carrusel inicial reenvia", sc:carousel(0, provider))
eq("carrusel: 2 mensajes encolados", sc:pending(), 2)
check("carrusel: no repite antes de 15 s", sc:carousel(14, provider) == false)
local pendingId = sc.jobs[1].id
check("carrusel: con trabajo pendiente no lo reinicia", sc:carousel(16, provider) and sc.jobs[1].id == pendingId and sc:pending() == 2)
sc:nextChunk(cs, 1)
sc:nextChunk(cs, 1)
eq("carrusel: cola vacia tras enviar los 2", sc:pending(), 0)
check("carrusel: repite a los 15 s aunque la cola no este llena", sc:carousel(31, provider))
eq("carrusel: vuelve a encolar 2", sc:pending(), 2)
local hs = C.NewScheduler()
hs:carousel(0, function(t) return "{\"t\":" .. t .. "}" end)
check("carrusel: el pesado (10) espera 60 s", hs:carouselDue(10, 59) == false and hs:carouselDue(10, 60))
check("carrusel: los demas tipos, 15 s", hs:carouselDue(1, 14.9) == false and hs:carouselDue(1, 15))

-- ---------------------------------------------------------------------------
print("(g) Canal de juego: cambios, delta y combate")
local key = "Tester-Realm"
ns.CharKey = function() return key end
ns.InCombat = function() return false end
CompaneroWoWDB = {
    settings = {},
    characters = {
        [key] = {
            character = { name = "Tester", level = 60, money = 100 },
            inventory = { items = { { id = 1, n = 5 } } },
            completedQuests = { 1, 2, 3 },
            combats = {},
        },
    },
    knowledge = { quests = {}, npcs = {} },
}

local function settle(now)
    for i = 1, 60 do C.Tick(now + i * 0.2, 860) end
end

local function lastJobOf(t)
    local found
    for _, job in ipairs(C.state.sched.jobs) do
        if job.type == t then found = job end
    end
    return found
end

-- Combate: no se revisan cambios (el carrusel si corre, un tipo por tick).
ns.InCombat = function() return true end
C.Tick(1000, 860)
check("combate: no calcula tipos", next(C.state.hash) == nil)

ns.InCombat = function() return false end
settle(1000)
check("fuera de combate: calcula tipos", C.state.hash[3] ~= nil and C.state.hash[2] ~= nil)

-- Cambio de inventario: nuevo mensaje solo si cambia el JSON.
local before = C.state.hash[3]
C.testing.refreshType(3, 2000)
eq("sin cambios: mismo hash", C.state.hash[3], before)
CompaneroWoWDB.characters[key].inventory.items[1].n = 6
C.testing.refreshType(3, 2100)
check("cambio de inventario: nuevo hash", C.state.hash[3] ~= before)
local bagJob = lastJobOf(3)
check("cambio de inventario: encola el tipo 3", bagJob ~= nil and string.find(bagJob.str, '"n":6', 1, true) ~= nil)

-- completedQuests: completo primero, luego delta {add}, y completo si algo desaparece.
C.state.completedSet = nil
C.state.hash[6] = nil
C.testing.refreshType(6, 3000)
eq("completedQuests: primer envio completo", lastJobOf(6).str, "[1,2,3]")
CompaneroWoWDB.characters[key].completedQuests = { 1, 2, 3, 4 }
C.testing.refreshType(6, 3100)
eq("completedQuests: delta con lo nuevo", lastJobOf(6).str, '{"add":[4]}')
CompaneroWoWDB.characters[key].completedQuests = { 1, 3 }
C.testing.refreshType(6, 3200)
eq("completedQuests: si algo sale -> completo", lastJobOf(6).str, "[1,3]")

-- Knowledge: revision por tandas; solo entradas nuevas o cambiadas, en {"npcs":{},"quests":{...}}.
CompaneroWoWDB.knowledge.quests[101] = { questID = 101, title = "A" }
C.testing.refreshType(9, 4000)
C.testing.scanAll()
C.testing.knowledgeStep(4000)
local kjob = lastJobOf(9)
check("knowledge: la primera vez incluye la 101", kjob ~= nil and string.find(kjob.str, '"101":', 1, true) ~= nil)
check("knowledge: el delta tiene la forma {npcs,quests}", kjob ~= nil and string.sub(kjob.str, 1, 9) == '{"npcs":{')
check("knowledge: el delta va por el carril normal (no lento)", kjob ~= nil and kjob.heavy == false)
CompaneroWoWDB.knowledge.quests[102] = { questID = 102, title = "B" }
C.state.sched.jobs = {}
C.testing.refreshType(9, 4100)
C.testing.scanAll()
C.testing.knowledgeStep(4100)
kjob = lastJobOf(9)
check("knowledge: el delta solo trae la 102", kjob ~= nil and string.find(kjob.str, '"102":', 1, true) ~= nil
    and string.find(kjob.str, '"101":', 1, true) == nil)
-- Sin cambios: la revision no encola nada.
C.state.sched.jobs = {}
C.testing.refreshType(9, 4200)
C.testing.scanAll()
C.testing.knowledgeStep(4200)
check("knowledge: sin cambios no encola", lastJobOf(9) == nil)

-- Combate en medio: se siguen enviando trozos ya encolados.
C.state.sched.jobs = {}
C.state.sched:enqueue(2, "{\"x\":1}")
C.state.sched.carouselAt = 5000 -- sin ciclo de carrusel en este tick
C.state.carouselTodo = nil
ns.InCombat = function() return true end
local queued = C.Tick(5000, 860)
check("combate: sigue saliendo lo ya encolado", queued ~= nil and queued.type == 2)
ns.InCombat = function() return false end

-- Carrusel completo: con cola vacia se reenvian todos los tipos con datos.
C.state.sched.jobs = {}
C.state.sched.carouselAt = nil
local seenTypes = {}
for i = 1, 40 do
    local ch = C.Tick(6000 + i * 0.2, 860)
    if ch and ch.index == 0 then seenTypes[ch.type] = true end
end
local nTypes = 0
for _ in pairs(seenTypes) do nTypes = nTypes + 1 end
check("carrusel repartido: cada tipo con datos sale al menos una vez", nTypes >= 5, tostring(nTypes))

-- ---------------------------------------------------------------------------
print("(h) Medicion de coste (luajit, orientativa)")
do
    local status2 = ns.BuildPayload({ mapID = 1429, x = 0.5, y = 0.5 })
    local sch = C.state.sched
    local blob = string.rep("0123456789abcdef", 1000) -- 16 KB
    local iters = 200
    local clock = os.clock()
    local colorsOut = {}
    for i = 1, iters do
        sch:enqueue(2, blob)
        local ch = sch:nextChunk(C.Capacity(860), i)
        local fr = C.BuildFrame(status2, i % 256, 860, ch)
        colorsOut = ns.EncodeFrameColors(fr, 860, colorsOut)
    end
    local dt = (os.clock() - clock) / iters
    print(string.format("  info  Tick+trama+colores N=860: %.1f us por refresco (luajit, sin API de WoW)", dt * 1e6))
    check("coste por refresco < 5 ms en luajit", dt < 0.005)
end

if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
