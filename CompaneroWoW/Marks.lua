-- Marks.lua: /compa marca. Pone una marca en el mapa con la API oficial de Blizzard
-- (C_Map.SetUserWaypoint + C_SuperTrack, que muestra la flecha 3D y el pin de Blizzard)
-- y un pin propio "Compañero" en el mapa del mundo (MapCanvas, opcional).
-- Sin argumentos usa las APIs de Blizzard primero y Questie solo si esta instalado (Native.lua).
-- La app NUNCA dispara esto: solo el usuario lo escribe en el chat.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Get, Num, Str, Clean = ns.Call, ns.Get, ns.Num, ns.Str, ns.Clean

ns.Marks = ns.Marks or {}
local M = ns.Marks
-- Marca actual: { mapID, x, y (0..1), text, blizzard (bool) } o nil.

local function say(msg)
    if type(ns.Say) == "function" then ns.Say(msg) end
end

local function trim(s)
    return (string.gsub(s or "", "^%s*(.-)%s*$", "%1"))
end

-- Nombre de mapa para el chat: API, luego el registro de CompaneroWoWDB.maps, luego el id.
local function mapName(mapID)
    local info = Clean(Call("C_Map.GetMapInfo", mapID))
    local name = type(info) == "table" and Str(info.name)
    if name then return name end
    if type(ns.EnsureDB) == "function" then
        local db = ns.EnsureDB()
        if type(db.maps) == "table" and db.maps[mapID] then return db.maps[mapID] end
    end
    return "mapa " .. tostring(mapID)
end

local function validMap(mapID)
    if not Get("C_Map.GetMapInfo") then return false end
    return Clean(Call("C_Map.GetMapInfo", mapID)) ~= nil
end

local function persist(value)
    if type(CompaneroWoWDB) == "table" then CompaneroWoWDB.mark = value end
end

-- ---------------------------------------------------------------------------
-- Analisis de argumentos (funcion pura, testeable)
-- ---------------------------------------------------------------------------

-- "45.2" / "67,1" -> numero (coma o punto). nil si no es un numero sin signo.
local function normNum(tok)
    if type(tok) ~= "string" then return nil end
    local s = string.gsub(tok, ",", ".")
    if not string.match(s, "^%d+%.?%d*$") and not string.match(s, "^%.%d+$") then return nil end
    return tonumber(s)
end

-- Hasta n tokens separados por espacios. Devuelve la lista y el resto (texto libre, sin recortar mayusculas).
local function headTokens(s, n)
    local heads, pos = {}, 1
    for i = 1, n do
        local a, b = string.find(s, "%S+", pos)
        if not a then break end
        heads[i] = string.sub(s, a, b)
        pos = b + 1
    end
    return heads, trim(string.sub(s, pos))
end

local USAGE = "Uso: /compa marca [<mapID>] <x> <y> [texto] | /compa marca | /compa marca borrar"

-- raw: texto tras "marca". isMap(id) -> bool (opcional) confirma un mapID de 1..100.
-- Devuelve { kind = "auto" | "clear" | "coords" | "error", mapID?, x?, y? (0..1), text?, message? }.
function M.ParseArgs(raw, isMap)
    local s = trim(raw)
    if s == "" then return { kind = "auto" } end
    if string.lower(s) == "borrar" then return { kind = "clear" } end
    local lower = string.lower(s)

    local h3 = headTokens(s, 3)
    local mapN = h3[1] and string.match(h3[1], "^%d+$") and tonumber(h3[1]) or nil
    local x3, y3 = normNum(h3[2]), normNum(h3[3])
    if mapN and x3 and y3 and (mapN > 100 or (isMap and isMap(mapN))) then
        if x3 > 100 or y3 > 100 then return { kind = "error", message = "Las coordenadas van de 0 a 100." } end
        local _, text = headTokens(s, 3)
        return { kind = "coords", mapID = mapN, x = x3 / 100, y = y3 / 100, text = text }
    end

    local h2 = headTokens(s, 2)
    local x, y = normNum(h2[1]), normNum(h2[2])
    if x and y then
        if x > 100 or y > 100 then return { kind = "error", message = "Las coordenadas van de 0 a 100." } end
        local _, text = headTokens(s, 2)
        return { kind = "coords", x = x / 100, y = y / 100, text = text }
    end
    return { kind = "error", message = USAGE }
