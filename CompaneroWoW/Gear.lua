-- Gear.lua: equipo, proficiencias, candidatos de equipo, mazmorras y ventana de misiones
-- (contrato GEAR / QUEST-WINDOW / DUNGEONS). Escribe en CompaneroWoWDB.characters[key]:
--   gear, proficiencies, completedQuests, questWindow, gearCandidates, dungeons.
-- Solo LEE APIs oficiales y los datos instalados (QuestieDB y ForeverDungeonJournal, opcionales). Cada parte
-- va protegida: un fallo deja {available=false, reason} y nunca un error de Lua.
--
-- Trabajo: una coroutine (pipeline) que cede cada pocos elementos. Cada tick avanza hasta 4 ms,
-- se pausa en combate y se reanuda al salir. Los objetos sin cargar se piden con
-- C_Item.RequestLoadItemDataByID y se completan al recibir ITEM_DATA_LOAD_RESULT (nunca bloquea).
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean
local QW = ns.QuestWindow

local G = {}
ns.Gear = G

local TICK_SEC = 0.05        -- intervalo entre ticks
local TICK_MS = 4            -- presupuesto de tiempo por tick
local EXPORT_THROTTLE = 10   -- segundos entre exportaciones automaticas
local LOGIN_DELAY = 5        -- Questie y los demas addons terminan de cargar tras PLAYER_LOGIN
local LOGIN_RETRY = 20
local CHECK_EVERY = 25       -- elementos entre dos cesiones de la coroutine
local MAX_SLOTS = 19
local MAX_DUNGEONS = 6
local MAX_CANDIDATES = 300
local DUNGEON_BELOW = 3      -- rango de mazmorra: se solapa con [nivel-3, nivel+5]
local DUNGEON_ABOVE = 5
local DUNGEON_MAX_BYTES = 30 * 1024
local CANDIDATE_MAX_BYTES = 100 * 1024 -- 80 KB + origen por elemento
local DUAL_WIELD_SPELL = 674

local SLOT_NAMES = {
    [1] = "HEAD", [2] = "NECK", [3] = "SHOULDER", [4] = "SHIRT", [5] = "CHEST", [6] = "WAIST",
    [7] = "LEGS", [8] = "FEET", [9] = "WRIST", [10] = "HANDS", [11] = "FINGER0", [12] = "FINGER1",
    [13] = "TRINKET0", [14] = "TRINKET1", [15] = "BACK", [16] = "MAINHAND", [17] = "SECONDARYHAND",
    [18] = "RANGED", [19] = "TABARD",
}

-- Clases de objeto (Blizzard) en ingles.
local CLASS_EN = { [2] = "Weapon", [4] = "Armor" }

-- Subclases de armadura y de arma (ids de Blizzard) con su nombre en ingles.
local ARMOR_LIST = { { 1, "Cloth" }, { 2, "Leather" }, { 3, "Mail" }, { 4, "Plate" }, { 6, "Shields" } }
local WEAPON_LIST = {
    { 0, "One-Handed Axes" }, { 1, "Two-Handed Axes" }, { 2, "Bows" }, { 3, "Guns" },
    { 4, "One-Handed Maces" }, { 5, "Two-Handed Maces" }, { 6, "Polearms" },
    { 7, "One-Handed Swords" }, { 8, "Two-Handed Swords" }, { 10, "Staves" }, { 13, "Fist Weapons" },
    { 15, "Daggers" }, { 16, "Thrown" }, { 18, "Crossbows" }, { 19, "Wands" },
}
local ARMOR_SUB_EN, WEAPON_SUB_EN = {}, {}
for _, p in ipairs(ARMOR_LIST) do ARMOR_SUB_EN[p[1]] = p[2] end
for _, p in ipairs(WEAPON_LIST) do WEAPON_SUB_EN[p[1]] = p[2] end
-- Nombres de habilidad (ingles) alternativos de una subclase.
local SKILL_ALIAS = { Plate = { "Plate Mail" } }

-- ---------------------------------------------------------------------------
-- Utilidades
-- ---------------------------------------------------------------------------

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function nowMs()
    return Num(ns.Call("debugprofilestop"))
end

local function inCombat()
    return ns.InCombat()
end

local function fullyInWorld()
    if type(ns.IsFullyInWorld) == "function" then return ns.IsFullyInWorld() end
    return false
end

local function recordFailure(err)
    ns.errorCount = (ns.errorCount or 0) + 1
    ns.lastError = string.sub(tostring(err), 1, 200)
end

