-- Combat.lua: revision de los ultimos 10 combates (COMBAT REVIEW CONTRACT).
-- Escribe en CompaneroWoWDB.characters[key].combats (el mas nuevo primero).
-- Solo LEE la API oficial de C_DamageMeter (Blizzard_DamageMeter) y datos no secretos.
--
-- Trabajo dentro de combate: solo la marca de inicio (tiempo, zona, objetivos) al entrar
-- (PLAYER_REGEN_DISABLED). No se lee C_DamageMeter durante el combate, salvo al morir
-- (PLAYER_DEAD): la muerte cierra un registro provisional que se actualiza al salir de combate.
-- Cada lectura se protege: valores secretos (issecretvalue) se descartan sin error.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean

local C = {}
ns.Combat = C

local MAX_RECORDS = 10        -- ring buffer: se conservan los 10 ultimos
local MIN_DURATION = 3        -- s: por debajo y sin dano, el combate es trivial y no se guarda
local FINALIZE_DELAY = 1      -- s tras salir de combate (o morir) antes de leer la sesion
local TOP_SPELLS = 12
local TOP_SOURCES = 8
local TOP_LAST_HITS = 5
local MAX_TARGETS = 5
local MAX_RECENT_TARGETS = 3  -- objetivos vistos fuera de combate desde el ultimo combate

-- UnitClassification -> valores del contrato (rareelite cuenta como rare).
local CLASSIFICATION = {
    normal = "normal", trivial = "normal", minus = "normal",
    elite = "elite", rare = "rare", rareelite = "rare", worldboss = "worldboss",
}

-- Estado en memoria.
local fight = nil   -- combate en curso: marca de inicio, datos de fin y registro provisional
local recent = {}   -- objetivos seleccionados fuera de combate (los mas recientes)

-- ---------------------------------------------------------------------------
-- Utilidades
-- ---------------------------------------------------------------------------

local function enumValue(group, key)
    local E = _G.Enum
    if type(E) ~= "table" or type(E[group]) ~= "table" then return nil end
    return E[group][key]
end

local function sessionCurrent() return enumValue("DamageMeterSessionType", "Current") end

-- true si la API de sesiones, sus enumeraciones y el medidor activo existen (si no, no se registra nada).
function C.Available()
    if ns.Get("C_DamageMeter.GetCombatSessionFromType") == nil then return false end
    if sessionCurrent() == nil or enumValue("DamageMeterType", "DamageDone") == nil then return false end
    if ns.Get("C_DamageMeter.IsDamageMeterAvailable") then
        return Clean(Call("C_DamageMeter.IsDamageMeterAvailable")) == true
    end
    return true
end

local function nowClock()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function round1(v)
    return math.floor(v * 10 + 0.5) / 10
end

local function pct(v, max)
    if v == nil or max == nil or max <= 0 then return nil end
    return math.floor(v * 100 / max + 0.5)
end

local function spellName(spellID)
    return Str(Call("C_Spell.GetSpellName", spellID))
end

-- ---------------------------------------------------------------------------
-- Objetivos
-- ---------------------------------------------------------------------------

-- Objetivo actual si es un enemigo con nombre no secreto. nil si no hay o es secreto.
local function readTarget()
    if Clean(Call("UnitExists", "target")) ~= true then return nil end
    if Clean(Call("UnitCanAttack", "player", "target")) ~= true then return nil end
    local name = Str(Call("UnitName", "target"))
    if not name then return nil end
    local t = { name = name }
    local kind, npcID = ns.ParseGUID(Str(Call("UnitGUID", "target")))
    if (kind == "Creature" or kind == "Vehicle") and npcID then t.npcID = npcID end
    local level = Num(Call("UnitLevel", "target"))
    if level then t.level = level end
    local cls = Str(Call("UnitClassification", "target"))
    if cls and CLASSIFICATION[cls] then t.classification = CLASSIFICATION[cls] end
    return t
end

local function sameTarget(a, b)
    if a.npcID and b.npcID then return a.npcID == b.npcID end
    return a.name == b.name
end

local function addTarget(list, t, cap)
    if not t then return end
    for _, o in ipairs(list) do
        if sameTarget(o, t) then return end
    end
    if #list < cap then list[#list + 1] = t end
end

