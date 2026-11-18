-- World.lua: aprende del mundo lo que el jugador observa y lo guarda en CompaneroWoWDB.world
-- (cuenta, compartido por personajes): vuelos (nodos y tiempos/costes medidos), vendedores,
-- instructores, cadenas de misiones y consumibles. Solo por eventos: sin OnUpdate y sin trabajo
-- por frame. Dentro de combate no se lee nada: el trabajo se aplaza hasta salir de combate.
-- Los cambios se envian en la ventana de compartir (msgType 16, Channel.lua), solo los bloques
-- cambiados desde el ultimo envio.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Get, Num, Str, Clean = ns.Call, ns.Get, ns.Num, ns.Str, ns.Clean
local type, pairs, ipairs, tostring, tonumber = type, pairs, ipairs, tostring, tonumber
local floor, min = math.floor, math.min
local sort, concat = table.sort, table.concat
local lower, find, sub, gsub = string.lower, string.find, string.sub, string.gsub

local W = {}
ns.World = W

-- Limites del contrato WORLD DATA.
W.MAX_VENDORS = 400
W.MAX_VENDOR_ITEMS = 80
W.MAX_TRAINERS = 300
W.MAX_TRAINER_SERVICES = 120
W.MAX_NODES = 600          -- puntos de vuelo por faccion
W.MAX_COSTS = 1500         -- trayectos medidos por faccion
W.MAX_CHAINS = 1500        -- misiones con cadena conocida
W.MAX_CONSUMABLES = 600
W.MAX_BYTES = 300 * 1024   -- tope total de CompaneroWoWDB.world (JSON de cada entrada mas su clave)
W.CHAIN_WINDOW = 120       -- s entre entregar una mision y aceptar otra del mismo PNJ para encadenarlas
W.BAG_THROTTLE = 5         -- s minimos entre barridos de bolsas
W.READS_PER_EVENT = 25     -- tooltips de objetos leidos por evento (evita picos)
W.RIDE_MAX_SEC = 3600      -- un trayecto mas largo se descarta
W.CHANNEL_BUDGET = 50000   -- bytes de JSON por mensaje msgType 16 (Json.MAX_BYTES del canal: 98304)
W.CHANNEL_FULL_EVERY = 600 -- s entre reenvios completos del mundo

local MAX_PENDING = 80     -- trabajo aplazado por combate (se descarta lo que sobre)

-- Secciones con tope de numero de entradas (sectionOf) y las que expulsan la entrada mas antigua.
local CAPPED = {
    vendors = W.MAX_VENDORS, trainers = W.MAX_TRAINERS,
    questChains = W.MAX_CHAINS, consumables = W.MAX_CONSUMABLES,
}
local EVICTABLE = { vendors = true, trainers = true }

-- Estado en memoria (no se guarda). Hashes y tamanos de cada entrada, para el canal y el tope.
local idx = {
    indexed = false, total = 0, refused = 0,
    sizes = {}, hashes = {}, paths = {}, json = {}, counts = {},
    dirty = {}, sent = {}, fullAt = nil,
}
local S = {
    taxiOpen = false, taxiSnap = nil, pendingRide = nil, ride = nil,
    merchantOpen = false, merchant = nil,
    trainerOpen = false, trainer = nil,
    lastTurnIn = nil, questieDone = {},
    bagAt = nil, bagTimer = false, bagPending = false,
    parsed = {},            -- [itemID] = false: mirado y no es consumible
}
local pendingWork = {}

-- ---------------------------------------------------------------------------
-- Utilidades
-- ---------------------------------------------------------------------------

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function inCombat()
    return Call("InCombatLockdown") == true
end

local function playerFaction()
    return Str(Call("UnitFactionGroup", "player")) or "Neutral"
end

local function playerPos()
    if ns.GetPlayerPosition then return ns.GetPlayerPosition() end
    return nil, nil, nil
end

-- Numero de una cadena del tooltip: «1.234» y «1,234» son miles; «2,5» y «2.5» son decimales.
local function toNumber(s)
    if type(s) ~= "string" then return nil end
    if s:match("^%d%d?%d?[%.,]%d%d%d$") or s:match("^%d%d?%d?[%.,]%d%d%d[%.,]%d%d%d$") then
        return tonumber((gsub(s, "[%.,]", "")))
    end
    return tonumber((gsub(s, ",", ".")))
end
W.ToNumber = toNumber

-- Id de objeto de un enlace |Hitem:ID:...|h.
local function itemIDFromLink(link)
    if type(link) ~= "string" then return nil end
    local id = string.match(link, "item:(%d+)")
    return id and tonumber(id) or nil
end

-- Cola de trabajo aplazado: se ejecuta al salir de combate, en orden.
local function later(fn)
    if #pendingWork < MAX_PENDING then pendingWork[#pendingWork + 1] = fn end
end

local function runPending()
    local list = pendingWork
    pendingWork = {}
    for _, fn in ipairs(list) do ns.SafeCall(fn) end
end

-- Ejecuta fn ahora, o lo aplaza si hay combate. Ninguna lectura de API ocurre en combate.
local function whenOutOfCombat(fn)
    if inCombat() then
        later(fn)
    else
        ns.SafeCall(fn)
    end
end

-- ---------------------------------------------------------------------------
-- Base de datos: CompaneroWoWDB.world y su indice (tamanos, hashes, caps)
-- ---------------------------------------------------------------------------

