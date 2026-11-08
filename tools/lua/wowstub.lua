-- wowstub.lua: stubs de globals de WoW para cargar los modulos del addon en LuaJIT.
-- Uso: require("wowstub") desde la raiz del repo (package.path = "tools/lua/?.lua;" .. package.path).
-- Por defecto solo evita errores al cargar y al llamar a metodos de frames (no simula comportamiento).
-- Bajo demanda (install*): datos de prueba de Questie y APIs de mapa/misiones que REGISTRAN sus llamadas
-- en M.calls ({ name = "C_Map.SetUserWaypoint", args = {...} }) para que los tests puedan comprobarlas.

local M = {}

bit = require("bit")

-- Registro de llamadas (nombre + argumentos). M.reset() lo vacia.
M.calls = {}
local function rec(name, ...)
    M.calls[#M.calls + 1] = { name = name, args = { ... } }
end
M.rec = rec

function M.callsNamed(name)
    local out = {}
    for _, c in ipairs(M.calls) do
        if c.name == name then out[#out + 1] = c end
    end
    return out
end

-- Objeto generico: cualquier metodo existe, devuelve el propio objeto y queda registrado.
local function newObject()
    local obj = {}
    setmetatable(obj, {
        __index = function(_, key)
            return function(self, ...)
                rec(key, ...)
                return self
            end
        end,
    })
    return obj
end
M.newObject = newObject

if CreateFrame == nil then
    function CreateFrame(_, _, _)
        return newObject()
    end
end

if GetPhysicalScreenSize == nil then
    function GetPhysicalScreenSize()
        return 1920, 1080
    end
end

-- issecretvalue NO se define aqui: el test lo instala cuando lo necesita.

-- ---------------------------------------------------------------------------
-- Estado mutable de los tests (mapa, jugador, registro de misiones...).
-- ---------------------------------------------------------------------------
M.state = {}

-- Vacia el registro y restaura el estado por defecto (en la misma tabla: las APIs instaladas la referencian).
function M.reset()
    M.calls = {}
    for k in pairs(M.state) do M.state[k] = nil end
    local defaults = {
        now = 1700000000,       -- time()
        clock = 100,            -- GetTime()
        mapID = 1429,           -- Elwynn (uiMapID)
        x = 0.45, y = 0.67,     -- posicion del jugador (0..1)
        level = 9,
        shownMap = 1429,        -- mapa mostrado en WorldMapFrame
        superTracked = 0,
        questLog = {},          -- entradas de C_QuestLog.GetInfo (incluye cabeceras)
        objectives = {},        -- [questID] = lista de C_QuestLog.GetQuestObjectives
        complete = {},          -- [questID] = bool (IsComplete)
        flagged = {},           -- [questID] = bool (IsQuestFlaggedCompleted)
        noWaypointMaps = {},    -- [mapID] = true -> CanSetUserWaypointOnMap false
        userWaypoint = nil,
        timers = {},            -- C_Timer.After pendientes { sec, fn }
        faction = "Alliance",   -- UnitFactionGroup("player")
        locale = "enUS",        -- idioma del cliente: nombres localizados de QuestieDB (si los hay)
    }
    for k, v in pairs(defaults) do M.state[k] = v end
end
M.reset()

-- ---------------------------------------------------------------------------
-- Datos de Questie de prueba (formato real de QuestieDB / ZoneDB).
-- Coordenadas de spawns en 0..100, claves = areaId (como Questie).
-- ---------------------------------------------------------------------------
-- Areas de Questie (areaId) -> uiMapID. Incluye las capitales (1519 Ventormenta, 1637 Orgrimmar...).
local AREA_TO_UI = {
    [12] = 1429, [40] = 1436,                       -- Elwynn, Westfall
    [1519] = 84, [1537] = 87, [1657] = 89,          -- Ventormenta, Forjaz, Darnassus
    [1637] = 85, [1638] = 88, [1497] = 90,          -- Orgrimmar, Cima del Trueno, Entrañas
}
local UI_TO_AREA = {}
for area, ui in pairs(AREA_TO_UI) do UI_TO_AREA[ui] = area end
M.AREA_TO_UI = AREA_TO_UI

-- Mismas mascaras que npcFlags del juego Forever (QuestieDB corrections/enum/expansions.lua).
local NPC_FLAGS = { VENDOR = 4, FLIGHT_MASTER = 8, TRAINER = 16, INNKEEPER = 128, BANKER = 256, AUCTIONEER = 4096 }

local NPC = {
    [1000] = { name = "Marshal Dughan", npcFlags = 0, spawns = { [12] = { { 45.2, 67.1 } } } },
    [1001] = { name = "Granjero Kel", npcFlags = 0, spawns = { [12] = { { 50, 50 }, { 20, 80 } } } },
    [1002] = { name = "Maestra de vuelo Allison", subName = "Vuelo", npcFlags = NPC_FLAGS.FLIGHT_MASTER, spawns = { [12] = { { 40, 40 } } } },
    [1003] = { name = "Posadera Ana", npcFlags = NPC_FLAGS.INNKEEPER, spawns = { [12] = { { 30, 30 } } } },
    [1004] = { name = "[DND] Ocultito", npcFlags = NPC_FLAGS.VENDOR, spawns = { [12] = { { 1, 1 } } } },
    [1006] = { name = "Capitán Westfall", npcFlags = 0, spawns = { [40] = { { 25, 25 } } } },
    -- Capital de Ventormenta (Alliance). 1200 solo por subtitulo, en ingles y en espanol (loc).
    [1200] = { name = "Lirio the Herbalist", npcFlags = 0, subName = "Herbalism Trainer",
        loc = { esES = { name = "Lirio el herborista", subName = "Instructora de Herboristería" } },
        spawns = { [1519] = { { 40, 60 } } } },
    [1201] = { name = "Banquero Tharn", subName = "Banquero", npcFlags = NPC_FLAGS.BANKER, spawns = { [1519] = { { 40, 40 } } } },
    [1202] = { name = "Armero Olaf", subName = "Armero", npcFlags = 0, spawns = { [1519] = { { 55, 35 } } } },
    [1204] = { name = "Instructor de Armas Brunn", npcFlags = NPC_FLAGS.TRAINER, spawns = { [1519] = { { 60, 20 } } } },
    [1205] = { name = "Vendedor de Pan Mara", npcFlags = NPC_FLAGS.VENDOR, spawns = { [1519] = { { 62, 22 } } } },
    -- Capital de Orgrimmar (Horde): rol solo por subtitulo ("Suministros" = vendedor).
    [1203] = { name = "Cocinera Grukk", subName = "Suministros de Cocina", npcFlags = 0, spawns = { [1637] = { { 30, 30 } } } },
    [1100] = { name = "Lobo", npcFlags = 0, spawns = {
        [12] = { { 30, 40 }, { 20, 80 }, { 10, 10 }, { 50, 50 }, { 60, 60 }, { 70, 70 },
                 { 80, 80 }, { 90, 90 }, { 33, 33 }, { 44, 44 }, { 55, 55 }, { 66, 66 } },
        [40] = { { 5, 5 } },
    } },
    [1101] = { name = "Lobo alfa", npcFlags = 0, spawns = { [12] = { { 15, 15 } } } },
    [1102] = { name = "Lobo alfa menor", npcFlags = 0, spawns = {} },
}
M.NPC = NPC

local OBJECT = {
    [3000] = { name = "Buzón de Elwynn", spawns = { [12] = { { 60, 60 } } } },
    [3001] = { name = "Cofre de la granja", spawns = { [12] = { { 10, 90 } }, [999] = { { 1, 1 } } } }, -- area 999 no existe
}
local ITEM = {
    [4000] = { name = "Carta sellada" },
}

-- startedBy / finishedBy: [1] npcs, [2] objetos, [3] items.
-- objectives: [1] monstruos {id, texto}, [2] objetos, [3] items, [5] killcredit {{IdList, RootId, Texto}}.
local QUEST = {
    [2001] = { name = "Peligro en la granja", questLevel = 10, requiredLevel = 5,
        startedBy = { { 1000 }, {}, {} }, finishedBy = { { 1001 }, {}, {} },
        objectives = { { { 1100, "Lobos eliminados" } }, { { 3001, "Revisa el cofre" } } },
        preQuestSingle = { 120, 121 }, nextQuestInChain = 2002 },
    [2002] = { name = "Carta para el capitán", questLevel = 11, requiredLevel = 6,
        startedBy = { {}, {}, { 4000 } }, finishedBy = { { 1006 }, {}, {} }, objectives = {} },
    [2003] = { name = "Cosecha de Elwynn", questLevel = 9, requiredLevel = 4,
        startedBy = { { 1001 }, {}, {} }, finishedBy = { { 1001 }, {}, {} },
        objectives = { {}, { { 3000, "Buzón revisado" } } } },
    [2004] = { name = "Demasiado alto", questLevel = 40, requiredLevel = 35,
        startedBy = { { 1001 }, {}, {} }, finishedBy = { { 1001 }, {}, {} }, objectives = {} },
    [2005] = { name = "Puesto de vuelo", questLevel = 10, requiredLevel = 5,
        startedBy = { { 1002 }, {}, {} }, finishedBy = { { 1002 }, {}, {} }, objectives = {} },
    [2007] = { name = "Alfas", questLevel = 9, requiredLevel = 4,
        startedBy = { { 1001 }, {}, {} }, finishedBy = { { 1001 }, {}, {} },
        objectives = { {}, {}, {}, nil, { { { 1101, 1102 }, 1101, "Lobos alfa" } } } },
    [2010] = { name = "Westfall: entrega", questLevel = 10, requiredLevel = 5,
        startedBy = { { 1006 }, {}, {} }, finishedBy = { { 1006 }, {}, {} }, objectives = {} },
}
M.QUEST = QUEST

-- Quests que ZoneDB.GetZonesWithQuests considera de cada zona (areaId).
local ZONE_QUESTS = {
    [12] = { [2001] = true, [2002] = true, [2003] = true, [2004] = true, [2005] = true, [2007] = true },
    [40] = { [2010] = true },
}

local function fieldOf(tbl, id, key)
    local row = tbl[id]
    if type(row) ~= "table" then return nil end
    return row[key]
end

-- Instala QuestieLoader / QuestieDB / ZoneDB de prueba.
-- opts: failQuest = questID que lanza error; failNpc = npcID que lanza error;
-- noZone = sin ZoneDB; zoneFill = N PNJs extra con subtitulo "Maestro de armas" en la zona 12 (Elwynn);
-- capitalFill = N PNJs extra con subtitulo "Vendedor" en Ventormenta (1519).
function M.installQuestie(opts)
    opts = opts or {}
    local S = M.state
    local modules = {}

    -- Tabla de PNJs de esta instalacion (copia de la base + relleno para pruebas de limites).
    local npcs = {}
    for id, row in pairs(NPC) do npcs[id] = row end
    for i = 1, (opts.zoneFill or 0) do
        npcs[9000 + i] = { name = "Maestro de Armas " .. i, subName = "Maestro de armas", npcFlags = 0,
            spawns = { [12] = { { i, i } } } }
    end
    for i = 1, (opts.capitalFill or 0) do
        npcs[9500 + i] = { name = "Vendedor " .. i, subName = "Vendedor", npcFlags = 0,
            spawns = { [1519] = { { (i % 99) + 1, (i * 7 % 99) + 1 } } } }
    end

    local QuestieDB = {
        npcFlags = NPC_FLAGS,
        NPCPointers = {},
    }
    for id in pairs(npcs) do QuestieDB.NPCPointers[id] = true end
    QuestieDB.QueryQuestSingle = function(id, key)
        if opts.failQuest == id then error("boom") end
        return fieldOf(QUEST, id, key)
    end
    -- Como Questie: nombre y subtitulo en el idioma del cliente si hay traduccion; si no, ingles.
    QuestieDB.QueryNPCSingle = function(id, key)
        if opts.failNpc == id then error("boom") end
        local row = npcs[id]
        if type(row) ~= "table" then return nil end
        local loc = row.loc and row.loc[S.locale]
        if loc and loc[key] ~= nil then return loc[key] end
        return row[key]
    end
    QuestieDB.QueryObjectSingle = function(id, key) return fieldOf(OBJECT, id, key) end
    QuestieDB.QueryItemSingle = function(id, key) return fieldOf(ITEM, id, key) end
    modules.QuestieDB = QuestieDB

    local ZoneDB = {}
    ZoneDB.GetUiMapIdByAreaId = function(_, areaId) return AREA_TO_UI[areaId] end
    ZoneDB.GetAreaIdByUiMapId = function(_, uiMapId) return UI_TO_AREA[uiMapId] end
    ZoneDB.GetZonesWithQuests = function(yield)
        assert(yield == false, "GetZonesWithQuests debe llamarse sin yield")
        return ZONE_QUESTS
    end
    modules.ZoneDB = ZoneDB

    _G.QuestieLoader = {
        ImportModule = function(_, name)
            -- Como el real: crea un marcador vacio si el modulo no existe.
            if modules[name] == nil then return { private = {} } end
            return modules[name]
        end,
    }
    if opts.noZone then modules.ZoneDB = nil end
end

function M.uninstallQuestie()
    _G.QuestieLoader = nil
end

-- Instala APIs de mapa, misiones, supertrack y mundo que registran llamadas.
function M.installGameAPI()
    local S = M.state
    local MAP_NAMES = { [1429] = "Elwynn", [1436] = "Westfall" }

    _G.time = function() return S.now end
    _G.GetTime = function() return S.clock end
    _G.UnitLevel = function() return S.level end
    _G.UnitFactionGroup = function() return S.faction end
    _G.UnitName = function() return "Tester" end
    _G.GetNormalizedRealmName = function() return "Ravencrest" end
    _G.InCombatLockdown = function() return S.inCombat == true end
    _G.ReloadUI = function() rec("ReloadUI") end
    _G.C_AddOns = {
        GetAddOnMetadata = function(addon, field)
            if addon == "Questie" and field == "Version" then return "12.6.4" end
            return nil
        end,
    }
    _G.C_Timer = {
        After = function(sec, fn)
            rec("C_Timer.After", sec)
            S.timers[#S.timers + 1] = { sec = sec, fn = fn }
        end,
    }

    _G.C_Map = {
        GetBestMapForUnit = function() return S.mapID end,
        GetPlayerMapPosition = function() return { GetXY = function() return S.x, S.y end } end,
        GetMapInfo = function(id)
            if MAP_NAMES[id] then return { name = MAP_NAMES[id], mapID = id } end
            return nil
        end,
        CanSetUserWaypointOnMap = function(id)
            rec("C_Map.CanSetUserWaypointOnMap", id)
            return not S.noWaypointMaps[id]
        end,
        SetUserWaypoint = function(point)
            rec("C_Map.SetUserWaypoint", point)
            S.userWaypoint = point
        end,
        ClearUserWaypoint = function()
            rec("C_Map.ClearUserWaypoint")
            S.userWaypoint = nil
        end,
        HasUserWaypoint = function() return S.userWaypoint ~= nil end,
        GetUserWaypoint = function() return S.userWaypoint end,
    }
    _G.UiMapPoint = {
        CreateFromCoordinates = function(mapID, x, y)
            rec("UiMapPoint.CreateFromCoordinates", mapID, x, y)
            return { uiMapID = mapID, position = { x = x, y = y } }
        end,
    }
    _G.C_SuperTrack = {
        GetSuperTrackedQuestID = function() return S.superTracked end,
        SetSuperTrackedQuestID = function(id) rec("C_SuperTrack.SetSuperTrackedQuestID", id); S.superTracked = id end,
        SetSuperTrackedUserWaypoint = function(v) rec("C_SuperTrack.SetSuperTrackedUserWaypoint", v) end,
    }
    _G.C_QuestLog = {
        GetNumQuestLogEntries = function() return #S.questLog end,
        GetInfo = function(i) return S.questLog[i] end,
        GetQuestObjectives = function(qid) return S.objectives[qid] end,
        IsComplete = function(qid) return S.complete[qid] == true end,
        IsQuestFlaggedCompleted = function(qid) return S.flagged[qid] == true end,
    }

    -- Mapa del mundo: proveedores de datos como los de Blizzard (CreateFromMixins + AddDataProvider).
    _G.MapCanvasDataProviderMixin = {}
    _G.CreateFromMixins = function(...)
        local t = {}
        for i = 1, select("#", ...) do
            for k, v in pairs((select(i, ...))) do t[k] = v end
        end
        return t
    end
    local canvas = newObject()
    canvas.GetWidth = function() return 800 end
    canvas.GetHeight = function() return 600 end
    canvas.GetFrameLevel = function() return 1 end
    _G.WorldMapFrame = {
        providers = {},
        AddDataProvider = function(self, p)
            rec("WorldMapFrame:AddDataProvider", p)
            self.providers[#self.providers + 1] = p
        end,
        GetMapID = function() return S.shownMap end,
        GetCanvas = function() return canvas end,
        RefreshAllDataProviders = function() rec("WorldMapFrame:RefreshAllDataProviders") end,
    }
    _G.GameTooltip = newObject()
end

function M.uninstallGameAPI()
    for _, name in ipairs({ "time", "GetTime", "UnitLevel", "UnitName", "UnitFactionGroup", "GetNormalizedRealmName",
                            "InCombatLockdown", "ReloadUI", "C_AddOns", "C_Timer", "C_Map", "UiMapPoint",
                            "C_SuperTrack", "C_QuestLog", "MapCanvasDataProviderMixin", "CreateFromMixins",
                            "WorldMapFrame", "GameTooltip" }) do
        _G[name] = nil
    end
end

return M
