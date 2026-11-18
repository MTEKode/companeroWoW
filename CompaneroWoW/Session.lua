-- Session.lua: historial de la sesion del personaje (contrato ON-DEMAND, bloque characters[key].session).
-- La app ya no lee el juego en segundo plano: el addon lleva la sesion y la envia por el canal (tipo 14).
--
-- Inicio: el primer dato valido tras PLAYER_LOGIN, o tras >= 10 min sin actividad.
--   Tras un /reload se reanuda si la ultima actividad fue hace < 10 min (c.sessionBase.activeAt);
--   el XP y los niveles ganados durante el /reload se cuentan igual (se compara con la linea base guardada).
-- Actividad (muestreo cada 5 s por C_Timer): no AFK y (cambio de XP, de oro o de posicion, un evento
--   de mision, muerte o enemigo, o combate). Solo el tiempo activo cuenta en playedSec y timeSplit;
--   cada intervalo va a un unico cubo: combate > montura > descanso > otro.
-- XP (xpGained): suma los incrementos de UnitXP. Al subir de nivel suma lo que faltaba para
--   UnitXPMax del nivel anterior mas el XP del nuevo nivel. Si se saltan niveles entre lecturas,
--   los intermedios usan UnitXPMax actual (aproximacion).
-- Muestras (xpSamples): cada 60 s, las ultimas 60: { t, xp = xpGained de la sesion, level }.
--   xpPerHour: sobre la ventana de muestras (hasta 1 h de reloj); sin ventana, media por tiempo activo.
-- Enemigos (kills): evento PARTY_KILL, que no necesita combat log. arg1 = GUID de la victima; se
--   deduplica con los ultimos 50 GUID. Cuenta las muertes de enemigos acreditadas al jugador
--   (no solo el objetivo seleccionado).
-- Muertes: PLAYER_DEAD. Misiones: QUEST_TURNED_IN(questID) y QUEST_ACCEPTED(questLogIndex, questID).
-- Zonas: mapID (ns.GetPlayerPosition) de cada zona distinta visitada.
-- Sesiones anteriores (previous): las ultimas 5, dentro de la propia sesion.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num = ns.Call, ns.Num
local floor, max, abs = math.floor, math.max, math.abs
local format, concat = string.format, table.concat

local Ses = {}
ns.Session = Ses

Ses.TICK_SEC = 5           -- muestreo (C_Timer.NewTicker)
Ses.IDLE_SEC = 600         -- 10 min sin actividad: la sesion se cierra; un /reload mas corto la reanuda
Ses.SAMPLE_SEC = 60        -- una muestra de XP por minuto
Ses.MAX_SAMPLES = 60       -- la ventana de XP/h es de una hora
Ses.MAX_PREVIOUS = 5       -- sesiones anteriores guardadas
Ses.MAX_LIST = 300         -- tope de misiones y zonas por sesion
Ses.MAX_DT = 10            -- un intervalo mayor (salto de reloj, pantalla de carga) cuenta como mucho esto
Ses.MAX_RECENT_KILLS = 50  -- GUID de enemigos recientes para no contar dos veces la misma muerte
local POS_EPS = 0.0005     -- cambio de posicion (fraccion del mapa) que cuenta como movimiento

-- Estado en memoria. La sesion persistida es c.session (y su linea base c.sessionBase).
local cur, base = nil, nil
local lastClock = nil      -- GetTime del ultimo tick (no se guarda: tras /reload se empieza sin intervalo)
local pending = false      -- hubo un evento de actividad desde el ultimo tick
local started = false
local recentKills, recentSet = {}, {}

-- ---------------------------------------------------------------------------
-- Lectura del juego (todo protegido: un dato ausente o secreto no rompe nada)
-- ---------------------------------------------------------------------------

local function inWorld()
    return ns.IsFullyInWorld == nil or ns.IsFullyInWorld()
end

local function clockNow()
    return Num(Call("GetTime")) or ns.Now()
end