end

-- ---------------------------------------------------------------------------
-- Pin propio en el mapa del mundo (opcional: solo si la API existe)
-- ---------------------------------------------------------------------------

local PIN_SIZE = 16
local provider = nil   -- proveedor registrado en WorldMapFrame
local pinFrame = nil   -- frame del pin sobre el canvas

local function showTooltip()
    local tip = _G.GameTooltip
    if not tip or not M.mark then return end
    ns.Method(tip, "SetOwner", pinFrame, "ANCHOR_RIGHT")
    ns.Method(tip, "SetText", "Compañero: " .. (M.mark.text ~= "" and M.mark.text or mapName(M.mark.mapID)))
    ns.Method(tip, "Show")
end

local function hideTooltip()
    if _G.GameTooltip then ns.Method(_G.GameTooltip, "Hide") end
end

-- Registra el proveedor una vez. Si WorldMapFrame aun no existe (carga bajo demanda), no pasa nada.
local function ensureProvider()
    if provider then return true end
    local wm = _G.WorldMapFrame
    local mixin = _G.MapCanvasDataProviderMixin
    if not wm or type(wm.AddDataProvider) ~= "function" or type(_G.CreateFromMixins) ~= "function" or not mixin then
        return false
    end
    local ok, p = ns.Pcall(_G.CreateFromMixins, mixin)
    if not ok or type(p) ~= "table" then return false end
    p.RefreshAllData = function() M.RefreshCanvas() end
    p.RemoveAllData = function() if pinFrame then ns.Method(pinFrame, "Hide") end end
    if not ns.Pcall(wm.AddDataProvider, wm, p) then return false end
    provider = p
    return true
end

-- Coloca u oculta el pin segun la marca y el mapa que se ve.
function M.RefreshCanvas()
    local wm = _G.WorldMapFrame
    if not wm then return end
    ensureProvider()
    local shown = Num(ns.Method(wm, "GetMapID"))
    if not M.mark or shown ~= M.mark.mapID then
        if pinFrame then ns.Method(pinFrame, "Hide") end
        return
    end
    local canvas = ns.Method(wm, "GetCanvas") or wm
    if not pinFrame then
        pinFrame = ns.NewFrame("Frame", nil, canvas)
        if not pinFrame then return end
        ns.Method(pinFrame, "SetSize", PIN_SIZE, PIN_SIZE)
        local level = Num(ns.Method(canvas, "GetFrameLevel")) or 1
        ns.Method(pinFrame, "SetFrameLevel", level + 50)
        local tex = ns.Method(pinFrame, "CreateTexture", nil, "OVERLAY")
        if tex then
            ns.Method(tex, "SetAllPoints")
            ns.Method(tex, "SetTexture", "Interface\\Minimap\\Ping\\ping5")
            ns.Method(tex, "SetVertexColor", 1, 0.82, 0.1, 1) -- oro
        end
        ns.Method(pinFrame, "EnableMouse", true)
        ns.Method(pinFrame, "SetScript", "OnEnter", function() ns.SafeCall(showTooltip) end)
        ns.Method(pinFrame, "SetScript", "OnLeave", function() ns.SafeCall(hideTooltip) end)
    end
    local w = Num(ns.Method(canvas, "GetWidth"))
    local h = Num(ns.Method(canvas, "GetHeight"))
    if not w or not h or w <= 0 or h <= 0 then return end
    ns.Method(pinFrame, "ClearAllPoints")
    ns.Method(pinFrame, "SetPoint", "CENTER", canvas, "TOPLEFT", M.mark.x * w, -M.mark.y * h)
    ns.Method(pinFrame, "Show")
end

-- Refresca el pin del mapa del mundo (y pide a Blizzard que repinte sus proveedores).
function M.Refresh()
    ensureProvider()
    ns.SafeCall(M.RefreshCanvas)
    local wm = _G.WorldMapFrame
    if wm then ns.Method(wm, "RefreshAllDataProviders") end
end

