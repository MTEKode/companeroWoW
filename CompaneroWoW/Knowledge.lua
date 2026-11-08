-- Knowledge.lua: base de conocimiento que aprende SOLO de lo que el jugador observa.
-- Guarda misiones (CompaneroWoWDB.knowledge.quests) y PNJs (knowledge.npcs).
-- Nunca guarda jugadores como PNJs. Todo valor secreto se descarta.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Get, Num, Str, Clean = ns.Call, ns.Get, ns.Num, ns.Str, ns.Clean

-- Tablas de conocimiento (creadas si faltan). nil si la DB aun no existe.
local function kb()
    if type(CompaneroWoWDB) ~= "table" then return nil end
    local db = CompaneroWoWDB
    if type(db.knowledge) ~= "table" then db.knowledge = {} end
    if type(db.knowledge.quests) ~= "table" then db.knowledge.quests = {} end
    if type(db.knowledge.npcs) ~= "table" then db.knowledge.npcs = {} end
    return db.knowledge
end

-- Asigna solo si el valor no es nil (no borra datos previos).
local function setIf(t, k, v)
    if v ~= nil then t[k] = v end
end

local function questRec(qid)
    local k = kb()
    if not k then return nil end
    local q = k.quests[qid]
    if type(q) ~= "table" then
        q = { questID = qid, firstSeen = ns.Now() }
        k.quests[qid] = q
    end
    q.questID = qid
    return q
end

-- Limites de knowledge.npcs: total y por mapa. Al llenarse se expulsa el PNJ menos visto recientemente.
local MAX_NPCS_TOTAL = 2000
local MAX_NPCS_PER_MAP = 150

local function seenAt(rec)
    return rec.lastSeen or rec.firstSeen or 0
end

-- Expulsa el registro menos visto que cumpla match(rec). Devuelve true si expulso alguno.
local function evictOldest(npcs, match)
    local oldestID, oldestAt = nil, nil
    for id, rec in pairs(npcs) do
        if type(rec) == "table" and match(rec) then
            local at = seenAt(rec)
            if oldestAt == nil or at < oldestAt then oldestID, oldestAt = id, at end
        end
    end
    if oldestID == nil then return false end
    npcs[oldestID] = nil
    return true
end

-- Hace sitio para un PNJ nuevo en mapID: antes de insertarlo, respeta el tope por mapa y el total.
local function makeRoomFor(npcs, mapID)
    if mapID ~= nil then
        local onMap = 0
        for _, rec in pairs(npcs) do
            if type(rec) == "table" and rec.mapID == mapID then onMap = onMap + 1 end
        end
        while onMap >= MAX_NPCS_PER_MAP do
            if not evictOldest(npcs, function(rec) return rec.mapID == mapID end) then break end
            onMap = onMap - 1
        end
    end
    local total = 0
    for _ in pairs(npcs) do total = total + 1 end
    while total >= MAX_NPCS_TOTAL do
        if not evictOldest(npcs, function() return true end) then break end
        total = total - 1
    end
end

-- Inserta o actualiza un PNJ. info.isQuestGiver/isVendor/isTrainer solo se activan a true.
local function upsertNPC(npcID, info)
    local k = kb()
    if not k or not npcID or npcID <= 0 then return end
    local rec = k.npcs[npcID]
    local now = ns.Now()
    if type(rec) ~= "table" then
        makeRoomFor(k.npcs, info.mapID)
        rec = { npcID = npcID, firstSeen = now }
        k.npcs[npcID] = rec
    end
    rec.npcID = npcID
    setIf(rec, "name", info.name)
    setIf(rec, "mapID", info.mapID)
    setIf(rec, "x", info.x)
    setIf(rec, "y", info.y)
    setIf(rec, "reaction", info.reaction)
    if info.isQuestGiver then rec.isQuestGiver = true end
    if info.isVendor then rec.isVendor = true end
    if info.isTrainer then rec.isTrainer = true end
    rec.lastSeen = now
end

-- Datos de una unidad PNJ (Creature/Vehicle, no jugador). nil si no aplica.
local function unitNPC(unit)
    local kind, npcID = ns.ParseGUID(Call("UnitGUID", unit))
    if (kind ~= "Creature" and kind ~= "Vehicle") or not npcID then return nil end
    if Clean(Call("UnitIsPlayer", unit)) == true then return nil end
    local mapID, x, y = ns.GetPlayerPosition()
    return { npcID = npcID, name = Str(Call("UnitName", unit)), mapID = mapID, x = x, y = y }
end

-- Item de recompensa de la ventana de mision (kind = "reward" | "choice").
local function questItem(kind, i, isChoice)
    local link = Str(Call("GetQuestItemLink", kind, i))
    local _, _, numItems, _, _, itemID = Call("GetQuestItemInfo", kind, i)
    local idStr = link and string.match(link, "item:(%d+)") or nil
    itemID = Num(itemID) or (idStr and tonumber(idStr)) or nil
    if not itemID and not link then return nil end
    return { itemID = itemID, link = link, count = Num(numItems), choice = isChoice }
end