local function unavailable(reason)
    return { available = false, reason = reason }
end

-- Slug del nombre de mazmorra (clave estable): "Scarlet Monastery: Library" -> "scarlet-monastery-library".
function G.Slug(name)
    local s = string.lower(name or "")
    s = string.gsub(s, "[^%w]+", "-")
    s = string.gsub(s, "^%-+", "")
    s = string.gsub(s, "%-+$", "")
    return s
end

-- Rango de nivel de FDJ: "13-20" -> 13, 20. Un numero suelto -> n, n. nil si no es un rango.
function G.ParseRange(v)
    if type(v) == "number" then return v, v end
    if type(v) ~= "string" then return nil end
    local a, b = string.match(v, "^%s*(%d+)%s*%-%s*(%d+)%s*$")
    if a then return tonumber(a), tonumber(b) end
    local n = string.match(v, "^%s*(%d+)%s*$")
    if n then return tonumber(n), tonumber(n) end
    return nil
end

-- ---------------------------------------------------------------------------
-- Personaje: nivel, faccion, raza, clase, misiones completadas
-- ---------------------------------------------------------------------------

function G.Me()
    local level = Num(Call("UnitLevel", "player")) or 1
    local faction = Str(Call("UnitFactionGroup", "player"))
    local _, _, raceID = Call("UnitRace", "player")
    local _, _, classID = Call("UnitClass", "player")
    return {
        level = level,
        faction = faction,
        raceBit = QW.RaceBit(Num(raceID)),
        classBit = QW.ClassBit(Num(classID)),
        completed = {},
    }
end