-- ---------------------------------------------------------------------------
-- Marcar y borrar
-- ---------------------------------------------------------------------------

-- Marca un punto (x, y en 0..1) en mapID. Devuelve true si quedo guardada.
function M.Set(mapID, x, y, text)
    text = text or ""
    local canSet = true
    if Get("C_Map.CanSetUserWaypointOnMap") then
        canSet = Clean(Call("C_Map.CanSetUserWaypointOnMap", mapID)) == true
    end

    local blizzard = false
    if canSet and Get("C_Map.SetUserWaypoint") and Get("UiMapPoint.CreateFromCoordinates") then
        local okPoint, point = ns.Pcall(_G.UiMapPoint.CreateFromCoordinates, mapID, x, y)
        if okPoint and point then
            blizzard = ns.Pcall(_G.C_Map.SetUserWaypoint, point) == true
        end
    end
    if blizzard and Get("C_SuperTrack.SetSuperTrackedUserWaypoint") then
        ns.Pcall(_G.C_SuperTrack.SetSuperTrackedUserWaypoint, true)
    elseif not blizzard and M.mark and M.mark.blizzard and Get("C_Map.ClearUserWaypoint") then
        -- La marca anterior estaba en Blizzard y la nueva no: no dejamos una flecha vieja.
        ns.Pcall(_G.C_Map.ClearUserWaypoint)
    end

    M.mark = { mapID = mapID, x = x, y = y, text = text, blizzard = blizzard }
    persist(M.mark)
    M.Refresh()

    local msg = string.format("Marca puesta en %s (%.1f, %.1f)", mapName(mapID), x * 100, y * 100)
    if text ~= "" then msg = msg .. ": " .. text end
    if not canSet then
        msg = msg .. ". Blizzard no permite marcar en este mapa: solo queda el pin de Compañero."
    elseif not blizzard then
        msg = msg .. ". No se pudo usar el marcador de Blizzard; solo queda el pin de Compañero."
    end
    say(msg)
    return true
end

-- Quita la marca de Compañero (y la del cliente si la pusimos nosotros).
function M.Clear()
    if not M.mark then
        say("No hay ninguna marca de Compañero.")
        return false
    end
    -- Siempre: una marca anterior en Blizzard pudo quedar aunque la ultima no llegara a ponerse.
    if Get("C_Map.ClearUserWaypoint") then ns.Pcall(_G.C_Map.ClearUserWaypoint) end
    if Get("C_SuperTrack.SetSuperTrackedUserWaypoint") then
        ns.Pcall(_G.C_SuperTrack.SetSuperTrackedUserWaypoint, false)
    end
    M.mark = nil
    persist(nil)
    M.Refresh()
    say("Marca de Compañero borrada.")
    return true
end

-- Mision a marcar: la super-seguida si esta en el registro; si no, la primera del registro.
local function pickQuest(entries)
    local tracked = Num(Call("C_SuperTrack.GetSuperTrackedQuestID"))
    if tracked and tracked > 0 then
        for _, e in ipairs(entries) do
            if e.questID == tracked then return tracked, e.info end
        end
    end
    local first = entries[1]
    if first then return first.questID, first.info end
    return nil
end

