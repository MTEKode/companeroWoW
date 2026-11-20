-- Integrations.lua: exporta a CompaneroWoWDB.characters[key] los datos de otros addons, segun el
-- ADDON INTEGRATIONS CONTRACT (T17/T18):
--   training  <- WhatsTraining: WT_API (GetSpells, Status, IsReady, RegisterCallback)
--   profPlan  <- SkillUpForever.API v1 (Professions, RequiredMaterials, ShoppingList)
-- Solo LEE APIs publicas. Cada bloque va protegido: si la fuente falta o falla queda
-- {available = false, reason = "..."}; nunca un error de Lua. Exportacion fuera de combate,
-- con throttle de 10 s, al entrar, en eventos relevantes y en /compa sync.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Num, Str, Clean = ns.Call, ns.Num, ns.Str, ns.Clean

local EXPORT_THROTTLE = 10     -- segundos entre exportaciones automaticas
local LOGIN_DELAY = 3          -- espera tras PLAYER_LOGIN a que las addons fuente terminen de cargar
local LOGIN_RETRY = 15         -- SkillUpForever no avisa de que esta listo: segundo intento de login
local TRAINING_MAX = 40
local PROF_MAX = 8
local STEPS_MAX = 5
local MATERIALS_MAX = 16
local SHOP_MAX = 20
local BLOCK_MAX_BYTES = 20 * 1024

ns.Integrations = ns.Integrations or {}
local I = ns.Integrations

local lastExportAt = nil   -- tiempo (GetTime) de la ultima exportacion
local exportPending = false
local combatPending = false
local wtHooked = false
local OWNER = "CompaneroWoW" -- dueño de los callbacks de WT_API (RegisterCallback(owner, evento, fn))

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

local function unavailable(reason)
    return { available = false, reason = reason }
end

-- Tamano aproximado de un bloque (cadenas + 8 bytes por numero o clave): limite de 20 KB.
local function approxBytes(v)
    local t = type(v)
    if t == "string" then return #v + 2 end
    if t == "number" or t == "boolean" then return 8 end
    if t ~= "table" then return 0 end
    local n = 0
    for k, x in pairs(v) do n = n + approxBytes(k) + approxBytes(x) end
    return n
end

local function itemName(itemID)
    return Str(Call("C_Item.GetItemNameByID", itemID))
end

-- Bloque que ya no cabe: se devuelve como no disponible (nunca se guarda un bloque enorme).
local function fit(block)
    if approxBytes(block) > BLOCK_MAX_BYTES then return unavailable("bloque demasiado grande") end
    return block
end

-- Ejecuta un constructor de bloque protegido. Un fallo -> no disponible (error contado).
local function safeBlock(build)
    local v = ns.SafeCall(build)
    if type(v) ~= "table" then return unavailable("error interno") end
    return v
end

-- ---------------------------------------------------------------------------
-- WhatsTraining (WT_API)
-- ---------------------------------------------------------------------------

-- Estados de WT_API.Status -> categoria del contrato. Otros estados (conocido, ignorado, mascota) no se exportan.
local function statusGroups(S)
    local map = {}
    local function put(key, group)
        if type(key) == "string" then map[key] = group end
    end
    put(S.AVAILABLE, "AVAILABLE")
    put(S.WEAPON_AVAILABLE, "AVAILABLE")
    put(S.NEXTLEVEL, "NEXTLEVEL")
    put(S.WEAPON_NEXTLEVEL, "NEXTLEVEL")
    put(S.NOTLEVEL, "OTHER")
    put(S.WEAPON_NOTLEVEL, "OTHER")
    put(S.MISSINGREQS, "OTHER")
    put(S.MISSINGTALENT, "OTHER")
    return map
end