local function readState()
    local st = {
        level = Num(Call("UnitLevel", "player")),
        xp = Num(Call("UnitXP", "player")),
        max = Num(Call("UnitXPMax", "player")),
        money = Num(Call("GetMoney")),
        afk = ns.Clean(Call("UnitIsAFK", "player")) == true,
        combat = ns.InCombat ~= nil and ns.InCombat() == true,
        mounted = ns.Clean(Call("IsMounted")) == true,
        resting = ns.Clean(Call("IsResting")) == true,
    }
    if ns.GetPlayerPosition then
        st.mapID, st.x, st.y = ns.GetPlayerPosition()
    end
    return st
end

-- Tabla del personaje en CompaneroWoWDB.characters (la crea si falta).
local function charTable()
    local key = ns.CharKey and ns.CharKey()
    if not key then return nil end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    return c
end

-- Deja la sesion en el SavedVariables del personaje (son las mismas tablas: solo asegura la referencia).
local function store()
    local c = charTable()
    if c and cur then
        c.session = cur
        c.sessionBase = base
    end
end

-- ---------------------------------------------------------------------------
-- Formatos (texto en espanol)
-- ---------------------------------------------------------------------------

-- 12345 -> "12.345" (separador de miles con punto).
local function fmtInt(n)
    n = floor((n or 0) + 0.5)
    local neg = n < 0
    local s = format("%d", abs(n))
    local parts = {}
    while #s > 3 do
        table.insert(parts, 1, s:sub(-3))
        s = s:sub(1, -4)
    end
    table.insert(parts, 1, s)
    return (neg and "-" or "") .. concat(parts, ".")
end

-- Segundos -> "1 h 12 min", "45 min", "30 s".
local function fmtDur(sec)
    sec = max(0, floor((sec or 0) + 0.5))
    if sec < 60 then return format("%d s", sec) end
    local h = floor(sec / 3600)
    local m = floor((sec % 3600) / 60)
    if h > 0 then return format("%d h %d min", h, m) end
    return format("%d min", m)
end

-- Cobre -> "12 oro, 34 plata, 56 cobre".
local function fmtMoney(cu)
    cu = floor((cu or 0) + 0.5)
    local neg = cu < 0
    cu = abs(cu)
    local txt = format("%s oro, %d plata, %d cobre", fmtInt(floor(cu / 10000)), floor((cu % 10000) / 100), cu % 100)
    return (neg and "-" or "") .. txt
end

-- ---------------------------------------------------------------------------
-- XP, oro, zonas, tiempo, muestras
-- ---------------------------------------------------------------------------

local function rebase(st)
    base.level = st.level
    base.xp = st.xp
    if st.max then base.max = st.max end
end

-- Cuenta el XP desde la ultima lectura (incluye subidas de nivel). true si hubo ganancia.
local function accountXP(st)
    if not (st.level and st.xp) then return false end
    if base.level == nil then
        rebase(st)
        cur.level.now = st.level
        return false
    end
    local gained = 0
    if st.level > base.level then
        local levels = st.level - base.level
        gained = max(0, (base.max or 0) - (base.xp or 0)) + st.xp
        gained = gained + (levels - 1) * max(0, st.max or base.max or 0)
    elseif st.level == base.level and st.xp > base.xp then
        gained = st.xp - base.xp
    end
    cur.xpGained = cur.xpGained + gained
    cur.level.now = st.level
    rebase(st)
    return gained > 0
end

local function syncMoney(st)
    if st.money and cur then cur.money.now = st.money end
end

local function addZone(mapID)
    if not mapID or mapID <= 0 then return end
    local list = cur.zones
    for _, id in ipairs(list) do
        if id == mapID then return end
    end
    if #list < Ses.MAX_LIST then list[#list + 1] = mapID end
end

local function addId(list, id)
    if id and #list < Ses.MAX_LIST then list[#list + 1] = id end
end