local function onQuestDetail()
    local qid = Num(Call("GetQuestID"))
    if not qid or qid <= 0 then return end
    local q = questRec(qid)
    if not q then return end
    setIf(q, "title", Str(Call("GetTitleText")))
    setIf(q, "objectivesText", Str(Call("GetObjectiveText")))
    setIf(q, "descriptionText", Str(Call("GetQuestText")))
    local npc = unitNPC("npc")
    if npc then
        setIf(q, "giverNpcID", npc.npcID)
        setIf(q, "giverName", npc.name)
        setIf(q, "giverMapID", npc.mapID)
        setIf(q, "giverX", npc.x)
        setIf(q, "giverY", npc.y)
        upsertNPC(npc.npcID, { name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y, isQuestGiver = true })
    end
    q.lastSeen = ns.Now()
end

local function onQuestComplete()
    local qid = Num(Call("GetQuestID"))
    if not qid or qid <= 0 then return end
    local q = questRec(qid)
    if not q then return end
    local npc = unitNPC("npc")
    if npc then
        setIf(q, "turnInNpcID", npc.npcID)
        setIf(q, "turnInName", npc.name)
        setIf(q, "turnInMapID", npc.mapID)
        setIf(q, "turnInX", npc.x)
        setIf(q, "turnInY", npc.y)
        upsertNPC(npc.npcID, { name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y, isQuestGiver = true })
    end
    local items = {}
    local nRewards = Num(Call("GetNumQuestRewards")) or 0
    for i = 1, nRewards do
        local it = questItem("reward", i, false)
        if it then items[#items + 1] = it end
    end
    local nChoices = Num(Call("GetNumQuestChoices")) or 0
    for i = 1, nChoices do
        local it = questItem("choice", i, true)
        if it then items[#items + 1] = it end
    end
    q.rewards = {
        money = Num(Call("GetRewardMoney")),
        xp = Num(Call("GetRewardXP")),
        items = items,
    }
    q.lastSeen = ns.Now()
end

-- Completa titulo y nivel desde C_QuestLog si faltan.
local function onQuestAccepted(a, b)
    local qid = Num(b) or Num(a)
    if not qid then return end
    local q = questRec(qid)
    if not q then return end
    if not q.title or not q.level then
        local idx = Num(Call("C_QuestLog.GetLogIndexForQuestID", qid))
        local info = idx and Clean(Call("C_QuestLog.GetInfo", idx)) or nil
        if type(info) == "table" then
            if not q.title then q.title = Str(info.title) end
            if not q.level then q.level = Num(info.level) end
        end
    end
    if not q.title then q.title = Str(Call("C_QuestLog.GetTitleForQuestID", qid)) end
    q.lastSeen = ns.Now()
end

local function countEntries(t)
    if type(t) ~= "table" then return 0 end
    return #t
end

local function onGossip()
    local npc = unitNPC("npc")
    if not npc then return end
    local available = Clean(Call("C_GossipInfo.GetAvailableQuests"))
    local active = Clean(Call("C_GossipInfo.GetActiveQuests"))
    local isVendor, isTrainer = false, false
    local opts = Clean(Call("C_GossipInfo.GetOptions"))
    if type(opts) == "table" then
        for _, o in ipairs(opts) do
            local t = type(o) == "table" and Str(o.type) or nil
            if t == "vendor" then isVendor = true elseif t == "trainer" then isTrainer = true end
        end
    end
    upsertNPC(npc.npcID, {
        name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y,
        reaction = Num(Call("UnitReaction", "npc")),
        isQuestGiver = (countEntries(available) + countEntries(active)) > 0,
        isVendor = isVendor,
        isTrainer = isTrainer,
    })
end

local function onMerchant()
    local npc = unitNPC("npc")
    if not npc then return end
    upsertNPC(npc.npcID, {
        name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y,
        reaction = Num(Call("UnitReaction", "npc")),
        isVendor = true,
    })
end

local function onTrainer()
    local npc = unitNPC("npc")
    if not npc then return end
    upsertNPC(npc.npcID, {
        name = npc.name, mapID = npc.mapID, x = npc.x, y = npc.y,
        reaction = Num(Call("UnitReaction", "npc")),
        isTrainer = true,
    })
end

-- Objetivo no hostil (Creature/Vehicle que no es jugador) -> PNJ.
local function onTargetChanged()
    if Clean(Call("UnitExists", "target")) ~= true then return end
    local kind, npcID = ns.ParseGUID(Call("UnitGUID", "target"))
    if (kind ~= "Creature" and kind ~= "Vehicle") or not npcID then return end
    if Clean(Call("UnitIsPlayer", "target")) == true then return end
    if Clean(Call("UnitCanAttack", "player", "target")) == true then return end
    local mapID, x, y = ns.GetPlayerPosition()
    upsertNPC(npcID, {
        name = Str(Call("UnitName", "target")),
        mapID = mapID, x = x, y = y,
        reaction = Num(Call("UnitReaction", "player", "target")),
    })
end

local handlers = {
    QUEST_DETAIL = onQuestDetail,
    QUEST_COMPLETE = onQuestComplete,
    QUEST_ACCEPTED = onQuestAccepted,
    GOSSIP_SHOW = onGossip,
    MERCHANT_SHOW = onMerchant,
    TRAINER_SHOW = onTrainer,
    PLAYER_TARGET_CHANGED = onTargetChanged,
}

ns.Knowledge = {
    OnEvent = function(event, ...)
        local h = handlers[event]
        if h then h(...) end
    end,
}

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(ns.Knowledge.OnEvent, event, ...)
    end)
end
