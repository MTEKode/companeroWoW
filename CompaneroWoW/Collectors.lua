-- Collectors.lua: cada CollectX devuelve una tabla plana segun el esquema compartido.
-- Cada campo no disponible se omite (nil). Nunca lanzan error: las APIs se
-- consultan con ns.Call (existencia + pcall) y los secretos se descartan.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Get, Num, Str, Clean = ns.Call, ns.Get, ns.Num, ns.Str, ns.Clean
local floor = math.floor

-- ---------------------------------------------------------------------------
-- Utilidades compartidas
-- ---------------------------------------------------------------------------

-- Registra el nombre de un mapa la primera vez que se ve.
function ns.RecordMap(mapID)
    mapID = Num(mapID)
    if not mapID or mapID <= 0 or type(CompaneroWoWDB) ~= "table" then return end
    local maps = CompaneroWoWDB.maps
    if type(maps) ~= "table" then maps = {}; CompaneroWoWDB.maps = maps end
    if maps[mapID] then return end
    local info = Clean(Call("C_Map.GetMapInfo", mapID))
    local name = type(info) == "table" and Str(info.name)
    if name then maps[mapID] = name end
end

-- mapID, x, y del jugador (x,y en 0..1). Campos desconocidos -> nil.
function ns.GetPlayerPosition()
    local mapID = Num(Call("C_Map.GetBestMapForUnit", "player"))
    if mapID and mapID <= 0 then mapID = nil end
    if not mapID then return nil, nil, nil end
    local x, y
    local pos = Clean(Call("C_Map.GetPlayerMapPosition", mapID, "player"))
    if pos ~= nil then
        local px, py = ns.SafeCall(function() return pos:GetXY() end)
        px, py = Num(px), Num(py)
        if px and py and px >= 0 and px <= 1 and py >= 0 and py <= 1 then
            x, y = px, py
        end
    end
    return mapID, x, y
end

-- Porcentaje de XP (0..100). 0 en nivel maximo, nil si desconocido.
local function isMaxLevel()
    local v = Clean(Call("IsPlayerAtEffectiveMaxLevel"))
    if type(v) == "boolean" then return v end
    local level = Num(Call("UnitLevel", "player"))
    local maxLevel = Num(Call("GetMaxLevelForPlayerExpansion"))
    if level and maxLevel then return level >= maxLevel end
    return nil
end

function ns.XPPercent()
    if isMaxLevel() == true then return 0 end
    local xp = Num(Call("UnitXP", "player"))
    local xpMax = Num(Call("UnitXPMax", "player"))
    if not xp or not xpMax or xpMax <= 0 then return nil end
    return ns.ClampInt(floor(xp / xpMax * 100), 0, 100, 0)
end

-- ---------------------------------------------------------------------------
-- Bolsas
-- ---------------------------------------------------------------------------