local function trainingBlock()
    if type(_G.WT_API) ~= "table" then return unavailable("WhatsTraining no está cargado") end
    if Clean(Call("WT_API.IsReady")) ~= true then return unavailable("WhatsTraining aún no está listo") end
    local S = ns.SafeCall(function() return _G.WT_API.Status end)
    if type(S) ~= "table" then return unavailable("WhatsTraining no tiene estados") end
    local records = Call("WT_API.GetSpells")
    if type(records) ~= "table" then return unavailable("WhatsTraining no devolvió hechizos") end

    local groups = statusGroups(S)
    local list = {}
    for _, r in ipairs(records) do
        local group = type(r) == "table" and groups[r.status] or nil
        local id = type(r) == "table" and Num(r.id) or nil -- los tomos (ids de texto) no se exportan
        local name = type(r) == "table" and Str(r.name) or nil
        if group and id and name then
            list[#list + 1] = {
                spellID = id, name = name, rank = Str(r.subText), level = Num(r.level),
                cost = Num(r.cost), status = group,
            }
        end
    end
    -- Disponibles primero; despues por nivel, y por id para un orden estable.
    table.sort(list, function(a, b)
        local ga = a.status == "AVAILABLE" and 0 or 1
        local gb = b.status == "AVAILABLE" and 0 or 1
        if ga ~= gb then return ga < gb end
        local la, lb = a.level or 0, b.level or 0
        if la ~= lb then return la < lb end
        return a.spellID < b.spellID
    end)
    local available = 0
    for _, e in ipairs(list) do
        if e.status == "AVAILABLE" then available = available + 1 end
    end
    local spells = {}
    for i = 1, math.min(#list, TRAINING_MAX) do spells[i] = list[i] end
    return {
        available = true, source = "WhatsTraining", exportedAt = ns.Now(),
        availableCount = available, spells = spells,
    }
end

-- ---------------------------------------------------------------------------
-- SkillUpForever (SkillUpForever.API v1)
-- ---------------------------------------------------------------------------

local function materialsFor(recipe)
    local out = {}
    local raw = Call("SkillUpForever.API.RequiredMaterials", recipe.spellID, recipe.count)
    if type(raw) ~= "table" then return out end
    for _, m in ipairs(raw) do
        if #out >= MATERIALS_MAX then break end
        local itemID = type(m) == "table" and Num(m.itemID) or nil
        local count = type(m) == "table" and Num(m.count) or nil
        if itemID and count then
            out[#out + 1] = { itemID = itemID, name = itemName(itemID), count = count }
        end
    end
    return out
end

-- Lo que falta comprar/conseguir de la profesion (solo lo que falta: missing > 0).
local function shoppingFor(skillLineID)
    local out = {}
    local lists = Call("SkillUpForever.API.ShoppingList", skillLineID)
    local list = type(lists) == "table" and lists[1] or nil
    if type(list) ~= "table" or type(list.items) ~= "table" then return out end
    for _, row in ipairs(list.items) do
        if #out >= SHOP_MAX then break end
        local itemID = type(row) == "table" and Num(row.itemID) or nil
        local missing = type(row) == "table" and Num(row.missing) or nil
        if itemID and missing and missing > 0 then
            out[#out + 1] = { itemID = itemID, name = itemName(itemID), count = missing }
        end
    end
    return out
end

local function professionEntry(p)
    local skillLineID = Num(p.skillLineID)
    local name = Str(p.name)
    if not skillLineID or not name then return nil end
    local nextSteps = {}
    for _, rec in ipairs(type(p.recipes) == "table" and p.recipes or {}) do
        if #nextSteps >= STEPS_MAX then break end
        local recipeName = type(rec) == "table" and Str(rec.name) or nil
        local spellID = type(rec) == "table" and Num(rec.spellID) or nil
        if recipeName and spellID then
            nextSteps[#nextSteps + 1] = {
                recipe = recipeName, fromSkill = Num(rec.fromRank), toSkill = Num(rec.toRank),
                materials = materialsFor({ spellID = spellID, count = Num(rec.count) }),
            }
        end
    end
    return {
        skillLineID = skillLineID, name = name, rank = Num(p.rank), maxRank = Num(p.maxRank),
        nextSteps = nextSteps, shopping = shoppingFor(skillLineID),
    }
end

local function profPlanBlock()
    if type(_G.SkillUpForever) ~= "table" or type(_G.SkillUpForever.API) ~= "table" then
        return unavailable("SkillUpForever no está cargado")
    end
    if Num(ns.SafeCall(function() return _G.SkillUpForever.API.version end)) ~= 1 then
        return unavailable("versión de la API no compatible")
    end
    local profs = Call("SkillUpForever.API.Professions")
    if type(profs) ~= "table" then return unavailable("SkillUpForever no devolvió profesiones") end
    local out = {}
    for _, p in ipairs(profs) do
        if #out >= PROF_MAX then break end
        local e = type(p) == "table" and professionEntry(p) or nil
        if e then out[#out + 1] = e end
    end
    local block = { available = true, source = "SkillUpForever", exportedAt = ns.Now(), professions = out }
    if approxBytes(block) > BLOCK_MAX_BYTES then
        -- Demasiado: se quitan las listas de compra (los pasos son lo importante) y se vuelve a medir.
        for _, e in ipairs(out) do e.shopping = {} end
    end
    return block
end

-- ---------------------------------------------------------------------------
-- Exportacion
-- ---------------------------------------------------------------------------

-- Exporta ahora (sin throttle) a la entrada del personaje. Fuera de combate. Devuelve la entrada o nil.
function ns.ExportIntegrationsNow()
    if ns.InCombat() then return nil end
    if not ns.IsFullyInWorld() then return nil end -- carga/salida: las fuentes pueden no estar listas
    local key = ns.CharKey()
    if not key then return nil end
    local db = ns.EnsureDB()
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    -- Un bloque no disponible no sustituye a uno disponible anterior (ns.PreferAvailable).
    c.training = ns.PreferAvailable(c.training, fit(safeBlock(trainingBlock)))
    c.profPlan = ns.PreferAvailable(c.profPlan, fit(safeBlock(profPlanBlock)))
    if ns.Storage then ns.Storage.SaveState(c) end
    -- Va al final: los bloques anteriores pueden haber contado errores silenciados.
    c.health = { luaErrors = ns.errorCount or 0, lastError = ns.lastError }
    lastExportAt = nowSec()
    return c
end

-- Exportacion automatica: fuera de combate, como maximo una vez cada 10 s (la de combate se difiere).
function ns.RequestIntegrationsExport()
    if ns.InCombat() then
        combatPending = true
        return false
    end
    local t = nowSec()
    if lastExportAt == nil or t - lastExportAt >= EXPORT_THROTTLE then
        return ns.ExportIntegrationsNow() ~= nil
    end
    if not exportPending and ns.Get("C_Timer.After") then
        exportPending = true
        ns.Call("C_Timer.After", EXPORT_THROTTLE - (t - lastExportAt), function()
            exportPending = false
            ns.SafeCall(ns.RequestIntegrationsExport)
        end)
    end
    return false
end

-- Lineas para /compa estado: una por integracion, segun la ultima exportacion guardada.
function ns.IntegrationsStatusLines()
    local db = ns.EnsureDB()
    local key = ns.CharKey()
    local c = (key and db.characters[key]) or {}
    local lines = {}

    local t = c.training
    if type(t) ~= "table" then
        lines[#lines + 1] = "WhatsTraining: sin exportar todavía (/compa sync)"
    elseif t.available ~= true then
        lines[#lines + 1] = "WhatsTraining: no disponible (" .. tostring(t.reason or "?") .. ")"
    else
        lines[#lines + 1] = string.format("WhatsTraining: %d hechizos disponibles", t.availableCount or 0)
    end

    local p = c.profPlan
    if type(p) ~= "table" then
        lines[#lines + 1] = "SkillUpForever: sin exportar todavía (/compa sync)"
    elseif p.available ~= true then
        lines[#lines + 1] = "SkillUpForever: no disponible (" .. tostring(p.reason or "?") .. ")"
    else
        lines[#lines + 1] = string.format("SkillUpForever: %d profesiones", #(p.professions or {}))
    end
    return lines
end

-- ---------------------------------------------------------------------------
-- Disparadores: login, eventos y callbacks de WhatsTraining
-- ---------------------------------------------------------------------------

local function delayed(sec, fn)
    if ns.Get("C_Timer.After") then
        ns.Call("C_Timer.After", sec, function() ns.SafeCall(fn) end)
    else
        ns.SafeCall(fn)
    end
end

local function hookWhatsTraining()
    if wtHooked or type(_G.WT_API) ~= "table" then return end
    local ev = ns.SafeCall(function() return _G.WT_API.Event end)
    if type(ev) ~= "table" then return end
    local onChange = function() ns.SafeCall(ns.RequestIntegrationsExport) end
    if ev.READY then Call("WT_API.RegisterCallback", OWNER, ev.READY, onChange) end
    if ev.SPELLS_CHANGED then Call("WT_API.RegisterCallback", OWNER, ev.SPELLS_CHANGED, onChange) end
    wtHooked = true
end

local function requestNow() ns.SafeCall(ns.RequestIntegrationsExport) end

local handlers = {
    PLAYER_LOGIN = function()
        hookWhatsTraining()
        delayed(LOGIN_DELAY, requestNow)
        delayed(LOGIN_RETRY, requestNow)
    end,
    PLAYER_LEVEL_UP = requestNow,
    SKILL_LINES_CHANGED = requestNow,
    TRAINER_CLOSED = requestNow,
    PLAYER_REGEN_ENABLED = function()
        if combatPending then
            combatPending = false
            requestNow()
        end
    end,
}

ns.Integrations.OnEvent = function(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

local frame = ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(I.OnEvent, event, ...)
    end)
end
