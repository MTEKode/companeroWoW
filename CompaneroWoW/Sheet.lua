-- Sheet.lua: hoja de personaje y datos de hechizos (CHARACTER SHEET + SPELL DATA CONTRACT).
-- Lee las APIs del juego y guarda characters[key].sheet (SavedVariables) y encola el tipo 12
-- del canal en vivo ("sheet", mismo JSON). Nunca lee valores secretos: ns.Num / ns.Str / ns.Clean.
-- Refresco: eventos de hoja, throttle de 2 s, diferido en combate hasta PLAYER_REGEN_ENABLED.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean
local floor = math.floor
local type, pairs, ipairs, tostring = type, pairs, ipairs, tostring

local SH = {}
ns.Sheet = SH

SH.THROTTLE = 2            -- s entre refrescos
SH.MAX_BYTES = 40000       -- tope del JSON de la hoja (contrato: < 40 KB)
SH.MAX_SPELLS = 80         -- hechizos con descripcion (contrato: max 80)
-- Escuelas de hechizo para GetSpellCritChance / GetSpellBonusDamage (1 = fisico, se omite).
SH.SCHOOLS = { 2, 3, 4, 5, 6, 7 }
-- Indice de UnitResistance por escuela (convencion de clasico; NO verificado en Forever).
SH.RESIST_INDEX = { holy = 1, fire = 2, nature = 3, frost = 4, shadow = 5, arcane = 6 }
SH.STAT_INDEX = { str = 1, agi = 2, sta = 3, int = 4, spi = 5 }
SH.WEAPON_SUBCLASSES = { 0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 13, 14, 15, 16, 18, 19, 20 }

SH.stats = { refreshes = 0, writes = 0, deferred = 0, errors = 0 }

-- Redondeo a 2 decimales (estable para el hash de cambios).
local function round2(v)
    if v == nil then return nil end
    return floor(v * 100 + 0.5) / 100
end

-- Texto de tooltip sin codigos de color ni texturas; nil si queda vacio.
local function cleanText(s)
    if not s then return nil end
    s = string.gsub(s, "|c%x%x%x%x%x%x%x%x", "")
    s = string.gsub(s, "|r", "")
    s = string.gsub(s, "|T.-|t", "")
    s = string.gsub(s, "%s+", " ")
    s = string.gsub(s, "^ ", "")
    s = string.gsub(s, " $", "")
    if s == "" then return nil end
    return s
end

-- Valor de una tabla de rating de combate si la constante CR_* existe (p. ej. "CR_EXPERTISE").
local function crBonus(globalName)
    local idx = _G[globalName]
    if type(idx) ~= "number" then return nil end
    return Num(Call("GetCombatRatingBonus", idx))
end

-- Maximo de una funcion por escuela (solo valores numericos).
local function maxOverSchools(fn)
    local best
    for _, s in ipairs(SH.SCHOOLS) do
        local v = Num(Call(fn, s))
        if v and (best == nil or v > best) then best = v end
    end
    return best
end

-- ---------------------------------------------------------------------------
-- Personaje y combate
-- ---------------------------------------------------------------------------

local function collectStats()
    local base, total = {}, {}
    for k, i in pairs(SH.STAT_INDEX) do
        local b, t = Call("UnitStat", "player", i)
        base[k] = Num(b)
        total[k] = Num(t)
    end
    return base, total
end

-- Nombre de la forma/postura activa (C_Spell.GetSpellName del hechizo de la forma). nil sin forma.
local function stanceName()
    local idx = Num(Call("GetShapeshiftForm"))
    if not idx or idx == 0 then return nil end
    local _, _, _, spellID = Call("GetShapeshiftFormInfo", idx)
    local sid = Num(spellID)
    if not sid then return nil end
    return Str((Call("C_Spell.GetSpellName", sid)))
end

local function attackPowerOf(fn)
    local b, pos, neg = Call(fn, "player")
    b, pos, neg = Num(b), Num(pos), Num(neg)
    if not b then return nil end
    return { ap = b + (pos or 0) + (neg or 0), base = b, pos = pos or 0, neg = neg or 0 }