local function bagIndexes()
    local list = {}
    local maxBag = Num(_G.NUM_BAG_SLOTS) or 4
    for b = 0, maxBag do list[#list + 1] = b end
    local bagEnum = _G.Enum and _G.Enum.BagIndex
    local reagent = type(bagEnum) == "table" and Num(bagEnum.ReagentBag)
    if reagent then list[#list + 1] = reagent end
    return list
end

local function bagSlots(bag)
    local n = Num(Call("C_Container.GetContainerNumSlots", bag))
    if n == nil then n = Num(Call("GetContainerNumSlots", bag)) end
    return n
end

local function bagFree(bag)
    local free = Num(Call("C_Container.GetContainerNumFreeSlots", bag))
    if free == nil then free = Num(Call("GetContainerNumFreeSlots", bag)) end
    return free
end

-- Id de un objeto a partir de su enlace ("|Hitem:2589:..."). nil si no lo lleva.
local function linkItemID(link)
    link = Str(link)
    if not link then return nil end
    return tonumber(string.match(link, "item:(%d+)"))
end

local function containerItem(bag, slot)
    if Get("C_Container.GetContainerItemInfo") then
        local info = Clean(Call("C_Container.GetContainerItemInfo", bag, slot))
        if type(info) ~= "table" then return nil end
        return {
            bag = bag,
            slot = slot,
            itemID = Num(info.itemID) or linkItemID(info.hyperlink),
            link = Str(info.hyperlink),
            count = Num(info.stackCount),
            quality = Num(info.quality),
        }
    end
    local tex, count, _, quality, _, _, link, _, _, itemID = Call("GetContainerItemInfo", bag, slot)
    if Clean(tex) == nil then return nil end
    return {
        bag = bag,
        slot = slot,
        -- Los clientes antiguos no devuelven el decimo valor (itemID): sale del enlace.
        itemID = Num(itemID) or linkItemID(link),
        link = Str(link),
        count = Num(count),
        quality = Num(quality),
    }
end

-- Lectura de un contenedor cualquiera (bolsas del banco incluidas): para Extras.lua.
ns.ContainerSlots = bagSlots
ns.ContainerItem = containerItem

-- Slots libres totales (nil si la API no esta disponible). Usado por la franja.
function ns.CountFreeBagSlots()
    local total = nil
    for _, bag in ipairs(bagIndexes()) do
        local free = bagFree(bag)
        if free then total = (total or 0) + free end
    end
    return total
end

-- ---------------------------------------------------------------------------
-- Collectors del esquema
-- ---------------------------------------------------------------------------

function ns.CollectCharacter()
    local className, classFile = Call("UnitClass", "player")
    local guildName = Call("GetGuildInfo", "player")
    local specIdx = Num(Call("GetSpecialization"))
    local _, specName = nil, nil
    if specIdx then _, specName = Call("GetSpecializationInfo", specIdx) end
    return {
        name = ns.PlayerName(),
        realm = ns.RealmName(),
        race = Str(Call("UnitRace", "player")),
        class = Str(className),
        classFile = Str(classFile),
        faction = Str(Call("UnitFactionGroup", "player")),
        sex = Num(Call("UnitSex", "player")),
        level = Num(Call("UnitLevel", "player")),
        xp = Num(Call("UnitXP", "player")),
        xpMax = Num(Call("UnitXPMax", "player")),
        restedXP = Num(Call("GetXPExhaustion")),
        money = Num(Call("GetMoney")),
        guild = Str(guildName),
        isMaxLevel = isMaxLevel(),
        specOrTalentSummary = Str(specName),
    }
end

function ns.CollectLocation()
    local mapID, x, y = ns.GetPlayerPosition()
    ns.RecordMap(mapID)
    local instanceName, instanceType = Call("GetInstanceInfo")
    return {
        mapID = mapID,
        zone = Str(Call("GetRealZoneText")),
        subZone = Str(Call("GetSubZoneText")),
        x = x,
        y = y,
        instanceName = Str(instanceName),
        instanceType = Str(instanceType),
    }
end

local function questObjectives(qid)
    local list = Clean(Call("C_QuestLog.GetQuestObjectives", qid))
    if type(list) ~= "table" then return nil end
    local out = {}
    for _, o in ipairs(list) do
        if type(o) == "table" then
            out[#out + 1] = {
                text = Str(o.text),
                type = Str(o.type),
                finished = Clean(o.finished),
                numFulfilled = Num(o.numFulfilled),
                numRequired = Num(o.numRequired),
            }
        end
    end
    return out
end

local function isWatchedQuest(qid, logIndex)
    if Get("C_QuestLog.GetQuestWatchType") then
        return Clean(Call("C_QuestLog.GetQuestWatchType", qid)) ~= nil
    end
    if logIndex and Get("IsQuestWatched") then
        return Clean(Call("IsQuestWatched", logIndex)) == true
    end
    return nil
end

function ns.CollectQuests()
    local out = {}
    local superTracked = Num(Call("C_SuperTrack.GetSuperTrackedQuestID"))
    if Get("C_QuestLog.GetNumQuestLogEntries") then
        local n = Num(Call("C_QuestLog.GetNumQuestLogEntries")) or 0
        local zone = nil
        for i = 1, n do
            local info = Clean(Call("C_QuestLog.GetInfo", i))
            if type(info) == "table" then
                if Clean(info.isHeader) == true then
                    zone = Str(info.title)
                else
                    local qid = Num(info.questID)
                    if qid and qid > 0 then
                        local done = Clean(Call("C_QuestLog.IsComplete", qid))
                        if done == nil then done = Clean(info.isComplete) end
                        local failed = Clean(Call("C_QuestLog.IsFailed", qid))
                        if failed == nil then failed = Clean(info.isFailed) end
                        out[qid] = {
                            questID = qid,
                            title = Str(info.title),
                            level = Num(info.level),
                            zoneOrSort = zone,
                            isComplete = done,
                            isFailed = failed,
                            isTracked = isWatchedQuest(qid, i),
                            isSuperTracked = (superTracked == qid) or nil,
                            objectives = questObjectives(qid),
                        }
                    end
                end
            end
        end
    elseif Get("GetNumQuestLogEntries") then
        -- Respaldo: API legacy de registro de misiones.
        local n = Num(Call("GetNumQuestLogEntries")) or 0
        local zone = nil
        for i = 1, n do
            local title, level, _, isHeader, _, isComplete, _, qid = Call("GetQuestLogTitle", i)
            if Clean(isHeader) == true then
                zone = Str(title)
            else
                qid = Num(qid)
                if qid and qid > 0 then
                    local objectives = {}
                    local nObj = Num(Call("GetNumQuestLeaderBoards", i)) or 0
                    for j = 1, nObj do
                        local text, otype, finished, numFulfilled, numRequired = Call("GetQuestLogLeaderBoard", j, i)
                        objectives[#objectives + 1] = {
                            text = Str(text),
                            type = Str(otype),
                            finished = Clean(finished),
                            numFulfilled = Num(numFulfilled),
                            numRequired = Num(numRequired),
                        }
                    end
                    out[qid] = {
                        questID = qid,
                        title = Str(title),
                        level = Num(level),
                        zoneOrSort = zone,
                        isComplete = Clean(isComplete) and true or false,
                        isTracked = isWatchedQuest(qid, i),
                        isSuperTracked = (superTracked == qid) or nil,
                        objectives = objectives,
                    }
                end
            end
        end
    end
    return out
end

-- Objetos equipables de la bolsa con sus stats (y arma con daño y velocidad). El motor los puntua.
local BAG_EQUIP_MAX = 60

-- Anade a un objeto de la bolsa la info de equipo (solo los primeros BAG_EQUIP_MAX equipables).
local function addEquipInfo(item, count)
    local G = ns.Gear
    if count >= BAG_EQUIP_MAX or item.itemID == nil or not G or not G.BagItemInfo then return count end
    local info = G.BagItemInfo(item.itemID)
    if not info then return count end
    item.name = info.name
    item.equipLoc = info.equipLoc
    item.class = info.className
    item.subclass = info.subName
    item.itemLevel = info.itemLevel
    item.stats = info.stats
    item.weapon = info.weapon
    return count + 1
end

function ns.CollectInventory()
    local out = { items = {} }
    local freeTotal, slotsTotal = nil, 0
    local equipCount = 0
    for _, bag in ipairs(bagIndexes()) do
        local n = bagSlots(bag) or 0
        slotsTotal = slotsTotal + n
        local free = bagFree(bag)
        if free then freeTotal = (freeTotal or 0) + free end
        for slot = 1, n do
            local item = containerItem(bag, slot)
            if item then
                equipCount = addEquipInfo(item, equipCount)
                out.items[#out.items + 1] = item
            end
        end
    end
    out.freeSlots = freeTotal
    out.totalSlots = slotsTotal
    return out
end

local function itemQuality(itemID, link)
    local q = Num(Call("C_Item.GetItemQualityByID", itemID or link))
    if q == nil and link then
        local _, _, legacyQuality = Call("GetItemInfo", link)
        q = Num(legacyQuality)
    end
    return q
end

function ns.CollectEquipment()
    local out = {}
    for slot = 1, 19 do
        local link = Str(Call("GetInventoryItemLink", "player", slot))
        if link then
            local itemID = Num(Call("GetInventoryItemID", "player", slot))
            local ilvl = Num(Call("C_Item.GetDetailedItemLevelInfo", link))
            if ilvl == nil then ilvl = Num(Call("GetDetailedItemLevelInfo", link)) end
            out[slot] = {
                slotID = slot,
                itemID = itemID,
                link = link,
                itemLevel = ilvl,
                quality = itemQuality(itemID, link),
            }
        end
    end
    return out
end

-- Miembro de grupo a partir de una unidad ("player", "party1", ...).
local function unitMember(unit)
    local name = Str(Call("UnitName", unit))
    if not name then return nil end
    return {
        name = name,
        class = Str(Call("UnitClass", unit)),
        level = Num(Call("UnitLevel", unit)),
        role = Str(Call("UnitGroupRolesAssigned", unit)),
        online = Clean(Call("UnitIsConnected", unit)),
    }
end

function ns.CollectGroup()
    local inGroup = Clean(Call("IsInGroup")) == true
    local inRaid = Clean(Call("IsInRaid")) == true
    local members = {}
    if inRaid then
        local n = Num(Call("GetNumGroupMembers")) or 0
        for i = 1, n do
            local name, _, _, level, class, _, _, online, _, role, _, combatRole = Call("GetRaidRosterInfo", i)
            name = Str(name)
            if name then
                members[#members + 1] = {
                    name = name,
                    class = Str(class),
                    level = Num(level),
                    role = Str(combatRole) or Str(role),
                    online = Clean(online),
                }
            end
        end
    elseif inGroup then
        local me = unitMember("player")
        if me then members[#members + 1] = me end
        local n = Num(Call("GetNumSubgroupMembers")) or 0
        for i = 1, n do
            local m = unitMember("party" .. i)
            if m then members[#members + 1] = m end
        end
    end
    return { inGroup = inGroup, inRaid = inRaid, members = members }
end

local function spellBank()
    local e = _G.Enum and _G.Enum.SpellBookSpellBank
    return (type(e) == "table" and Num(e.Player)) or 0
end

local function collectSpellsModern()
    local out = {}
    local lines = Num(Call("C_SpellBook.GetNumSpellBookSkillLines"))
    if lines == nil then return nil end
    local bank = spellBank()
    for li = 1, lines do
        local line = Clean(Call("C_SpellBook.GetSpellBookSkillLineInfo", li))
        if type(line) == "table" then
            local offset = Num(line.itemIndexOffset) or 0
            local count = Num(line.numSpellBookItems) or 0
            for j = offset + 1, offset + count do
                local item = Clean(Call("C_SpellBook.GetSpellBookItemInfo", j, bank))
                local sid = type(item) == "table" and Num(item.spellID) or nil
                if sid then
                    local name, sub = Call("C_SpellBook.GetSpellBookItemName", j, bank)
                    out[#out + 1] = {
                        spellID = sid,
                        name = Str(name) or Str(Call("C_Spell.GetSpellName", sid)),
                        rank = Str(sub),
                    }
                end
            end
        end
    end
    return out
end

local function collectSpellsLegacy()
    local out = {}
    local tabs = Num(Call("GetNumSpellTabs")) or 0
    for t = 1, tabs do
        local _, _, offset, numSpells = Call("GetSpellTabInfo", t)
        offset, numSpells = Num(offset) or 0, Num(numSpells) or 0
        for j = offset + 1, offset + numSpells do
            local spellType, spellID = Call("GetSpellBookItemInfo", j, "spell")
            local sid = Num(spellID)
            if sid and Str(spellType) ~= "FLYOUT" then
                local name, rank = Call("GetSpellBookItemName", j, "spell")
                out[#out + 1] = { spellID = sid, name = Str(name), rank = Str(rank) }
            end
        end
    end
    return out
end

function ns.CollectSpells()
    return collectSpellsModern() or collectSpellsLegacy() or {}
end

-- Talentos: best-effort. El contenido de raw depende de la API disponible.
function ns.CollectTalents()
    local raw = {}
    local configID = Num(Call("C_ClassTalents.GetActiveConfigID"))
    if not configID then return { raw = raw } end
    raw.configID = configID
    local specIdx = Num(Call("GetSpecialization"))
    if specIdx then raw.specID = Num(Call("GetSpecializationInfo", specIdx)) end
    local cfg = Clean(Call("C_Traits.GetConfigInfo", configID))
    local trees = type(cfg) == "table" and Clean(cfg.treeIDs) or nil
    local nodes = {}
    if type(trees) == "table" then
        for _, treeID in ipairs(trees) do
            local tid = Num(treeID)
            local nodeIDs = tid and Clean(Call("C_Traits.GetTreeNodes", tid)) or nil
            if type(nodeIDs) == "table" then
                for _, nodeID in ipairs(nodeIDs) do
                    local nid = Num(nodeID)
                    local info = nid and Clean(Call("C_Traits.GetNodeInfo", configID, nid)) or nil
                    if type(info) == "table" then
                        local purchased = Num(info.ranksPurchased) or Num(info.activeRank) or 0
                        local entry = Clean(info.activeEntry)
                        if purchased > 0 and type(entry) == "table" then
                            nodes[nid] = { entryID = Num(entry.entryID), rank = purchased }
                        end
                    end
                end
            end
        end
    end
    raw.nodes = nodes
    return { raw = raw }
end

-- Profesiones. Respaldo por nombre (en ingles) si GetProfessions no existe.
local PROF_NAMES = {
    Alchemy = true, Blacksmithing = true, Enchanting = true, Engineering = true,
    Herbalism = true, Inscription = true, Jewelcrafting = true, Leatherworking = true,
    Mining = true, Skinning = true, Tailoring = true, Cooking = true, Fishing = true,
    Archaeology = true, ["First Aid"] = true,
}
local PROF_SECONDARY = { Cooking = true, Fishing = true, Archaeology = true, ["First Aid"] = false }

function ns.CollectProfessions()
    local out = {}
    if Get("GetProfessions") then
        local p1, p2, arch, fish, cook = Call("GetProfessions")
        local slots = { { p1, false }, { p2, false }, { arch, true }, { fish, true }, { cook, true } }
        for _, s in ipairs(slots) do
            local idx = Num(s[1])
            if idx then
                local name, _, rank, maxRank, _, _, skillLine = Call("GetProfessionInfo", idx)
                out[#out + 1] = {
                    name = Str(name),
                    skillLineID = Num(skillLine),
                    rank = Num(rank),
                    maxRank = Num(maxRank),
                    isSecondary = s[2],
                }
            end
        end
    else
        local n = Num(Call("GetNumSkillLines")) or 0
        for i = 1, n do
            local name, isHeader, _, rank, _, _, maxRank = Call("GetSkillLineInfo", i)
            name = Str(name)
            if name and Clean(isHeader) ~= true and PROF_NAMES[name] then
                out[#out + 1] = {
                    name = name,
                    rank = Num(rank),
                    maxRank = Num(maxRank),
                    isSecondary = PROF_SECONDARY[name] == true,
                }
            end
        end
    end
    return out
end

-- Reputaciones. current/max = progreso dentro del nivel actual de reputacion.
function ns.CollectReputations()
    local out = {}
    if Get("C_Reputation.GetFactionDataByIndex") then
        local n = Num(Call("C_Reputation.GetNumFactions")) or 0
        for i = 1, n do
            local d = Clean(Call("C_Reputation.GetFactionDataByIndex", i))
            if type(d) == "table" then
                local isHeader = Clean(d.isHeader) == true
                local hasRep = Clean(d.isHeaderWithRep) == true
                local fid = Num(d.factionID)
                if fid and (not isHeader or hasRep) then
                    local cur = Num(d.currentStanding)
                    local floorV = Num(d.currentReactionThreshold)
                    local nextV = Num(d.nextReactionThreshold)
                    out[#out + 1] = {
                        factionID = fid,
                        name = Str(d.name),
                        standing = Num(d.reaction),
                        current = cur and floorV and (cur - floorV) or nil,
                        max = nextV and floorV and (nextV - floorV) or nil,
                    }
                end
            end
        end
    elseif Get("GetFactionInfo") then
        local n = Num(Call("GetNumFactions")) or 0
        for i = 1, n do
            local name, _, standingID, barMin, barMax, barValue, _, _, isHeader, _, hasRep, _, _, fid = Call("GetFactionInfo", i)
            fid = Num(fid)
            if fid and (Clean(isHeader) ~= true or Clean(hasRep) == true) then
                local mn, mx, val = Num(barMin), Num(barMax), Num(barValue)
                out[#out + 1] = {
                    factionID = fid,
                    name = Str(name),
                    standing = Num(standingID),
                    current = val and mn and (val - mn) or nil,
                    max = mx and mn and (mx - mn) or nil,
                }
            end
        end
    end
    return out
end