-- Lista ordenada de misiones completadas, o nil si la API no esta disponible.
function G.CompletedList()
    local raw = Clean(Call("C_QuestLog.GetAllCompletedQuestIDs"))
    if type(raw) ~= "table" then return nil end
    local out = {}
    for _, x in ipairs(raw) do
        local id = Num(x)
        if id and id > 0 then out[#out + 1] = id end
    end
    table.sort(out)
    return out
end

-- ---------------------------------------------------------------------------
-- Proficiencias: subclases que el personaje puede equipar (habilidades de la lista de habilidades)
-- ---------------------------------------------------------------------------

-- Nombres de las lineas de habilidad del personaje (sin cabeceras, con rango > 0 si lo dice).
local function skillNames()
    local names = {}
    local n = Num(Call("GetNumSkillLines")) or 0
    for i = 1, n do
        local name, isHeader, _, rank = Call("GetSkillLineInfo", i)
        name = Str(name)
        if name and Clean(isHeader) ~= true and (Num(rank) == nil or Num(rank) > 0) then
            names[name] = true
        end
    end
    return names
end

-- Tiene la habilidad: nombre ingles, localizado o alias ingles.
local function hasSkill(names, english, localized)
    if names[english] or (localized and names[localized]) then return true end
    for _, alias in ipairs(SKILL_ALIAS[english] or {}) do
        if names[alias] then return true end
    end
    return false
end

local function spellKnown(spellID)
    local v = Clean(Call("C_SpellBook.IsSpellKnown", spellID))
    if v == nil then v = Clean(Call("IsPlayerSpell", spellID)) end
    return v == true
end

-- { armor = {...}, weapons = {...}, dualWield = bool, armorLoc = {...}, weaponsLoc = {...} }.
-- Nombres en ingles (los que usa la app); los localizados vienen de GetItemSubClassInfo.
function G.CollectProficiencies()
    local names = skillNames()
    local out = { armor = {}, weapons = {}, armorLoc = {}, weaponsLoc = {}, dualWield = spellKnown(DUAL_WIELD_SPELL) }
    for _, p in ipairs(ARMOR_LIST) do
        local loc = Str(Call("GetItemSubClassInfo", 4, p[1]))
        if hasSkill(names, p[2], loc) then
            out.armor[#out.armor + 1] = p[2]
            out.armorLoc[#out.armorLoc + 1] = loc or p[2]
        end
    end
    for _, p in ipairs(WEAPON_LIST) do
        local loc = Str(Call("GetItemSubClassInfo", 2, p[1]))
        if hasSkill(names, p[2], loc) then
            out.weapons[#out.weapons + 1] = p[2]
            out.weaponsLoc[#out.weaponsLoc + 1] = loc or p[2]
        end
    end
    return out
end

local function hasName(list, name)
    for _, v in ipairs(list or {}) do
        if v == name then return true end
    end
    return false
end

-- ---------------------------------------------------------------------------
-- Objetos: lectura sin bloquear, con cache y carga asincrona
-- ---------------------------------------------------------------------------

G.itemCache = {}   -- [itemID] = info | false (no se pudo cargar)
local loading = {} -- [itemID] = true mientras se pide la carga

-- ---------------------------------------------------------------------------
-- Armas: daño y velocidad leidos del tooltip sin depender del idioma del cliente. El formato viene
-- de las cadenas globales del juego (DAMAGE_TEMPLATE, SPEED_TEMPLATE: «%s - %s Daño», «Velocidad %s»
-- en español). Si faltan, se leen los numeros: «N - N» para el daño y un decimal al final para la velocidad.
-- ---------------------------------------------------------------------------

local weaponCache = {} -- [itemID] = { min, max, speed, dps } (solo lo que se pudo leer)
local weaponTip = nil  -- tooltip oculto para la ruta de respaldo

-- Numero de una cadena del tooltip: «1.234» y «1,234» son miles; «2,5» y «2.5» son decimales.
function G.ToNumber(s)
    if type(s) ~= "string" then return nil end
    if s:match("^%d%d?%d?[%.,]%d%d%d$") or s:match("^%d%d?%d?[%.,]%d%d%d[%.,]%d%d%d$") then
        return tonumber((s:gsub("[%.,]", "")))
    end
    return tonumber((s:gsub(",", ".")))
end

-- Patron de una cadena de formato del juego. Cada %s se vuelve una captura de numero. nil si no hay cadena.
local function templatePattern(tpl)
    if type(tpl) ~= "string" or tpl == "" then return nil end
    local pat = tpl:gsub("[%^%$%(%)%%%.%[%]%*%+%-%?]", "%%%0")
    pat = pat:gsub("%%%%s", "([%%d%%.,]+)")
    if not pat:find("(", 1, true) then return nil end
    return pat
end

-- Lee min, max, velocidad y DPS de las lineas de un tooltip. globals son las cadenas del juego (_G por
-- defecto). Pura: no toca la API. Devuelve nil si no hay daño y velocidad.
function G.ParseWeaponLines(lines, globals)
    globals = globals or _G
    local dmgPat = templatePattern(globals.DAMAGE_TEMPLATE)
    local spdPat = templatePattern(globals.SPEED_TEMPLATE)
    local out = {}
    local texts = {}
    for _, line in ipairs(lines or {}) do
        if type(line) == "string" and line ~= "" then texts[#texts + 1] = line end
    end
    for _, t in ipairs(texts) do
        if not out.min and dmgPat then
            local a, b = t:match(dmgPat)
            if a and b then out.min, out.max = G.ToNumber(a), G.ToNumber(b) end
        end
        if not out.speed and spdPat then
            local s = t:match(spdPat)
            if s then out.speed = G.ToNumber(s) end
        end
    end
    -- Respaldo sin cadenas del juego: el primer par «N - N» es el daño; un decimal al final, la velocidad.
    for _, t in ipairs(texts) do
        if not out.min then
            local a, b = t:match("(%d[%d%.,]*)%s*%-%s*(%d[%d%.,]*)")
            if a and b and not t:find("%%") then out.min, out.max = G.ToNumber(a), G.ToNumber(b) end
        end
    end
    for _, t in ipairs(texts) do
        if not out.speed and not t:find("%%") then
            local s = t:match("(%d+[%.,]%d%d)%s*$")
            if s then out.speed = G.ToNumber(s) end
        end
    end
    if not (out.min and out.max and out.speed) or out.min <= 0 or out.max < out.min or out.speed <= 0 then
        return nil
    end
    out.dps = (out.min + out.max) / 2 / out.speed
    return out
end

-- Lineas del tooltip de un objeto: la API de tooltip si existe, si no el tooltip oculto.
local function weaponTooltipLines(itemID, link)
    local data = nil
    if itemID and ns.Get("C_TooltipInfo.GetItemByID") then
        data = Clean(Call("C_TooltipInfo.GetItemByID", itemID))
    end
    if type(data) == "table" and type(data.lines) == "table" then
        local out = {}
        for _, line in ipairs(data.lines) do
            local t = Str(type(line) == "table" and line.leftText or nil)
            if t then out[#out + 1] = t end
        end
        return out
    end
    if not weaponTip then
        weaponTip = Call("CreateFrame", "GameTooltip", "CompaneroItemTooltip", nil, "GameTooltipTemplate")
        if weaponTip == nil then return nil end
        ns.Method(weaponTip, "SetOwner", _G.UIParent, "ANCHOR_NONE")
    end
    ns.Method(weaponTip, "ClearLines")
    if link then
        ns.Method(weaponTip, "SetHyperlink", link)
    elseif itemID then
        ns.Method(weaponTip, "SetItemByID", itemID)
    end
    local out = {}
    for i = 1, 40 do
        local fs = _G["CompaneroItemTooltipTextLeft" .. i]
        if fs == nil then break end
        local t = Str(ns.Method(fs, "GetText"))
        if t then out[#out + 1] = t end
    end
    return out
end

-- Daño y velocidad de un arma (nil si no se pueden leer). Solo guarda lo que se leyo.
function G.WeaponStats(itemID, link)
    if itemID and weaponCache[itemID] then return weaponCache[itemID] end
    local lines = weaponTooltipLines(itemID, link)
    local w = lines and G.ParseWeaponLines(lines, _G) or nil
    if w and itemID then weaponCache[itemID] = w end
    return w
end

-- info del objeto o nil. Segundo valor: true si se pidio la carga (pendiente), false si no hay nada que esperar.
local function readItem(itemID)
    local cached = G.itemCache[itemID]
    if cached ~= nil then return cached or nil, false end
    local r = { Call("C_Item.GetItemInfo", itemID) }
    local name, link = Str(r[1]), Str(r[2])
    local stats = link and Clean(Call("C_Item.GetItemStats", link)) or nil
    if not name or not link or type(stats) ~= "table" then
        if not loading[itemID] then
            loading[itemID] = true
            ns.Call("C_Item.RequestLoadItemDataByID", itemID)
        end
        return nil, true
    end
    local info = {
        itemID = itemID, name = name, link = link, stats = stats,
        quality = Num(r[3]), itemLevel = Num(r[4]), requiredLevel = Num(r[5]),
        className = Str(r[6]), subName = Str(r[7]), equipLoc = Str(r[9]),
        classID = Num(r[12]), subclassID = Num(r[13]),
    }
    if info.classID == 2 then info.weapon = G.WeaponStats(itemID, link) end
    G.itemCache[itemID] = info
    loading[itemID] = nil
    return info, false
end

-- Info de un objeto de la bolsa si es equipo (armadura o arma con hueco), o nil. Pide la carga si falta.
function G.BagItemInfo(itemID)
    local info = readItem(itemID)
    if not info or info.equipLoc == nil or info.equipLoc == "" then return nil end
    if info.classID ~= 2 and info.classID ~= 4 then return nil end
    return info
end

local function classNameEN(info)
    return CLASS_EN[info.classID] or info.className
end

local function subclassNameEN(info)
    if info.classID == 4 then return ARMOR_SUB_EN[info.subclassID] or info.subName end
    if info.classID == 2 then return WEAPON_SUB_EN[info.subclassID] or info.subName end
    return info.subName
end

-- Equipable por la proficiencia de la clase (prof de CollectProficiencies) y el tipo de hueco.
-- info = { classID, subclassID, equipLoc }. Funcion pura.
function G.IsUsable(info, prof)
    if not info or not info.equipLoc or info.equipLoc == "" then return false end
    local cls, sub = info.classID, info.subclassID
    if cls == 4 then
        if sub == 0 then return true end -- miscelanea (capas, anillos, cuellos)
        local name = ARMOR_SUB_EN[sub]
        if not name or not hasName(prof.armor, name) then return false end
    elseif cls == 2 then
        if sub == 14 then return true end -- miscelanea de arma
        local name = WEAPON_SUB_EN[sub]
        if not name or not hasName(prof.weapons, name) then return false end
        if info.equipLoc == "INVTYPE_WEAPONOFFHAND" and not prof.dualWield then return false end
    else
        return false
    end
    return true
end

-- Objeto equipado (slots 1..19). Devuelve tabla gear y el numero de objetos pendientes de carga.
function G.CollectGear()
    local gear, pending = {}, 0
    for slot = 1, MAX_SLOTS do
        local link = Str(Call("GetInventoryItemLink", "player", slot))
        if link then
            local itemID = Num(Call("GetInventoryItemID", "player", slot))
            if itemID == nil then itemID = tonumber(string.match(link, "item:(%d+)")) end
            if itemID then
                local info, waiting = readItem(itemID)
                if info then
                    gear[slot] = {
                        slotID = slot, slot = SLOT_NAMES[slot], itemID = itemID, name = info.name,
                        link = info.link, itemLevel = info.itemLevel, requiredLevel = info.requiredLevel,
                        quality = info.quality, equipLoc = info.equipLoc, class = classNameEN(info),
                        subclass = subclassNameEN(info), stats = info.stats, weapon = info.weapon,
                    }
                elseif waiting then
                    pending = pending + 1
                end
            end
        end
    end
    return gear, pending
end

-- ---------------------------------------------------------------------------
-- Candidatos de equipo: recompensas de la ventana y botin de mazmorras
-- ---------------------------------------------------------------------------

-- Entrada de candidato, o nil si no sirve. why = "loading" si falta cargar el objeto.
-- env = { db, level, prof }. Usa QuestieDB (clase y requisito) para no pedir objetos que no son equipo.
local function candidateEntry(env, itemID, sources)
    local db = env.db
    if db then
        local qClass = Num(ns.SafeCall(db.QueryItemSingle, itemID, "class"))
        if qClass and qClass ~= 2 and qClass ~= 4 then return nil end
        local qReq = Num(ns.SafeCall(db.QueryItemSingle, itemID, "requiredLevel"))
        if qReq and qReq > env.level + QW.ABOVE then return nil end
    end
    local info, waiting = readItem(itemID)
    if not info then
        if waiting then return nil, "loading" end
        return nil
    end
    if (info.requiredLevel or 0) > env.level + QW.ABOVE then return nil end
    if not G.IsUsable(info, env.prof) then return nil end
    return {
        itemID = itemID, name = info.name, itemLevel = info.itemLevel, requiredLevel = info.requiredLevel,
        quality = info.quality, equipLoc = info.equipLoc, class = classNameEN(info),
        subclass = subclassNameEN(info), stats = info.stats, weapon = info.weapon, sources = sources,
    }
end

-- Bloque gearCandidates (coroutine). sourceMap = { [itemID] = { fuente, ... } }.
function G.CandidatesWork(env, sourceMap)
    local ids = {}
    for id in pairs(sourceMap) do ids[#ids + 1] = id end
    table.sort(ids)
    local found, pending = {}, 0
    for i, id in ipairs(ids) do
        local entry, why = candidateEntry(env, id, sourceMap[id])
        if entry then
            found[#found + 1] = entry
        elseif why == "loading" then
            pending = pending + 1
        end
        if i % CHECK_EVERY == 0 then coroutine.yield(1) end
    end
    table.sort(found, function(a, b)
        local la, lb = a.itemLevel or 0, b.itemLevel or 0
        if la ~= lb then return la > lb end
        return a.itemID < b.itemID
    end)
    local items, count, bytes = {}, 0, 0
    local srcSet = {}
    for _, e in ipairs(found) do
        if count >= MAX_CANDIDATES then break end
        local size = QW.ApproxBytes(e) + 8
        if bytes + size > CANDIDATE_MAX_BYTES then break end
        bytes = bytes + size
        count = count + 1
        items[e.itemID] = e
        for _, src in ipairs(e.sources or {}) do srcSet[src.source or "questie"] = true end
    end
    local block = { available = true, exportedAt = ns.Now(), items = items, sources = ns.Native.SourceSummary(srcSet) }
    if pending > 0 then block.pending = pending end
    return block
end

-- ---------------------------------------------------------------------------
-- Mazmorras (ForeverDungeonJournal)
-- ---------------------------------------------------------------------------

-- Bloque dungeons: mazmorras cuyo rango de nivel se solapa con [nivel-3, nivel+5], las mas cercanas primero.
-- Mazmorras sin ForeverDungeonJournal: lista del Encounter Journal de Blizzard (fuente "juego").
-- Sin rango de nivel ni botin (el Encounter Journal no los da): levelsKnown = false.
function G.BuildJournalDungeons()
    local list = ns.Native.JournalInstances()
    if list == nil then
        return {
            available = false, reason = "ForeverDungeonJournal no está instalado (opcional) y el juego no expone el diario de mazmorras",
            sources = ns.Native.SourceSummary({}),
        }
    end
    local out = {}
    for _, d in ipairs(list) do
        out[#out + 1] = {
            key = G.Slug(d.name), name = d.name, instanceID = d.instanceID, source = "juego",
            levelsKnown = false, quests = {}, bosses = {},
        }
    end
    return { available = true, exportedAt = ns.Now(), list = out, sources = ns.Native.SourceSummary({ juego = true }) }
end

function G.BuildDungeons(level)
    local fdj = _G.ForeverDungeonJournal_NS
    local db = type(fdj) == "table" and fdj.DB or nil
    if type(db) ~= "table" then return G.BuildJournalDungeons() end
    local order = {}
    if type(fdj.ORDER) == "table" then
        for _, name in ipairs(fdj.ORDER) do order[#order + 1] = name end
    else
        for name in pairs(db) do order[#order + 1] = name end
        table.sort(order)
    end
    local lo, hi = level - DUNGEON_BELOW, level + DUNGEON_ABOVE
    local picked = {}
    for _, name in ipairs(order) do
        local d = db[name]
        local minL, maxL = G.ParseRange(type(d) == "table" and d.level or nil)
        if minL and minL <= hi and maxL >= lo then
            picked[#picked + 1] = { name = name, d = d, minL = minL, maxL = maxL, dist = math.abs((minL + maxL) / 2 - level) }
        end
    end
    table.sort(picked, function(a, b)
        if a.dist ~= b.dist then return a.dist < b.dist end
        return a.name < b.name
    end)

    local list, bytes = {}, 0
    for _, p in ipairs(picked) do
        if #list >= MAX_DUNGEONS then break end
        local d = p.d
        local e = {
            key = G.Slug(p.name), name = p.name, minLevel = p.minL, maxLevel = p.maxL, zone = d.location,
            source = "fdj", levelsKnown = true,
            quests = {}, bosses = {},
        }
        if type(d.entrance) == "table" then
            e.mapID, e.x, e.y = Num(d.entrance.mapID), Num(d.entrance.x), Num(d.entrance.y)
        end
        for _, q in ipairs(type(d.quests) == "table" and d.quests or {}) do
            local id = type(q) == "table" and Num(q.id) or nil
            if id then e.quests[#e.quests + 1] = id end
        end
        for _, b in ipairs(type(d.bosses) == "table" and d.bosses or {}) do
            local boss = { name = Str(b.name) or "?", loot = {} }
            for _, l in ipairs(type(b.loot) == "table" and b.loot or {}) do
                local id = type(l) == "table" and Num(l[1]) or nil
                if id then boss.loot[#boss.loot + 1] = id end
            end
            e.bosses[#e.bosses + 1] = boss
        end
        local size = QW.ApproxBytes(e) + 8
        if bytes + size > DUNGEON_MAX_BYTES then break end
        bytes = bytes + size
        list[#list + 1] = e
    end
    return { available = true, exportedAt = ns.Now(), list = list, sources = ns.Native.SourceSummary({ fdj = true }) }
end

-- ---------------------------------------------------------------------------
-- Pipeline: una coroutine con todo el trabajo. out recibe los bloques al terminar.
-- ---------------------------------------------------------------------------

-- Fuentes de candidatos: recompensas de la ventana y botin de las mazmorras de la lista.
local function sourcesOf(rewardItems, dungeons)
    local map = {}
    local function add(itemID, src)
        local list = map[itemID]
        if not list then
            list = {}
            map[itemID] = list
        end
        list[#list + 1] = src
    end
    for _, r in ipairs(rewardItems or {}) do
        add(r.itemID, { kind = "quest", questID = r.questID, choice = r.choice or nil, source = r.source or "questie" })
    end
    if dungeons and dungeons.available == true then
        for _, d in ipairs(dungeons.list) do
            for _, b in ipairs(d.bosses) do
                for _, id in ipairs(b.loot) do
                    add(id, { kind = "dungeon", dungeonKey = d.key, boss = b.name, source = d.source or "fdj" })
                end
            end
        end
    end
    return map
end

local function pipeline(out)
    return function()
        local db, zone = nil, nil
        if type(ns.Questie) == "table" and type(ns.Questie.Modules) == "function" then
            db, zone = ns.Questie.Modules()
        end
        local me = G.Me()
        out.gear, out.gearPending = G.CollectGear()
        out.proficiencies = G.CollectProficiencies()
        out.dungeons = G.BuildDungeons(me.level)
        coroutine.yield(1)

        local completed = G.CompletedList()
        local rewardItems = {}
        if completed == nil then
            out.questWindow = unavailable("no se pudo leer las misiones completadas")
        else
            out.completedQuests = table.concat(completed, ",")
            for _, id in ipairs(completed) do me.completed[id] = true end
            me.exclusiveOf = function(id)
                if not db then return {} end
                return QW.IdList(ns.SafeCall(db.QueryQuestSingle, id, "exclusiveTo")) or {}
            end
            local mapID, px, py = ns.GetPlayerPosition()
            local res = {}
            -- Indice inverso de Questie (una vez por sesion; trozos de coroutine dentro de este pipeline).
            local reverse = nil
            if db then reverse = QW.ReverseIndexWork(db) end
            QW.Work({
                db = db, zone = zone, me = me, rewardIdx = QW.RewardIndex(), reverse = reverse,
                mapID = mapID, px = px, py = py, zoneNames = {},
            }, res)
            out.questWindow = res.block
            rewardItems = res.rewardItems or {}
            coroutine.yield(1)
        end

        local prof = out.proficiencies
        local env = { db = db, level = me.level, prof = prof }
        out.gearCandidates = G.CandidatesWork(env, sourcesOf(rewardItems, out.dungeons))
    end
end

-- ---------------------------------------------------------------------------
-- Planificacion: un solo trabajo a la vez, en ticks de C_Timer; pausa en combate y carga.
-- ---------------------------------------------------------------------------

local job = nil          -- { co, out } del trabajo en curso
local wantFull = false   -- hay una exportacion pedida
local ticker = nil
local throttleTimer = false
local lastStartAt = nil

local function stopTicker()
    if ticker and ticker.Cancel then ns.Method(ticker, "Cancel") end
    ticker = nil
end

local function busy()
    return job ~= nil or wantFull
end

-- Escribe los bloques en el personaje. Un bloque no disponible no sustituye a uno bueno.
local function writeResults(out)
    local key = ns.CharKey()
    if not key then return end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    if out.gear and (next(out.gear) ~= nil or c.gear == nil) then c.gear = out.gear end
    if out.proficiencies then c.proficiencies = out.proficiencies end
    if out.completedQuests and (out.completedQuests ~= "" or c.completedQuests == nil) then
        c.completedQuests = out.completedQuests
    end
    if out.questWindow then c.questWindow = ns.PreferAvailable(c.questWindow, out.questWindow) end
    if out.gearCandidates then c.gearCandidates = ns.PreferAvailable(c.gearCandidates, out.gearCandidates) end
    if out.dungeons then c.dungeons = ns.PreferAvailable(c.dungeons, out.dungeons) end
    G.last = out
end

-- Avanza el trabajo hasta agotar el presupuesto (nil = sin limite). Nunca dentro de combate.
local function runBudget(budgetMs)
    local t0 = nowMs()
    while job and not inCombat() do
        local ok, err = coroutine.resume(job.co)
        if not ok then
            recordFailure(err)
            job = nil
            return
        end
        if coroutine.status(job.co) == "dead" then
            local done = job
            job = nil
            ns.SafeCall(writeResults, done.out)
            return
        end
        local now = nowMs()
        if budgetMs and t0 and now and now - t0 >= budgetMs then return end
    end
end

local function startAllowed()
    local t = nowSec()
    if lastStartAt == nil or t - lastStartAt >= EXPORT_THROTTLE then return true end
    if not throttleTimer and ns.Get("C_Timer.After") then
        throttleTimer = true
        ns.Call("C_Timer.After", EXPORT_THROTTLE - (t - lastStartAt), function()
            throttleTimer = false
            G.RequestExport()
        end)
    end
    return false
end

local function startJob()
    wantFull = false
    lastStartAt = nowSec()
    local out = {}
    job = { co = coroutine.create(pipeline(out)), out = out }
end

local function tick()
    if inCombat() or not fullyInWorld() then
        stopTicker()
        return
    end
    if not job then
        if not wantFull then
            stopTicker()
            return
        end
        if not startAllowed() then
            stopTicker()
            return
        end
        startJob()
    end
    runBudget(TICK_MS)
    if not job and not wantFull then stopTicker() end
end

local function ensureTicker()
    if ticker or not busy() then return end
    if inCombat() or not fullyInWorld() then return end
    if ns.Get("C_Timer.NewTicker") then
        ticker = ns.Call("C_Timer.NewTicker", TICK_SEC, function() ns.SafeCall(tick) end)
        return
    end
    -- Sin NewTicker: cadena de C_Timer.After. El token deja de valer al parar el ticker.
    local token = {}
    ticker = token
    local function loop()
        ns.Call("C_Timer.After", TICK_SEC, function()
            if ticker ~= token then return end
            ns.SafeCall(tick)
            if ticker == token then loop() end
        end)
    end
    loop()
end

-- Pide una exportacion completa (fuera de combate, en mundo; con throttle de 10 s).
function G.RequestExport()
    wantFull = true
    ensureTicker()
end

-- Exportacion inmediata hasta el final (sin presupuesto): /compa sync, antes de recargar la interfaz.
function G.FlushNow()
    if inCombat() or not fullyInWorld() then return nil end
    if not job then startJob() end
    local guard = 0
    while job and guard < 100000 do
        runBudget(nil)
        guard = guard + 1
    end
    wantFull = false
    return G.last
end

-- ---------------------------------------------------------------------------
-- /compa estado
-- ---------------------------------------------------------------------------

function G.StatusLines()
    local db = ns.EnsureDB()
    local key = ns.CharKey()
    local c = (key and db.characters[key]) or {}
    local lines = {}

    if type(c.gear) ~= "table" then
        lines[#lines + 1] = "Equipo: sin exportar todavía (/compa sync)"
    else
        local n = 0
        for _ in pairs(c.gear) do n = n + 1 end
        lines[#lines + 1] = string.format("Equipo: %d objetos equipados", n)
    end

    local w = c.questWindow
    local counts = type(w) == "table" and QW.Counts(w) or nil
    if type(w) ~= "table" then
        lines[#lines + 1] = "Misiones: sin exportar todavía (/compa sync)"
    elseif w.available ~= true then
        lines[#lines + 1] = "Misiones: no disponible (" .. tostring(w.reason or "?") .. ")"
    else
        lines[#lines + 1] = string.format("Misiones: %d accesibles ahora, %d pronto (nivel %s, %s-%s)",
            counts.now, counts.soon, tostring(w.level), tostring(w.minLevel), tostring(w.maxLevel))
    end

    local cand = c.gearCandidates
    if type(cand) ~= "table" then
        lines[#lines + 1] = "Candidatos de equipo: sin exportar todavía (/compa sync)"
    elseif cand.available ~= true then
        lines[#lines + 1] = "Candidatos de equipo: no disponible (" .. tostring(cand.reason or "?") .. ")"
    else
        local n = 0
        for _ in pairs(cand.items or {}) do n = n + 1 end
        local extra = cand.pending and (", " .. cand.pending .. " pendientes de cargar") or ""
        lines[#lines + 1] = string.format("Candidatos de equipo: %d%s", n, extra)
    end

    local d = c.dungeons
    if type(d) ~= "table" then
        lines[#lines + 1] = "Mazmorras: sin exportar todavía (/compa sync)"
    elseif d.available ~= true then
        lines[#lines + 1] = "Mazmorras: no disponible (" .. tostring(d.reason or "?") .. ")"
    else
        lines[#lines + 1] = string.format("Mazmorras en tu rango: %d", #(d.list or {}))
    end
    return lines
end

-- ---------------------------------------------------------------------------
-- Disparadores
-- ---------------------------------------------------------------------------

-- Resultado de ITEM_DATA_LOAD_RESULT: el objeto ya esta en cache (o fallo) y se reexporta.
function G.OnItemLoaded(itemID, success)
    if type(itemID) ~= "number" then return end
    loading[itemID] = nil
    if success == false then
        G.itemCache[itemID] = false
    end
    G.RequestExport()
end

local function delayed(sec, fn)
    if ns.Get("C_Timer.After") then
        ns.Call("C_Timer.After", sec, function() ns.SafeCall(fn) end)
    else
        ns.SafeCall(fn)
    end
end

local function requestNow() G.RequestExport() end

local handlers = {
    PLAYER_LOGIN = function()
        delayed(LOGIN_DELAY, requestNow)
        delayed(LOGIN_RETRY, requestNow)
    end,
    PLAYER_LEVEL_UP = requestNow,
    QUEST_TURNED_IN = requestNow,
    PLAYER_EQUIPMENT_CHANGED = requestNow,
    SKILL_LINES_CHANGED = requestNow,
    ITEM_DATA_LOAD_RESULT = function(itemID, success) G.OnItemLoaded(itemID, success) end,
    PLAYER_ENTERING_WORLD = ensureTicker,
    PLAYER_REGEN_ENABLED = ensureTicker,
}

function G.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(G.OnEvent, event, ...)
    end)
end

-- Para pruebas: hay trabajo en curso o pedido.
function G.Busy()
    return busy()
end

-- Para pruebas: borra el estado en memoria (cache de objetos, trabajo, ticker, throttle).
function G.ResetForTests()
    job = nil
    wantFull = false
    ticker = nil
    throttleTimer = false
    lastStartAt = nil
    G.itemCache = {}
    for k in pairs(loading) do loading[k] = nil end
    G.last = nil
    QW.reverse = nil
end

-- Puntos de entrada para Core.lua (/compa sync, /compa estado).
ns.FlushGear = G.FlushNow
ns.GearStatusLines = G.StatusLines
ns.RequestGearExport = G.RequestExport

return G
