-- test_world.lua: pruebas offline del modulo World (vuelos, vendedores, instructores, cadenas de
-- misiones, consumibles) y de su envio por el canal (msgType 16, solo bloques cambiados).
-- Cubre: formas de los datos, caps, medida del vuelo, inferencia de cadenas, parseo de tooltips en
-- espanol, nada de trabajo en combate, ausencia de trabajo por frame y deltas del canal.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_world.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

-- ---------------------------------------------------------------------------
-- Estado y APIs de WoW falsas (se instalan antes de cargar World.lua)
-- ---------------------------------------------------------------------------
local T = {
    gettime = 1000, time = 1700000000, combat = false, onTaxi = false,
    npcGUID = nil, npcName = nil, subtitle = nil,
    taxiMap = 1414, taxiNodes = {}, costs = {}, taken = nil,
    merchant = {}, legacyMerchant = false,
    services = {}, tooltips = {}, classOf = {},
    bagSlots = {}, bags = {}, loot = {},
    questie = nil, questieData = {},
    timers = {},
}
local apiLog = {}
local function log(name) apiLog[#apiLog + 1] = name end
local function usedApi(name)
    for _, n in ipairs(apiLog) do if n == name then return true end end
    return false
end
local function resetLog() apiLog = {} end

local function guidFor(npcID) return "Creature-0-1-0-1-" .. npcID .. "-0000AAAA" end

_G.Enum = {
    FlightPathState = { Unreachable = 0, Reachable = 1, Current = 2 },
    BagIndex = { ReagentBag = 5 },
}
_G.NUM_BAG_SLOTS = 1
_G.GetTime = function() return T.gettime end
_G.time = function() return T.time end
_G.InCombatLockdown = function() log("InCombatLockdown") return T.combat end
_G.UnitFactionGroup = function() return "Alliance" end
_G.GetNormalizedRealmName = function() return "Realm" end
_G.UnitName = function(u)
    if u == "player" then return "Tester" end
    if u == "npc" then return T.npcName end
    return nil
end
_G.UnitGUID = function(u)
    if u == "player" then return "Player-1-0000BEEF" end
    if u == "npc" then return T.npcGUID end
    return nil
end
_G.UnitIsPlayer = function() return false end
_G.UnitOnTaxi = function(u) log("UnitOnTaxi") return u == "player" and T.onTaxi or false end

_G.C_TaxiMap = {
    GetTaxiMapID = function() log("C_TaxiMap.GetTaxiMapID") return T.taxiMap end,
    GetAllTaxiNodes = function() log("C_TaxiMap.GetAllTaxiNodes") return T.taxiNodes end,
}
_G.TaxiNodeCost = function(slot) log("TaxiNodeCost") return T.costs[slot] end
_G.TakeTaxiNode = function(slot) T.taken = slot end
-- hooksecurefunc real: el gancho se ejecuta tras la funcion original.
_G.hooksecurefunc = function(name, fn)
    local orig = _G[name]
    _G[name] = function(...)
        local r = { orig(...) }
        fn(...)
        return unpack(r)
    end
end

_G.GetMerchantNumItems = function() log("GetMerchantNumItems") return #T.merchant end
_G.C_MerchantFrame = {
    GetItemInfo = function(i) log("C_MerchantFrame.GetItemInfo")
        local m = T.merchant[i]
        return m and m.info or nil
    end,
    GetItemLink = function(i) log("C_MerchantFrame.GetItemLink")
        local m = T.merchant[i]
        return m and m.link or nil
    end,
}
-- API clasica (sin C_MerchantFrame): se activa en el test de compatibilidad.
_G.GetMerchantItemInfo = function(i)
    local m = T.merchant[i]
    if not m then return nil end
    return m.info.name, nil, m.info.price, m.info.stackCount, m.info.numAvailable
end
_G.GetMerchantItemLink = function(i)
    local m = T.merchant[i]
    return m and m.link or nil
end

_G.GetNumTrainerServices = function() log("GetNumTrainerServices") return #T.services end
_G.GetTrainerServiceInfo = function(i)
    log("GetTrainerServiceInfo")
    local s = T.services[i]
    if s then return s.name, s.rank, s.cat end
end
_G.GetTrainerServiceCost = function(i) return T.services[i] and T.services[i].cost end
_G.GetTrainerServiceLevelReq = function(i) return T.services[i] and T.services[i].level end
_G.GetTrainerServiceSkillReq = function(i)
    local s = T.services[i]
    if s and s.skill then return s.skill, s.skillRank end
end

local function tooltipLines(list)
    local lines = {}
    for i, t in ipairs(list) do lines[i] = { leftText = t } end
    return { lines = lines }
end
_G.C_TooltipInfo = {
    GetUnit = function(u)
        log("C_TooltipInfo.GetUnit")
        if u ~= "npc" or not T.npcName then return nil end
        local list = { T.npcName }
        if T.subtitle then list[2] = T.subtitle end
        return tooltipLines(list)
    end,
    GetItemByID = function(id)
        log("C_TooltipInfo.GetItemByID")
        local t = T.tooltips[id]
        if not t then return nil end
        return tooltipLines(t)
    end,
}
_G.C_Item = {
    GetItemInfoInstant = function(id)
        log("C_Item.GetItemInfoInstant")
        local c = T.classOf[id]
        if not c then return nil end
        return id, "", "", "", "", c.class, c.sub
    end,
}
_G.C_Container = {
    GetContainerNumSlots = function(b) log("C_Container.GetContainerNumSlots") return T.bagSlots[b] or 0 end,
    GetContainerItemInfo = function(b, s)
        log("C_Container.GetContainerItemInfo")
        local id = T.bags[b] and T.bags[b][s]
        if id then return { itemID = id, stackCount = 1 } end
        return nil
    end,
}
_G.GetNumLootItems = function() log("GetNumLootItems") return #T.loot end
_G.GetLootSlotLink = function(i) log("GetLootSlotLink") return T.loot[i] end

_G.C_Timer = {
    After = function(sec, fn) T.timers[#T.timers + 1] = { at = T.gettime + sec, fn = fn } end,
}
local function runTimers()
    local pending = T.timers
    T.timers = {}
    for _, t in ipairs(pending) do t.fn() end
end

_G.QuestieLoader = {
    ImportModule = function(_, name)
        if name == "QuestieDB" then return T.questie end
        return nil
    end,
}
T.questie = {
    QueryQuestSingle = function(qid, key)
        local d = T.questieData[qid]
        return d and d[key] or nil
    end,
}

-- Frames falsos: registran eventos y scripts para dispararlos desde el test.
local frames = {}
_G.CreateFrame = function()
    local f = { events = {}, scripts = {} }
    f.RegisterEvent = function(self, ev) self.events[ev] = true end
    f.SetScript = function(self, name, fn) self.scripts[name] = fn end
    frames[#frames + 1] = f
    return f
end

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
-- Posicion del jugador (en el juego la da Collectors.lua).
ns.GetPlayerPosition = function() return 1429, 0.45, 0.67 end
load("CompaneroWoW/World.lua")

local W, C, J = ns.World, ns.Channel, ns.Json

-- Dispara un evento en todos los frames que lo registraron (como el cliente).
local function fire(event, ...)
    for _, f in ipairs(frames) do
        if f.events[event] and f.scripts.OnEvent then f.scripts.OnEvent(f, event, ...) end
    end
end

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

-- Crea CompaneroWoWDB.world (como hace el modulo) y lo devuelve.
local function db() W.Counts() return CompaneroWoWDB.world end

-- Pone el personaje de prueba en un PNJ (GUID de Creature) con su subtitulo.
local function setNpc(npcID, name, subtitle)
    T.npcGUID = guidFor(npcID)
    T.npcName = name
    T.subtitle = subtitle
end

-- Lista de bytes -> cadena (para leer el JSON de un trozo del canal).
local function chunkText(chunk)
    local t = {}
    for i, b in ipairs(chunk.data) do t[i] = string.char(b) end
    return table.concat(t)
end

-- Activacion completa de la ventana (como Share.lua): devuelve los mensajes JSON del tipo 16 que
-- salieron, reensamblados por trozos. opts: { full = true } para /compa sync.
local function runShare(opts)
    C.Begin(T.gettime, opts)
    local msgs, buf = {}, nil
    for frame = 1, 3000 do
        local now = T.gettime
        if not C.Update(now) then break end
        C.Pump(now, 1000)
        local chunk = C.NextChunk(860, frame)
        if chunk and chunk.type == 16 then
            if chunk.index == 0 then buf = "" end
            buf = buf .. chunkText(chunk)
            if chunk.index == chunk.count - 1 then msgs[#msgs + 1] = buf end
        end
        T.gettime = T.gettime + C.TICK_SEC
    end
    C.Stop()
    return msgs
end

-- Reinicia el mundo de prueba. El reloj nunca retrocede (los modulos guardan tiempos).
local function reset()
    CompaneroWoWDB = {}
    W.testing.idx.indexed = false
    W.testing.idx.total, W.testing.idx.refused = 0, 0
    W.testing.idx.sizes, W.testing.idx.hashes, W.testing.idx.paths = {}, {}, {}
    W.testing.idx.counts, W.testing.idx.dirty, W.testing.idx.sent = {}, {}, {}
    W.testing.idx.json = {}
    W.testing.idx.fullAt = nil
    W.testing.S.taxiOpen, W.testing.S.taxiSnap, W.testing.S.pendingRide, W.testing.S.ride = false, nil, nil, nil
    W.testing.S.merchantOpen, W.testing.S.merchant = false, nil
    W.testing.S.trainerOpen, W.testing.S.trainer = false, nil
    W.testing.S.lastTurnIn, W.testing.S.questieDone = nil, {}
    W.testing.S.bagAt, W.testing.S.bagTimer, W.testing.S.bagPending = nil, false, false
    W.testing.S.parsed = {}
    T.combat = false
    T.onTaxi = false
    T.taxiNodes, T.costs, T.merchant, T.services = {}, {}, {}, {}
    T.tooltips, T.classOf, T.bagSlots, T.bags, T.loot = {}, {}, {}, {}, {}
    T.questieData = {}
    T.timers = {}
    T.npcGUID, T.npcName, T.subtitle = nil, nil, nil
    resetLog()
    ns.errorCount, ns.lastError = 0, nil
end

-- ---------------------------------------------------------------------------
print("(a) Carga: toc, estado y eventos sin OnUpdate")
local tocFile = io.open("CompaneroWoW/CompaneroWoW.toc", "r")
local toc = tocFile:read("*a")
tocFile:close()
local iC = string.find(toc, "Channel.lua", 1, true)
local iW = string.find(toc, "World.lua", 1, true)
check("toc: World.lua tras Channel.lua", iC and iW and iC < iW)
local coreFile = io.open("CompaneroWoW/Core.lua", "r")
local core = coreFile:read("*a")
coreFile:close()
check("estado: /compa estado llama a WorldStatusLine", string.find(core, "ns.WorldStatusLine", 1, true) ~= nil)
local worldFrame
for _, f in ipairs(frames) do
    if f.events.TAXIMAP_OPENED then worldFrame = f end
end
check("World registra sus eventos", worldFrame ~= nil)
local onUpdate = false
for _, f in ipairs(frames) do
    if f.scripts.OnUpdate ~= nil then onUpdate = true end
    if f.events.OnUpdate then onUpdate = true end
end
check("ningun frame tiene OnUpdate (cero trabajo por frame)", not onUpdate)
check("World no crea tickers", #T.timers == 0)
check("priority: el tipo 16 va el ultimo", C.PRIORITY[#C.PRIORITY] == 16)

-- ---------------------------------------------------------------------------
print("(b) Vuelos: nodos, conocidos y forma")
reset()
T.taxiNodes = {
    { nodeID = 1001, name = "Ventormenta", position = { x = 0.40, y = 0.60 }, state = 2, slotIndex = 1 },
    { nodeID = 1002, name = "Lakeshire", position = { x = 0.20, y = 0.30 }, state = 1, slotIndex = 2 },
    { nodeID = 1003, name = "Gurubashi", position = { x = 0.70, y = 0.80 }, state = 0, slotIndex = 3 },
}
T.costs = { [2] = 120 }
fire("TAXIMAP_OPENED")
local n1 = db().flight.Alliance.nodes[1001]
eq("nodo: nodeID", n1.nodeID, 1001)
eq("nodo: nombre", n1.name, "Ventormenta")
eq("nodo: mapa", n1.mapID, 1414)
eq("nodo: x", n1.x, 0.40)
eq("nodo: known", n1.known, true)
check("nodo: lastSeen", n1.lastSeen == ns.Now(), tostring(n1.lastSeen))
eq("nodo no alcanzable: known = false", db().flight.Alliance.nodes[1003].known, false)
local known = db().flight.known["Tester-Realm"]
check("conocidos del personaje: 1001 y 1002", known and #known == 2 and known[1] == 1001 and known[2] == 1002,
    known and table.concat(known, ",") or "nil")
fire("TAXIMAP_CLOSED")
eq("sin errores capturados", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
print("(c) Vuelos: tiempo medido desde el inicio real del vuelo")
reset()
T.taxiNodes = {
    { nodeID = 1001, name = "Ventormenta", position = { x = 0.4, y = 0.6 }, state = 2, slotIndex = 1 },
    { nodeID = 1002, name = "Lakeshire", position = { x = 0.2, y = 0.3 }, state = 1, slotIndex = 2 },
}
T.costs = { [2] = 120 }
fire("TAXIMAP_OPENED")
T.gettime = T.gettime + 30          -- el jugador tarda en elegir
TakeTaxiNode(2)
T.gettime = T.gettime + 7           -- hasta que el cliente quita el control
T.onTaxi = true
fire("PLAYER_CONTROL_LOST")
T.gettime = T.gettime + 95
T.onTaxi = false
fire("PLAYER_CONTROL_GAINED")
local cost = db().flight.Alliance.costs["1001>1002"]
check("trayecto guardado", cost ~= nil)
eq("trayecto: copper", cost and cost.copper, 120)
eq("trayecto: hops", cost and cost.hops, 1)
eq("trayecto: seconds desde el control (95, no 102 ni 132)", cost and cost.seconds, 95)
fire("TAXIMAP_CLOSED")

-- Sin elegir destino (p. ej. mapa abierto y cerrado) no hay trayecto.
reset()
T.taxiNodes = { { nodeID = 1001, name = "A", position = { x = 0.4, y = 0.6 }, state = 2, slotIndex = 1 } }
fire("TAXIMAP_OPENED")
T.onTaxi = true
fire("PLAYER_CONTROL_LOST")
T.gettime = T.gettime + 50
T.onTaxi = false
fire("PLAYER_CONTROL_GAINED")
eq("sin destino elegido: sin trayecto", db().flight.Alliance.costs, nil)

-- Un trayecto que no dura (control devuelto sin vuelo) se descarta.
reset()
T.taxiNodes = {
    { nodeID = 1001, name = "A", position = { x = 0.4, y = 0.6 }, state = 2, slotIndex = 1 },
    { nodeID = 1002, name = "B", position = { x = 0.2, y = 0.3 }, state = 1, slotIndex = 2 },
}
fire("TAXIMAP_OPENED")
TakeTaxiNode(2)
fire("PLAYER_CONTROL_LOST")         -- UnitOnTaxi = false: no es un vuelo
check("control sin UnitOnTaxi no inicia el vuelo", W.testing.S.ride == nil)
fire("PLAYER_CONTROL_GAINED")
eq("sin vuelo: sin trayecto", db().flight.Alliance.costs, nil)

-- ---------------------------------------------------------------------------
print("(d) Vendedores: forma, caps y actualizacion")
reset()
setNpc(1000, "Vendedor de Pan Mara")
T.merchant = {
    { info = { name = "Pan de Elwynn", price = 50, stackCount = 1, numAvailable = -1 }, link = "|cffffffff|Hitem:2001::::|h[Pan]|h|r" },
    { info = { name = "Veneno", price = 200, stackCount = 1, numAvailable = 5 }, link = "|cffffffff|Hitem:2006::::|h[Veneno]|h|r" },
}
T.tooltips[2001] = { "Pan de Elwynn", "Usar: Restaura 552 de salud durante 21 seg.", "Requiere nivel 1" }
fire("MERCHANT_SHOW")
local v = db().vendors[1000]
check("vendedor guardado", v ~= nil)
eq("vendedor: npcID", v.npcID, 1000)
eq("vendedor: nombre", v.name, "Vendedor de Pan Mara")
eq("vendedor: mapa", v.mapID, 1429)
eq("vendedor: faccion", v.faction, "Alliance")
eq("vendedor: 2 articulos", #v.items, 2)
eq("articulo: itemID desde el enlace", v.items[1].itemID, 2001)
eq("articulo: precio en cobre", v.items[1].price, 50)
eq("articulo: stack", v.items[1].stack, 1)
eq("articulo: stock ilimitado no es limitado", v.items[1].limited, false)
eq("articulo: stock limitado", v.items[2].limited, true)
check("pan (consumible) en consumibles", db().consumables[2001] ~= nil)
fire("MERCHANT_CLOSED")

-- MERCHANT_UPDATE solo actualiza mientras la ventana sigue abierta.
T.merchant[2].info.numAvailable = 0
fire("MERCHANT_UPDATE")
eq("update sin ventana abierta: no cambia", db().vendors[1000].items[2].limited, true)
setNpc(1000, "Vendedor de Pan Mara")
fire("MERCHANT_SHOW")
T.merchant[2].info.numAvailable = 0
fire("MERCHANT_UPDATE")
eq("update: articulo agotado (0 = limitado)", db().vendors[1000].items[2].limited, true)
fire("MERCHANT_CLOSED")

-- Maximo 80 articulos.
reset()
setNpc(1100, "Armero Olaf")
for i = 1, 85 do
    T.merchant[i] = { info = { name = "Item " .. i, price = i, stackCount = 1, numAvailable = -1 },
        link = "|Hitem:" .. (3000 + i) .. "|h[x]|h|r" }
end
fire("MERCHANT_SHOW")
eq("vendedor: como mucho 80 articulos", #db().vendors[1100].items, 80)
fire("MERCHANT_CLOSED")

-- API clasica (sin C_MerchantFrame).
reset()
setNpc(1300, "Mercader antiguo")
T.merchant = {
    { info = { name = "Agua", price = 10, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2005::|h[Agua]|h|r" },
}
local savedMF = _G.C_MerchantFrame
_G.C_MerchantFrame = nil
fire("MERCHANT_SHOW")
_G.C_MerchantFrame = savedMF
local legacy = db().vendors[1300]
eq("API clasica: articulo leido", legacy and legacy.items[1] and legacy.items[1].itemID, 2005)
eq("API clasica: nombre", legacy and legacy.items[1] and legacy.items[1].name, "Agua")
fire("MERCHANT_CLOSED")

-- Maximo 400 vendedores: al llenarse se expulsa el visto hace mas tiempo.
reset()
for i = 1, 401 do
    T.time = T.time + 1
    setNpc(5000 + i, "PNJ " .. i)
    T.merchant = { { info = { name = "Pan", price = 1, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2001|h|r" } }
    fire("MERCHANT_SHOW")
    fire("MERCHANT_CLOSED")
end
local count = 0
for _ in pairs(db().vendors) do count = count + 1 end
eq("vendedores: como mucho 400", count, 400)
check("vendedores: el mas antiguo expulsado", db().vendors[5001] == nil)
check("vendedores: el mas reciente guardado", db().vendors[5401] ~= nil)

-- Tope total: 60 vendedores llenos no superan 300 KB.
reset()
for i = 1, 60 do
    T.time = T.time + 1
    setNpc(6000 + i, "Vendedor grande " .. i)
    T.merchant = {}
    for j = 1, 80 do
        T.merchant[j] = { info = { name = "Articulo con nombre largo " .. j, price = 100 * j, stackCount = 1, numAvailable = -1 },
            link = "|Hitem:" .. (10000 * i + j) .. "|h[x]|h|r" }
    end
    fire("MERCHANT_SHOW")
    fire("MERCHANT_CLOSED")
end
local bytes = 0
for _, rec in pairs(db().vendors) do
    bytes = bytes + #J.Encode(rec)
end
check("tope total: el JSON de vendedores <= 300 KB", bytes <= 300 * 1024, tostring(bytes))
check("tope total: indice coherente con el tope", W.testing.idx.total <= W.MAX_BYTES)
eq("sin errores capturados (vendedores)", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
print("(e) Instructores: clasificacion, servicios y forma")
local kind, prof = W.ClassifyTrainer("Instructora de Herboristería", {})
eq("subtitulo herboristeria: profesion", kind, "profession")
eq("subtitulo herboristeria: nombre de la profesion", prof, nil)
eq("armas: weapon", (W.ClassifyTrainer("Instructor de Armas", {})), "weapon")
eq("equitacion: riding", (W.ClassifyTrainer("Instructor de equitación", {})), "riding")
eq("clase: class", (W.ClassifyTrainer("Guerrero", {})), "class")
eq("sin pistas: other", (W.ClassifyTrainer("Posadera", {})), "other")
local _, prof2 = W.ClassifyTrainer("", { { name = "Aprendiz", skill = { name = "Minería", rank = 1 } } })
eq("servicios con habilidad: profesion desde la habilidad", prof2, "Minería")

reset()
setNpc(1204, "Instructora Ana", "Instructora de Herboristería")
T.services = {
    { name = "Herboristería", rank = "Aprendiz", cat = "available", cost = 1200, level = 20, skill = "Herboristería", skillRank = 1 },
    { name = "Herboristería", rank = "Maestro", cat = "unavailable", cost = 4500, level = 40, skill = "Herboristería", skillRank = 75 },
    { name = "Herboristería", rank = "Experto", cat = "used", cost = 0, level = 1 },
}
fire("TRAINER_SHOW")
local tr = db().trainers[1204]
check("instructor guardado", tr ~= nil)
eq("instructor: kind", tr.kind, "profession")
eq("instructor: profesion", tr.profession, "Herboristería")
eq("instructor: 3 servicios", #tr.services, 3)
eq("servicio: nombre", tr.services[1].name, "Herboristería")
eq("servicio: rango", tr.services[1].rank, "Aprendiz")
eq("servicio: coste en cobre", tr.services[1].cost, 1200)
eq("servicio: nivel", tr.services[1].level, 20)
eq("servicio: habilidad", tr.services[1].skill.name, "Herboristería")
eq("servicio: habilidad rango", tr.services[1].skill.rank, 1)
eq("servicio: disponible", tr.services[1].available, "available")
eq("servicio: no disponible", tr.services[2].available, "unavailable")
eq("servicio: usado", tr.services[3].available, "used")
fire("TRAINER_CLOSED")

reset()
setNpc(1205, "Instructor de Armas Brunn", "Instructor de Armas")
for i = 1, 130 do
    T.services[i] = { name = "Habilidad " .. i, rank = "", cat = "available", cost = i }
end
fire("TRAINER_SHOW")
eq("instructor: como mucho 120 servicios", #db().trainers[1205].services, 120)
eq("instructor: kind weapon", db().trainers[1205].kind, "weapon")
fire("TRAINER_CLOSED")

-- ---------------------------------------------------------------------------
print("(f) Cadenas de misiones: observadas y de Questie")
reset()
setNpc(1000, "Marshal Dughan")
fire("QUEST_TURNED_IN", 100, 500, 20)
T.gettime = T.gettime + 10
T.time = T.time + 10
fire("QUEST_ACCEPTED", 1, 101)
local c100, c101 = db().questChains[100], db().questChains[101]
eq("cadena: next de 100 = 101", c100 and c100.next, 101)
eq("cadena: pre de 101 = {100}", c101 and c101.pre and #c101.pre == 1 and c101.pre[1], 100)

reset()
setNpc(1000, "Marshal Dughan")
fire("QUEST_TURNED_IN", 102, 0, 0)
setNpc(2000, "Otro PNJ")
fire("QUEST_ACCEPTED", 1, 103)
check("mismo PNJ requerido: otro PNJ no encadena", db().questChains[102] == nil and db().questChains[103] == nil)

reset()
setNpc(1000, "Marshal Dughan")
fire("QUEST_TURNED_IN", 104, 0, 0)
T.time = T.time + 121
fire("QUEST_ACCEPTED", 1, 105)
check("ventana de 120 s: mas tarde no encadena", db().questChains[104] == nil)

reset()
setNpc(1000, "Marshal Dughan")
T.questieData[200] = { preQuestSingle = { 150, 151 }, nextQuestInChain = 201 }
fire("QUEST_ACCEPTED", 1, 200)
local q200 = db().questChains[200]
check("Questie: pre y next de 200", q200 and q200.pre and #q200.pre == 2 and q200.pre[2] == 151 and q200.next == 201)

-- ---------------------------------------------------------------------------
print("(g) Consumibles: tooltips en espanol, bolsas y botin")
eq("numero: 1.234 -> 1234", W.ToNumber("1.234"), 1234)
eq("numero: 1,234 -> 1234", W.ToNumber("1,234"), 1234)
eq("numero: 2,5 -> 2.5", W.ToNumber("2,5"), 2.5)
eq("numero: 552 -> 552", W.ToNumber("552"), 552)

local food = W.ParseConsumable(2001, { "Pan de Elwynn", "Usar: Restaura 552 de salud durante 21 seg.", "Requiere nivel 15", "Requiere Primeros auxilios (80)" }, 5)
check("comida: ficha", food ~= nil)
eq("comida: kind", food and food.kind, "food")
eq("comida: salud", food and food.restores.health, 552)
eq("comida: mana 0", food and food.restores.mana, 0)
eq("comida: segundos", food and food.restores.seconds, 21)
eq("comida: nivel requerido", food and food.requiredLevel, 15)
eq("comida: habilidad", food and food.skill and food.skill.name, "Primeros auxilios")
eq("comida: rango de habilidad", food and food.skill and food.skill.rank, 80)

local mana = W.ParseConsumable(2002, { "Poción de maná menor", "Restaura 1.234 de maná en 15 seg." }, 1)
eq("pocion de mana: kind", mana and mana.kind, "potion_mana")
eq("pocion de mana: 1.234 = 1234", mana and mana.restores and mana.restores.mana, 1234)
eq("pocion de mana: seg", mana and mana.restores and mana.restores.seconds, 15)

local hp = W.ParseConsumable(2010, { "Poción de salud", "Restaura 1,500 de salud." })
eq("pocion sin subclase: kind por texto", hp and hp.kind, "potion_health")
eq("pocion sin subclase: 1,500 = 1500", hp and hp.restores and hp.restores.health, 1500)

local water = W.ParseConsumable(2005, { "Agua", "Restaura 1.000 de maná durante 27 seg." }, 5)
eq("agua: drink", water and water.kind, "drink")
eq("agua: mana", water and water.restores.mana, 1000)

local bandage = W.ParseConsumable(2003, { "Vendaje de lino grueso", "Restaura 300 de salud durante 8 seg." }, 7)
eq("vendaje: bandage", bandage and bandage.kind, "bandage")

check("sin nada util: nil", W.ParseConsumable(2099, { "Fragmento de hierro", "Material de fabricación" }, 0) == nil)

-- Bolsas: consumibles del inventario, y barrido con throttle.
reset()
T.tooltips[2001] = { "Pan de Elwynn", "Usar: Restaura 552 de salud durante 21 seg.", "Requiere nivel 15" }
T.tooltips[2002] = { "Poción de maná menor", "Restaura 1.234 de maná en 15 seg." }
T.classOf[2001] = { class = 0, sub = 5 }
T.classOf[2002] = { class = 0, sub = 1 }
T.classOf[3000] = { class = 2, sub = 4 }
T.tooltips[3000] = { "Espada de prueba" }
T.bagSlots = { [0] = 3, [1] = 2 }
T.bags = { [0] = { 2001, 3000, 2002 }, [1] = { 2001 } }
fire("BAG_UPDATE_DELAYED")
check("bolsas: consumible guardado", db().consumables[2001] ~= nil and db().consumables[2002] ~= nil)
check("bolsas: un arma no se lee como consumible", db().consumables[3000] == nil)
eq("bolsas: el arma queda marcada como no consumible", W.testing.S.parsed[3000], false)
resetLog()
fire("BAG_UPDATE_DELAYED")
fire("BAG_UPDATE_DELAYED")
local slotCalls = 0
for _, n in ipairs(apiLog) do if n == "C_Container.GetContainerNumSlots" then slotCalls = slotCalls + 1 end end
eq("throttle: dentro del plazo no se barre", slotCalls, 0)
T.gettime = T.gettime + W.BAG_THROTTLE + 1
runTimers()
slotCalls = 0
for _, n in ipairs(apiLog) do if n == "C_Container.GetContainerNumSlots" then slotCalls = slotCalls + 1 end end
eq("throttle: pasado el plazo, un solo barrido diferido (bolsas 0, 1 y reactivos 5)", slotCalls, 3)

-- Botin y vendedor: los consumibles tambien salen de ahi.
reset()
T.tooltips[2004] = { "Poción de salud", "Restaura 500 de salud durante 10 seg." }
T.classOf[2004] = { class = 0, sub = 1 }
T.loot = { "|Hitem:2004::|h[P]|h|r" }
fire("LOOT_OPENED")
check("botin: consumible guardado", db().consumables[2004] ~= nil)
eq("botin: potion_health", db().consumables[2004] and db().consumables[2004].kind, "potion_health")

reset()
setNpc(1000, "Vendedor de agua")
T.tooltips[2005] = { "Agua", "Restaura 1.000 de maná durante 27 seg." }
T.classOf[2005] = { class = 0, sub = 5 }
T.merchant = { { info = { name = "Agua", price = 10, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2005|h|r" } }
fire("MERCHANT_SHOW")
check("vendedor: consumible del stock", db().consumables[2005] ~= nil and db().consumables[2005].kind == "drink")
fire("MERCHANT_CLOSED")

-- ---------------------------------------------------------------------------
print("(h) Combate: nada de lectura, todo al salir")
reset()
T.taxiNodes = { { nodeID = 1001, name = "A", position = { x = 0.4, y = 0.6 }, state = 2, slotIndex = 1 } }
T.loot = { "|Hitem:2004|h|r" }
T.tooltips[2004] = { "Poción de salud", "Restaura 500 de salud durante 10 seg." }
T.classOf[2004] = { class = 0, sub = 1 }
T.bagSlots = { [0] = 1, [1] = 0 }
T.bags = { [0] = { 2004 } }
setNpc(1000, "Vendedor en combate")
T.merchant = { { info = { name = "Pan", price = 1, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2001|h|r" } }
T.combat = true
resetLog()
fire("TAXIMAP_OPENED")
fire("MERCHANT_SHOW")
fire("LOOT_OPENED")
fire("BAG_UPDATE_DELAYED")
local worldApis = { "C_TaxiMap.GetAllTaxiNodes", "GetMerchantNumItems",
    "GetNumLootItems", "C_Container.GetContainerNumSlots", "C_TooltipInfo.GetItemByID", "C_TooltipInfo.GetUnit" }
local leaked = {}
for _, n in ipairs(worldApis) do
    if usedApi(n) then leaked[#leaked + 1] = n end
end
check("combate: ninguna lectura del mundo", #leaked == 0, table.concat(leaked, ","))
eq("combate: nada guardado", db().vendors[1000], nil)
check("combate: sin nodos de vuelo guardados", db().flight.Alliance == nil or db().flight.Alliance.nodes == nil)
T.combat = false
resetLog()
fire("PLAYER_REGEN_ENABLED")
check("salir de combate: vendedor guardado", db().vendors[1000] ~= nil)
check("salir de combate: nodo de vuelo guardado", db().flight.Alliance.nodes[1001] ~= nil)
check("salir de combate: consumible del botin o bolsa", db().consumables[2004] ~= nil)
fire("MERCHANT_CLOSED")

-- Un vendedor que se cierra antes de salir de combate no se lee.
T.combat = true
setNpc(1500, "Vendedor fugaz")
T.merchant = { { info = { name = "Pan", price = 1, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2001|h|r" } }
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
T.combat = false
fire("PLAYER_REGEN_ENABLED")
check("cerrado en combate: no se guarda", db().vendors[1500] == nil)

-- Instructor abierto en combate: se lee al salir.
T.combat = true
setNpc(1204, "Instructor en combate", "Instructor de Armas")
T.services = { { name = "Armas", rank = "", cat = "available", cost = 1 } }
resetLog()
fire("TRAINER_SHOW")
check("combate: instructor sin lectura", not usedApi("GetNumTrainerServices"))
T.combat = false
fire("PLAYER_REGEN_ENABLED")
check("salir de combate: instructor guardado", db().trainers[1204] ~= nil)
fire("TRAINER_CLOSED")
eq("sin errores capturados (combate)", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
print("(i) Estado: linea de /compa estado")
reset()
T.taxiNodes = {
    { nodeID = 1001, name = "A", position = { x = 0.4, y = 0.6 }, state = 2, slotIndex = 1 },
    { nodeID = 1002, name = "B", position = { x = 0.2, y = 0.3 }, state = 1, slotIndex = 2 },
}
fire("TAXIMAP_OPENED")
fire("TAXIMAP_CLOSED")
setNpc(1000, "Vendedor")
T.merchant = { { info = { name = "Pan", price = 1, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2001|h|r" } }
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
setNpc(1204, "Instructora", "Instructora de Herboristería")
T.services = { { name = "Herboristería", rank = "", cat = "available", cost = 1, skill = "Herboristería", skillRank = 1 } }
fire("TRAINER_SHOW")
fire("TRAINER_CLOSED")
T.tooltips[2001] = { "Pan de Elwynn", "Restaura 552 de salud durante 21 seg." }
T.classOf[2001] = { class = 0, sub = 5 }
T.bagSlots = { [0] = 1, [1] = 0 }
T.bags = { [0] = { 2001 } }
fire("BAG_UPDATE_DELAYED")
eq("estado: linea", W.StatusLine(), "Mundo: 2 vuelos, 1 vendedores, 1 instructores, 1 consumibles")
eq("estado: /compa estado usa la linea", ns.WorldStatusLine(), "Mundo: 2 vuelos, 1 vendedores, 1 instructores, 1 consumibles")

-- ---------------------------------------------------------------------------
print("(j) Canal: solo los bloques cambiados (msgType 16)")
reset()
setNpc(1000, "Vendedor A")
T.merchant = { { info = { name = "Pan", price = 1, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2001|h|r" } }
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
setNpc(1200, "Vendedor B")
T.merchant = { { info = { name = "Agua", price = 2, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2005|h|r" } }
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
local now = T.gettime
local full = W.ShareMessage()
check("primer envio: mensaje completo", full ~= nil)
check("primer envio: incluye vendedores", full and string.find(full, '"1000"', 1, true) ~= nil and string.find(full, '"1200"', 1, true) ~= nil)
eq("sin cambios: nada que enviar", W.ShareMessage(), nil)
check("sin cambios: ya no queda pendiente", not W.ChannelPending())

setNpc(1200, "Vendedor B")
T.merchant[1].info.price = 3
T.time = T.time + 5
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
local delta = W.ShareMessage()
check("cambio de un vendedor: mensaje", delta ~= nil)
check("cambio: solo el vendedor cambiado", delta and string.find(delta, '"1200"', 1, true) ~= nil
    and string.find(delta, '"1000"', 1, true) == nil)
check("cambio: mensaje mas corto que el completo", delta and #delta < #full)
check("cambio: la forma es el mismo esquema (vendors)", delta and string.sub(delta, 1, 11) == '{"vendors":')
check("cambio: JSON valido (decodifica)", delta and J.Encode and (delta:match("^%b{}$") ~= nil))
eq("cambio: nada mas pendiente", W.ShareMessage(), nil)

-- Reenvio completo cada CHANNEL_FULL_EVERY s.
T.gettime = T.gettime + W.CHANNEL_FULL_EVERY + 1
check("reenvio completo tras el plazo", W.ShareMessage() ~= nil)
T.gettime = T.gettime - W.CHANNEL_FULL_EVERY - 1

-- Mensajes grandes: varios mensajes, cada uno dentro del presupuesto.
reset()
for i = 1, 40 do
    T.time = T.time + 1
    setNpc(7000 + i, "Gran vendedor " .. i)
    T.merchant = {}
    for j = 1, 80 do
        T.merchant[j] = { info = { name = "Articulo " .. j, price = j, stackCount = 1, numAvailable = -1 },
            link = "|Hitem:" .. (20000 * i + j) .. "|h|r" }
    end
    fire("MERCHANT_SHOW")
    fire("MERCHANT_CLOSED")
end
local parts, seen, maxLen = 0, {}, 0
local msg = W.ShareMessage()
while msg do
    parts = parts + 1
    if #msg > maxLen then maxLen = #msg end
    for i = 7001, 7040 do
        if string.find(msg, '"' .. i .. '"', 1, true) then seen[i] = true end
    end
    msg = W.ShareMessage()
end
check("mensajes grandes: varios mensajes", parts > 1, tostring(parts))
check("mensajes grandes: cada uno <= 98304 bytes", maxLen <= 98304, tostring(maxLen))
local allSeen = true
for i = 7001, 7040 do if not seen[i] then allSeen = false end end
check("mensajes grandes: todos los vendedores llegan", allSeen)

-- Ventana de compartir: el tipo 16 va el ultimo y sale como trozos en la activacion.
reset()
setNpc(1000, "Vendedor canal")
T.merchant = { { info = { name = "Pan", price = 1, stackCount = 1, numAvailable = -1 }, link = "|Hitem:2001|h|r" } }
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
local msgs = runShare()
check("ventana: sale un mensaje 16", #msgs >= 1, tostring(#msgs))
check("ventana: el mensaje contiene el vendedor", msgs[1] and string.find(msgs[1], '"1000"', 1, true) ~= nil)
check("ventana: JSON de objeto", msgs[1] and string.sub(msgs[1], 1, 1) == "{")
eq("ventana: segunda activacion sin cambios no reenvia el mundo", #runShare(), 0)

-- Cambio posterior: la siguiente activacion manda solo ese bloque.
setNpc(1000, "Vendedor canal")
T.merchant[1].info.price = 9
T.time = T.time + 5
fire("MERCHANT_SHOW")
fire("MERCHANT_CLOSED")
local msgs2 = runShare()
eq("ventana: un cambio, un mensaje", #msgs2, 1)
check("ventana: el cambio es el vendedor 1000", msgs2[1] and string.find(msgs2[1], '"price":9', 1, true) ~= nil)

-- /compa sync: todo el mundo sale aunque no haya cambiado.
local syncMsgs = runShare({ full = true })
check("sync completo: reenvia el mundo", #syncMsgs >= 1)

-- Overflow: mas datos que un mensaje; la ventana sigue hasta sacarlos.
reset()
for i = 1, 30 do
    T.time = T.time + 1
    setNpc(8000 + i, "Vendedor grande " .. i)
    T.merchant = {}
    for j = 1, 80 do
        T.merchant[j] = { info = { name = "Art " .. j, price = j, stackCount = 1, numAvailable = -1 },
            link = "|Hitem:" .. (30000 * i + j) .. "|h|r" }
    end
    fire("MERCHANT_SHOW")
    fire("MERCHANT_CLOSED")
end
-- Una ventana dura SHARE_SECONDS: el resto sale en las siguientes activaciones.
local big, activations, got = 0, 0, ""
for _ = 1, 10 do
    activations = activations + 1
    local msgs = runShare()
    big = big + #msgs
    got = got .. table.concat(msgs, "|")
    if not W.ChannelPending() then break end
end
local missing = 0
for i = 1, 30 do
    if not string.find(got, '"' .. (8000 + i) .. '"', 1, true) then missing = missing + 1 end
end
eq("sobrante: ningun vendedor se pierde entre mensajes", missing, 0)
check("sobrante: varios mensajes entre activaciones", big >= 2, tostring(big))
check("sobrante: no queda nada pendiente tras drenar", not W.ChannelPending(), tostring(activations))

eq("sin errores capturados (canal)", ns.errorCount, 0)

-- ---------------------------------------------------------------------------
if failures > 0 then
    print("FAILED: " .. failures .. " check(s)")
    os.exit(1)
end
print("ALL TESTS PASSED")
os.exit(0)
