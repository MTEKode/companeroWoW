-- test_ui.lua: marco de Compañero en el juego (T40). Cubre: parser de la respuesta (negrita, viñetas,
-- "||", lineas de accion, pegado ajeno o incompleto, id anterior, tope de 4000 caracteres); envio de
-- preguntas (id persistido, prioridad maxima, reenvio cada 2 s, sustitucion, historial); tecla y /compa
-- (abrir, pregunta directa, sync sin recarga, sync reload, combate); envio solo de lo que cambia y fin
-- temprano; despacho de acciones (marca, barras, macro, combate); marco desactivado por defecto (tecla,
-- /compa, /compa ?, migracion), Escape y clic en el mundo quitan el foco; estado del marco (posicion, tamaño
-- minimo, Escape). Frames simulados; el canal corre por el OnUpdate real de Bridge.lua.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_ui.lua
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
-- Entorno: reloj, chat, frames simulados, globales de WoW
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

-- Llamadas a metodos de frames (para comprobar geometria, foco y clics).
local calls = {}
local allObjects = {}
local Obj = {}
local function newObj(kind, name)
    local o = {
        kind = kind, name = name, shown = false, text = "", focus = false, scripts = {},
        points = {}, w = 0, h = 0, enabled = true, alpha = 1,
    }
    allObjects[#allObjects + 1] = o
    return setmetatable(o, { __index = function(t, k)
        if Obj[k] then return Obj[k] end
        return function(self, ...)
            calls[#calls + 1] = { obj = self, name = k, args = { ... } }
            return nil
        end
    end })
end
Obj.Show = function(self) self.shown = true end
Obj.Hide = function(self)
    local was = self.shown
    self.shown = false
    if was and self.scripts.OnHide then self.scripts.OnHide(self) end
end
Obj.IsShown = function(self) return self.shown end
Obj.SetText = function(self, t) self.text = t end
Obj.GetText = function(self) return self.text or "" end
Obj.SetFocus = function(self) self.focus = true end
Obj.ClearFocus = function(self) self.focus = false end
Obj.HasFocus = function(self) return self.focus == true end
Obj.IsMouseOver = function(self) return self.mouseOver == true end
Obj.SetSize = function(self, w, h) self.w, self.h = w, h end
Obj.GetWidth = function(self) return self.w end
Obj.GetHeight = function(self) return self.h end
Obj.SetPoint = function(self, ...)
    local a = { ... }
    self.points[#self.points + 1] = { point = a[1], rel = a[2], relPoint = a[3], x = a[4], y = a[5] }
end
Obj.ClearAllPoints = function(self) self.points = {} end
Obj.GetPoint = function(self)
    local p = self.points[1]
    if not p then return nil end
    return p.point, p.rel, p.relPoint, p.x, p.y
end
Obj.SetScript = function(self, name, fn) self.scripts[name] = fn end
Obj.SetEnabled = function(self, v) self.enabled = v and true or false end
Obj.Enable = function(self) self.enabled = true end
Obj.Disable = function(self) self.enabled = false end
Obj.SetAlpha = function(self, a) self.alpha = a end
Obj.CreateFontString = function(self) return newObj("FontString") end
Obj.CreateTexture = function(self) return newObj("Texture") end

local function callsOn(obj, name)
    local out = {}
    for _, c in ipairs(calls) do
        if c.obj == obj and c.name == name then out[#out + 1] = c end
    end
    return out
end

local function findButtons(label)
    local out = {}
    for _, o in ipairs(allObjects) do
        if o.kind == "Button" and string.find(o.text or "", label, 1, true) then out[#out + 1] = o end
    end
    return out
end

_G.GetTime = function() return clock end
_G.time = function() return 1700000000 end
_G.InCombatLockdown = function() return inCombat end
_G.CreateFrame = function(kind, name, parent, template)
    return newObj(kind, name)
end
_G.UIParent = {
    GetWidth = function() return 1920 end,
    GetHeight = function() return 768 end,
    GetEffectiveScale = function() return 1 end,
}
_G.GetPhysicalScreenSize = function() return 1920, 1080 end
_G.UISpecialFrames = {}
SlashCmdList = {}
CompaneroWoWDB = nil
local reloads = 0
_G.ReloadUI = function() reloads = reloads + 1 end

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
load("CompaneroWoW/Markup.lua")
load("CompaneroWoW/Ask.lua")
load("CompaneroWoW/UI.lua")

local KEY = "Tester-Realm"
ns.CharKey = function() return KEY end
ns.Say = function(msg) chat[#chat + 1] = msg end
-- Sin lecturas reales del personaje (no interesan aqui): la revision de datos no hace nada.
ns.RequestSnapshot = function() return false end
ns.SnapshotAll = function() return true end
-- Acciones de las respuestas: registro de llamadas (las funciones reales estan en Marks/Bars/Macros).
local marks, bars, macros = {}, {}, {}
ns.Marks = { Set = function(mapID, x, y, text) marks[#marks + 1] = { mapID = mapID, x = x, y = y, text = text } end }
ns.Bars = { Apply = function(args) bars[#bars + 1] = args end }
ns.Macros = { Create = function(args) macros[#macros + 1] = args end }
ns.InCombat = function() return inCombat end

local C, Sh, Ask, UI, Markup = ns.Channel, ns.Share, ns.Ask, ns.UI, ns.Markup
ns.InitBridge()
local F = ns.bridge.frame

-- Captura de los trozos que sale por el canal (Bridge llama a C.NextChunk en cada trama).
local captured = {}
local realNext = C.NextChunk
C.NextChunk = function(N, frameNo)
    local ch = realNext(N, frameNo)
    if ch then captured[#captured + 1] = { type = ch.type, index = ch.index, t = clock } end
    return ch
end

-- Datos del personaje de prueba: lo que envia cada tipo (sin conocimiento ni sesion).
local function freshDB()
    CompaneroWoWDB = nil
    C.testing.reset()
    ns.EnsureDB()
    CompaneroWoWDB.settings.frame = true -- los tests del marco lo quieren activado (por defecto va desactivado)
    CompaneroWoWDB.characters[KEY] = {
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
    chat = {}
    captured = {}
    marks, bars, macros = {}, {}, {}
    Ask.pending = nil
    inCombat = false
end

-- Avanza el reloj y ejecuta el OnUpdate real de la franja (como el juego) durante 'seconds'.
-- Devuelve el instante en que la ventana se cerro (o nil si sigue abierta).
local function drive(seconds)
    local stop = clock + seconds
    while clock < stop - 1e-9 do
        clock = clock + 0.2
        local fn = F.scripts.OnUpdate
        if not fn then return clock end
        fn(F, 0.2)
    end
    return nil
end

-- Tipos enviados (primer trozo de cada mensaje) desde 'from' (instante).
local function typesSince(from)
    local seen, out = {}, {}
    for _, ch in ipairs(captured) do
        if ch.t > from and ch.index == 0 and not seen[ch.type] then
            seen[ch.type] = true
            out[#out + 1] = ch.type
        end
    end
    table.sort(out)
    return out
end
local function listOf(t)
    local parts = {}
    for i, v in ipairs(t) do parts[i] = tostring(v) end
    return table.concat(parts, ",")
end
local function hasType(list, t)
    for _, v in ipairs(list) do if v == t then return true end end
    return false
end

local function jobOf(t)
    local found
    for _, job in ipairs(C.state.sched and C.state.sched.jobs or {}) do
        if job.type == t then found = job end
    end
    return found
end

-- ---------------------------------------------------------------------------
outln("(a) Parser de la respuesta (Markup.Parse)")
local text = "[CWOW1 #7 modelo/x]\n**Hola** mundo\n- uno\n- dos||tres\n@marca 1429 42.5 37.2 Forja||A\n@barras 1:100,2:200\n@macro Saludo|INV_Misc_QuestionMark|/say hola\\n/wave\n@fuente Fuente: guía\n[/CWOW1]"
local r, err = Markup.Parse(text)
check("parser: pega valida", r ~= nil, tostring(err))
if r then
    eq("parser: id", r.id, 7)
    eq("parser: provider", r.provider, "modelo/x")
    eq("parser: negrita: primer segmento en negrita", r.blocks[1].segs[1].bold, true)
    eq("parser: negrita: texto", r.blocks[1].segs[1].text, "Hola")
    eq("parser: texto normal tras la negrita", r.blocks[1].segs[2].bold, false)
    eq("parser: viñeta", r.blocks[2].kind, "bullet")
    eq("parser: viñeta sin el guion", r.blocks[2].segs[1].text, "uno")
    eq("parser: || es un pipe literal", r.blocks[3].segs[1].text, "dos|tres")
    local m = r.blocks[4]
    eq("parser: @marca: accion", m.kind == "action" and m.action, "marca")
    eq("parser: @marca: mapID", m.mapID, 1429)
    eq("parser: @marca: x", m.x, 42.5)
    eq("parser: @marca: y", m.y, 37.2)
    eq("parser: @marca: texto con pipe", m.text, "Forja|A")
    eq("parser: @barras", r.blocks[5].action == "barras" and r.blocks[5].args, "1:100,2:200")
    local mc = r.blocks[6]
    eq("parser: @macro: nombre", mc.name, "Saludo")
    eq("parser: @macro: args con \\n literal", mc.args, "Saludo|INV_Misc_QuestionMark|/say hola\\n/wave")
    eq("parser: @fuente", r.blocks[7].kind == "source" and r.blocks[7].segs[1].text, "Fuente: guía")
end
local r2 = Markup.Parse("[CWOW1 #8]\n@macro Mx|Inv|/cast A||B\n@marca 1429 10 20 x\n[/CWOW1]")
eq("parser: @macro con || en el cuerpo", r2 and r2.blocks[1].args, "Mx|Inv|/cast A|B")
eq("parser: sin provider", r2 and r2.provider, nil)
local r3 = Markup.Parse("[CWOW1 #9 p]\n@macro Mx|/cast X\n@marca 1429 abc 20 x\n[/CWOW1]")
eq("parser: @macro de dos campos (sin icono)", r3 and r3.blocks[1].args, "Mx|/cast X")
eq("parser: @marca mal formada -> texto normal", r3 and r3.blocks[2].kind, "para")
local r4 = Markup.Parse("[CWOW1 #10 p]\n@marca 1429 150 20 x\n[/CWOW1]")
eq("parser: @marca fuera de 0-100 -> texto normal", r4 and r4.blocks[1].kind, "para")
local foreign, ferr = Markup.Parse("hola que tal")
check("parser: pegado ajeno rechazado", foreign == nil and ferr == Markup.NOT_ANSWER, tostring(ferr))
eq("parser: mensaje de pegado ajeno", ferr, "Eso no es una respuesta de Compañero.")
local _, e2 = Markup.Parse("[CWOW1 #3 p]\nhola")
eq("parser: sin cierre -> incompleta", e2, Markup.INCOMPLETE)
local _, e3 = Markup.Parse("[CWOW1 #0 p]\nhola\n[/CWOW1]")
eq("parser: id 0 no es valido", e3, Markup.NOT_ANSWER)
local _, e4 = Markup.Parse("")
eq("parser: vacio es ajeno", e4, Markup.NOT_ANSWER)
local _, e5 = Markup.Parse("[CWOW1 #4 p]\n   \n[/CWOW1]")
eq("parser: cuerpo vacio", e5, Markup.EMPTY)
local long = Markup.Parse("[CWOW1 #1 p]\n" .. string.rep("a", 5000) .. "\n[/CWOW1]")
check("tope: 5000 caracteres -> recortada", long and long.truncated == true)
eq("tope: 4000 caracteres exactos", long and Markup.CharLen(long.body), 4000)
local uni = Markup.Parse("[CWOW1 #1 p]\n" .. string.rep("ñ", 4500) .. "\n[/CWOW1]")
eq("tope: UTF-8 cuenta caracteres", uni and Markup.CharLen(uni.body), 4000)
eq("tope: UTF-8 sin cortar un caracter", uni and #uni.body, 8000)
local short = Markup.Parse("[CWOW1 #1 p]\nA\n[/CWOW1]")
check("tope: respuesta corta no se marca", short and short.truncated == false)

-- ---------------------------------------------------------------------------
outln("(b) Preguntas: id persistido, prioridad, reenvio, sustitucion, historial")
freshDB()
local ok1, id1 = Ask.Send("¿Dónde está la forja?")
check("pregunta: se envia", ok1 == true)
eq("pregunta: id 1", id1, 1)
local q1 = C.LastQuestion()
check("pregunta: JSON con id y texto", q1 and string.find(q1, '"id":1', 1, true) and string.find(q1, '"text":"¿Dónde está la forja?"', 1, true) ~= nil, tostring(q1))
check("pregunta: viaja como msgType 15 en la cola", jobOf(15) ~= nil)
local _, id2 = Ask.Send("Segunda")
eq("pregunta: id 2 (crece)", id2, 2)
eq("pregunta: id persistido en SavedVariables", CompaneroWoWDB.characters[KEY].ask.lastId, 2)
do
    -- Respuestas guardadas: como mucho 2 KB de texto cada una (Storage.ANSWER_MAX_BYTES).
    local blocks = {}
    for i = 1, 6 do blocks[i] = { kind = "para", segs = { { text = string.rep("t", 1000), bold = false } } } end
    local copy = Ask.StoredCopy({ id = 7, provider = "m", blocks = blocks, body = "x" })
    check("respuesta guardada: bloques hasta 2 KB y marcada como truncada", #copy.blocks == 2 and copy.truncated == true, tostring(#copy.blocks))
    local copy2 = Ask.StoredCopy({ id = 8, body = string.rep("y", 5000) })
    eq("respuesta sin bloques: cuerpo cortado a 2 KB", #copy2.body, 2048)
end
-- Tras "recargar" el modulo (la cuenta sigue en SavedVariables), el id continua.
Ask.pending = nil
local _, id3 = Ask.Send("Tercera")
eq("pregunta: el id sigue tras reabrir", id3, 3)
check("pregunta: vacia no se envia", Ask.Send("   ") == false)
local longQ = string.rep("x", 501)
check("pregunta: mas de 500 caracteres no se envia", Ask.Send(longQ) == false)
eq("pregunta: el rechazo no gasta id", CompaneroWoWDB.characters[KEY].ask.lastId, 3)
local _, idc = Ask.Send("uno\ndos")
eq("pregunta: saltos de linea -> espacios", string.find(C.LastQuestion(), '"text":"uno dos"', 1, true) ~= nil and idc, 4)

-- Prioridad: la pregunta sale antes que cualquier dato (el plan tiene 1 y 12 delante en el orden normal).
freshDB()
Ask.Send("Prioridad")
C.Pump(clock, 1e9)
local first = C.NextChunk(860, 1)
eq("prioridad: la pregunta es el primer trozo", first and first.type, 15)

-- Reenvio cada 2 s durante la ventana: con 3 s de ventana, sale al inicio y a los 2 s.
freshDB()
Ask.Send("Reenvio")
local t0 = clock
-- Una trama manda un trozo: la ventana acaba cuando la cola queda vacia (y han pasado 3 s).
drive(6)
local qSends = 0
for _, ch in ipairs(captured) do
    if ch.type == 15 and ch.index == 0 then qSends = qSends + 1 end
end
eq("reenvio: 2 envios de la pregunta en la ventana (inicio y a los 2 s)", qSends, 2)
check("reenvio: la ventana acaba al quedar vacia (>= 3 s)", not C.IsSharing() and clock - t0 >= 3 - 1e-6, tostring(clock - t0))

-- Sustitucion: una pregunta nueva reemplaza la anterior en los reenvios.
freshDB()
Ask.Send("Primera")
drive(0.4)
Ask.Send("Segunda")
drive(2.6)
local lastQ15
for _, ch in ipairs(captured) do
    if ch.type == 15 then lastQ15 = ch end
end
check("sustitucion: el ultimo envio de la pregunta es la segunda", C.LastQuestion() and string.find(C.LastQuestion(), '"id":2', 1, true) ~= nil)
eq("sustitucion: hay trozo de la pregunta", lastQ15 ~= nil, true)

-- Historial: las ultimas 10 preguntas, la mas reciente primero.
freshDB()
for i = 1, 12 do Ask.Send("P" .. i) end
local hist = Ask.History()
eq("historial: 10 preguntas", #hist, 10)
eq("historial: la mas reciente primero", hist[1], "P12")
eq("historial: la mas antigua guardada es la 3", hist[10], "P3")
eq("historial: las preguntas no pasan de 10 en SV", #CompaneroWoWDB.characters[KEY].ask.questions, 10)

-- Sin envio de datos: strip off impide preguntar.
freshDB()
ns.SetStripEnabled(false)
check("strip off: no se pregunta", Ask.Send("Hola") == false)
check("strip off: se avisa", chatHas("/compa strip on"))
ns.SetStripEnabled(true)

-- ---------------------------------------------------------------------------
outln("(c) Respuestas: guardar, ignorar anteriores, tope de 10")
freshDB()
check("recibir: id 5", Ask.Receive("[CWOW1 #5 p]\nA\n[/CWOW1]") ~= nil)
local okOld, errOld = Ask.Receive("[CWOW1 #4 p]\nB\n[/CWOW1]")
check("recibir: id 4 (anterior) se ignora", okOld == nil and string.find(errOld or "", "anterior", 1, true) ~= nil, tostring(errOld))
eq("recibir: la respuesta anterior no cambia la lista", #Ask.Answers(), 1)
Ask.Receive("[CWOW1 #5 p]\nA bis\n[/CWOW1]")
eq("recibir: el mismo id reemplaza, no duplica", #Ask.Answers(), 1)
eq("recibir: la respuesta de id 5 es la ultima", Ask.Answers()[1].blocks[1].segs[1].text, "A bis")
Ask.Receive("[CWOW1 #6 p]\nC\n[/CWOW1]")
eq("recibir: id 6 se anade", #Ask.Answers(), 2)
eq("recibir: ultima mostrada = 6", Ask.LastShown(), 6)
check("recibir: pegado ajeno no toca la lista", Ask.Receive("hola") == nil and #Ask.Answers() == 2)
for i = 7, 20 do Ask.Receive("[CWOW1 #" .. i .. " p]\nR\n[/CWOW1]") end
eq("recibir: guarda como mucho 10 respuestas", #Ask.Answers(), 10)
eq("recibir: la mas antigua que queda es la 11", Ask.Answers()[1].id, 11)
check("recibir: respuesta de id 5 descartada ahora", Ask.Receive("[CWOW1 #5 p]\nZ\n[/CWOW1]") == nil)

-- ---------------------------------------------------------------------------
outln("(d) Acciones: marca, barras, macro; combate")
freshDB()
Ask.Receive("[CWOW1 #30 p]\n@marca 1429 42.5 37.2 Forja\n@barras 1:100,2:200\n@macro Saludo|INV_Misc_QuestionMark|/say hola\\n/wave\n[/CWOW1]")
local act = Ask.Answers()[1].blocks
check("acciones: marca llama a Marks.Set", Ask.Run(act[1]) == true and #marks == 1)
eq("acciones: marca: mapa", marks[1] and marks[1].mapID, 1429)
check("acciones: marca: coordenadas en fraccion", marks[1] and math.abs(marks[1].x - 0.425) < 1e-9 and math.abs(marks[1].y - 0.372) < 1e-9)
eq("acciones: marca: texto", marks[1] and marks[1].text, "Forja")
Ask.Run(act[2])
eq("acciones: barras llama a Bars.Apply", bars[1], "1:100,2:200")
Ask.Run(act[3])
eq("acciones: macro llama a Macros.Create", macros[1], "Saludo|INV_Misc_QuestionMark|/say hola\\n/wave")
inCombat = true
check("combate: la marca no se hace", Ask.Run(act[1]) == false and #marks == 1)
check("combate: se avisa", chatHas("En combate"))
local nb, nm = #bars, #macros
Ask.Run(act[2])
Ask.Run(act[3])
eq("combate: barras y macro no se hacen", #bars + #macros, nb + nm)
inCombat = false

-- ---------------------------------------------------------------------------
outln("(e) Marco: abrir, enfocar, enviar, pegar, botones")
freshDB()
local okOpen = UI.Open()
check("marco: se abre", okOpen and UI.IsShown())
local qbox, paste = UI.w.qbox, UI.w.paste
check("marco: pregunta y pegado existen", qbox ~= nil and paste ~= nil)
check("marco: el campo de pegado esta invisible (alpha 0)", paste.alpha == 0)
qbox.text = "¿Qué hago en Ventormenta?"
qbox.scripts.OnEnterPressed(qbox)
check("marco: Enter envia la pregunta", Ask.pending ~= nil and qbox.text == "")
check("marco: el foco pasa al campo de pegado", paste.focus == true)
eq("marco: aviso «Pensando…»", UI.w.hint.text, UI.PASTE_HINT)
paste.text = "[CWOW1 #" .. Ask.pending .. " modelo]\n**Forja** en Ventormenta\n- Ve al norte\n@marca 1429 42.5 37.2 Forja\n@fuente Fuente: guía\n[/CWOW1]"
paste.scripts.OnTextChanged(paste, true)
eq("marco: la respuesta pegada se muestra (lista)", #Ask.Answers(), 1)
eq("marco: el campo de pegado se vacia", paste.text, "")
eq("marco: aviso de respuesta recibida", UI.w.status.text, "Respuesta #" .. Ask.Answers()[1].id .. " recibida.")
eq("marco: el aviso «Pensando…» se quita", UI.w.hint.text, "")
local markBtn = findButtons("Marcar")
eq("marco: un boton «Marcar»", #markBtn, 1)
if markBtn[1] then
    markBtn[1].scripts.OnClick(markBtn[1])
    eq("marco: el boton llama a la accion marca", marks[#marks] and marks[#marks].mapID, 1429)
end
paste.text = "hola, eso no es una respuesta"
paste.scripts.OnTextChanged(paste, true)
eq("marco: pegado ajeno -> aviso", UI.w.status.text, Markup.NOT_ANSWER)
eq("marco: pegado ajeno no cambia la lista", #Ask.Answers(), 1)
-- Historial con flechas
qbox.text = ""
qbox.scripts.OnArrowPressed(qbox, "UP")
eq("marco: Arriba trae la ultima pregunta", qbox.text, "¿Qué hago en Ventormenta?")
qbox.scripts.OnArrowPressed(qbox, "DOWN")
eq("marco: Abajo vuelve al campo vacio", qbox.text, "")
-- Combate: los botones quedan desactivados
inCombat = true
UI.SyncCombat()
eq("combate: el boton de marca queda desactivado", markBtn[1] and markBtn[1].enabled, false)
inCombat = false
UI.SyncCombat()
eq("fuera de combate: el boton vuelve a estar activo", markBtn[1] and markBtn[1].enabled, true)
-- Navegacion entre respuestas
Ask.pending = nil
Ask.Receive("[CWOW1 #100 p]\nSegunda respuesta\n[/CWOW1]")
UI.index = nil
UI.Render()
eq("navegacion: muestra la ultima respuesta", UI.w.counter.text, "2 / 2")
UI.Step(-1)
eq("navegacion: ‹ va a la anterior", UI.w.counter.text, "1 / 2")
UI.Step(1)
eq("navegacion: › vuelve a la ultima", UI.w.counter.text, "2 / 2")

-- Tecla de compartir (marco activado): abre el marco SIN foco y empieza la ventana (sin pregunta: no hay id 15).
freshDB()
UI.Hide()
qbox.focus = false
_G.CompaneroWoW_ShareKey()
check("tecla: abre el marco", UI.IsShown())
check("tecla: NO enfoca la pregunta (las teclas de movimiento seguirian en el juego)", qbox.focus ~= true and paste.focus ~= true)
check("tecla: empieza la ventana de envio", C.IsSharing() and F.scripts.OnUpdate ~= nil)
check("tecla sin pregunta: no hay mensaje 15", jobOf(15) == nil)
drive(25)
check("tecla: la ventana acaba (<= 20 s)", not C.IsSharing())
_G.CompaneroWoW_ShareKey()
check("tecla: con el marco abierto y sin foco, lo deja abierto", UI.IsShown())
qbox.focus = true -- el jugador pone el cursor en la pregunta
_G.CompaneroWoW_ShareKey()
check("tecla: pulsar con el foco en la pregunta cierra el marco", UI.IsShown() == false)
check("tecla: al cerrar, la pregunta pierde el foco", qbox.focus ~= true)
_G.CompaneroWoW_ShareKey()
check("tecla: pulsar con el marco cerrado lo abre", UI.IsShown())
-- Dentro de la ventana el campo sigue enfocado: el Escape (UISpecialFrames) lo cierra.
local special = false
for _, n in ipairs(_G.UISpecialFrames) do if n == "CompaneroWoWFrame" then special = true end end
check("Escape cierra el marco (UISpecialFrames)", special)

-- ---------------------------------------------------------------------------
outln("(f) Comandos /compa: marco, pregunta directa, sync sin recarga, sync reload, combate")
freshDB()
UI.Hide()
SlashCmdList.COMPA("")
check("/compa sin texto abre el marco", UI.IsShown() and UI.w.qbox.focus == true)
SlashCmdList.COMPA("? ¿Qué es la Forja?")
eq("/compa ? <texto> envia la pregunta", CompaneroWoWDB.characters[KEY].ask.lastId, 1)
check("/compa ? <texto>: el JSON lleva el texto con mayusculas", string.find(C.LastQuestion(), "Qué es la Forja?", 1, true) ~= nil)
SlashCmdList.COMPA("?Otra")
eq("/compa ?texto (sin espacio) tambien envia", CompaneroWoWDB.characters[KEY].ask.lastId, 2)
freshDB()
_G.CompaneroWoW_ShareKey()
drive(25) -- una activacion normal: deja todo enviado (sin cambios despues)
local before = clock
SlashCmdList.COMPA("sync")
check("/compa sync: sin recarga de la interfaz", reloads == 0)
check("/compa sync: arranca la ventana", C.IsSharing())
drive(25)
local sync = typesSince(before - 1e-9)
check("/compa sync: envia todo aunque no haya cambiado (tipo 1 y 3)", hasType(sync, 1) and hasType(sync, 3), listOf(sync))
SlashCmdList.COMPA("sync reload")
eq("/compa sync reload: recarga la interfaz", reloads, 1)
-- En combate no se sincroniza.
freshDB()
inCombat = true
SlashCmdList.COMPA("sync")
check("/compa sync en combate: no arranca", C.IsSharing() == false and chatHas("combate"))
inCombat = false

-- ---------------------------------------------------------------------------
outln("(g) Solo lo que cambia; fin temprano; claves cada 10 min")
freshDB()
_G.CompaneroWoW_ShareKey()
local s1 = clock
local end1 = drive(25)
local types1 = typesSince(s1 - 1e-9)
check("cambios: el primer envio manda los datos (1, 3, 11)", hasType(types1, 1) and hasType(types1, 3) and hasType(types1, 11), listOf(types1))
check("cambios: el primer envio acaba antes de 20 s (3 s minimo)", end1 ~= nil and end1 - s1 <= 20 + 1e-6)
-- Segundo envio sin cambios: nada de datos, solo la franja de estado.
captured = {}
local s2 = clock
_G.CompaneroWoW_ShareKey()
local end2 = drive(25)
local types2 = typesSince(s2 - 1e-9)
eq("cambios: sin cambios no se envia ningun tipo de datos", listOf(types2), "")
check("cambios: con todo enviado la ventana acaba a los 3 s", end2 ~= nil and end2 - s2 >= 3 - 1e-6 and end2 - s2 <= 3.4, tostring(end2 and (end2 - s2)))
-- Cambio de un dato: solo ese tipo sale.
CompaneroWoWDB.characters[KEY].inventory = { items = { { id = 1, n = 6 } } }
captured = {}
local s3 = clock
_G.CompaneroWoW_ShareKey()
drive(25)
local types3 = typesSince(s3 - 1e-9)
eq("cambios: cambia el inventario -> solo el tipo 3", listOf(types3), "3")
-- Tipos clave: se reenvian si el ultimo envio tiene 10 min o mas.
captured = {}
clock = clock + 601
local s4 = clock
_G.CompaneroWoW_ShareKey()
drive(25)
local types4 = typesSince(s4 - 1e-9)
check("claves: a los 10 min se reenvian personaje, misiones y equipo", hasType(types4, 1) and hasType(types4, 2) and hasType(types4, 4), listOf(types4))
check("claves: lo que no es clave y no cambio no sale", not hasType(types4, 3) and not hasType(types4, 5) and not hasType(types4, 11), listOf(types4))
-- Contador de omitidos en /compa estado.
local statusLines = C.StatusLines()
check("estado: cuenta los mensajes sin cambios", string.find(statusLines[1], "sin cambios", 1, true) ~= nil, statusLines[1])

-- ---------------------------------------------------------------------------
outln("(h) Estado del marco: posicion, tamaño por personaje, minimo, sin escala")
freshDB()
UI.Hide()
UI.Open()
local f = UI.frame
eq("marco: tamaño por defecto (ancho)", f.w, 420)
eq("marco: tamaño por defecto (alto)", f.h, 460)
check("marco: escala la da UIParent (no se fija SetScale)", #callsOn(f, "SetScale") == 0)
check("marco: redimensionable con minimo 320x260",
    #callsOn(f, "SetResizeBounds") + #callsOn(f, "SetMinResize") > 0)
local rb = callsOn(f, "SetResizeBounds")[1] or callsOn(f, "SetMinResize")[1]
eq("marco: minimo ancho", rb and rb.args[1], 320)
eq("marco: minimo alto", rb and rb.args[2], 260)
-- Mover y redimensionar, y al cerrar se guarda por personaje.
f.points = { { point = "TOPLEFT", rel = nil, relPoint = "TOPLEFT", x = 120, y = -80 } }
f.w, f.h = 500, 380
UI.Hide()
local saved = CompaneroWoWDB.characters[KEY].ui
check("estado: se guarda al cerrar", saved ~= nil)
eq("estado: posicion x", saved and saved.x, 120)
eq("estado: tamaño ancho", saved and saved.w, 500)
eq("estado: tamaño alto", saved and saved.h, 380)
-- Otro personaje no hereda la posicion.
ns.CharKey = function() return "Otro-Realm" end
CompaneroWoWDB.characters["Otro-Realm"] = {}
UI.frame = nil
UI.w = {}
UI.Open()
eq("estado: otro personaje usa el tamaño por defecto", UI.frame.w, 420)
ns.CharKey = function() return KEY end
-- Tamaño por debajo del minimo: se sube a 320x260 al abrir.
UI.Hide()
UI.frame = nil
UI.w = {}
CompaneroWoWDB.characters[KEY].ui = { point = "TOPLEFT", relPoint = "TOPLEFT", x = 10, y = -10, w = 100, h = 100 }
UI.Open()
eq("estado: tamaño minimo ancho 320", UI.frame.w, 320)
eq("estado: tamaño minimo alto 260", UI.frame.h, 260)
eq("estado: posicion restaurada", UI.frame.points[1] and UI.frame.points[1].x, 10)

-- ---------------------------------------------------------------------------
outln("(i) Marco en el juego desactivado (settings.frame): migracion, tecla, /compa, Escape y clic")
-- Migracion: una base sin settings.frame (usuarios anteriores) queda en false.
CompaneroWoWDB = nil
ns.EnsureDB()
eq("migracion: settings.frame queda en false", CompaneroWoWDB.settings.frame, false)
check("migracion: ns.IsFrameOn() es false", ns.IsFrameOn() == false)
-- Con el marco desactivado, nada lo abre ni le da el foco (la ventana de la app es la que se usa).
-- La seccion (h) reconstruyo el marco: los campos son los actuales.
qbox, paste = UI.w.qbox, UI.w.paste
freshDB()
UI.Hide()
CompaneroWoWDB.settings.frame = false
local function anyFocus()
    for _, k in ipairs({ "qbox", "paste" }) do
        if UI.w[k] and UI.w[k].focus then return true end
    end
    return false
end
_G.CompaneroWoW_ShareKey()
check("off: la tecla no abre el marco", not UI.IsShown())
check("off: la tecla no pone el foco", not anyFocus())
check("off: la tecla empieza la ventana de envio", C.IsSharing())
drive(25)
SlashCmdList.COMPA("enviar")
check("off: /compa enviar no abre el marco", not UI.IsShown() and C.IsSharing())
drive(25)
SlashCmdList.COMPA("sync")
check("off: /compa sync no abre el marco ni enfoca", not UI.IsShown() and not anyFocus() and C.IsSharing())
drive(25)
chat = {}
SlashCmdList.COMPA("")
check("off: /compa sin texto no abre el marco", not UI.IsShown() and not anyFocus())
check("off: /compa sin texto muestra la ayuda", chatHas("Comandos ("))
chat = {}
SlashCmdList.COMPA("? Hola")
check("off: /compa ? avisa de que el marco esta desactivado", chatHas("El marco está desactivado: usa la ventana de la app o /compa marco on"))
check("off: /compa ? no envia la pregunta", not (CompaneroWoWDB.characters[KEY].ask and CompaneroWoWDB.characters[KEY].ask.lastId))
chat = {}
SlashCmdList.COMPA("estado")
check("off: /compa estado: Marco en el juego desactivado", chatHas("Marco en el juego: desactivado"))
-- Una pregunta enviada sin marco no pone el foco en el campo de pegado.
UI.OnQuestionSent(7)
check("off: una pregunta enviada no enfoca el campo de pegado", not anyFocus())
-- /compa marco on: abre el marco con el cursor en la pregunta (explicito).
chat = {}
SlashCmdList.COMPA("marco on")
check("marco on: guardado", CompaneroWoWDB.settings.frame == true and ns.IsFrameOn())
check("marco on: abre el marco", UI.IsShown())
check("marco on: el cursor va a la pregunta (explicito)", qbox.focus == true)
check("marco on: mensaje en espanol", chatHas("Marco en el juego: activado"))
chat = {}
SlashCmdList.COMPA("marco")
check("marco: sin argumento muestra el estado", chatHas("Marco en el juego: activado"))
SlashCmdList.COMPA("marco ?")
check("marco: argumento no valido -> uso", chatHas("Uso: /compa marco on|off"))
chat = {}
SlashCmdList.COMPA("estado")
check("marco on: /compa estado: Marco en el juego activado", chatHas("Marco en el juego: activado"))
-- Escape: quita el foco y cierra.
qbox.scripts.OnEscapePressed(qbox)
check("Escape en la pregunta: quita el foco", not qbox.focus)
check("Escape en la pregunta: cierra el marco", not UI.IsShown())
SlashCmdList.COMPA("")
check("/compa sin texto (marco on): abre con el cursor en la pregunta", UI.IsShown() and qbox.focus == true)
paste.scripts.OnEscapePressed(paste)
check("Escape en el campo de pegado: quita el foco y cierra", not anyFocus() and not UI.IsShown())
-- Cerrar sin Escape (boton) tambien quita el foco.
SlashCmdList.COMPA("")
UI.Hide()
check("cerrar: la pregunta pierde el foco", not qbox.focus)
-- Clic en el mundo: quita el foco si el raton no esta sobre el marco; dentro del marco lo mantiene.
local regs = callsOn(UI.frame, "RegisterEvent")
local registered = false
for _, c in ipairs(regs) do if c.args[1] == "GLOBAL_MOUSE_DOWN" then registered = true end end
check("clic: el marco escucha GLOBAL_MOUSE_DOWN", registered)
SlashCmdList.COMPA("")
UI.frame.mouseOver = true
UI.frame.scripts.OnEvent(UI.frame, "GLOBAL_MOUSE_DOWN", "LeftButton")
check("clic dentro del marco: mantiene el foco", qbox.focus == true)
UI.frame.mouseOver = false
UI.frame.scripts.OnEvent(UI.frame, "GLOBAL_MOUSE_DOWN", "LeftButton")
check("clic en el mundo: quita el foco", not qbox.focus)
check("clic en el mundo: el marco sigue abierto", UI.IsShown())
-- Apagarlo cierra el marco.
SlashCmdList.COMPA("marco off")
check("marco off: guardado", CompaneroWoWDB.settings.frame == false and not ns.IsFrameOn())
check("marco off: cierra el marco", not UI.IsShown() and not anyFocus())
check("marco off: mensaje en espanol", chatHas("Marco en el juego: desactivado"))
-- Con el marco off, la tecla con el marco abierto no lo cierra ni lo reabre (solo comparte).
UI.Open()
_G.CompaneroWoW_ShareKey()
check("off con el marco abierto: la tecla no lo cierra", UI.IsShown())

-- ---------------------------------------------------------------------------
outln("(j) Textos y ficheros")
local tocText = io.open("CompaneroWoW/CompaneroWoW.toc", "rb"):read("*a")
check("toc: carga Markup.lua, Ask.lua y UI.lua", string.find(tocText, "Markup.lua", 1, true) and string.find(tocText, "Ask.lua", 1, true) and string.find(tocText, "UI.lua", 1, true))
check("toc: Markup.lua antes que UI.lua", string.find(tocText, "Markup.lua", 1, true) < string.find(tocText, "UI.lua", 1, true))

if failures > 0 then
    outln("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
outln("ALL TESTS PASSED")
os.exit(0)