end

-- Nombres localizados de las subclases de arma (GetItemSubClassInfo), para casar con GetSkillLineInfo.
local function subclassNames()
    local out = {}
    for _, sub in ipairs(SH.WEAPON_SUBCLASSES) do
        local name = Str((Call("GetItemSubClassInfo", 2, sub)))
        if name then out[sub] = name end
    end
    return out
end

-- Filas de habilidades por nombre: { [nombre] = { rank, max } } (sin cabeceras).
local function skillRows()
    local rows = {}
    local n = Num(Call("GetNumSkillLines")) or 0
    for i = 1, n do
        local name, isHeader, _, rank, _, _, maxRank = Call("GetSkillLineInfo", i)
        name = Str(name)
        if name and Clean(isHeader) ~= true then
            rows[name] = { rank = Num(rank), max = Num(maxRank) }
        end
    end
    return rows
end

-- Subclase de arma equipada en un hueco (16 = mano principal, 17 = secundaria). nil si no hay arma.
local function equippedWeaponSub(slot)
    local link = Str((Call("GetInventoryItemLink", "player", slot)))
    if not link then return nil end
    local _, _, _, _, _, classID, subID = Call("C_Item.GetItemInfoInstant", link)
    if Num(classID) ~= 2 then return nil end
    return Num(subID)
end