local function db()
    if type(CompaneroWoWDB) ~= "table" then return nil end
    local w = CompaneroWoWDB.world
    if type(w) ~= "table" then
        w = {}
        CompaneroWoWDB.world = w
    end
    if type(w.flight) ~= "table" then w.flight = {} end
    if type(w.flight.known) ~= "table" then w.flight.known = {} end
    for _, k in ipairs({ "vendors", "trainers", "questChains", "consumables" }) do
        if type(w[k]) ~= "table" then w[k] = {} end
    end
    return w
end

local function flatOf(path)
    local parts = {}
    for i = 1, #path do parts[i] = tostring(path[i]) end
    return concat(parts, "|")
end

local function copyPath(path)
    local out = {}
    for i = 1, #path do out[i] = path[i] end
    return out
end

local function getAt(path)
    local cur = db()
    for i = 1, #path do
        if type(cur) ~= "table" then return nil end
        cur = cur[path[i]]
    end
    return cur
end

local function setAt(path, value)
    local cur = db()
    if not cur then return false end
    for i = 1, #path - 1 do
        local k = path[i]
        if type(cur[k]) ~= "table" then cur[k] = {} end
        cur = cur[k]
    end
    cur[path[#path]] = value
    return true
end

-- Seccion de una ruta: "vendors", "trainers", "questChains", "consumables" o "flight|<faccion>|nodes|costs".
-- flight.known no tiene tope (nil).
local function sectionOf(path)
    if path[1] == "flight" then
        if path[2] == "known" then return nil end
        return "flight|" .. tostring(path[2]) .. "|" .. tostring(path[3])
    end
    return path[1]
end

local function capOf(sec)
    if sub(sec, -6) == "|nodes" then return W.MAX_NODES end
    if sub(sec, -6) == "|costs" then return W.MAX_COSTS end
    return CAPPED[sec]
end

-- Quita una entrada del mundo y de los indices.
local function forget(flat, path)
    local sec = sectionOf(path)
    idx.total = idx.total - (idx.sizes[flat] or 0)
    idx.sizes[flat], idx.hashes[flat], idx.paths[flat], idx.sent[flat] = nil, nil, nil, nil
    idx.json[flat] = nil
    idx.dirty[flat] = nil
    if sec and idx.counts[sec] then idx.counts[sec] = idx.counts[sec] - 1 end
    setAt(path, nil)
end

-- Expulsa la entrada de la seccion con menos lastSeen (distinta de exceptFlat). false si no hay.
local function evictOldest(sec, exceptFlat)
    local w = db()
    local t = w and w[sec]
    if type(t) ~= "table" then return false end
    local bestKey, bestAt = nil, nil
    for k, rec in pairs(t) do
        local flat = sec .. "|" .. tostring(k)
        if flat ~= exceptFlat and type(rec) == "table" then
            local at = Num(rec.lastSeen) or 0
            if bestAt == nil or at < bestAt or (at == bestAt and tostring(k) < tostring(bestKey)) then
                bestKey, bestAt = k, at
            end
        end
    end
    if bestKey == nil then return false end
    forget(sec .. "|" .. tostring(bestKey), { sec, bestKey })
    return true
end

-- Indexa lo que ya hay en SavedVariables (una vez por sesion): tamanos, hashes y contadores.
local function index()
    if idx.indexed then return true end
    local w = db()
    if not w then return false end
    idx.indexed = true
    local function add(path)
        local v = getAt(path)
        if v == nil then return end
        local json = ns.Json.Encode(v)
        if not json then return end
        local flat = flatOf(path)
        local size = #json + #flat
        idx.sizes[flat] = size
        idx.json[flat] = json
        idx.hashes[flat] = ns.Channel.HashText(json)
        idx.paths[flat] = copyPath(path)
        idx.total = idx.total + size
        local sec = sectionOf(path)
        if sec then idx.counts[sec] = (idx.counts[sec] or 0) + 1 end
    end
    for faction, f in pairs(w.flight) do
        if faction ~= "known" and type(f) == "table" then
            for id in pairs(f.nodes or {}) do add({ "flight", faction, "nodes", id }) end
            for key in pairs(f.costs or {}) do add({ "flight", faction, "costs", key }) end
        end
    end
    for ck in pairs(w.flight.known) do add({ "flight", "known", ck }) end
    for _, sec in ipairs({ "vendors", "trainers", "questChains", "consumables" }) do
        for k in pairs(w[sec]) do add({ sec, k }) end
    end
    return true
end

-- Guarda value en path si cambia. Respeta caps (expulsa vendedores/instructores antiguos) y el
-- tope total. Devuelve true si cambio la base de datos.
local function store(path, value)
    if not index() then return false end
    local json = ns.Json.Encode(value)
    if not json then return false end
    local flat = flatOf(path)
    local h = ns.Channel.HashText(json)
    if idx.hashes[flat] == h then return false end
    local sec = sectionOf(path)
    local isNew = idx.hashes[flat] == nil
    local size = #json + #flat
    local old = idx.sizes[flat] or 0
    if isNew and sec then
        local cap = capOf(sec)
        if cap and (idx.counts[sec] or 0) >= cap then
            if not (EVICTABLE[path[1]] and evictOldest(sec, nil)) then
                idx.refused = idx.refused + 1
                return false
            end
        end
    end
    while idx.total - old + size > W.MAX_BYTES do
        if not (EVICTABLE[path[1]] and evictOldest(path[1], flat)) then
            idx.refused = idx.refused + 1
            return false
        end
    end
    setAt(path, value)
    idx.total = idx.total - old + size
    idx.sizes[flat] = size
    idx.json[flat] = json
    idx.hashes[flat] = h
    idx.paths[flat] = copyPath(path)
    if isNew and sec then idx.counts[sec] = (idx.counts[sec] or 0) + 1 end
    idx.dirty[flat] = true
    return true
end

-- ---------------------------------------------------------------------------
-- Vuelos
-- ---------------------------------------------------------------------------

-- Estado de un nodo: "current", "reachable" (incluye DISTANT) o "unreachable".
local function flightStatus(state)
    local fp = _G.Enum and _G.Enum.FlightPathState
    local current = type(fp) == "table" and fp.Current or 2
    local unreachable = type(fp) == "table" and fp.Unreachable or 0
    if state == nil then return "unreachable" end
    if state == current then return "current" end
    if state == unreachable then return "unreachable" end
    return "reachable"
end

local function legacyStatus(kind)
    if kind == "CURRENT" then return "current" end
    if kind == nil or kind == "NONE" then return "unreachable" end
    return "reachable"
end

-- Lee los nodos del mapa de vuelo abierto (API nueva; si no, la clasica). Lista de nodos.
local function readTaxiNodes(mapID)
    local out = {}
    local list = Clean(Call("C_TaxiMap.GetAllTaxiNodes", mapID))
    if type(list) == "table" then
        for i, n in ipairs(list) do
            local nodeID = type(n) == "table" and Num(n.nodeID) or nil
            if nodeID then
                local pos = type(n.position) == "table" and n.position or {}
                out[#out + 1] = {
                    id = nodeID, name = Str(n.name), x = Num(pos.x), y = Num(pos.y),
                    status = flightStatus(Clean(n.state)), slot = Num(n.slotIndex) or i,
                }
            end
        end
        return out
    end
    local count = Num(Call("NumTaxiNodes")) or 0
    for i = 1, min(count, 300) do
        local x, y = Call("TaxiNodePosition", i)
        out[#out + 1] = {
            -- la API clasica no da nodeID: se usa mapa*1000 + indice (estable mientras no cambie el mapa)
            id = (mapID or 0) * 1000 + i, name = Str(Call("TaxiNodeName", i)),
            x = Num(x), y = Num(y), status = legacyStatus(Str(Call("TaxiNodeGetType", i))), slot = i,
        }
    end
    return out
end

-- Al abrir el mapa de vuelo: nodos, conocidos del personaje y coste de cada destino.
local function readTaxi()
    if not index() then return end
    local mapID = Num(Call("C_TaxiMap.GetTaxiMapID"))
    local nodes = readTaxiNodes(mapID)
    local fac = playerFaction()
    local now = ns.Now()
    local origin, bySlot = nil, {}
    local learned = {}
    for _, n in ipairs(nodes) do
        if n.status == "current" then origin = n.id end
        if n.status ~= "unreachable" then
            learned[#learned + 1] = n.id
            bySlot[n.slot] = { nodeID = n.id, cost = Num(Call("TaxiNodeCost", n.slot)) }
        end
        if n.name and n.x and n.y then
            local path = { "flight", fac, "nodes", n.id }
            local prev = getAt(path)
            local known = n.status ~= "unreachable" or (type(prev) == "table" and prev.known == true)
            if type(prev) ~= "table" or prev.name ~= n.name or prev.known ~= known
                or prev.mapID ~= mapID or prev.x ~= n.x or prev.y ~= n.y then
                store(path, { nodeID = n.id, name = n.name, mapID = mapID, x = n.x, y = n.y, known = known, lastSeen = now })
            end
        end
    end
    S.taxiSnap = { origin = origin, bySlot = bySlot }
    local ck = ns.CharKey and ns.CharKey()
    if ck and #learned > 0 then
        local set, list = {}, {}
        local prevList = getAt({ "flight", "known", ck })
        if type(prevList) == "table" then
            for _, id in ipairs(prevList) do
                if not set[id] then set[id] = true; list[#list + 1] = id end
            end
        end
        for _, id in ipairs(learned) do
            if not set[id] then set[id] = true; list[#list + 1] = id end
        end
        sort(list)
        store({ "flight", "known", ck }, list)
    end
end

-- Elegir destino en el mapa: guarda origen, destino y coste hasta que el vuelo empiece.
local function onTakeTaxi(slot)
    local snap = S.taxiSnap
    slot = Num(slot)
    if not snap or not slot or not snap.origin then return end
    local dest = snap.bySlot[slot]
    if not dest or dest.nodeID == snap.origin then return end
    S.pendingRide = { from = snap.origin, to = dest.nodeID, copper = dest.cost }
end

-- Inicio real del vuelo: PLAYER_CONTROL_LOST con UnitOnTaxi.
local function onControlLost()
    if not S.pendingRide then return end
    if Clean(Call("UnitOnTaxi", "player")) ~= true then return end
    S.ride = S.pendingRide
    S.ride.startedAt = nowSec()
    S.pendingRide = nil
end

-- Aterrizaje: PLAYER_CONTROL_GAINED fuera del taxi. Mide el tiempo y guarda el trayecto.
local function onControlGained()
    local ride = S.ride
    if not ride then return end
    if Clean(Call("UnitOnTaxi", "player")) == true then return end
    S.ride = nil
    local secs = nowSec() - ride.startedAt
    if secs <= 0 or secs > W.RIDE_MAX_SEC then return end
    local key = tostring(ride.from) .. ">" .. tostring(ride.to)
    store({ "flight", playerFaction(), "costs", key }, {
        copper = ride.copper, hops = 1, seconds = floor(secs + 0.5),
    })
end

-- ---------------------------------------------------------------------------
-- Vendedores
-- ---------------------------------------------------------------------------

-- Datos del PNJ de la unidad dada ("npc" = el que abre la ventana). nil si no es un PNJ.
local function npcInfo(unit)
    local kind, npcID = ns.ParseGUID(Call("UnitGUID", unit))
    if (kind ~= "Creature" and kind ~= "Vehicle") or not npcID then return nil end
    if Clean(Call("UnitIsPlayer", unit)) == true then return nil end
    local mapID, x, y = playerPos()
    return { npcID = npcID, name = Str(Call("UnitName", unit)), mapID = mapID, x = x, y = y }
end

local function npcIDOf(unit)
    local kind, npcID = ns.ParseGUID(Call("UnitGUID", unit))
    if kind == "Creature" or kind == "Vehicle" then return npcID end
    return nil
end

-- Articulos del vendedor abierto (hasta 80): precio en cobre, stack, stock limitado.
local function merchantItems()
    local out = {}
    local n = Num(Call("GetMerchantNumItems")) or 0
    for i = 1, n do
        if #out >= W.MAX_VENDOR_ITEMS then break end
        local info = Clean(Call("C_MerchantFrame.GetItemInfo", i))
        local name, price, stack, avail, itemID
        if type(info) == "table" then
            name, price = Str(info.name), Num(info.price)
            stack, avail = Num(info.stackCount), Num(info.numAvailable)
            itemID = Num(info.itemID)
        else
            local n1, _, p, q, numAvail = Call("GetMerchantItemInfo", i)
            name, price, stack, avail = Str(n1), Num(p), Num(q), Num(numAvail)
        end
        if not itemID then
            itemID = itemIDFromLink(Str(Call("C_MerchantFrame.GetItemLink", i)))
                or itemIDFromLink(Str(Call("GetMerchantItemLink", i)))
        end
        if itemID then
            out[#out + 1] = {
                itemID = itemID, name = name, price = price or 0, stack = stack or 1,
                limited = avail ~= nil and avail >= 0,
            }
        end
    end
    return out
end

local function storeVendor(npc, items)
    store({ "vendors", npc.npcID }, {
        npcID = npc.npcID, name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y,
        faction = playerFaction(), lastSeen = ns.Now(), items = items,
    })
    -- Consumibles entre los articulos del vendedor (tooltip; lo hace considerItem).
    for i = 1, min(#items, W.READS_PER_EVENT) do
        W.ConsiderItem(items[i].itemID)
    end
end

local function onMerchantShow()
    if not S.merchantOpen then return end -- se cerro antes de salir de combate
    local npc = npcInfo("npc")
    if not npc then return end
    S.merchant = npc
    storeVendor(npc, merchantItems())
end

local function onMerchantUpdate()
    if S.merchantOpen and S.merchant then storeVendor(S.merchant, merchantItems()) end
end

-- ---------------------------------------------------------------------------
-- Instructores
-- ---------------------------------------------------------------------------

-- Palabras de subtitulo (en minusculas, sin tildes donde se puede) -> tipo de instructor.
local RIDING_WORDS = { "montar", "equita", "riding", "cabalgadura", "mount" }
local WEAPON_WORDS = { "armas", "weapon" }
local PROFESSION_WORDS = {
    "herborist", "herbalism", "miner", "desoll", "skinning", "alquim", "alchem", "herrer", "blacksmith",
    "peleter", "leatherwork", "ingenier", "engineer", "cocin", "cooking", "sastr", "tailor",
    "encantam", "enchant", "primeros auxilios", "first aid", "pesca", "fishing", "inscrip",
    "joyer", "jewel", "arqueolog", "archaeolog",
}
local CLASS_WORDS = {
    "guerrero", "warrior", "paladin", "cazador", "hunter", "picaro", "rogue", "sacerdote", "priest",
    "chaman", "shaman", "mago", "mage", "brujo", "warlock", "druida", "druid", "monje", "monk",
    "caballero de la muerte", "death knight", "evocador", "evoker",
}

local function anyWord(low, words)
    for _, w in ipairs(words) do
        if find(low, w, 1, true) then return true end
    end
    return false
end

-- Tipo de instructor a partir del subtitulo del PNJ y de sus servicios.
-- Devuelve kind ("riding" | "weapon" | "profession" | "class" | "other") y profesion (si aplica).
function W.ClassifyTrainer(subtitle, services)
    local low = lower(subtitle or "")
    local skillCount = {}
    local allSkill = #(services or {}) > 0
    for _, svc in ipairs(services or {}) do
        local sk = type(svc) == "table" and svc.skill or nil
        if type(sk) == "table" and sk.name then
            skillCount[sk.name] = (skillCount[sk.name] or 0) + 1
        else
            allSkill = false
        end
    end
    local topSkill, topN = nil, 0
    for name, n in pairs(skillCount) do
        if n > topN or (n == topN and name < topSkill) then topSkill, topN = name, n end
    end
    if anyWord(low, RIDING_WORDS) then return "riding", nil end
    if anyWord(low, WEAPON_WORDS) then return "weapon", nil end
    if anyWord(low, PROFESSION_WORDS) or (allSkill and topSkill) then
        return "profession", topSkill
    end
    if anyWord(low, CLASS_WORDS) then return "class", nil end
    return "other", nil
end

-- Subtitulo del PNJ (segunda linea del tooltip de la unidad). nil si no hay.
local function unitSubtitle(unit)
    local data = Clean(Call("C_TooltipInfo.GetUnit", unit))
    if type(data) ~= "table" or type(data.lines) ~= "table" then return nil end
    local line = data.lines[2]
    return type(line) == "table" and Str(line.leftText) or nil
end

-- Servicios del instructor abierto (hasta 120).
local function trainerServices()
    local out = {}
    local n = min(Num(Call("GetNumTrainerServices")) or 0, W.MAX_TRAINER_SERVICES)
    for i = 1, n do
        local name, rank, category = Call("GetTrainerServiceInfo", i)
        name = Str(name)
        if name then
            local avail = Clean(category)
            if avail ~= "available" and avail ~= "used" then avail = "unavailable" end
            local svc = {
                name = name, rank = Str(rank),
                cost = Num(Call("GetTrainerServiceCost", i)),
                level = Num(Call("GetTrainerServiceLevelReq", i)),
                available = avail,
            }
            local skillName, skillRank = Call("GetTrainerServiceSkillReq", i)
            skillName = Str(skillName)
            if skillName then svc.skill = { name = skillName, rank = Num(skillRank) } end
            out[#out + 1] = svc
        end
    end
    return out
end

local function storeTrainer(npc)
    local services = trainerServices()
    local kind, profession = W.ClassifyTrainer(unitSubtitle("npc"), services)
    store({ "trainers", npc.npcID }, {
        npcID = npc.npcID, name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y,
        kind = kind, profession = profession, services = services, lastSeen = ns.Now(),
    })
end

local function onTrainerShow()
    if not S.trainerOpen then return end -- se cerro antes de salir de combate
    local npc = npcInfo("npc")
    if not npc then return end
    S.trainer = npc
    storeTrainer(npc)
end

local function onTrainerUpdate()
    if S.trainerOpen and S.trainer then storeTrainer(S.trainer) end
end

-- ---------------------------------------------------------------------------
-- Cadenas de misiones
-- ---------------------------------------------------------------------------

local function chainCopy(qid)
    local rec = getAt({ "questChains", qid })
    local out = { pre = {} }
    if type(rec) == "table" then
        if type(rec.pre) == "table" then
            for _, v in ipairs(rec.pre) do out.pre[#out.pre + 1] = v end
        end
        out.next = rec.next
    end
    return out
end

local function saveChain(qid, rec)
    if #rec.pre == 0 then rec.pre = nil end
    if rec.next == nil and rec.pre == nil and rec.chainName == nil then return end
    store({ "questChains", qid }, rec)
end

-- Enlace observado: prev se entrego y qid se acepto despues, del mismo PNJ.
local function linkChain(prevQid, qid)
    local prev = chainCopy(prevQid)
    prev.next = qid
    saveChain(prevQid, prev)
    local cur = chainCopy(qid)
    local found = false
    for _, v in ipairs(cur.pre) do
        if v == prevQid then found = true end
    end
    if not found then cur.pre[#cur.pre + 1] = prevQid end
    saveChain(qid, cur)
end

-- Questie (si esta instalado): mision previa y siguiente, una sola lectura por mision.
local function fillFromQuestie(qid)
    if S.questieDone[qid] then return end
    S.questieDone[qid] = true
    local loader = _G.QuestieLoader
    if type(loader) ~= "table" or type(loader.ImportModule) ~= "function" then return end
    local ok, qdb = ns.Pcall(loader.ImportModule, loader, "QuestieDB")
    if not ok or type(qdb) ~= "table" or type(qdb.QueryQuestSingle) ~= "function" then return end
    local pre = ns.SafeCall(qdb.QueryQuestSingle, qid, "preQuestSingle")
    local nextQ = Num(ns.SafeCall(qdb.QueryQuestSingle, qid, "nextQuestInChain"))
    local cur = chainCopy(qid)
    local changed = false
    if type(pre) == "table" and #cur.pre == 0 then
        for _, v in ipairs(pre) do
            local n = Num(v)
            if n then cur.pre[#cur.pre + 1] = n; changed = true end
        end
    end
    if nextQ and nextQ > 0 and cur.next == nil then cur.next = nextQ; changed = true end
    if changed then saveChain(qid, cur) end
end

local function onQuestTurnedIn(qid)
    qid = Num(qid)
    if not qid then return end
    S.lastTurnIn = { qid = qid, npc = npcIDOf("npc"), at = ns.Now() }
    fillFromQuestie(qid)
end

local function onQuestAccepted(a, b)
    local qid = Num(b) or Num(a)
    if not qid then return end
    local last = S.lastTurnIn
    S.lastTurnIn = nil
    local npc = npcIDOf("npc")
    if last and last.qid ~= qid and last.npc ~= nil and npc == last.npc
        and ns.Now() - last.at <= W.CHAIN_WINDOW then
        linkChain(last.qid, qid)
    end
    fillFromQuestie(qid)
end

-- ---------------------------------------------------------------------------
-- Consumibles
-- ---------------------------------------------------------------------------

-- Patrones (minusculas) del tooltip. Los numeros se capturan como texto y se convierten con toNumber.
local HEALTH_PATS = {
    "restaura%s+([%d%.,]+)%s+de%s+salud", "restaura%s+([%d%.,]+)%s+puntos%s+de%s+salud",
    "restaura%s+([%d%.,]+)%s+de%s+vida", "restores%s+([%d%.,]+)%s+health",
}
local MANA_PATS = {
    "restaura%s+([%d%.,]+)%s+de%s+man", "restaura%s+([%d%.,]+)%s+puntos%s+de%s+man",
    "salud%s+y%s+([%d%.,]+)%s+de%s+man", "restores%s+([%d%.,]+)%s+mana",
}
local SECONDS_PATS = { "durante%s+(%d+)%s+seg", "en%s+(%d+)%s+seg", "over%s+(%d+)%s+sec", "for%s+(%d+)%s+sec" }
local LEVEL_PATS = { "requiere%s+nivel%s+(%d+)", "requires%s+level%s+(%d+)", "nivel%s+requerido:?%s*(%d+)" }
local FOOD_HINT = { "comida", "alimento", "comer", "food" }
local DRINK_HINT = { "bebida", "beber", "agua", "drink" }

local function firstCapture(low, pats)
    for _, p in ipairs(pats) do
        local v = string.match(low, p)
        if v then return v end
    end
    return nil
end

-- Habilidad requerida: «Requiere Primeros auxilios (80)» / «Requires First Aid (80)».
local function parseSkill(line)
    for _, p in ipairs({ "[Rr]equiere%s+(.-)%s*%((%d+)%)", "[Rr]equires%s+(.-)%s*%((%d+)%)" }) do
        local name, rank = string.match(line, p)
        if name and name ~= "" and not find(lower(name), "nivel", 1, true) and not find(lower(name), "level", 1, true) then
            return { name = name, rank = tonumber(rank) }
        end
    end
    return nil
end

-- Clase por subclase de consumible (Enum.ItemConsumableSubclass): 1 pocion, 2 elixir, 4 pergamino,
-- 5 comida y bebida, 7 vendaje. Otras subclases: nil (se decide por el texto).
local SUBCLASS = { potion = 1, elixir = 2, scroll = 4, food = 5, bandage = 7 }

-- Consumible desde las lineas de su tooltip (cifras independientes del idioma). Puro: no toca la API.
-- lines: textos del tooltip (linea 1 = nombre). subclassID: opcional. nil si no aporta nada util.
function W.ParseConsumable(itemID, lines, subclassID)
    if type(lines) ~= "table" or #lines == 0 then return nil end
    local name = lines[1]
    local health, mana, seconds, reqLevel, skill
    local lowAll = {}
    for _, line in ipairs(lines) do
        if type(line) == "string" then
            local low = lower(line)
            lowAll[#lowAll + 1] = low
            if not health then
                local v = firstCapture(low, HEALTH_PATS)
                health = v and toNumber(v) or nil
            end
            if not mana then
                local v = firstCapture(low, MANA_PATS)
                mana = v and toNumber(v) or nil
            end
            if not seconds and (health or mana) then
                local v = firstCapture(low, SECONDS_PATS)
                seconds = v and tonumber(v) or nil
            end
            if not reqLevel then
                local v = firstCapture(low, LEVEL_PATS)
                reqLevel = v and tonumber(v) or nil
            end
            if not skill then skill = parseSkill(line) end
        end
    end
    local text = concat(lowAll, "\n")
    local foodHint, drinkHint = false, false
    for _, w in ipairs(FOOD_HINT) do if find(text, w, 1, true) then foodHint = true end end
    for _, w in ipairs(DRINK_HINT) do if find(text, w, 1, true) then drinkHint = true end end

    local kind
    if subclassID == SUBCLASS.bandage then kind = "bandage"
    elseif subclassID == SUBCLASS.scroll then kind = "scroll"
    elseif subclassID == SUBCLASS.elixir then kind = "elixir"
    elseif subclassID == SUBCLASS.food then
        kind = (mana and not health) and "drink" or "food"
    elseif subclassID == SUBCLASS.potion then
        if health and not mana then kind = "potion_health"
        elseif mana and not health then kind = "potion_mana"
        else kind = "other" end
    elseif find(text, "vendaje", 1, true) or find(text, "bandage", 1, true) then kind = "bandage"
    elseif find(text, "pergamino", 1, true) or find(text, "scroll", 1, true) then kind = "scroll"
    elseif health and mana then kind = foodHint and "food" or (drinkHint and "drink" or "other")
    elseif health then kind = foodHint and "food" or "potion_health"
    elseif mana then kind = drinkHint and "drink" or "potion_mana"
    else kind = "other" end

    local rec = { itemID = itemID, name = name, kind = kind }
    if health or mana then
        rec.restores = { health = health or 0, mana = mana or 0, seconds = seconds }
    end
    if reqLevel then rec.requiredLevel = reqLevel end
    if skill then rec.skill = skill end
    if not rec.restores and not rec.requiredLevel and not rec.skill and kind == "other" then
        return nil
    end
    return rec
end

-- Clase de consumible (ItemClass) de un objeto, o nil si la API no lo dice.
local function itemClassInfo(itemID)
    local _, _, _, _, _, classID, subclassID = Call("C_Item.GetItemInfoInstant", itemID)
    if Num(classID) == nil then
        _, _, _, _, _, classID, subclassID = Call("GetItemInfoInstant", itemID)
    end
    return Num(classID), Num(subclassID)
end

-- Lineas del tooltip de un objeto (nil si aun no hay datos).
local function itemTooltipLines(itemID)
    local data = Clean(Call("C_TooltipInfo.GetItemByID", itemID))
    if type(data) ~= "table" or type(data.lines) ~= "table" then return nil end
    local out = {}
    for _, line in ipairs(data.lines) do
        local t = Str(type(line) == "table" and line.leftText or nil)
        if t then out[#out + 1] = t end
    end
    return out
end

-- Mira un objeto (una vez): si es consumible, guarda su ficha. true si leyo el tooltip.
function W.ConsiderItem(itemID)
    itemID = Num(itemID)
    if not itemID or itemID <= 0 then return false end
    if S.parsed[itemID] == false then return false end
    if getAt({ "consumables", itemID }) ~= nil then return false end
    local classID, subclassID = itemClassInfo(itemID)
    if classID and classID ~= 0 then
        S.parsed[itemID] = false
        return false
    end
    local lines = itemTooltipLines(itemID)
    if not lines then return false end
    local rec = W.ParseConsumable(itemID, lines, subclassID)
    if rec then
        store({ "consumables", itemID }, rec)
    else
        S.parsed[itemID] = false
    end
    return true
end

local function bagIndexes()
    local list = {}
    local maxBag = Num(_G.NUM_BAG_SLOTS) or 4
    for b = 0, maxBag do list[#list + 1] = b end
    local bagEnum = _G.Enum and _G.Enum.BagIndex
    local reagent = type(bagEnum) == "table" and Num(bagEnum.ReagentBag)
    if reagent then list[#list + 1] = reagent end
    return list
end

-- Barrido de bolsas: mira cada objeto distinto (como mucho READS_PER_EVENT lecturas de tooltip).
local function scanBags()
    S.bagAt = nowSec()
    local reads = 0
    for _, bag in ipairs(bagIndexes()) do
        local n = Num(Call("C_Container.GetContainerNumSlots", bag)) or 0
        for slot = 1, n do
            local info = Clean(Call("C_Container.GetContainerItemInfo", bag, slot))
            local itemID = type(info) == "table" and Num(info.itemID) or nil
            if itemID and reads < W.READS_PER_EVENT and W.ConsiderItem(itemID) then
                reads = reads + 1
            end
        end
    end
end

-- BAG_UPDATE_DELAYED llega en rafagas: como mucho un barrido cada BAG_THROTTLE s.
local function scheduleBags()
    if inCombat() then
        S.bagPending = true
        return
    end
    local t = nowSec()
    if S.bagAt == nil or t - S.bagAt >= W.BAG_THROTTLE then
        scanBags()
        return
    end
    if not S.bagTimer and ns.Get("C_Timer.After") then
        S.bagTimer = true
        ns.Call("C_Timer.After", W.BAG_THROTTLE - (t - S.bagAt), function()
            S.bagTimer = false
            ns.SafeCall(scheduleBags)
        end)
    end
end

local function onLoot()
    local n = Num(Call("GetNumLootItems")) or 0
    local reads = 0
    for i = 1, min(n, W.READS_PER_EVENT) do
        local itemID = itemIDFromLink(Str(Call("GetLootSlotLink", i)))
        if itemID and W.ConsiderItem(itemID) then reads = reads + 1 end
    end
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

local handlers = {
    TAXIMAP_OPENED = function()
        S.taxiOpen = true
        whenOutOfCombat(function()
            if S.taxiOpen then readTaxi() end
        end)
    end,
    TAXIMAP_CLOSED = function()
        S.taxiOpen = false
        S.taxiSnap = nil
    end,
    PLAYER_CONTROL_LOST = onControlLost,
    PLAYER_CONTROL_GAINED = onControlGained,
    MERCHANT_SHOW = function()
        S.merchantOpen = true -- el estado se marca ya; la lectura espera a salir de combate
        whenOutOfCombat(onMerchantShow)
    end,
    MERCHANT_UPDATE = function()
        whenOutOfCombat(onMerchantUpdate)
    end,
    MERCHANT_CLOSED = function()
        S.merchantOpen, S.merchant = false, nil
    end,
    TRAINER_SHOW = function()
        S.trainerOpen = true
        whenOutOfCombat(onTrainerShow)
    end,
    TRAINER_UPDATE = function()
        whenOutOfCombat(onTrainerUpdate)
    end,
    TRAINER_CLOSED = function()
        S.trainerOpen, S.trainer = false, nil
    end,
    QUEST_TURNED_IN = function(qid)
        whenOutOfCombat(function() onQuestTurnedIn(qid) end)
    end,
    QUEST_ACCEPTED = function(a, b)
        whenOutOfCombat(function() onQuestAccepted(a, b) end)
    end,
    BAG_UPDATE_DELAYED = scheduleBags,
    LOOT_OPENED = function()
        whenOutOfCombat(onLoot)
    end,
    PLAYER_REGEN_ENABLED = function()
        runPending()
        if S.bagPending then
            S.bagPending = false
            scheduleBags()
        end
    end,
}

-- Despacho (tambien usado por tools/lua/test_world.lua).
function W.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

-- Gancho de la seleccion de destino (hooksecurefunc: no cambia la llamada original).
local function installTakeHook()
    if type(_G.hooksecurefunc) ~= "function" or not Get("TakeTaxiNode") then return end
    ns.Pcall(_G.hooksecurefunc, "TakeTaxiNode", function(slot)
        ns.SafeCall(onTakeTaxi, slot)
    end)
end

-- Linea para /compa estado: nodos de vuelo, vendedores, instructores y consumibles.
function W.Counts()
    local w = db()
    if not w then return 0, 0, 0, 0 end
    local nodes = 0
    for faction, f in pairs(w.flight) do
        if faction ~= "known" and type(f) == "table" and type(f.nodes) == "table" then
            for _ in pairs(f.nodes) do nodes = nodes + 1 end
        end
    end
    local function count(t)
        local n = 0
        for _ in pairs(t) do n = n + 1 end
        return n
    end
    return nodes, count(w.vendors), count(w.trainers), count(w.consumables)
end

function W.StatusLine()
    local flights, vendors, trainers, consumables = W.Counts()
    return string.format("Mundo: %d vuelos, %d vendedores, %d instructores, %d consumibles",
        flights, vendors, trainers, consumables)
end
ns.WorldStatusLine = W.StatusLine

-- ---------------------------------------------------------------------------
-- Ventana de compartir (msgType 16): solo los bloques cambiados
-- ---------------------------------------------------------------------------

-- Arbol de claves -> JSON. Las hojas llevan el JSON ya codificado de su entrada (store), asi que
-- componer el mensaje no recorre tablas vivas (la serializacion puede ceder entre tramas).
local function treeInsert(tree, path, json)
    local cur = tree
    for i = 1, #path - 1 do
        local k = tostring(path[i])
        local nxt = cur.kids[k]
        if not nxt then
            nxt = { kids = {} }
            cur.kids[k] = nxt
        end
        cur = nxt
    end
    cur.kids[tostring(path[#path])] = { json = json }
end

local function treeJSON(node)
    if node.json then return node.json end
    local keys = {}
    for k in pairs(node.kids) do keys[#keys + 1] = k end
    sort(keys)
    local parts = {}
    for i, k in ipairs(keys) do
        parts[i] = ns.Json.Quote(k) .. ":" .. treeJSON(node.kids[k])
    end
    return "{" .. concat(parts, ",") .. "}"
end

-- Marca todo el mundo para reenviarlo (/compa sync): la siguiente ventana lo manda entero.
function W.ForceFull()
    idx.fullAt = nil
    idx.sent = {}
end

-- true si quedan bloques por enviar (la ventana lo vuelve a pedir si tiene tiempo).
function W.ChannelPending()
    return next(idx.dirty) ~= nil
end

-- JSON con los bloques cambiados desde el ultimo envio, con el mismo esquema que
-- CompaneroWoWDB.world (solo las entradas cambiadas), o nil si no hay nada. Presupuesto
-- CHANNEL_BUDGET por mensaje; lo que no cabe queda pendiente. Cada CHANNEL_FULL_EVERY s reenvia todo.
-- Las entradas que salen se dan por enviadas aqui: Channel.lua no construye otro mensaje 16 hasta
-- que el anterior salga, asi que no se pierden.
function W.ShareMessage()
    if not index() then return nil end
    local now = nowSec()
    if idx.fullAt == nil or now - idx.fullAt >= W.CHANNEL_FULL_EVERY then
        idx.fullAt = now
        idx.sent = {}
        for flat in pairs(idx.hashes) do idx.dirty[flat] = true end
    end
    if next(idx.dirty) == nil then return nil end
    local list = {}
    for flat in pairs(idx.dirty) do list[#list + 1] = flat end
    sort(list)
    local tree, used = { kids = {} }, 0
    for _, flat in ipairs(list) do
        local path, h, json = idx.paths[flat], idx.hashes[flat], idx.json[flat]
        if path == nil or h == nil or json == nil or idx.sent[flat] == h then
            idx.dirty[flat] = nil
        else
            local cost = #json + #flat + 8
            if used > 0 and used + cost > W.CHANNEL_BUDGET then break end
            treeInsert(tree, path, json)
            used = used + cost
            idx.sent[flat] = h
            idx.dirty[flat] = nil
        end
    end
    if used == 0 then return nil end
    return treeJSON(tree)
end

-- ---------------------------------------------------------------------------
-- Registro de eventos
-- ---------------------------------------------------------------------------

local EVENTS = {
    "TAXIMAP_OPENED", "TAXIMAP_CLOSED", "PLAYER_CONTROL_LOST", "PLAYER_CONTROL_GAINED",
    "MERCHANT_SHOW", "MERCHANT_UPDATE", "MERCHANT_CLOSED",
    "TRAINER_SHOW", "TRAINER_UPDATE", "TRAINER_CLOSED",
    "QUEST_TURNED_IN", "QUEST_ACCEPTED", "BAG_UPDATE_DELAYED", "LOOT_OPENED",
    "PLAYER_REGEN_ENABLED",
}

installTakeHook()

local frame = ns.NewFrame("Frame")
if frame then
    for _, ev in ipairs(EVENTS) do
        ns.Method(frame, "RegisterEvent", ev)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(W.OnEvent, event, ...)
    end)
end

-- Internos expuestos solo para tools/lua/test_world.lua.
W.testing = {
    store = store, getAt = getAt, index = index, idx = idx, S = S,
    readTaxi = readTaxi, onTakeTaxi = onTakeTaxi, scanBags = scanBags,
    pendingCount = function() return #pendingWork end,
}