-- Sin argumentos: siguiente paso de la mision, en este orden:
--   1) Blizzard: C_QuestLog.GetNextWaypoint (objetivo o entrega)            -> "según el juego"
--   2) Blizzard: area de objetivo en el mapa (C_QuestLog.GetQuestsOnMap)    -> "según el juego"
--   3) Questie, solo si esta instalado (opcional): objetivos o finalizadores -> "según Questie"
-- Sin nada de lo anterior avisa y sugiere /compa marca <x> <y> [texto].
local function markFromQuestie(data, qid, title, objs, firstOpen, mapID, px, py)
    local quest = type(data.quests) == "table" and data.quests[qid] or nil
    if type(quest) ~= "table" then return false end
    local suffix = " · según Questie"
    if firstOpen == nil then
        -- Entrega: todos los finalizadores con su nombre.
        local candidates = {}
        for _, f in ipairs(quest.finishers or {}) do
            for _, s in ipairs(f.spawns or {}) do
                candidates[#candidates + 1] = { mapID = s.mapID, x = s.x, y = s.y, name = f.name }
            end
        end
        candidates = ns.Questie.NearestFirst(candidates, mapID, px, py)
        local best = candidates[1]
        if not best then return false end
        M.Set(best.mapID, best.x, best.y, "Entregar a " .. (best.name or "PNJ") .. " (" .. title .. ")" .. suffix)
        return true
    end
    local expo = quest.objectives or {}
    if #expo ~= #objs then return false end
    local o = expo[firstOpen]
    local candidates = {}
    for _, s in ipairs((o and o.spawns) or {}) do
        candidates[#candidates + 1] = { mapID = s.mapID, x = s.x, y = s.y }
    end
    candidates = ns.Questie.NearestFirst(candidates, mapID, px, py)
    local best = candidates[1]
    if not best then return false end
    M.Set(best.mapID, best.x, best.y, ((o and o.name) or "objetivo") .. " (" .. title .. ")" .. suffix)
    return true
end

function M.MarkAuto()
    local qid, info = pickQuest(ns.Native.LogEntries())
    if not qid then
        say("No tienes misiones en el registro.")
        return false
    end
    local title = (info and Str(info.title)) or tostring(qid)
    local objs = Clean(Call("C_QuestLog.GetQuestObjectives", qid))
    if type(objs) ~= "table" then objs = {} end
    local firstOpen = nil
    for i, o in ipairs(objs) do
        if type(o) == "table" and Clean(o.finished) ~= true and firstOpen == nil then firstOpen = i end
    end
    local label = "Entregar"
    if firstOpen then
        label = Str(objs[firstOpen].text) or "Objetivo"
    end
    local tag = label .. " (" .. title .. ")"

    local mapID, px, py = ns.GetPlayerPosition()
    local wp = ns.Native.Waypoint(qid)
    if wp then
        M.Set(wp.mapID, wp.x, wp.y, tag .. " · según el juego")
        return true
    end
    if mapID and firstOpen then
        local best, bestD = nil, math.huge
        for _, p in ipairs(ns.Native.QuestPOIs(mapID)) do
            if p.questID == qid then
                local d = (px and py) and math.sqrt((p.x - px) ^ 2 + (p.y - py) ^ 2) or math.huge
                if best == nil or d < bestD then best, bestD = p, d end
            end
        end
        if best then
            M.Set(best.mapID, best.x, best.y, tag .. " · según el juego")
            return true
        end
    end

    if ns.QuestieInstalled() then
        local data = ns.BuildQuestieExport()
        local quest = data.available == true and type(data.quests) == "table" and data.quests[qid] or nil
        if type(quest) == "table" and firstOpen and #(quest.objectives or {}) ~= #objs then
            say("No puedo emparejar los objetivos de Questie con tu registro. Usa /compa marca <x> <y> [texto].")
            return false
        end
        if type(quest) == "table" and markFromQuestie(data, qid, title, objs, firstOpen, mapID, px, py) then
            return true
        end
    end
    local hint = ns.QuestieInstalled() and "" or " Questie no está instalado (es opcional)."
    say("No sé dónde está el siguiente paso de «" .. title .. "»." .. hint .. " Usa /compa marca <x> <y> [texto].")
    return false
end

-- Despacho de "/compa marca ...". rest = texto original (con mayusculas) tras "marca".
function M.Command(rest)
    local p = M.ParseArgs(rest, validMap)
    if p.kind == "clear" then return M.Clear() end
    if p.kind == "error" then
        say(p.message)
        return false
    end
    if p.kind == "auto" then return M.MarkAuto() end
    local mapID = p.mapID or ns.GetPlayerPosition()
    if not mapID then
        say("No sé en qué mapa estás. Usa /compa marca <mapID> <x> <y> [texto].")
        return false
    end
    return M.Set(mapID, p.x, p.y, p.text)
end

-- Al entrar al mundo: recupera nuestra marca guardada y engancha el mapa del mundo.
function M.Init()
    if type(CompaneroWoWDB) == "table" and type(CompaneroWoWDB.mark) == "table" then
        M.mark = CompaneroWoWDB.mark
    end
    M.Refresh()
end