-- Reparte dt segundos de tiempo activo entre los cubos (combate > montura > descanso > otro).
local function addTime(st, dt)
    dt = floor(dt + 0.5)
    if dt <= 0 then return end
    local ts = cur.timeSplit
    if st.combat then
        ts.combatSec = ts.combatSec + dt
    elseif st.mounted then
        ts.mountedSec = ts.mountedSec + dt
    elseif st.resting then
        ts.restingSec = ts.restingSec + dt
    else
        ts.otherSec = ts.otherSec + dt
    end
    cur.playedSec = cur.playedSec + dt
end

local function maybeSample(wall)
    if base.sampleAt and wall - base.sampleAt < Ses.SAMPLE_SEC then return end
    local list = cur.xpSamples
    list[#list + 1] = { t = wall, xp = cur.xpGained, level = cur.level.now }
    while #list > Ses.MAX_SAMPLES do table.remove(list, 1) end
    base.sampleAt = wall
end

-- XP por hora: ventana de muestras (si cubre al menos un minuto) o media por tiempo activo.
local function rateOf(s)
    local list = s.xpSamples
    local n = #list
    if n >= 2 then
        local a, b = list[1], list[n]
        if b.t - a.t >= Ses.SAMPLE_SEC then
            return (b.xp - a.xp) * 3600 / (b.t - a.t)
        end
    end
    if s.playedSec >= Ses.SAMPLE_SEC then
        return s.xpGained * 3600 / s.playedSec
    end
    return 0
end

local function positionChanged(st)
    if st.mapID ~= base.mapID then return true end
    if st.x == nil or base.x == nil then return false end
    return abs(st.x - base.x) > POS_EPS or abs(st.y - base.y) > POS_EPS
end

-- ---------------------------------------------------------------------------
-- Ciclo de vida: inicio, reanudacion, cierre por inactividad
-- ---------------------------------------------------------------------------

-- Copia de las sesiones anteriores + la sesion s (cerrada en endedAt). Maximo MAX_PREVIOUS.
local function entryOf(s, endedAt)
    return {
        startedAt = s.startedAt,
        endedAt = endedAt or s.updatedAt or s.startedAt,
        playedSec = s.playedSec or 0,
        xpGained = s.xpGained or 0,
        levelStart = s.level and s.level.start,
        levelEnd = s.level and s.level.now,
        moneyDelta = s.money and ((s.money.now or 0) - (s.money.start or 0)) or 0,
        questsCompleted = #(s.questsCompleted or {}),
        kills = s.kills or 0,
        deaths = s.deaths or 0,
    }
end

local function previousAfter(s, endedAt)
    local list = {}
    if type(s.previous) == "table" then
        for i, e in ipairs(s.previous) do list[i] = e end
    end
    list[#list + 1] = entryOf(s, endedAt)
    while #list > Ses.MAX_PREVIOUS do table.remove(list, 1) end
    return list
end

local function newSession(st, wall, previous)
    cur = {
        startedAt = wall,
        updatedAt = wall,
        playedSec = 0,
        level = { start = st.level, now = st.level },
        xpGained = 0,
        xpPerHour = 0,
        money = { start = st.money or 0, now = st.money or 0 },
        questsCompleted = {},
        questsAccepted = {},
        kills = 0,
        deaths = 0,
        zones = {},
        timeSplit = { combatSec = 0, mountedSec = 0, restingSec = 0, otherSec = 0 },
        xpSamples = {},
        previous = previous or {},
    }
    base = { level = st.level, xp = st.xp, max = st.max, mapID = st.mapID, x = st.x, y = st.y, activeAt = wall }
    addZone(st.mapID)
    lastClock = nil
    maybeSample(wall)
end

-- Arranque con la primera lectura valida: reanuda la sesion guardada o abre una nueva.
local function ensure(st)
    if cur then return true end
    local c = charTable()
    if not c then return false end
    local wall = ns.Now()
    local ps, pb = c.session, c.sessionBase
    local usable = type(ps) == "table" and type(ps.level) == "table" and type(ps.zones) == "table"
    if usable and type(pb) == "table" and type(pb.activeAt) == "number" and wall - pb.activeAt < Ses.IDLE_SEC then
        cur, base = ps, pb
        if type(cur.previous) ~= "table" then cur.previous = {} end
        accountXP(st)
        syncMoney(st)
        addZone(st.mapID)
        lastClock = nil
    else
        local prev = {}
        if usable then
            -- El XP ganado desde la ultima lectura guardada es de la sesion anterior.
            if type(pb) == "table" and pb.level ~= nil then
                cur, base = ps, pb
                accountXP(st)
                syncMoney(st)
            end
            prev = previousAfter(ps, type(pb) == "table" and pb.activeAt or ps.updatedAt)
        end
        newSession(st, wall, prev)
    end
    store()
    return true
end

-- Actividad tras una pausa de >= IDLE_SEC: cierra la sesion (en su ultima actividad) y abre otra.
local function rollover(st, wall)
    local prev = previousAfter(cur, base.activeAt)
    newSession(st, wall, prev)
end

local function ensureNow()
    if cur then return true end
    if not inWorld() then return false end
    local st = readState()
    if not (st.level and st.xp) then return false end
    return ensure(st)
end

-- Un tick de muestreo (cada TICK_SEC). Devuelve true si hubo lectura valida.
function Ses.Tick()
    if not inWorld() then return false end
    local st = readState()
    if not (st.level and st.xp) then return false end
    if not cur then return ensure(st) end

    local wall = ns.Now()
    local clock = clockNow()
    local dt = 0
    if lastClock then dt = clock - lastClock end
    lastClock = clock
    if dt < 0 then dt = 0 elseif dt > Ses.MAX_DT then dt = Ses.MAX_DT end

    local gainedXP = accountXP(st)
    syncMoney(st)
    addZone(st.mapID)
    local moved = positionChanged(st)
    local isActive = (not st.afk) and (pending or gainedXP or moved or st.combat)
    pending = false
    if isActive then
        if wall - base.activeAt >= Ses.IDLE_SEC then
            rollover(st, wall)
            dt = 0 -- el intervalo anterior a la actividad era de pausa
        end
        base.activeAt = wall
        addTime(st, dt)
    end
    base.mapID, base.x, base.y = st.mapID, st.x, st.y
    cur.updatedAt = wall
    maybeSample(wall)
    cur.xpPerHour = floor(rateOf(cur) + 0.5)
    store()
    return true
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

-- PLAYER_XP_UPDATE y PLAYER_LEVEL_UP: el XP se cuenta al instante (no espera al tick).
local function onXP()
    if not ensureNow() then return end
    if accountXP(readState()) then pending = true end
end

local function onMoney()
    if not ensureNow() then return end
    syncMoney(readState())
    pending = true
end

local function onKill(guid)
    if not ensureNow() then return end
    guid = ns.Str(guid)
    if guid then
        if recentSet[guid] then return end
        recentSet[guid] = true
        recentKills[#recentKills + 1] = guid
        if #recentKills > Ses.MAX_RECENT_KILLS then
            recentSet[table.remove(recentKills, 1)] = nil
        end
    end
    cur.kills = cur.kills + 1
    pending = true
end

local function onDeath()
    if not ensureNow() then return end
    cur.deaths = cur.deaths + 1
    pending = true
end

local function onTurnedIn(qid)
    if not ensureNow() then return end
    addId(cur.questsCompleted, Num(qid))
    pending = true
end

local function onAccepted(a, b)
    if not ensureNow() then return end
    addId(cur.questsAccepted, Num(b) or Num(a))
    pending = true
end

local function onZone()
    if not ensureNow() then return end
    if ns.GetPlayerPosition then addZone((ns.GetPlayerPosition())) end
end

local handlers = {
    PLAYER_LOGIN = function() Ses.Init() end,
    PLAYER_LEVEL_UP = onXP,
    PLAYER_XP_UPDATE = onXP,
    PLAYER_MONEY = onMoney,
    PARTY_KILL = onKill,
    PLAYER_DEAD = onDeath,
    QUEST_TURNED_IN = onTurnedIn,
    QUEST_ACCEPTED = onAccepted,
    ZONE_CHANGED_NEW_AREA = onZone,
}

function Ses.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

-- Arranca el muestreo (una vez por sesion de juego) y, si ya hay datos, la sesion.
function Ses.Init()
    if started then return end
    started = true
    ns.Call("C_Timer.NewTicker", Ses.TICK_SEC, function() ns.SafeCall(Ses.Tick) end)
    ensureNow()
end

local frame = ns.NewFrame and ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(Ses.OnEvent, event, ...)
    end)
end

-- ---------------------------------------------------------------------------
-- Canal (tipo 14) y comandos
-- ---------------------------------------------------------------------------

local LIST_KEYS = { "questsCompleted", "questsAccepted", "zones", "xpSamples", "previous" }

-- Copia de la sesion para el canal: las listas vacias salen como [] (no como {}).
function Ses.Public(s)
    if type(s) ~= "table" then return nil end
    local out = {}
    for k, v in pairs(s) do out[k] = v end
    for _, key in ipairs(LIST_KEYS) do
        if type(out[key]) ~= "table" then out[key] = {} end
        if next(out[key]) == nil then
            out[key] = (ns.Json and ns.Json.Array) and ns.Json.Array() or {}
        end
    end
    return out
end


-- Lineas para /compa sesion.
function Ses.SummaryLines()
    ensureNow()
    if not cur then return { "Sesión: sin datos todavía (espera a estar dentro del mundo)." } end
    local ts = cur.timeSplit
    local lines = {}
    local pct = (base.max and base.max > 0) and floor(100 * (base.xp or 0) / base.max + 0.5) or 0
    lines[#lines + 1] = format("Sesión: %s activos, nivel %d (%d%% del nivel).", fmtDur(cur.playedSec), cur.level.now, pct)
    local eta = ""
    local rate = rateOf(cur)
    local remaining = base.max and (base.max - (base.xp or 0)) or 0
    if rate > 0 and remaining > 0 then
        eta = format(" Siguiente nivel en ~%s.", fmtDur(remaining * 3600 / rate))
    end
    lines[#lines + 1] = format("XP: +%s (%s XP/h).%s", fmtInt(cur.xpGained), fmtInt(cur.xpPerHour), eta)
    lines[#lines + 1] = format("Oro: %s.", fmtMoney(cur.money.now - cur.money.start))
    lines[#lines + 1] = format("Misiones: %d completadas, %d aceptadas. Enemigos: %d. Muertes: %d. Zonas: %d.",
        #cur.questsCompleted, #cur.questsAccepted, cur.kills, cur.deaths, #cur.zones)
    lines[#lines + 1] = format("Tiempo: combate %s, montura %s, descanso %s, otro %s.",
        fmtDur(ts.combatSec), fmtDur(ts.mountedSec), fmtDur(ts.restingSec), fmtDur(ts.otherSec))
    lines[#lines + 1] = format("Sesiones anteriores guardadas: %d.", #cur.previous)
    return lines
end

-- Una linea para /compa estado.
function Ses.StatusLine()
    ensureNow()
    if not cur then return "Sesión: sin datos todavía" end
    return format("Sesión: %s activos, nivel %d, %s XP/h, %d misiones, %d enemigos, %d muertes",
        fmtDur(cur.playedSec), cur.level.now, fmtInt(cur.xpPerHour),
        #cur.questsCompleted, cur.kills, cur.deaths)
end

-- Para pruebas: olvida el estado en memoria (como tras un /reload; la sesion guardada queda en SV).
function Ses.ResetForTests()
    cur, base, lastClock = nil, nil, nil
    pending = false
    started = false
    recentKills, recentSet = {}, {}
end

Ses.fmtInt, Ses.fmtDur = fmtInt, fmtDur -- para pruebas

return Ses