-- Objetivos fuera de combate: se conservan los MAX_RECENT_TARGETS mas recientes.
local function pushRecent(t)
    if not t then return end
    for i, o in ipairs(recent) do
        if sameTarget(o, t) then
            table.remove(recent, i)
            break
        end
    end
    recent[#recent + 1] = t
    while #recent > MAX_RECENT_TARGETS do table.remove(recent, 1) end
end

-- ---------------------------------------------------------------------------
-- Marcas de combate
-- ---------------------------------------------------------------------------

local function startFight()
    local targets = {}
    for _, t in ipairs(recent) do addTarget(targets, t, MAX_TARGETS) end
    recent = {}
    addTarget(targets, readTarget(), MAX_TARGETS)
    fight = {
        startClock = nowClock(),
        startedAt = ns.Now(),
        zone = Str(Call("GetRealZoneText")),
        mapID = Num(Call("C_Map.GetBestMapForUnit", "player")),
        inInstance = Clean(Call("IsInInstance")) == true,
        targets = targets,
        died = false,
    }
end

-- Lee la sesion cuando ya no hay trabajo pendiente (tras un retardo corto).
local function schedule(f)
    local function run() ns.SafeCall(C.Finalize, f) end
    if ns.Get("C_Timer.After") then
        ns.Call("C_Timer.After", FINALIZE_DELAY, run)
    else
        run()
    end
end

local function onRegenEnabled()
    if not fight then return end
    addTarget(fight.targets, readTarget(), MAX_TARGETS)
    fight.endClock = nowClock()
    fight.endedAt = ns.Now()
    local f = fight
    fight = nil
    schedule(f)
end

local function onDead()
    if not fight then return end -- muerte fuera de combate: no hay combate que registrar
    fight.died = true
    fight.diedClock = nowClock()
    schedule(fight)
end

-- ---------------------------------------------------------------------------
-- Lectura de sesiones (C_DamageMeter, sesion Current = el combate que acaba de terminar)
-- ---------------------------------------------------------------------------

local function readSession(meter)
    local s = Clean(Call("C_DamageMeter.GetCombatSessionFromType", sessionCurrent(), meter))
    if type(s) == "table" then return s end
    return nil
end

-- Fuente de un tipo de medidor (jugador o guid): totalAmount y combatSpells.
local function readSource(meter, guid)
    if not guid then return nil end
    local src = Clean(Call("C_DamageMeter.GetCombatSessionSourceFromType", sessionCurrent(), meter, guid))
    if type(src) == "table" then return src end
    return nil
end

-- GUID del jugador dentro de la sesion (isLocalPlayer); si no, UnitGUID("player").
local function localGUID(session)
    local list = session and Clean(session.combatSources)
    if type(list) == "table" then
        for _, src in ipairs(list) do
            src = Clean(src)
            if type(src) == "table" and Clean(src.isLocalPlayer) == true then
                local g = Str(src.sourceGUID)
                if g then return g end
            end
        end
    end
    return Str(Call("UnitGUID", "player"))
end

-- Lista de hechizos de una fuente, mayor cantidad primero: { spellID, amount, creature }.
local function spellList(src)
    local out = {}
    local list = src and Clean(src.combatSpells)
    if type(list) ~= "table" then return out end
    for _, sp in ipairs(list) do
        sp = Clean(sp)
        if type(sp) == "table" then
            local id, amount = Num(sp.spellID), Num(sp.totalAmount)
            if id and amount then
                out[#out + 1] = { spellID = id, amount = amount, creature = Str(sp.creatureName) }
            end
        end
    end
    table.sort(out, function(a, b) return a.amount > b.amount end)
    return out
end

-- Filas para el registro: spellID, name, amount (hits no lo expone la API: no se rellena).
local function spellRows(list, limit)
    local out = {}
    for i = 1, math.min(#list, limit) do
        local e = list[i]
        local row = { spellID = e.spellID, amount = e.amount }
        row.name = spellName(e.spellID)
        out[#out + 1] = row
    end
    return out
end

-- Dano recibido por origen (creatureName), agregado por nombre; spellID = 0.
local function damageSources(list)
    local byName, names = {}, {}
    for _, e in ipairs(list) do
        local name = e.creature
        if name then
            if not byName[name] then
                byName[name] = 0
                names[#names + 1] = name
            end
            byName[name] = byName[name] + e.amount
        end
    end
    local out = {}
    for _, name in ipairs(names) do out[#out + 1] = { name = name, spellID = 0, amount = byName[name] } end
    table.sort(out, function(a, b) return a.amount > b.amount end)
    local capped = {}
    for i = 1, math.min(#out, TOP_SOURCES) do capped[i] = out[i] end
    return capped
end

-- Registro de un combate a partir de la sesion actual. nil si es trivial (sin datos utiles).
function C.Build(f)
    local endClock = f.endClock or f.diedClock or nowClock()
    local duration = round1(math.max(0, endClock - f.startClock))
    local session = readSession(enumValue("DamageMeterType", "DamageDone"))
    local guid = localGUID(session)

    local dmgSrc = readSource(enumValue("DamageMeterType", "DamageDone"), guid)
    local healSrc = readSource(enumValue("DamageMeterType", "HealingDone"), guid)
    local takenSrc = readSource(enumValue("DamageMeterType", "DamageTaken"), guid)
    local dmgTotal = dmgSrc and Num(dmgSrc.totalAmount)
    local dmgSpells = spellList(dmgSrc)
    local healSpells = spellList(healSrc)
    local takenSpells = spellList(takenSrc)

    if not f.died and (dmgTotal or 0) == 0 and duration < MIN_DURATION then return nil end

    local rec = {
        startedAt = f.startedAt,
        endedAt = f.endedAt or ns.Now(),
        duration = duration,
        died = f.died == true,
        zone = f.zone,
        mapID = f.mapID,
        inInstance = f.inInstance == true,
        targets = f.targets,
    }
    if dmgTotal or #dmgSpells > 0 then
        rec.damageDone = {
            total = dmgTotal,
            dps = (dmgTotal and duration > 0) and round1(dmgTotal / duration) or nil,
            spells = spellRows(dmgSpells, TOP_SPELLS),
        }
    end
    local healTotal = healSrc and Num(healSrc.totalAmount)
    if healTotal or #healSpells > 0 then
        rec.healingDone = { total = healTotal, spells = spellRows(healSpells, TOP_SPELLS) }
    end
    local takenTotal = takenSrc and Num(takenSrc.totalAmount)
    if takenTotal or #takenSpells > 0 then
        rec.damageTaken = { total = takenTotal, sources = damageSources(takenSpells) }
    end
    if rec.died then
        -- Mejor esfuerzo: la API no da orden temporal; se listan los origenes de mayor dano recibido.
        local lastHits = {}
        for i = 1, math.min(#takenSpells, TOP_LAST_HITS) do
            local e = takenSpells[i]
            lastHits[i] = { source = e.creature, spellName = spellName(e.spellID), amount = e.amount }
        end
        rec.death = { killer = lastHits[1] and lastHits[1].source or nil, lastHits = lastHits }
    end
    local hpPct = pct(Num(Call("UnitHealth", "player")), Num(Call("UnitHealthMax", "player")))
    local powerPct = pct(Num(Call("UnitPower", "player")), Num(Call("UnitPowerMax", "player")))
    if hpPct or powerPct then rec.resourcesAtEnd = { healthPct = hpPct, powerPct = powerPct } end
    return rec
end

-- Escribe el registro: un combate ya guardado (mismo recordId) se sustituye; si no, entra el primero.
local function store(f, rec)
    local key = ns.CharKey()
    if not key then return end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    if type(c.combats) ~= "table" then c.combats = {} end
    local list = c.combats
    if f.recordId then
        for i, r in ipairs(list) do
            if r.id == f.recordId then
                rec.id = f.recordId
                list[i] = rec
                return
            end
        end
    end
    local maxId = type(c.combatSeq) == "number" and c.combatSeq or 0
    for _, r in ipairs(list) do
        if type(r.id) == "number" and r.id > maxId then maxId = r.id end
    end
    rec.id = maxId + 1
    c.combatSeq = rec.id
    f.recordId = rec.id
    table.insert(list, 1, rec)
    while #list > MAX_RECORDS do table.remove(list) end
end

-- Lee la sesion y guarda el registro del combate f. Idempotente por combate (recordId).
function C.Finalize(f)
    if not C.Available() then return false end
    local rec = C.Build(f)
    if not rec then return false end
    store(f, rec)
    return true
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

local handlers = {
    PLAYER_REGEN_DISABLED = function()
        if not fight then startFight() end
    end,
    PLAYER_REGEN_ENABLED = onRegenEnabled,
    PLAYER_DEAD = onDead,
    PLAYER_TARGET_CHANGED = function()
        if fight or ns.InCombat() then return end
        pushRecent(readTarget())
    end,
}

function C.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(C.OnEvent, event, ...)
    end)
end

-- ---------------------------------------------------------------------------
-- Estado para /compa estado
-- ---------------------------------------------------------------------------

-- Resumen de un combate en una linea.
function C.Summary(r)
    local dmg = r.damageDone and r.damageDone.total
    local heal = r.healingDone and r.healingDone.total
    return string.format("%s, %.1f s, daño %d, curación %d%s",
        r.zone or "zona desconocida", r.duration or 0,
        math.floor(dmg or 0), math.floor(heal or 0),
        r.died and ", murió" or "")
end

function C.StatusLines()
    local key = ns.CharKey()
    local db = ns.EnsureDB()
    local c = key and db.characters[key]
    local list = (type(c) == "table" and type(c.combats) == "table") and c.combats or {}
    local lines = { string.format("Combates guardados: %d de %d", #list, MAX_RECORDS) }
    if list[1] then lines[#lines + 1] = "Último combate: " .. C.Summary(list[1]) end
    return lines
end

-- Para pruebas: borra el estado en memoria.
function C.ResetForTests()
    fight = nil
    recent = {}
end

ns.CombatStatusLines = C.StatusLines

return C
