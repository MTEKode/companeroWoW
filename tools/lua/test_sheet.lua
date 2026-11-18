-- test_sheet.lua: pruebas offline de la hoja de personaje y datos de hechizos (Sheet.lua).
-- Cubre: forma del contrato (stats, armadura, combate, defensa, resistencias, forma, talentos,
-- hechizos), valores secretos y APIs ausentes (se omiten, sin errores), descripcion por escaneo
-- de tooltip, refresco por eventos con throttle y aplazado en combate, no reescribir lo mismo,
-- mensaje de canal tipo 12 (reensamblado), tope de 80 hechizos y 40 KB, y /compa estado.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_sheet.lua
package.path = "tools/lua/?.lua;" .. package.path
local W = require("wowstub")

local ns = {}
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

local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end

-- Entorno comun: stubs de juego y de combate/mundo que Core.lua aporta en el addon real.
W.installGameAPI()
W.installSheet()
ns.InCombat = function() return W.state.inCombat == true end
ns.IsFullyInWorld = function() return true end
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Channel.lua")
load("CompaneroWoW/Sheet.lua")

local SH, C, J = ns.Sheet, ns.Channel, ns.Json
local KEY = "Tester-Ravencrest"

-- Lista de hechizos que devuelve CollectSpells (la parte de Collectors.lua queda fuera de esta prueba).
local SPELLS = {}
ns.CollectSpells = function() return SPELLS end

local function defaultSpells()
    return {
        { spellID = 772, name = "Rend", rank = "Rank 2" },
        { spellID = 100, name = "Charge" },
        { spellID = 12294, name = "Mortal Strike" },
    }
end

