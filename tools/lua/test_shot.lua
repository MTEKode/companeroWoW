-- test_shot.lua: canal de capturas (Shot.lua, T51). Cubre: geometria del panel (pequeno, grande,
-- varias capturas); tramas de captura (version 5, shotId, CRC, fila final); colores del panel que
-- decodifican a los bytes de la trama; CVar (guardar, fijar, restaurar, camino de fallo); division
-- en varias capturas (max 8, separadas 0.3 s); regla de combate; fallback sin Screenshot y bloqueado;
-- /compa canal y /compa estado; aviso de captura en el marco.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_shot.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

local failures = 0
local function outln(s) io.write(s, "\n") end
local function check(name, cond, detail)
    if cond then
        outln("  ok    " .. name)
    else
        failures = failures + 1
        outln("  FAIL  " .. name .. (detail and (" -> " .. detail) or ""))
    end
end
local function eq(name, got, want)
    check(name, got == want, string.format("got %s, want %s", tostring(got), tostring(want)))
end

-- ---------------------------------------------------------------------------
-- Entorno: reloj, temporizadores, frames y texturas falsos, CVar y Screenshot
-- ---------------------------------------------------------------------------
local clock = 1000
local inCombat = false
local chat = {}
_G.print = function(...)
    local parts = {}
    for i = 1, select("#", ...) do parts[#parts + 1] = tostring((select(i, ...))) end
    chat[#chat + 1] = table.concat(parts, " ")
end
local function chatHas(text)
    for _, m in ipairs(chat) do
        if string.find(m, text, 1, true) then return true end
    end
    return false
end

local timers = {}   -- { sec, fn } pendientes
_G.C_Timer = {
    After = function(sec, fn) timers[#timers + 1] = { sec = sec, fn = fn } end,
    NewTicker = function() return {} end,
}
-- Ejecuta los temporizadores de 'sec' segundos (en orden) hasta que no quede ninguno.
local function runTimers(sec)
    local ran, i = 0, 1
    while i <= #timers do
        if timers[i].sec == sec then
            local t = table.remove(timers, i)
            t.fn()
            ran = ran + 1
            i = 1
        else
            i = i + 1
        end
    end
    return ran
end
local function countTimers(sec)
    local n = 0
    for _, t in ipairs(timers) do if t.sec == sec then n = n + 1 end end
    return n
end

local texCreated = 0 -- texturas creadas (pool: no deben recrearse entre capturas)
local function newTexture()
    texCreated = texCreated + 1
    local t = { shown = true }
    function t:SetColorTexture(r, g, b, a) self.color = { r, g, b } end
    function t:SetSize(w, h) self.size = { w, h } end
    function t:SetPoint(...) self.point = { ... } end
    function t:Show() self.shown = true end
    function t:Hide() self.shown = false end
    function t:SetAllPoints() end
    function t:SetSnapToPixelGrid() end
    function t:SetTexelSnappingBias() end
    return t
end

local named = {}
local function newFrame(name)
    local f = { scripts = {}, shown = false, events = {}, texs = {}, alpha = 1, size = nil, strata = nil }
    function f:SetScript(n, fn) self.scripts[n] = fn end
    function f:Show() self.shown = true end
    function f:Hide() self.shown = false end
    function f:RegisterEvent(e) self.events[e] = true end
    function f:CreateTexture()
        local t = newTexture()
        self.texs[#self.texs + 1] = t
        return t
    end
    function f:SetSize(w, h) self.size = { w, h } end
    function f:SetPoint() end
    function f:SetFrameStrata(s) self.strata = s end
    function f:SetAlpha(a) self.alpha = a end
    function f:GetAlpha() return self.alpha end
    function f:GetEffectiveScale() return 1 end
    if name then named[name] = f end
    return setmetatable(f, { __index = function() return function(self) return self end end })
end

_G.CreateFrame = function(_, name) return newFrame(name) end
_G.UIParent = {
    GetWidth = function() return 1920 end,
    GetHeight = function() return 768 end,
    GetEffectiveScale = function() return 1 end,
}
_G.GetPhysicalScreenSize = function() return 1920, 1080 end
_G.GetTime = function() return clock end
_G.time = function() return 1700000000 end
_G.InCombatLockdown = function() return inCombat end
_G.SlashCmdList = {}

-- CVar: formatos aceptados por el cliente, modo de fallo y valores.
local cvars = { screenshotFormat = "jpg", screenshotQuality = "8" }
local supported = { png = true, tga = true, jpg = true }
local setMode = "normal"          -- "normal" | "throw" (SetCVar protegido)
_G.C_CVar = {
    GetCVar = function(name) return cvars[name] end,
    SetCVar = function(name, value)
        if setMode == "throw" then error("SetCVar protegido") end
        if name == "screenshotFormat" and not supported[value] then return end -- el cliente lo ignora
        cvars[name] = tostring(value)
    end,
}

-- Screenshot(): registra el formato y si el panel estaba visible en el instante de la captura.
local shotLog = {}
local screenshotMode = "normal"   -- "normal" | "throw" (bloqueado)
_G.Screenshot = function()
    local panel = named["CompaneroWoWShot"]
    shotLog[#shotLog + 1] = {
        format = cvars.screenshotFormat, quality = cvars.screenshotQuality,
        panelShown = panel and panel.shown or false,
        size = panel and panel.size or nil,
    }
    if screenshotMode == "throw" then error("Screenshot bloqueado") end
end

local function fireEvent(ok)
    local f = named["CompaneroWoWShotEvents"]
    if f and f.scripts.OnEvent then
        f.scripts.OnEvent(f, ok and "SCREENSHOT_SUCCEEDED" or "SCREENSHOT_FAILED")
    end
end

-- ---------------------------------------------------------------------------
-- Carga de modulos (como test_share.lua)
-- ---------------------------------------------------------------------------
local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Storage.lua")
load("CompaneroWoW/Collectors.lua")
load("CompaneroWoW/Bridge.lua")
load("CompaneroWoW/Channel.lua")
load("CompaneroWoW/Core.lua")
load("CompaneroWoW/Share.lua")
load("CompaneroWoW/Shot.lua")

local KEY = "Tester-Realm"
ns.CharKey = function() return KEY end
ns.Say = function(msg) chat[#chat + 1] = msg end
local C, Sh, Shot = ns.Channel, ns.Share, ns.Shot

-- Personaje con datos pequenos (o grandes, con big = true: varias capturas).
local function freshDB(big)
    texCreated = 0
    CompaneroWoWDB = nil
    C.testing.reset()
    Shot.testing.reset()
    ns.EnsureDB()
    local c = {
        character = { name = "Tester", level = 60, money = 100 },
        quests = { { id = 1 } },
        location = { mapID = 1 },
        equipment = { { slot = 1 } },
        inventory = { items = { { id = 1, n = 5 } } },
        professions = { { name = "Tailoring" } },
        actionBars = { { slot = 1, id = 7 } },
        completedQuests = { 1, 2, 3 },
        macros = { account = { used = 1, max = 120, list = {} } },
        sheet = { level = 60 },
    }
    if big then
        local quests, done, equip, items = {}, {}, {}, {}
        for i = 1, 1000 do quests[i] = { id = i, title = string.rep("m", 60) } end
        for i = 1, 14000 do done[i] = 10000 + i end
        for i = 1, 1000 do equip[i] = { slot = i % 16, id = 20000 + i, name = string.rep("e", 50) } end
        for i = 1, 1000 do items[i] = { id = 30000 + i, n = i, name = string.rep("i", 50) } end
        c.quests, c.completedQuests, c.equipment, c.inventory = quests, done, equip, { items = items }
    end
    CompaneroWoWDB.characters[KEY] = c
    CompaneroWoWDB.settings.strip = true
    cvars.screenshotFormat, cvars.screenshotQuality = "jpg", "8"
    supported = { png = true, tga = true, jpg = true }
    setMode, screenshotMode = "normal", "normal"
    shotLog = {}
    timers = {}
    chat = {}
    inCombat = false
end

ns.InitBridge()

-- ---------------------------------------------------------------------------
outln("geometria del panel (tope 4096 bloques por captura)")
-- ---------------------------------------------------------------------------
local g = Shot.Geometry({ 100 }, 2)
eq("payload pequeno: ancho minimo 256 px (N = 128)", g.W, 256)
eq("payload pequeno: N = 128 bloques", g.N, 128)
eq("payload pequeno: una fila de datos", g.rows, 1)
eq("payload pequeno: una captura", g.shots, 1)
eq("payload pequeno: 256 texturas (N x 2 filas)", g.blocks, 256)
local W, H = Shot.PanelSize(g.N, g.rows, g.blockPx)
eq("payload pequeno: marco 256 px de ancho", W, 256)
eq("payload pequeno: altura minima 64 px", H, 64)

g = Shot.Geometry({}, 2)
eq("sin datos: panel de 256 px", g.W, 256)
eq("sin datos: cero filas de datos", g.rows, 0)
eq("sin datos: una captura (estado)", g.shots, 1)

g = Shot.Geometry({ 4000 }, 2)
eq("4 KB (cambios tipicos): 512 px, una captura", g.W, 512)
check("4 KB: menos de 4096 texturas", g.blocks <= 4096, tostring(g.blocks))
W, H = Shot.PanelSize(g.N, g.rows, g.blockPx)
eq("4 KB: altura = max(64, (filas + final) x 2 px)", H, math.max(64, (g.rows + 1) * 2))

g = Shot.Geometry({ 20000 }, 2)
check("20 KB: una captura o varias, sin pasar de 8", g.shots <= 8, tostring(g.shots))
check("20 KB: texturas de la captura mayor <= 4096", g.blocks <= 4096, tostring(g.blocks))

g = Shot.Geometry({ 200000 }, 2)
eq("payload grande: 1024 px (N = 512)", g.W, 1024)
eq("payload grande: 7 filas de datos por captura", g.rowsPerShot, 7)
eq("payload grande: 4096 texturas como mucho", g.blocks, 4096)
check("payload grande: mas capturas de las que caben en un envio", g.shots > Shot.MAX_SHOTS, tostring(g.shots))

g = Shot.Geometry({ 100 }, 4)
eq("jpg (bloque 4 px): ancho 256 px = 64 bloques", g.N, 64)
eq("jpg: una captura", g.shots, 1)

-- ---------------------------------------------------------------------------
outln("tramas de captura (version 5)")
-- ---------------------------------------------------------------------------
local status = {}
for k = 1, 32 do status[k] = (k * 7) % 256 end
local data = {}
for k = 1, 50 do data[k] = (k * 13 + 5) % 256 end
local chunk = { type = 6, id = 513, index = 2, count = 5, data = data }
local f = Shot.BuildRow(status, 9, 128, 4242, chunk)
check("fila: construida", f ~= nil)
eq("fila: version 5", f[1], 5)
eq("fila: seq", f[2], 9)
eq("fila: N (LE)", f[3] + 256 * f[4], 128)
eq("fila: shotId (LE) en bytes 5-6", f[5] + 256 * f[6], 4242)
local statusOK = true
for k = 1, 32 do if f[6 + k] ~= status[k] then statusOK = false end end
check("fila: estado en bytes 7..38", statusOK)
eq("fila: msgType en byte 39", f[39], 6)
eq("fila: msgId en bytes 40-41", f[40] + 256 * f[41], 513)
eq("fila: chunkIndex en bytes 42-43", f[42] + 256 * f[43], 2)
eq("fila: chunkCount en bytes 44-45", f[44] + 256 * f[45], 5)
eq("fila: dataLen en bytes 46-47", f[46] + 256 * f[47], 50)
eq("fila: longitud = 49 + datos", #f, 49 + 50)
local p = Shot.ParseRow(f)
check("fila: CRC-16 valido (ParseRow)", p ~= nil)
if p then
    eq("fila: ParseRow shotId", p.shotId, 4242)
    eq("fila: ParseRow tipo", p.type, 6)
    local same = #p.data == 50
    for k = 1, 50 do if p.data[k] ~= data[k] then same = false end end
    check("fila: ParseRow datos iguales", same)
end
local bad = {}
for k, v in ipairs(f) do bad[k] = v end
bad[60] = (bad[60] + 1) % 256
local _, why = Shot.ParseRow(bad)
eq("fila: byte alterado -> CRC", why, "crc")
eq("capacidad N=128", Shot.Capacity(128), 132)
eq("capacidad N=512", Shot.Capacity(512), 708)
eq("trozo mayor que la fila -> nil", Shot.BuildRow(status, 0, 128, 1, { type = 1, id = 1, index = 0, count = 1, data = (function() local t = {} for k = 1, 133 do t[k] = 1 end return t end)() }), nil)

local chunks = {}
for i = 1, 3 do
    local d = {}
    for k = 1, 10 * i do d[k] = (i + k) % 256 end
    chunks[i] = { type = 2, id = 7, index = i - 1, count = 3, data = d }
end
local frames = Shot.PanelFrames(status, chunks, 128, 77)
eq("panel: tres filas de datos y una final", #frames, 4)
local last = Shot.ParseRow(frames[4])
eq("fila final: msgType 0", last.type, 0)
eq("fila final: dataLen 0", #last.data, 0)
eq("fila final: chunkCount = filas de datos", last.count, 3)
eq("fila final: shotId", last.shotId, 77)
eq("fila 2: seq 1", Shot.ParseRow(frames[2]).seq, 1)

-- ---------------------------------------------------------------------------
outln("colores del panel = bytes de la trama (v3.1)")
-- ---------------------------------------------------------------------------
-- Decodifica los colores de cada bloque de la fila (EncodeFrameColors) de vuelta a nibbles.
local NIB = {}
for n = 0, 15 do NIB[math.floor(n * 110 / 15 + 0.5)] = n end
local roundtripOK = true
for _, fr in ipairs(frames) do
    local colors = ns.EncodeFrameColors(fr, 128, {})
    -- bloques 1..7: magia (255,0,255), (0,255,255), calibracion y blanco
    local c1 = colors[1]
    if math.floor(c1 / 65536) ~= 255 or c1 % 256 ~= 255 then roundtripOK = false end
    for b = 8, 128 do
        local v = colors[b]
        local rr, gg, bb = math.floor(v / 65536), math.floor(v / 256) % 256, v % 256
        for c, val in ipairs({ rr, gg, bb }) do
            local k = (b - 8) * 3 + (c - 1)
            local j = math.floor(k / 2) + 1
            local want = 0
            if j <= #fr then
                want = (k % 2 == 0) and math.floor(fr[j] / 16) or (fr[j] % 16)
            end
            if NIB[val] ~= want then roundtripOK = false end
        end
    end
end
check("bloques de datos decodifican a los nibbles de cada fila", roundtripOK)

-- ---------------------------------------------------------------------------
outln("captura: CVar, panel y eventos")
-- ---------------------------------------------------------------------------
freshDB(false)
local now = 1000
local res = Shot.Start(now, {})
eq("captura: arranca", res, "started")
eq("captura: Screenshot aun no llamada (espera un frame)", #shotLog, 0)
local panel = named["CompaneroWoWShot"]
check("captura: panel mostrado antes de Screenshot", panel and panel.shown)
check("captura: panel de 256 px de ancho (en unidades del marco)",
    panel and panel.size and math.abs(panel.size[1] - 256 * 768 / 1080) < 1e-6, panel and panel.size and tostring(panel.size[1]))
runTimers(0)
eq("captura: Screenshot llamada tras un frame", #shotLog, 1)
eq("captura: formato png durante la captura", shotLog[1] and shotLog[1].format, "png")
check("captura: panel visible en el instante de Screenshot", shotLog[1] and shotLog[1].panelShown)
fireEvent(true)
eq("captura: SCREENSHOT_SUCCEEDED -> formato restaurado (jpg)", cvars.screenshotFormat, "jpg")
eq("captura: calidad restaurada (8)", cvars.screenshotQuality, "8")
eq("captura: panel oculto al terminar", panel and panel.shown, false)
eq("captura: una captura confirmada", Shot.sent, 1)
eq("captura: ultima captura con formato png", Shot.last and Shot.last.format, "png")
eq("captura: id de captura guardado", CompaneroWoWDB.settings.shotId, Shot.last and Shot.last.id)
check("captura: filas de datos > 0", Shot.last and Shot.last.rows >= 1)

-- Lineas de /compa estado
local lines = table.concat(Shot.StatusLines(), "\n")
check("estado: ultima captura con id", string.find(lines, "#%d+") ~= nil, lines)
check("estado: formato usado", string.find(lines, "formato png", 1, true) ~= nil, lines)
check("estado: filas y bytes", string.find(lines, "fila(s) de datos", 1, true) ~= nil, lines)

-- ---------------------------------------------------------------------------
outln("captura: formato de respaldo (solo jpg) y camino de fallo")
-- ---------------------------------------------------------------------------
freshDB(false)
supported = { jpg = true }
res = Shot.Start(now, {})
eq("solo jpg: arranca", res, "started")
runTimers(0)
eq("solo jpg: Screenshot con formato jpg", shotLog[1] and shotLog[1].format, "jpg")
eq("solo jpg: calidad 10 durante la captura", shotLog[1] and shotLog[1].quality, "10")
fireEvent(false)
eq("fallo SCREENSHOT_FAILED: formato restaurado", cvars.screenshotFormat, "jpg")
eq("fallo: calidad restaurada", cvars.screenshotQuality, "8")
eq("fallo: contado como error", Shot.errors, 1)
check("fallo: aviso en el chat", chatHas("SCREENSHOT_FAILED"))

freshDB(false)
setMode = "throw"
res = Shot.Start(now, {})
eq("SetCVar protegido: no hay formato -> unavailable", res, "unavailable")
eq("SetCVar protegido: CVar intacta", cvars.screenshotFormat, "jpg")
setMode = "normal"

freshDB(false)
supported = { png = true, tga = true, jpg = true }
screenshotMode = "throw"
res = Shot.Start(now, {})
eq("Screenshot protegido: arranca y dispara", res, "started")
runTimers(0)
eq("Screenshot protegido: marcado como bloqueado", Shot.blocked, true)
eq("Screenshot protegido: CVar restaurada", cvars.screenshotFormat, "jpg")
eq("bloqueado: la siguiente activacion no usa captura", Shot.Start(now, {}), "unavailable")
Shot.blocked = false

freshDB(false)
local realFn = _G.Screenshot
_G.Screenshot = nil
eq("sin Screenshot: unavailable", Shot.Start(now, {}), "unavailable")
_G.Screenshot = realFn

freshDB(false)
res = Shot.Start(now, {})
runTimers(0)
-- sin evento: el temporizador de 4 s da la captura por fallida y restaura las CVar
local ranTimeout = runTimers(Shot.TIMEOUT_SEC)
eq("sin confirmacion: temporizador de espera ejecutado", ranTimeout, 1)
eq("sin confirmacion: contado como error", Shot.errors, 1)
eq("sin confirmacion: CVar restaurada", cvars.screenshotFormat, "jpg")

-- ---------------------------------------------------------------------------
outln("fallo: los trozos vuelven a la cola (nada se pierde)")
-- ---------------------------------------------------------------------------
freshDB(false)
res = Shot.Start(now, {})
eq("requeue: arranca", res, "started")
runTimers(0)
local queuedBefore = C.Pending()
eq("requeue: al enviar, la cola queda vacia", queuedBefore, 0)
local firstBytes = Shot.testing.current() and Shot.testing.current().bytes
fireEvent(false)
check("requeue: tras el fallo, los mensajes vuelven a la cola", C.Pending() > 0, tostring(C.Pending()))
eq("requeue: ningun envio confirmado", Shot.sent, 0)
res = Shot.Start(now, {})
runTimers(0)
fireEvent(true)
eq("requeue: el reintento envia lo mismo (bytes)", Shot.last and Shot.last.bytes, firstBytes or nil)
check("requeue: reintento confirmado", Shot.sent == 1)

-- Scheduler: completion diferida y reencolado desde el trozo no confirmado.
do
    local S2 = C.NewScheduler()
    local done = 0
    local big = {}
    for k = 1, 300 do big[k] = string.char(k % 256) end
    S2:enqueueItem(3, 99, function() return table.concat(big) end, function() done = done + 1 end)
    local cap = 100
    local c1 = S2:nextChunk(cap, 1, true)
    local c2 = S2:nextChunk(cap, 2, true)
    eq("sched: dos trozos tomados", c1 ~= nil and c2 ~= nil, true)
    eq("sched: primer trozo indice 0", c1.index, 0)
    eq("sched: segundo trozo indice 1", c2.index, 1)
    eq("sched: aun no confirmado (onDone no llamado)", done, 0)
    S2:requeue({ c1, c2 })
    eq("sched: el trabajo vuelve a la cola", S2:pending(), 1)
    local r1 = S2:nextChunk(cap, 3, true)
    eq("sched: reenvio desde el trozo 0", r1.index, 0)
    local r2 = S2:nextChunk(cap, 4, true)
    local r3 = S2:nextChunk(cap, 5, true)
    eq("sched: tercer trozo (indice 2) tiene el final", r3 and r3.index, 2)
    eq("sched: onDone aun sin llamar", done, 0)
    C.ConfirmChunks({ r1, r2, r3 })
    eq("sched: confirmar completa el mensaje", done, 1)
    eq("sched: cola vacia tras confirmar", S2:pending(), 0)
    C.ConfirmChunks({ r3 })
    eq("sched: confirmar dos veces no repite onDone", done, 1)
end

-- Fallo a mitad de captura (abort): los trozos de la captura vuelven a la cola.
freshDB(true)
local realLive = ns.ReadLiveState
ns.ReadLiveState = function() error("fallo simulado al leer el estado") end
res = Shot.Start(now, {})
ns.ReadLiveState = realLive
eq("abort: Start devuelve unavailable", res, "unavailable")
eq("abort: sin captura en curso", Shot.Busy(), false)
check("abort: los trozos ya tomados vuelven a la cola", C.Pending() > 0, tostring(C.Pending()))
eq("abort: ningun envio confirmado", Shot.sent, 0)
res = Shot.Start(now, {})
eq("abort: la siguiente activacion arranca", res, "started")
runTimers(0)
fireEvent(true)
check("abort: el reintento confirma datos", Shot.sent >= 1)

-- ---------------------------------------------------------------------------
outln("medida: creacion y pintado del panel (4096 bloques)")
-- ---------------------------------------------------------------------------
freshDB(true)
local t0 = os.clock()
Shot.Start(now, {})
local tFirst = os.clock() - t0
local texAfterFirst = texCreated
runTimers(0)
fireEvent(true)
local t1 = os.clock()
runTimers(Shot.GAP_SEC) -- segunda captura: pinta el panel (reutiliza el pool)
local tSecond = os.clock() - t1
runTimers(0)
fireEvent(true)
local _, texAll = Shot.testing.panel()
outln(string.format("  info  panel 4096 bloques: 1a captura (crea + pinta) %.2f ms; 2a captura (reutiliza) %.2f ms; texturas de bloque creadas %d, en pool %d",
    tFirst * 1000, tSecond * 1000, texCreated - 1, #texAll))
local blockTex = texCreated - 1 -- una textura mas: el fondo negro del marco
check("medida: texturas de bloque no pasan de 4096", blockTex <= 4096, tostring(blockTex))
check("medida: la segunda captura no crea texturas nuevas", texCreated == texAfterFirst, string.format("%d vs %d", texCreated, texAfterFirst))

-- ---------------------------------------------------------------------------
outln("regla de combate")
-- ---------------------------------------------------------------------------
freshDB(false)
inCombat = true
local before = #shotLog
eq("combate sin pregunta: no captura", Shot.Start(now, {}), "combat")
eq("combate sin pregunta: no hay Screenshot", #shotLog, before)
eq("combate sin pregunta: Share no activa", Sh.Activate(true), false)
check("combate: aviso de solo preguntas", chatHas("solo preguntas"))
eq("combate con pregunta: si captura", Shot.Start(now, { question = true }), "started")
runTimers(0)
eq("combate con pregunta: Screenshot llamada", #shotLog, before + 1)
fireEvent(true)
inCombat = false

-- ---------------------------------------------------------------------------
outln("varias capturas: division, maximo y pausa")
-- ---------------------------------------------------------------------------
freshDB(true)
res = Shot.Start(now, {})
eq("grande: arranca", res, "started")
check("grande: primera captura programada", countTimers(0) == 1)
local ids = {}
local rounds = 0
while rounds < 20 do
    rounds = rounds + 1
    if runTimers(0) == 0 then break end
    local lastCall = shotLog[#shotLog]
    fireEvent(true)
    if countTimers(Shot.GAP_SEC) > 0 then
        runTimers(Shot.GAP_SEC)
    else
        break
    end
end
eq("grande: ocho capturas (tope)", #shotLog, 8)
eq("grande: enviadas 8", Shot.sent, 8)
check("grande: quedan datos para el siguiente envio", C.Pending() > 0)
eq("grande: CVar restaurada al final", cvars.screenshotFormat, "jpg")
eq("grande: ids de captura 1 a 8 (persistidos)", CompaneroWoWDB.settings.shotId, 8)
check("grande: aviso de datos enviados", chatHas("8 captura(s)"))
check("grande: aviso de lo que queda", chatHas("Quedan"))
check("grande: cada captura <= 4096 texturas", (function()
    local _, tex = Shot.testing.panel()
    return #tex <= 4096
end)())
check("grande: texturas de bloque creadas <= 4096 (pool, una vez)", texCreated - 1 <= 4096, tostring(texCreated))

-- Limite: con MAX_SHOTS = 1 se sale una captura y lo que no cabe queda en la cola (aviso).
freshDB(true)
Shot.MAX_SHOTS = 1
Shot.Start(now, {})
runTimers(0)
fireEvent(true)
eq("limite: una captura con MAX_SHOTS = 1", #shotLog, 1)
check("limite: aviso de datos para la siguiente captura", chatHas("siguiente captura"))
check("limite: quedan mensajes en la cola", C.Pending() > 0)
Shot.MAX_SHOTS = 8

-- ---------------------------------------------------------------------------
outln("canal: /compa canal y fallback a la franja")
-- ---------------------------------------------------------------------------
freshDB(false)
eq("canal por defecto: captura", Sh.Channel(), "captura")
Sh.ChannelCommand("tira")
eq("/compa canal tira: cambia", Sh.Channel(), "tira")
eq("/compa canal tira: guardado en settings", CompaneroWoWDB.settings.channel, "tira")
local nShots = #shotLog
eq("canal tira: activacion por la franja", Sh.Activate(true), true)
eq("canal tira: no hay captura", #shotLog, nShots)
eq("canal tira: franja visible", ns.bridge.frame and ns.bridge.frame.shown, true)
ns.StripRest()
Sh.ChannelCommand("captura")
eq("/compa canal captura: vuelve", Sh.Channel(), "captura")
Sh.ChannelCommand("otra")
check("/compa canal otra: uso", chatHas("Uso: /compa canal"))

freshDB(false)
_G.Screenshot = nil
chat = {}
eq("captura no disponible: cae a la franja", Sh.Activate(true), true)
check("captura no disponible: aviso", chatHas("Captura no disponible"))
check("captura no disponible: se comparte por la franja", chatHas("Compartiendo datos"))
ns.StripRest()
_G.Screenshot = realFn

-- Estado: canal en uso
freshDB(false)
local st = Sh.StatusLine()
check("estado: canal en uso", string.find(st, "Canal: captura", 1, true) ~= nil, st)

-- ---------------------------------------------------------------------------
outln("error interno: no deja la captura a medias")
-- ---------------------------------------------------------------------------
freshDB(false)
local realNextCap = C.NextChunkCapacity
C.NextChunkCapacity = function() error("fallo simulado") end
res = Shot.Start(now, {})
C.NextChunkCapacity = realNextCap
eq("error interno: Start devuelve unavailable", res, "unavailable")
eq("error interno: sin captura en curso", Shot.Busy(), false)
eq("error interno: sin activacion de canal", C.IsSharing(), false)
eq("error interno: CVar restauradas", cvars.screenshotFormat, "jpg")
eq("error interno: la siguiente captura funciona", Shot.Start(now, {}), "started")
runTimers(0)
fireEvent(true)
eq("error interno: recuperado tras el fallo", Shot.sent, 1)

-- ---------------------------------------------------------------------------
outln("aviso de captura en el marco")
-- ---------------------------------------------------------------------------
freshDB(false)
local uiMsg
ns.UI = { OnShotSent = function(id) uiMsg = id end }
res = Shot.Start(now, {})
runTimers(0)
fireEvent(true)
eq("marco: OnShotSent con el id de la captura", uiMsg, Shot.last and Shot.last.id)
ns.UI = nil

-- ---------------------------------------------------------------------------
outln("notice de captura: oculta durante la captura y restaurada")
-- ---------------------------------------------------------------------------
freshDB(false)
_G.ScreenshotStatus = newFrame(nil)
_G.ScreenshotStatus:SetAlpha(1)
Shot.Start(now, {})
eq("notice: oculta al capturar", _G.ScreenshotStatus.alpha, 0)
runTimers(0)
fireEvent(true)
runTimers(1)
eq("notice: restaurada tras 1 s", _G.ScreenshotStatus.alpha, 1)
_G.ScreenshotStatus = nil

outln("")
if failures == 0 then
    outln("ALL TESTS PASSED")
    os.exit(0)
else
    outln(string.format("%d FAILED", failures))
    os.exit(1)
end