local function collectWeapons(names, rows)
    local mhSub = equippedWeaponSub(16)
    local ohSub = equippedWeaponSub(17)
    local mhSkill = mhSub and names[mhSub] and rows[names[mhSub]]
    local ohSkill = ohSub and names[ohSub] and rows[names[ohSub]]
    local skills = {}
    for _, sub in ipairs(SH.WEAPON_SUBCLASSES) do
        local name = names[sub]
        local row = name and rows[name]
        if row then skills[#skills + 1] = { name = name, rank = row.rank, max = row.max } end
    end
    return mhSkill, ohSkill, skills
end

local function collectMelee(names, rows)
    local m = {}
    local ap = attackPowerOf("UnitAttackPower")
    if ap then
        m.ap, m.apBase, m.apPos, m.apNeg = ap.ap, ap.base, ap.pos, ap.neg
    end
    m.crit = round2(Num(Call("GetCritChance")))
    m.hit = round2(Num(Call("GetHitModifier")) or crBonus("CR_HIT_MELEE"))
    m.expertise = round2(Num(Call("GetExpertisePercent")) or crBonus("CR_EXPERTISE"))
    local mn, mx, omn, omx = Call("UnitDamage", "player")
    local ms, os = Call("UnitAttackSpeed", "player")
    m.mhSpeed = round2(Num(ms))
    m.attackSpeed = m.mhSpeed
    m.mhMin, m.mhMax = Num(mn), Num(mx)
    m.ohSpeed = round2(Num(os))
    if m.ohSpeed then
        m.ohMin, m.ohMax = Num(omn), Num(omx)
    end
    local mhSkill, ohSkill = collectWeapons(names, rows)
    m.mhSkill = mhSkill and mhSkill.rank or nil
    m.ohSkill = ohSkill and ohSkill.rank or nil
    return m
end

local function collectRanged()
    local r = {}
    local ap = attackPowerOf("UnitRangedAttackPower")
    if ap then r.ap = ap.ap end
    r.crit = round2(Num(Call("GetRangedCritChance")))
    local speed, lo, hi = Call("UnitRangedDamage", "player")
    r.speed = round2(Num(speed))
    r.min, r.max = Num(lo), Num(hi)
    return r
end

local function collectDefense(rows)
    local d = {}
    d.dodge = round2(Num(Call("GetDodgeChance")))
    d.parry = round2(Num(Call("GetParryChance")))
    d.block = round2(Num(Call("GetBlockChance")))
    d.blockValue = Num(Call("GetShieldBlock"))
    -- Habilidad de defensa: por nombre localizado (global DEFENSE) en las filas de habilidades.
    local defName = Str(_G.DEFENSE)
    local row = defName and rows[defName]
    d.defenseSkill = row and row.rank or nil
    return d
end

local function collectSpellStats()
    local s = {}
    s.crit = round2(maxOverSchools("GetSpellCritChance"))
    s.power = maxOverSchools("GetSpellBonusDamage")
    s.hit = round2(Num(Call("GetSpellHitModifier")) or crBonus("CR_HIT_SPELL"))
    s.haste = round2(Num(Call("GetHaste")))
    return s
end

local function collectRegen()
    local base, casting = Call("GetManaRegen")
    return { manaBase = round2(Num(base)), manaCasting = round2(Num(casting)) }
end

local function collectResist()
    local r = {}
    for name, idx in pairs(SH.RESIST_INDEX) do
        r[name] = Num(Call("UnitResistance", "player", idx))
    end
    return r
end

-- ---------------------------------------------------------------------------
-- Talentos
-- ---------------------------------------------------------------------------

-- Nombre y spellID de una entrada de talento (definicion del arbol moderno).
local function entrySpell(configID, entryID)
    local ei = Clean(Call("C_Traits.GetEntryInfo", configID, entryID))
    local defID = type(ei) == "table" and Num(ei.definitionID) or nil
    if not defID then return nil, nil end
    local di = Clean(Call("C_Traits.GetDefinitionInfo", defID))
    if type(di) ~= "table" then return nil, nil end
    local spellID = Num(di.spellID)
    local name = Str(di.overrideName)
    if not name and spellID then name = Str((Call("C_Spell.GetSpellName", spellID))) end
    return spellID, name
end

-- Talento de un nodo moderno (C_Traits.GetNodeInfo). nil si no tiene entrada identificable.
local function talentFromNode(configID, info)
    local entry = Clean(info.activeEntry)
    local entryID = type(entry) == "table" and Num(entry.entryID) or nil
    if not entryID then
        local ids = Clean(info.entryIDs)
        if type(ids) == "table" then entryID = Num(ids[1]) end
    end
    if not entryID then return nil end
    local spellID, name = entrySpell(configID, entryID)
    if not name and not spellID then return nil end
    return {
        name = name, spellID = spellID,
        rank = Num(info.ranksPurchased) or 0, maxRank = Num(info.maxRanks),
        x = Num(info.posX), y = Num(info.posY),
    }
end

-- Fila y columna a partir de las posiciones en pixeles: indices de valores distintos.
local function layoutTab(nodes)
    local ys, xs, seenY, seenX = {}, {}, {}, {}
    for _, n in ipairs(nodes) do
        if n.y and not seenY[n.y] then seenY[n.y] = true; ys[#ys + 1] = n.y end
        if n.x and not seenX[n.x] then seenX[n.x] = true; xs[#xs + 1] = n.x end
    end
    table.sort(ys)
    table.sort(xs)
    local rowOf, colOf = {}, {}
    for i, v in ipairs(ys) do rowOf[v] = i end
    for i, v in ipairs(xs) do colOf[v] = i end
    for _, n in ipairs(nodes) do
        n.row = n.y and rowOf[n.y] or nil
        n.col = n.x and colOf[n.x] or nil
    end
end

local function treeName(configID, treeID)
    local ti = Clean(Call("C_Traits.GetTreeInfo", configID, treeID))
    return type(ti) == "table" and Str(ti.name) or nil
end

-- Arboles modernos (C_ClassTalents + C_Traits). nil si la API no existe.
local function talentTabsModern()
    local configID = Num(Call("C_ClassTalents.GetActiveConfigID"))
    if not configID then return nil end
    local cfg = Clean(Call("C_Traits.GetConfigInfo", configID))
    local trees = type(cfg) == "table" and Clean(cfg.treeIDs) or nil
    if type(trees) ~= "table" then return nil end
    local tabs = {}
    for _, treeID in ipairs(trees) do
        local tid = Num(treeID)
        local nodeIDs = tid and Clean(Call("C_Traits.GetTreeNodes", tid)) or nil
        if type(nodeIDs) == "table" then
            local nodes = {}
            for _, nodeID in ipairs(nodeIDs) do
                local nid = Num(nodeID)
                local info = nid and Clean(Call("C_Traits.GetNodeInfo", configID, nid)) or nil
                if type(info) == "table" then
                    local t = talentFromNode(configID, info)
                    if t then nodes[#nodes + 1] = t end
                end
            end
            layoutTab(nodes)
            local tab = { name = treeName(configID, tid), points = 0 }
            local list = {}
            for _, n in ipairs(nodes) do
                tab.points = tab.points + (n.rank or 0)
                list[#list + 1] = {
                    name = n.name, rank = n.rank, maxRank = n.maxRank,
                    row = n.row, col = n.col, spellID = n.spellID,
                }
            end
            if #list > 0 then tab.talents = list end
            tabs[#tabs + 1] = tab
        end
    end
    return tabs
end

-- Arboles de la API clasica (GetNumTalentTabs / GetTalentInfo). Respaldo; nil si no existe.
local function talentTabsLegacy()
    local n = Num(Call("GetNumTalentTabs"))
    if not n or n < 1 then return nil end
    local tabs = {}
    for t = 1, n do
        local _, tabName, _, _, pts = Call("GetTalentTabInfo", t)
        local tab = { name = Str(tabName), points = Num(pts) or 0 }
        local list = {}
        local cnt = Num(Call("GetNumTalents", t)) or 0
        for i = 1, cnt do
            local name, _, tier, col, rank, maxRank, _, _, spellID = Call("GetTalentInfo", t, i)
            local nm = Str(name)
            if nm then
                list[#list + 1] = {
                    name = nm, rank = Num(rank) or 0, maxRank = Num(maxRank),
                    row = Num(tier), col = Num(col), spellID = Num(spellID),
                }
            end
        end
        if #list > 0 then tab.talents = list end
        tabs[#tabs + 1] = tab
    end
    return tabs
end

local function collectTalents()
    local tabs = talentTabsModern() or talentTabsLegacy()
    if not tabs then return nil end
    local t = { tabs = tabs }
    local unspent = Num(Call("UnitCharacterPoints", "player")) or Num(Call("GetUnspentTalentPoints"))
    if unspent then t.unspent = unspent end
    return t
end

-- ---------------------------------------------------------------------------
-- Hechizos conocidos: descripcion, coste, lanzamiento, enfriamiento y alcance
-- ---------------------------------------------------------------------------

local TIP_NAME = "CompaneroSheetTooltip"
local tipFrame, tipFailed = nil, false

-- Tooltip oculto (creado una vez, nunca se muestra).
local function tipObject()
    if tipFrame ~= nil or tipFailed then return tipFrame end
    local f = ns.Call("CreateFrame", "GameTooltip", TIP_NAME, nil, "GameTooltipTemplate")
    if f == nil then
        tipFailed = true
        return nil
    end
    tipFrame = f
    ns.Method(f, "SetOwner", _G.UIParent, "ANCHOR_NONE")
    return f
end

-- Respaldo: escanea las lineas del tooltip oculto del hechizo (la primera linea es el nombre).
local function tooltipScan(spellID)
    local tip = tipObject()
    if not tip then return nil end
    ns.Method(tip, "ClearLines")
    ns.Method(tip, "SetSpellByID", spellID)
    local parts = {}
    for i = 1, 40 do
        local fs = _G[TIP_NAME .. "TextLeft" .. i]
        if fs == nil then break end
        local t = cleanText(Str(ns.Method(fs, "GetText")))
        if t then parts[#parts + 1] = t end
    end
    table.remove(parts, 1)
    if #parts == 0 then return nil end
    return table.concat(parts, " ")
end

-- Auras de beneficio del jugador: spellID de cada una. El motor lee el grito de batalla de aqui para no
-- sumar dos veces su poder de ataque.
local BUFF_MAX = 40

local function buffSpellID(i)
    if ns.Get("C_UnitAuras.GetBuffDataByIndex") then
        local aura = Clean(Call("C_UnitAuras.GetBuffDataByIndex", "player", i))
        if type(aura) ~= "table" then return false end
        return true, Num(aura.spellId)
    end
    local r = { Call("UnitBuff", "player", i) }
    if r[1] == nil then return false end
    return true, Num(r[10])
end

local function collectBuffs()
    local out = {}
    for i = 1, BUFF_MAX do
        local ok, sid = buffSpellID(i)
        if not ok then break end
        if sid then out[#out + 1] = sid end
    end
    return out
end

-- Descripcion del hechizo tal como la muestra el cliente (incluye talentos y estadisticas).
local function spellTooltip(spellID)
    local d = cleanText(Str((Call("C_Spell.GetSpellDescription", spellID))))
    if d then return d end
    return tooltipScan(spellID)
end

-- Campos de coste, lanzamiento, enfriamiento y alcance (solo los que se pueden leer).
local function spellNumbers(spellID)
    local out = {}
    local info = Clean(Call("C_Spell.GetSpellInfo", spellID))
    if type(info) == "table" then
        local ct = Num(info.castTime)
        if ct then out.castTime = round2(ct / 1000) end
        out.range = Num(info.maxRange)
    end
    local costs = Clean(Call("C_Spell.GetSpellPowerCost", spellID))
    if type(costs) == "table" then
        local pick = nil
        for _, e in ipairs(costs) do
            if type(e) == "table" and (Num(e.cost) or 0) > 0 then pick = e break end
        end
        pick = pick or costs[1]
        if type(pick) == "table" then
            out.cost = Num(pick.cost)
            out.powerType = Str(pick.name)
        end
    end
    local cdMs = Num(Call("C_Spell.GetSpellBaseCooldown", spellID))
    if cdMs then out.cooldown = round2(cdMs / 1000) end
    return out
end

-- Lista de hechizos conocidos (ya leida por CollectSpells): max SH.MAX_SPELLS, sin repetir.
local function collectSpellList()
    local raw = ns.SafeCall(ns.CollectSpells) or {}
    local out, seen = {}, {}
    for _, s in ipairs(raw) do
        if #out >= SH.MAX_SPELLS then break end
        local sid = type(s) == "table" and Num(s.spellID) or nil
        if sid and not seen[sid] then
            seen[sid] = true
            local e = { spellID = sid, name = Str(s.name), rank = Str(s.rank), known = true }
            for k, v in pairs(spellNumbers(sid)) do e[k] = v end
            e.tooltip = spellTooltip(sid)
            out[#out + 1] = e
        end
    end
    return out
end

-- ---------------------------------------------------------------------------
-- Coleccion de la hoja
-- ---------------------------------------------------------------------------

-- Hoja completa sin exportedAt. Cada campo no disponible queda fuera (nil).
function SH.Collect()
    local s = {}
    s.level = Num(Call("UnitLevel", "player"))
    local _, classFile = Call("UnitClass", "player")
    s.class = Str(classFile)
    local _, raceFile = Call("UnitRace", "player")
    s.race = Str(raceFile)
    s.stance = stanceName()
    s.base, s.total = collectStats()
    s.health = Num(Call("UnitHealth", "player"))
    s.healthMax = Num(Call("UnitHealthMax", "player"))
    s.power = Num(Call("UnitPower", "player"))
    s.powerMax = Num(Call("UnitPowerMax", "player"))
    local _, powerToken = Call("UnitPowerType", "player")
    s.powerType = Str(powerToken)
    local armorBase, armorEff = Call("UnitArmor", "player")
    s.armor = Num(armorBase)
    s.armorEffective = Num(armorEff)

    local names = subclassNames()
    local rows = skillRows()
    s.melee = collectMelee(names, rows)
    s.ranged = collectRanged()
    s.defense = collectDefense(rows)
    s.spell = collectSpellStats()
    s.regen = collectRegen()
    s.resist = collectResist()
    s.talents = collectTalents()
    local _, _, weaponSkills = collectWeapons(names, rows)
    if #weaponSkills > 0 then s.weaponSkills = weaponSkills end
    local spells = collectSpellList()
    if #spells > 0 then s.spells = spells end
    local buffs = collectBuffs()
    if #buffs > 0 then s.buffs = buffs end
    return s
end

-- Igualdad profunda de dos valores, sin codificar a JSON (en reposo no se serializa nada).
local function sameValue(a, b, depth)
    if a == b then return true end
    if type(a) ~= "table" or type(b) ~= "table" or depth > 8 then return false end
    for k, v in pairs(a) do
        if not sameValue(v, b[k], depth + 1) then return false end
    end
    for k in pairs(b) do
        if a[k] == nil then return false end
    end
    return true
end

-- true si la hoja guardada (con exportedAt) tiene el mismo contenido que la recien leida.
local function sameSheet(old, new)
    if type(old) ~= "table" then return false end
    for k, v in pairs(old) do
        if k ~= "exportedAt" and not sameValue(v, new[k], 0) then return false end
    end
    for k in pairs(new) do
        if old[k] == nil then return false end
    end
    return true
end

-- Copia de la hoja para el canal (tipo 12): recorta hechizos del final hasta que quepa en MAX_BYTES.
-- Solo se llama al serializar la activacion (no en reposo). No toca la hoja guardada.
-- Devuelve la copia, o nil si no hay hoja.
function SH.ForShare(sheet)
    if type(sheet) ~= "table" then return nil end
    local copy = {}
    for k, v in pairs(sheet) do copy[k] = v end
    if type(sheet.spells) == "table" then
        local spells = {}
        for i, s in ipairs(sheet.spells) do spells[i] = s end
        copy.spells = spells
    end
    if not ns.Json then return copy end
    local json = ns.Json.Encode(copy)
    while (json == nil or #json > SH.MAX_BYTES) and copy.spells and #copy.spells > 0 do
        copy.spells[#copy.spells] = nil
        if #copy.spells == 0 then copy.spells = nil end
        json = ns.Json.Encode(copy)
    end
    return copy
end

-- ---------------------------------------------------------------------------
-- Escritura y refresco
-- ---------------------------------------------------------------------------

local lastAt = nil       -- GetTime() del ultimo refresco
local timerPending = false
local deferred = false   -- hay un refresco pedido durante combate

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function currentRecord()
    local db = CompaneroWoWDB
    local key = ns.CharKey and ns.CharKey()
    if type(db) ~= "table" or not key then return nil, nil end
    if type(db.characters) ~= "table" then db.characters = {} end
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    return c, key
end

-- Copia de la hoja para la bandeja (Storage.lua, tipo 12): sin exportedAt y sin textos de tooltip
-- de los hechizos (esos solo viajan en la ventana de envio, nunca se guardan).
local function outboxCopy(sheet)
    local copy = {}
    for k, v in pairs(sheet) do
        if k ~= "exportedAt" then copy[k] = v end
    end
    if type(sheet.spells) == "table" then
        local spells = {}
        for i, s in ipairs(sheet.spells) do
            local e = {}
            for k, v in pairs(s) do
                if k ~= "tooltip" then e[k] = v end
            end
            spells[i] = e
        end
        copy.spells = spells
    end
    return copy
end

-- Recalcula la hoja. Escribe solo si el contenido cambio. Devuelve true si escribio.
local function refreshNow()
    SH.stats.refreshes = SH.stats.refreshes + 1
    if ns.InCombat() then
        deferred = true
        SH.stats.deferred = SH.stats.deferred + 1
        return false
    end
    if ns.IsFullyInWorld and not ns.IsFullyInWorld() then return false end
    local c = currentRecord()
    if not c then return false end
    local sheet = SH.Collect()
    if sameSheet(c.sheet, sheet) then return false end
    sheet.exportedAt = ns.Now()
    c.sheet = sheet
    -- Cambio de hoja a la bandeja: un registro por dia (clave = dia desde 1970).
    if ns.Storage then
        ns.Storage.Learn(ns.Storage.TYPE_SHEET, { "sheet", math.floor(ns.Now() / 86400) }, outboxCopy(sheet))
    end
    SH.stats.writes = SH.stats.writes + 1
    if ns.Channel and ns.Channel.Send then ns.Channel.Send(12) end
    return true
end

-- Refresco con throttle: el primero corre ya; los siguientes se funden en un temporizador.
local function requestRefresh()
    if ns.InCombat() then
        deferred = true
        SH.stats.deferred = SH.stats.deferred + 1
        return false
    end
    local t = nowSec()
    if lastAt == nil or t - lastAt >= SH.THROTTLE then
        lastAt = t
        return refreshNow()
    end
    if not timerPending and ns.Get("C_Timer.After") then
        timerPending = true
        ns.Call("C_Timer.After", SH.THROTTLE - (t - lastAt), function()
            timerPending = false
            ns.SafeCall(requestRefresh)
        end)
    end
    return false
end

-- Para /compa sync: refresco inmediato (sin throttle), fuera de combate.
function ns.ExportSheetNow()
    if ns.InCombat() then
        deferred = true
        return false
    end
    lastAt = nowSec()
    return refreshNow()
end

local EVENTS = {
    "UNIT_STATS", "PLAYER_EQUIPMENT_CHANGED", "CHARACTER_POINTS_CHANGED", "SPELLS_CHANGED",
    "PLAYER_LEVEL_UP", "UPDATE_SHAPESHIFT_FORM", "PLAYER_ENTERING_WORLD", "PLAYER_REGEN_ENABLED",
}

function SH.OnEvent(event, unit)
    if event == "PLAYER_REGEN_ENABLED" then
        if deferred then
            deferred = false
            requestRefresh()
        end
        return
    end
    if event == "UNIT_STATS" and unit ~= nil and unit ~= "player" then return end
    requestRefresh()
end

-- ---------------------------------------------------------------------------
-- /compa estado
-- ---------------------------------------------------------------------------

-- Linea: "Hoja: AP x, crit y %, talentos a/b/c, hechizos n".
function SH.StatusLine()
    local db = CompaneroWoWDB
    local key = ns.CharKey and ns.CharKey()
    local c = type(db) == "table" and key and type(db.characters) == "table" and db.characters[key] or nil
    local s = type(c) == "table" and c.sheet or nil
    if type(s) ~= "table" then return "Hoja: sin datos" end
    local m = s.melee or {}
    local ap = m.ap and tostring(m.ap) or "?"
    local crit = m.crit and string.format("%.1f", m.crit) or "?"
    local pts = {}
    for _, tab in ipairs(s.talents and s.talents.tabs or {}) do
        pts[#pts + 1] = tostring(tab.points or 0)
    end
    local talents = #pts > 0 and table.concat(pts, "/") or "?"
    return string.format("Hoja: AP %s, crit %s %%, talentos %s, hechizos %d",
        ap, crit, talents, #(s.spells or {}))
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

local frame = ns.NewFrame and ns.NewFrame("Frame")
if frame then
    for _, ev in ipairs(EVENTS) do ns.Method(frame, "RegisterEvent", ev) end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, arg1)
        ns.SafeCall(SH.OnEvent, event, arg1)
    end)
end

-- Puntos de entrada para Core.lua.
ns.SheetStatusLine = SH.StatusLine

-- Internos expuestos solo para tools/lua/test_sheet.lua.
SH.testing = { refreshNow = refreshNow, requestRefresh = requestRefresh,
               cleanText = cleanText, reset = function()
                   lastAt, timerPending, deferred = nil, false, false
                   tipFrame, tipFailed = nil, false
               end }

return SH