-- Reinicia datos, estado del addon y la base de datos del personaje.
local function fresh()
    W.reset()
    W.uninstallSheet()
    W.installSheet()
    W.state.level = 19
    W.state.inCombat = false
    W.state.clock = 100
    W.state.now = 1700000000
    W.state.timers = {}
    CompaneroWoWDB = { characters = {} }
    SH.testing.reset()
    -- Cola del canal limpia: un mensaje a medio enviar de otra seccion no debe completarse aqui.
    C.state.sched = nil
    C.state.carouselAt = nil
    C.state.carouselTodo = nil
    for i = #SPELLS, 1, -1 do SPELLS[i] = nil end
    for _, s in ipairs(defaultSpells()) do SPELLS[#SPELLS + 1] = s end
end

local function sheetOf()
    local c = CompaneroWoWDB.characters[KEY]
    return c and c.sheet
end

-- Fila/fila de bloques del mensaje tipo 12 reensamblado desde los trozos del canal.
local function type12Messages(frames)
    local out, pending = {}, {}
    for _, ch in ipairs(frames) do
        if ch.type == 12 then
            local p = pending[ch.id]
            if not p then
                p = { count = ch.count, parts = {}, got = 0 }
                pending[ch.id] = p
            end
            if p.parts[ch.index + 1] == nil then
                p.parts[ch.index + 1] = ch.data
                p.got = p.got + 1
            end
            if p.got == p.count then
                local bytes = {}
                for i = 1, p.count do
                    for _, b in ipairs(p.parts[i]) do bytes[#bytes + 1] = string.char(b) end
                end
                out[#out + 1] = table.concat(bytes)
                pending[ch.id] = nil
            end
        end
    end
    return out
end

-- ---------------------------------------------------------------------------
print("(a) Forma del contrato con valores de referencia")
fresh()
ns.Sheet.testing.refreshNow()
local s = sheetOf()
check("hoja escrita en characters[key].sheet", type(s) == "table")
eq("level", s.level, 19)
eq("class", s.class, "WARRIOR")
eq("race", s.race, "Human")
eq("stance", s.stance, "Battle Stance")
eq("base.str", s.base.str, 60)
eq("total.str", s.total.str, 78)
eq("base.spi", s.base.spi, 30)
eq("health", s.health, 900)
eq("healthMax", s.healthMax, 900)
eq("power", s.power, 0)
eq("powerMax", s.powerMax, 100)
eq("powerType", s.powerType, "RAGE")
eq("armor", s.armor, 620)
eq("armorEffective", s.armorEffective, 620)
eq("melee.ap", s.melee.ap, 190)
eq("melee.apBase", s.melee.apBase, 180)
eq("melee.apPos", s.melee.apPos, 10)
eq("melee.apNeg", s.melee.apNeg, 0)
eq("melee.crit", s.melee.crit, 6.2)
eq("melee.hit", s.melee.hit, 0)
eq("melee.expertise", s.melee.expertise, 0)
eq("melee.mhMin", s.melee.mhMin, 30.1)
eq("melee.mhMax", s.melee.mhMax, 45.4)
eq("melee.mhSpeed", s.melee.mhSpeed, 2.6)
eq("melee.attackSpeed", s.melee.attackSpeed, 2.6)
eq("melee.mhSkill (2H sword)", s.melee.mhSkill, 95)
check("sin mano secundaria: ohSpeed/ohMin/ohMax/ohSkill ausentes",
    s.melee.ohSpeed == nil and s.melee.ohMin == nil and s.melee.ohMax == nil and s.melee.ohSkill == nil)
eq("ranged.ap", s.ranged.ap, 30)
eq("ranged.crit", s.ranged.crit, 4.0)
eq("ranged.min", s.ranged.min, 10)
eq("ranged.max", s.ranged.max, 20)
eq("ranged.speed", s.ranged.speed, 2.9)
eq("defense.dodge", s.defense.dodge, 5.1)
eq("defense.parry", s.defense.parry, 5.0)
eq("defense.block", s.defense.block, 0)
eq("defense.blockValue", s.defense.blockValue, 0)
eq("defense.defenseSkill", s.defense.defenseSkill, 95)
eq("spell.power (max de escuelas)", s.spell.power, 45)
eq("spell.crit", s.spell.crit, 0)
eq("spell.hit", s.spell.hit, 0)
eq("spell.haste", s.spell.haste, 0)
eq("regen.manaBase", s.regen.manaBase, 0)
eq("resist.fire", s.resist.fire, 0)
eq("resist.arcane", s.resist.arcane, 0)
check("exportedAt numerico", type(s.exportedAt) == "number")
check("weaponSkills solo armas: Two-Handed Swords 95/95",
    #s.weaponSkills == 1 and s.weaponSkills[1].name == "Two-Handed Swords"
    and s.weaponSkills[1].rank == 95 and s.weaponSkills[1].max == 95)

-- Talentos: Arms 1 + 2 + 0 = 3, Fury 0, Protection 2; 1 punto sin gastar.
check("talentos: tres arboles", s.talents and #s.talents.tabs == 3)
eq("talentos: unspent", s.talents.unspent, 1)
eq("talentos: Arms nombre", s.talents.tabs[1].name, "Arms")
eq("talentos: Arms puntos", s.talents.tabs[1].points, 3)
eq("talentos: Protection puntos", s.talents.tabs[3].points, 2)
eq("talentos: Fury puntos", s.talents.tabs[2].points, 0)
local ir = s.talents.tabs[1].talents[2]
eq("talento Improved Rend nombre", ir.name, "Improved Rend")
eq("talento Improved Rend rango", ir.rank, 2)
eq("talento Improved Rend maxRank", ir.maxRank, 3)
eq("talento Improved Rend fila", ir.row, 1)
eq("talento Improved Rend columna", ir.col, 2)
eq("talento Improved Rend spellID", ir.spellID, 12286)
local ss = s.talents.tabs[1].talents[3]
eq("talento sin entrada activa usa la primera entrada", ss.name, "Sweeping Strikes")
eq("talento sin aprender: rango 0", ss.rank, 0)
eq("talento sin aprender: fila 2", ss.row, 2)

-- Hechizos: 3 conocidos, con coste, lanzamiento, enfriamiento, alcance y descripcion.
eq("hechizos: 3 entradas", #s.spells, 3)
local rend = s.spells[1]
eq("Rend spellID", rend.spellID, 772)
eq("Rend nombre", rend.name, "Rend")
eq("Rend rango", rend.rank, "Rank 2")
eq("Rend conocido", rend.known, true)
eq("Rend coste", rend.cost, 10)
eq("Rend powerType", rend.powerType, "RAGE")
eq("Rend castTime (s)", rend.castTime, 0)
eq("Rend cooldown (s)", rend.cooldown, 0)
eq("Rend range", rend.range, 5)
eq("Rend descripcion del cliente", rend.tooltip,
    "Wounds the target causing them to bleed for 63 damage over 12 sec.")
local charge = s.spells[2]
eq("Charge cooldown (s)", charge.cooldown, 15)
eq("Charge range", charge.range, 25)
check("Charge sin coste: cost y powerType ausentes", charge.cost == nil and charge.powerType == nil)

-- ---------------------------------------------------------------------------
print("(b) Valores secretos: se omiten, el resto sigue")
fresh()
W.SHEET.secret.UnitStat = true
W.SHEET.descSecret[772] = true
ns.Sheet.testing.refreshNow()
s = sheetOf()
check("stats secretas omitidas (base y total)", s.base.str == nil and s.total.str == nil and s.base.agi == nil)
eq("AP no secreto no afectado", s.melee.apBase, 180)
check("descripcion secreta: tooltip omitido", s.spells[1].tooltip == nil)
eq("descripcion secreta: el resto del hechizo se conserva", s.spells[1].cost, 10)
eq("sin secretos en combate: AP intacto", s.melee.ap, 190)
W.SHEET.secret.UnitArmor = true
ns.Sheet.testing.refreshNow() -- mismo contenido salvo armadura: debe reescribir
s = sheetOf()
check("armadura secreta omitida", s.armor == nil and s.armorEffective == nil)

-- ---------------------------------------------------------------------------
print("(c) APIs ausentes: se omiten, sin errores contados")
fresh()
local errorsBefore = ns.errorCount
_G.GetDodgeChance = nil
_G.GetShieldBlock = nil
_G.C_Spell.GetSpellPowerCost = nil
_G.UnitRangedDamage = nil
ns.Sheet.testing.refreshNow()
s = sheetOf()
check("dodge ausente: no hay valor (con el resto de defensa)", s.defense.dodge == nil and s.defense.parry == 5)
check("blockValue ausente: no hay valor", s.defense.blockValue == nil)
check("sin coste: el hechizo queda sin cost/powerType", s.spells[1].cost == nil and s.spells[1].name == "Rend")
check("sin UnitRangedDamage: sin speed/min/max", s.ranged.speed == nil and s.ranged.min == nil)
eq("sin API no cuenta como error", ns.errorCount, errorsBefore)

-- Sin API de talentos (C_ClassTalents ausente): talents no se escribe.
fresh()
_G.C_ClassTalents = nil
ns.Sheet.testing.refreshNow()
check("sin API de talentos: talents ausente", sheetOf().talents == nil)
check("sin API de talentos: hoja igualmente escrita", sheetOf() ~= nil and sheetOf().level == 19)

-- Sin forma (postura): stance ausente.
fresh()
W.SHEET.form = 0
ns.Sheet.testing.refreshNow()
check("sin forma: stance ausente", sheetOf().stance == nil)

-- ---------------------------------------------------------------------------
print("(d) Descripcion: respaldo por escaneo de tooltip oculto")
fresh()
ns.Sheet.testing.refreshNow()
s = sheetOf()
local mortal = s.spells[3]
eq("sin GetSpellDescription: tooltip por escaneo (sin la linea del nombre)", mortal.tooltip,
    "Deals 160% weapon damage. Cost: 30 Rage")
eq("escaneo: coste desde C_Spell.GetSpellPowerCost", mortal.cost, 30)
eq("escaneo: enfriamiento 6 s", mortal.cooldown, 6)
check("el tooltip oculto se crea una vez (creacion registrada)", #W.SHEET.tipCalls >= 1)

-- ---------------------------------------------------------------------------
print("(e) Talentos sin modelo moderno: respaldo clasico")
fresh()
_G.C_ClassTalents = nil
_G.C_Traits = nil
_G.GetNumTalentTabs = function() return 1 end
_G.GetTalentTabInfo = function(t) return 1, "Armas", "", "", 4 end
_G.GetNumTalents = function(t) return 1 end
_G.GetTalentInfo = function(t, i) return "Golpe heroico", "", 1, 1, 2, 3, false, true, 1234 end
ns.Sheet.testing.refreshNow()
s = sheetOf()
eq("clasico: nombre de arbol", s.talents.tabs[1].name, "Armas")
eq("clasico: puntos del arbol", s.talents.tabs[1].points, 4)
eq("clasico: talento y spellID", s.talents.tabs[1].talents[1].spellID, 1234)
eq("clasico: rango y maximo", s.talents.tabs[1].talents[1].maxRank, 3)
_G.GetNumTalentTabs = nil
_G.GetTalentTabInfo = nil
_G.GetNumTalents = nil
_G.GetTalentInfo = nil

-- ---------------------------------------------------------------------------
print("(f) Refresco por eventos, throttle y combate")
fresh()
local frames = W.sheetFrames
local regFrame = nil
for _, f in ipairs(frames) do
    for _, ev in ipairs(f.events) do
        if ev == "UNIT_STATS" then regFrame = f end
    end
end
check("frame de eventos registrado", regFrame ~= nil)
local wanted = { "UNIT_STATS", "PLAYER_EQUIPMENT_CHANGED", "CHARACTER_POINTS_CHANGED", "SPELLS_CHANGED",
                 "PLAYER_LEVEL_UP", "UPDATE_SHAPESHIFT_FORM", "PLAYER_REGEN_ENABLED" }
local allRegistered = true
for _, ev in ipairs(wanted) do
    local found = false
    for _, r in ipairs(regFrame.events) do if r == ev then found = true end end
    if not found then allRegistered = false end
end
check("eventos del contrato registrados", allRegistered)

local function fire(event, arg)
    regFrame.onEvent(regFrame, event, arg)
end

fire("UNIT_STATS", "player")
check("UNIT_STATS del jugador escribe la hoja", sheetOf() ~= nil)
local writes0 = SH.stats.writes
fire("UNIT_STATS", "target") -- otra unidad: ignorado
eq("UNIT_STATS de otra unidad: no escribe", SH.stats.writes, writes0)

-- Throttle: un cambio dentro de 2 s se aplaza con un temporizador.
W.SHEET.ap[1] = 200
W.state.clock = 101
fire("PLAYER_EQUIPMENT_CHANGED")
eq("dentro de 2 s: no escribe todavia", SH.stats.writes, writes0)
eq("dentro de 2 s: deja un temporizador de 1 s", #W.callsNamed("C_Timer.After") >= 1
    and W.callsNamed("C_Timer.After")[#W.callsNamed("C_Timer.After")].args[1], 1)
W.state.clock = 102
W.runTimers()
eq("al vencer el temporizador: escribe el cambio", sheetOf().melee.ap, 200 + 10)

-- Mismo contenido: no reescribe.
local writes1 = SH.stats.writes
W.state.clock = 110
fire("SPELLS_CHANGED")
eq("contenido igual: no reescribe", SH.stats.writes, writes1)

-- Combate: se difiere hasta PLAYER_REGEN_ENABLED.
W.SHEET.ap[1] = 300
W.state.clock = 120
W.state.inCombat = true
fire("UNIT_STATS", "player")
eq("en combate: no escribe", SH.stats.writes, writes1)
eq("en combate: la hoja anterior se conserva", sheetOf().melee.ap, 210)
W.state.inCombat = false
fire("PLAYER_REGEN_ENABLED")
eq("al salir de combate: escribe lo pendiente", sheetOf().melee.ap, 310)
check("al salir de combate: el cambio se marca en el canal", SH.stats.writes > writes1)

-- ExportSheetNow en combate devuelve false y deja pendiente.
W.state.inCombat = true
eq("ExportSheetNow en combate: false", ns.ExportSheetNow(), false)
W.state.inCombat = false

-- ---------------------------------------------------------------------------
print("(g) Tope de hechizos y de tamano")
fresh()
SPELLS = {}
ns.CollectSpells = function() return SPELLS end
local long = string.rep("Inflige daño a gran escala. ", 20) -- ~560 caracteres
for i = 1, 100 do
    SPELLS[#SPELLS + 1] = { spellID = 50000 + i, name = "Hechizo " .. i }
    W.SHEET.desc[50000 + i] = long .. i
    W.SHEET.spellInfo[50000 + i] = { castTime = 0, maxRange = 5 }
    W.SHEET.cooldownMs[50000 + i] = 0
end
ns.Sheet.testing.refreshNow()
s = sheetOf()
check("como mucho 80 hechizos", #s.spells <= 80)
check("100 hechizos largos: se recortan para caber en 40 KB", #s.spells > 0 and #s.spells < 80)
local json = J.Encode(s)
check("hoja < 40 KB", json ~= nil and #json <= 40000, "bytes=" .. tostring(json and #json))
eq("tope de hechizos: 80 con descripcion corta", (function()
    fresh()
    SPELLS = {}
    ns.CollectSpells = function() return SPELLS end
    for i = 1, 100 do
        SPELLS[#SPELLS + 1] = { spellID = 60000 + i, name = "H" .. i }
        W.SHEET.desc[60000 + i] = "Corto."
    end
    ns.Sheet.testing.refreshNow()
    return #sheetOf().spells
end)(), 80)
check("tamano con 80 descripciones cortas < 40 KB",
    #J.Encode(sheetOf()) <= 40000)

-- ---------------------------------------------------------------------------
print("(h) Mensaje del canal tipo 12")
fresh()
ns.CollectSpells = function() return defaultSpells() end
local prio = C.PRIORITY
local pos12, pos4
for i, t in ipairs(prio) do
    if t == 12 then pos12 = i end
    if t == 4 then pos4 = i end
end
eq("tipo 12 justo despues de equipment (4)", pos12, pos4 + 1)
eq("tipo 12 en la tabla de rangos", C.RANK[12], pos12)
ns.Sheet.testing.refreshNow()
local chunks = {}
local now = 1000
for _ = 1, 400 do
    now = now + 1
    local ch = C.Tick(now, 860)
    if ch then chunks[#chunks + 1] = ch end
end
local msgs = type12Messages(chunks)
check("mensaje tipo 12 enviado por el canal", #msgs >= 1)
local want = J.Encode(sheetOf())
local matched = false
for _, m in ipairs(msgs) do if m == want then matched = true end end
check("el mensaje reensamblado es el JSON de la hoja", matched)

-- ---------------------------------------------------------------------------
print("(i) /compa estado")
fresh()
eq("sin hoja: linea de estado", SH.StatusLine(), "Hoja: sin datos")
CompaneroWoWDB = { characters = {} } -- no crea registro al mirar el estado
eq("sin registro del personaje: no lo crea", CompaneroWoWDB.characters[KEY], nil)
ns.Sheet.testing.refreshNow()
eq("linea de estado con los valores de referencia",
    SH.StatusLine(), "Hoja: AP 190, crit 6.2 %, talentos 3/0/2, hechizos 3")

-- ---------------------------------------------------------------------------
print("(j) Limpieza de texto de tooltip")
eq("quita colores y texturas", SH.testing.cleanText("|cffffd100Golpe|r |TIcon:12|t  fuerte"), "Golpe fuerte")
eq("solo espacios -> nil", SH.testing.cleanText("   "), nil)

-- ---------------------------------------------------------------------------
print("(k) Tipo 12 en la tabla de tipos del canal")
eq("nombre del tipo 12", C.TYPES[12].name, "sheet")
check("tipo 12 sin eventos propios (lo marca Sheet.lua)", #C.TYPES[12].events == 0)
check("la hoja cabe en el limite del mensaje del canal",
    #J.Encode(sheetOf()) <= C.MAX_MESSAGE_BYTES)

-- ---------------------------------------------------------------------------
print("(l) Carrusel: el tipo 12 solo viaja si hay hoja; sin hoja no gasta turnos")
fresh()
ns.CollectSpells = function() return defaultSpells() end
local function drain(n)
    local out = {}
    for _ = 1, n do
        now = now + 1
        local ch = C.Tick(now, 860)
        if ch then out[#out + 1] = ch end
    end
    return out
end
now = now + 100
C.ForceCarousel()
local noSheet = type12Messages(drain(400))
eq("sin hoja: ningun mensaje tipo 12", #noSheet, 0)
ns.Sheet.testing.refreshNow()
C.ForceCarousel()
local withSheet = type12Messages(drain(400))
check("con hoja: el carrusel reenvia el tipo 12", #withSheet >= 1)
local found = false
for _, m in ipairs(withSheet) do if m == J.Encode(sheetOf()) then found = true end end
check("carrusel: mensaje igual al JSON de la hoja", found)

W.uninstallSheet()

if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
os.exit(0)
