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
        tickers = {},           -- C_Timer.NewTicker activos { sec, fn, cancelled }
        npcQueries = 0,         -- llamadas a QueryNPCSingle (para medir trabajo sincrono)
        msNow = 0,              -- debugprofilestop(): reloj en ms
        msStep = 0,             -- cuanto avanza el reloj en cada llamada (0 = sin limite de tiempo)
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
        S.npcQueries = S.npcQueries + 1
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
        -- Ticker: el test lo avanza con M.tickAll(). Cancel lo quita de la lista.
        NewTicker = function(sec, fn)
            rec("C_Timer.NewTicker", sec)
            local t = { sec = sec, fn = fn, cancelled = false }
            function t:Cancel() self.cancelled = true end
            S.tickers[#S.tickers + 1] = t
            return t
        end,
    }
    _G.debugprofilestop = function()
        S.msNow = S.msNow + S.msStep
        return S.msNow
    end

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

-- Numero de tickers vivos (no cancelados).
function M.activeTickers()
    local n = 0
    for _, t in ipairs(M.state.tickers) do
        if not t.cancelled then n = n + 1 end
    end
    return n
end

-- Avanza una vez cada ticker vivo (como un tick del juego). Quita los cancelados.
function M.tickAll()
    local alive = {}
    for _, t in ipairs(M.state.tickers) do
        if not t.cancelled then alive[#alive + 1] = t end
    end
    M.state.tickers = alive
    for _, t in ipairs(alive) do
        if not t.cancelled then t.fn() end
    end
    local still = {}
    for _, t in ipairs(M.state.tickers) do
        if not t.cancelled then still[#still + 1] = t end
    end
    M.state.tickers = still
end

function M.uninstallGameAPI()
    for _, name in ipairs({ "time", "GetTime", "UnitLevel", "UnitName", "UnitFactionGroup", "GetNormalizedRealmName",
                            "InCombatLockdown", "ReloadUI", "C_AddOns", "C_Timer", "C_Map", "UiMapPoint",
                            "C_SuperTrack", "C_QuestLog", "MapCanvasDataProviderMixin", "CreateFromMixins",
                            "WorldMapFrame", "GameTooltip" }) do
        _G[name] = nil
    end
end


-- ---------------------------------------------------------------------------
-- APIs de Extras.lua: bolsas y banco (API clasica de contenedores), reputaciones, ventana de profesion
-- (trade skill y craft), monedas, hermandad, JcJ, mascota, bloqueos y piedra de hogar.
-- Opt-in: M.installExtras(). Sin llamarla (o tras uninstallExtras) ninguna de estas funciones existe.
-- M.EXTRAS guarda el estado mutable; M.EXTRAS.fail[nombre] = true hace que esa funcion lance un error.
-- ---------------------------------------------------------------------------
M.EXTRAS = nil

local EXTRAS_GLOBALS = {
    "GetContainerNumSlots", "GetContainerNumFreeSlots", "GetContainerItemInfo", "NUM_BAG_SLOTS", "NUM_BANKBAGSLOTS",
    "BANK_CONTAINER", "GetNumFactions", "GetFactionInfo", "GetMoney", "GetXPExhaustion", "GetTradeSkillLine",
    "GetNumTradeSkills", "GetTradeSkillInfo", "GetCraftDisplaySkillLine", "GetNumCrafts", "GetCraftInfo",
    "GetCurrencyListSize", "GetCurrencyListInfo", "GetNumGuildMembers", "GetGuildRosterInfo", "GetGuildInfo",
    "GetBindLocation", "GetPVPLifetimeStats", "UnitPVPRank", "GetPVPRankInfo", "GetHonorCurrency", "UnitExists",
    "UnitCreatureFamily", "GetPetActionInfo", "GetNumSavedInstances", "GetSavedInstanceInfo", "C_CurrencyInfo",
    "C_TradeSkillUI", "IsInGuild", -- IsInGuild no se instala: la pone la prueba que la necesita
}

function M.installExtras()
    local S = {
        fail = {},
        money = 123456, rested = 1500,
        -- [contenedor] = { slots = n, items = { [hueco] = { id, nombre, cantidad, calidad } } }
        containers = {
            [0] = { slots = 16, items = { [1] = { 2589, "Tela de lino", 20, 1 }, [2] = { 2589, "Tela de lino", 7, 1 },
                                          [3] = { 4306, "Tela de seda", 12, 1 }, [5] = { 6948, "Piedra de hogar", 1, 1 } } },
            [1] = { slots = 8, items = { [1] = { 1210, "Gema de sombra", 2, 2 } } },
            [-1] = { slots = 24, items = { [1] = { 4306, "Tela de seda", 40, 1 }, [2] = { 2840, "Barra de cobre", 15, 1 } } },
            [5] = { slots = 10, items = { [4] = { 7909, "Aguamarina", 3, 2 } } },
        },
        factions = {
            { name = "Alianza", header = true },
            { name = "Ventormenta", standing = 6, min = 9000, max = 21000, value = 12000, id = 72 },
            { name = "Forjaz", standing = 5, min = 3000, max = 9000, value = 4500, id = 47 },
        },
        trade = { name = "Sastrería", rank = 150, max = 225, list = {
            { "Ropa", "header" }, { "Camisa de lino blanca", "trivial" }, { "Bolsa de seda", "optimal" } } },
        craft = nil, -- { name, rank, max, list = { { nombre, subtexto, tipo } } }
        currencies = { { "JcJ", true }, { "Favor del mercader", false, 14 }, { "Distintivo de justicia", false, 3 } },
        guild = { name = "Legio VII", rank = "Oficial", rankIndex = 2, online = 2, members = {
            -- nombre, rango, indice, nivel, clase, zona, nota, nota de oficial, conectado, estado, clase (fichero)
            { "Tester-Ravencrest", "Oficial", 2, 30, "Guerrero", "Elwynn", "nota publica", "nota de oficial", true, 0, "WARRIOR" },
            { "Brutus", "Miembro", 4, 42, "Mago", "Tanaris", "alter de Pepe", "no fiable", false, 0, "MAGE" },
            { "Aela", "Miembro", 4, 18, "Cazador", "Los Baldíos", "", "", true, 0, "HUNTER" },
        } },
        hearth = "Ventormenta",
        pvp = { hk = 120, dk = 1, highest = 5, rank = 7, rankName = "Cabo", rankNumber = 3, honor = 450 },
        pet = { name = "Colmillo", level = 29, family = "Lobo",
                actions = { "PET_ACTION_ATTACK", "PET_ACTION_FOLLOW", "Mordedura", "Gruñido" } },
        lockouts = { { "Monasterio Escarlata", 111, 7200 }, { "Caducada", 112, 0 } },
    }
    M.EXTRAS = S
    -- Envuelve fn: si S.fail[name] esta puesto, lanza un error (como una API con otra forma).
    local function def(name, fn)
        _G[name] = function(...)
            if S.fail[name] then error("stub: " .. name .. " roto") end
            return fn(...)
        end
    end
    -- S.oneNil = true: los booleanos salen como en los clientes antiguos (1 / nil).
    local function flag(v)
        if S.oneNil then return v and 1 or nil end
        return v
    end
    _G.NUM_BAG_SLOTS, _G.NUM_BANKBAGSLOTS, _G.BANK_CONTAINER = 4, 6, -1
    def("GetContainerNumSlots", function(bag) return S.containers[bag] and S.containers[bag].slots or 0 end)
    def("GetContainerNumFreeSlots", function(bag)
        local c = S.containers[bag]
        if not c then return 0 end
        local used = 0
        for _ in pairs(c.items) do used = used + 1 end
        return c.slots - used
    end)
    def("GetContainerItemInfo", function(bag, slot)
        local it = S.containers[bag] and S.containers[bag].items[slot]
        if not it then return nil end
        local link = "|cffffffff|Hitem:" .. it[1] .. "::::::::30:::::|h[" .. it[2] .. "]|h|r"
        -- S.noItemID: cliente antiguo, sin el decimo valor (itemID).
        if S.noItemID then return 134000, it[3], false, it[4], false, false, link end
        return 134000, it[3], false, it[4], false, false, link, false, false, it[1]
    end)
    def("GetMoney", function() return S.money end)
    def("GetXPExhaustion", function() return S.rested end)
    def("GetNumFactions", function() return #S.factions end)
    def("GetFactionInfo", function(i)
        local f = S.factions[i]
        if not f then return nil end
        return f.name, "", f.standing, f.min, f.max, f.value, false, false, f.header == true, false, false, false, false, f.id
    end)
    def("GetTradeSkillLine", function()
        if not S.trade then return "UNKNOWN", 0, 0 end
        return S.trade.name, S.trade.rank, S.trade.max
    end)
    def("GetNumTradeSkills", function() return S.trade and #S.trade.list or 0 end)
    def("GetTradeSkillInfo", function(i)
        local r = S.trade and S.trade.list[i]
        if not r then return nil end
        return r[1], r[2], 0, true
    end)
    def("GetCraftDisplaySkillLine", function()
        if not S.craft then return nil end
        return S.craft.name, S.craft.rank, S.craft.max
    end)
    def("GetNumCrafts", function() return S.craft and #S.craft.list or 0 end)
    def("GetCraftInfo", function(i)
        local r = S.craft and S.craft.list[i]
        if not r then return nil end
        return r[1], r[2], r[3]
    end)
    def("GetCurrencyListSize", function() return #S.currencies end)
    def("GetCurrencyListInfo", function(i)
        local c = S.currencies[i]
        if not c then return nil end
        return c[1], flag(c[2]), true, false, false, c[3] or 0
    end)
    def("GetNumGuildMembers", function() return #S.guild.members, S.guild.online end)
    def("GetGuildRosterInfo", function(i)
        local m = S.guild.members[i]
        if not m then return nil end
        return m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8], flag(m[9]), m[10], m[11]
    end)
    def("GetGuildInfo", function() return S.guild.name, S.guild.rank, S.guild.rankIndex end)
    def("GetBindLocation", function() return S.hearth end)
    def("GetPVPLifetimeStats", function() return S.pvp.hk, S.pvp.dk, S.pvp.highest end)
    def("UnitPVPRank", function() return S.pvp.rank end)
    def("GetPVPRankInfo", function() return S.pvp.rankName, S.pvp.rankNumber end)
    -- S.pvp.honorExtra: segundo valor que devuelven algunos clientes (maximo, texto...).
    def("GetHonorCurrency", function()
        if S.pvp.honorExtra ~= nil then return S.pvp.honor, S.pvp.honorExtra end
        return S.pvp.honor
    end)
    def("UnitExists", function(unit)
        if unit == "pet" then return flag(S.pet ~= nil) end
        return flag(true)
    end)
    local unitName, unitLevel = _G.UnitName, _G.UnitLevel
    S.restore = { UnitName = unitName, UnitLevel = unitLevel }
    _G.UnitName = function(unit)
        if unit == "pet" then return S.pet and S.pet.name or nil end
        return unitName and unitName(unit) or nil
    end
    _G.UnitLevel = function(unit)
        if unit == "pet" then return S.pet and S.pet.level or nil end
        return unitLevel and unitLevel(unit) or nil
    end
    def("UnitCreatureFamily", function() return S.pet and S.pet.family or nil end)
    def("GetPetActionInfo", function(i)
        local a = S.pet and S.pet.actions[i]
        if not a then return nil end
        return a, "", 132000, string.sub(a, 1, 4) == "PET_"
    end)
    def("GetNumSavedInstances", function() return #S.lockouts end)
    def("GetSavedInstanceInfo", function(i)
        local l = S.lockouts[i]
        if not l then return nil end
        return l[1], l[2], l[3], 1, l[3] > 0
    end)
    return S
end

function M.uninstallExtras()
    local S = M.EXTRAS
    for _, name in ipairs(EXTRAS_GLOBALS) do _G[name] = nil end
    if S and S.restore then
        _G.UnitName, _G.UnitLevel = S.restore.UnitName, S.restore.UnitLevel
    end
    M.EXTRAS = nil
end

-- ---------------------------------------------------------------------------
-- Addons fuente de Integrations.lua: WhatsTraining (WT_API), SkillUpForever.API.
-- Son datos de prueba con la forma publica de cada API; se instalan y desinstalan por separado.
-- ---------------------------------------------------------------------------

-- Estados de WT_API.Status (valores reales de WhatsTraining/Categories.lua).
M.WT_STATUS = {
    AVAILABLE = "available", NEXTLEVEL = "nextLevel", NOTLEVEL = "notLevel", MISSINGREQS = "missingReqs",
    MISSINGTALENT = "missingTalent", IGNORED = "ignored", KNOWN = "known", KNOWN_PET = "knownPet",
    PET = "pet", WEAPON_AVAILABLE = "weaponAvailable", WEAPON_NEXTLEVEL = "weaponNextLevel",
    WEAPON_NOTLEVEL = "weaponNotLevel", WEAPON_KNOWN = "weaponKnown", WEAPON_IGNORED = "weaponIgnored",
}

-- Hechizos de prueba: 3 disponibles, 2 del siguiente nivel, 1 de otro estado, y 2 que no se exportan (conocido, ignorado).
local function defaultSpells()
    local st = M.WT_STATUS
    return {
        { id = 201, name = "Golpe heroico", subText = "Rango 2", level = 18, cost = 1200, status = st.AVAILABLE },
        { id = 202, name = "Carga", subText = nil, level = 4, cost = 50, status = st.AVAILABLE },
        { id = 203, name = "Desarmar", subText = "Rango 1", level = 10, cost = 300, status = st.AVAILABLE },
        { id = 204, name = "Mordisco", subText = nil, level = 12, cost = 0, status = st.NEXTLEVEL },
        { id = 205, name = "Tajo", subText = nil, level = 14, cost = 0, status = st.NEXTLEVEL },
        { id = 206, name = "Veneno", subText = nil, level = 30, cost = 0, status = st.NOTLEVEL },
        { id = 207, name = "Conocido", subText = nil, level = 2, cost = 0, status = st.KNOWN },
        { id = 208, name = "Ignorado", subText = nil, level = 3, cost = 0, status = st.IGNORED },
    }
end

-- Genera n hechizos disponibles (para probar el tope de 40).
function M.manySpells(n)
    local out = {}
    for i = 1, n do
        out[i] = { id = 5000 + i, name = "Hechizo " .. i, subText = "Rango 1", level = (i % 60) + 1,
                   cost = i * 10, status = M.WT_STATUS.AVAILABLE }
    end
    return out
end

-- Instala WT_API y SkillUpForever.API con datos de prueba (en M.state.addons).
-- opts.wtReady = false -> WT_API.IsReady() false; opts.wtSpells = lista de hechizos; opts.noSkillUp = true
-- omite SkillUpForever; opts.noWT = true omite WhatsTraining.
function M.installAddons(opts)
    opts = opts or {}
    local S = M.state
    S.addons = {
        wtReady = opts.wtReady ~= false,
        wtSpells = opts.wtSpells or defaultSpells(),
        wtCallbacks = {},          -- [event] = { fn, ... }
        skillCalls = 0,            -- llamadas a SkillUpForever.API.Professions
        profs = {
            { name = "Sastrería", skillLineID = 197, rank = 68, maxRank = 75, icon = 1,
              base = 68, max = 75, recipes = {
                  { spellID = 3908, name = "Túnica de lino", count = 3, fromRank = 68, toRank = 72 },
                  { spellID = 3909, name = "Bolsa de lino", count = 2, fromRank = 72, toRank = 75 },
              } },
        },
        names = { [2589] = "Tela de lino" },  -- C_Item.GetItemNameByID (ids sin nombre -> nil)
    }
    local A = S.addons

    if not opts.noWT then
        _G.WT_API = {
            version = 1,
            Status = M.WT_STATUS,
            Event = { READY = "Ready", SPELLS_CHANGED = "SpellsChanged", LEVEL_CHANGED = "LevelChanged" },
            IsReady = function() return A.wtReady end,
            GetSpells = function()
                local out = {}
                for _, r in ipairs(A.wtSpells) do
                    local c = {}
                    for k, v in pairs(r) do c[k] = v end
                    out[#out + 1] = c
                end
                return out
            end,
            RegisterCallback = function(owner, event, fn)
                rec("WT_API.RegisterCallback", owner, event)
                A.wtCallbacks[event] = A.wtCallbacks[event] or {}
                table.insert(A.wtCallbacks[event], fn)
            end,
        }
    end


    if not opts.noSkillUp then
        _G.SkillUpForever = { API = {
            version = 1,
            Professions = function()
                A.skillCalls = A.skillCalls + 1
                local out = {}
                for _, p in ipairs(A.profs) do
                    local recipes = {}
                    for _, r in ipairs(p.recipes) do
                        recipes[#recipes + 1] = { spellID = r.spellID, name = r.name, count = r.count,
                                                  fromRank = r.fromRank, toRank = r.toRank }
                    end
                    out[#out + 1] = { name = p.name, skillLineID = p.skillLineID, rank = p.base,
                                      maxRank = p.max, recipes = recipes }
                end
                return out
            end,
            RequiredMaterials = function(recipeID, crafts)
                rec("SkillUpForever.RequiredMaterials", recipeID, crafts)
                crafts = crafts or 1
                return { { itemID = 2589, count = 2 * crafts }, { itemID = 4306, count = crafts } }
            end,
            ShoppingList = function(skillLineID)
                rec("SkillUpForever.ShoppingList", skillLineID)
                return { {
                    profession = "Sastrería", skillLine = skillLineID, target = 75, total = 0, unpriced = 0,
                    items = {
                        { itemID = 2589, need = 30, have = 0, missing = 30, source = "vendor" },
                        { itemID = 4306, need = 5, have = 5, missing = 0, source = "vendor" },
                    },
                } }
            end,
        } }
    end

    _G.C_Item = _G.C_Item or {}
    _G.C_Item.GetItemNameByID = function(id) return A.names[id] end
end

-- Quita WT_API, SkillUpForever y C_Item (para probar addons ausentes).
function M.uninstallAddons()
    _G.WT_API = nil
    _G.SkillUpForever = nil
    if _G.C_Item then _G.C_Item.GetItemNameByID = nil end
    M.state.addons = nil
end

-- Dispara un evento de WT_API a sus callbacks registrados.
function M.fireWT(event, ...)
    local list = M.state.addons and M.state.addons.wtCallbacks[event] or {}
    for _, fn in ipairs(list) do fn(event, ...) end
end

-- Ejecuta (y vacia) los C_Timer.After pendientes, como si hubiera pasado su tiempo.
function M.runTimers()
    local pending = M.state.timers
    M.state.timers = {}
    for _, t in ipairs(pending) do t.fn() end
end

-- ---------------------------------------------------------------------------
-- Datos de prueba de Gear.lua / QuestWindow.lua: misiones (prerrequisitos, mascaras de raza y clase,
-- exclusivas, recompensas), objetos (uno sin cargar), ForeverDungeonJournal, habilidades y equipo.
-- Se instala y desinstala aparte (installGear / uninstallGear); M.GEAR guarda el estado mutable.
-- ---------------------------------------------------------------------------
M.RACE_ALLIANCE = 77   -- 1+4+8+64: Humano, Enano, Elfo de la noche, Gnomo
M.RACE_HORDE = 178     -- 2+16+32+128: Orco, No-muerto, Tauren, Trol

local GEAR_NPC = {
    [5001] = { name = "Sacerdote Aldo", spawns = { [12] = { { 45, 60 } } } },
    [5002] = { name = "Capitana Vega", spawns = { [40] = { { 30, 30 } } } },
}
M.GEAR_NPC = GEAR_NPC

local SUB_NAMES_ARMOR = { [0] = "Miscellaneous", [1] = "Cloth", [2] = "Leather", [3] = "Mail", [4] = "Plate", [6] = "Shields" }
local SUB_NAMES_WEAPON = {
    [0] = "One-Handed Axes", [1] = "Two-Handed Axes", [2] = "Bows", [3] = "Guns", [4] = "One-Handed Maces",
    [5] = "Two-Handed Maces", [6] = "Polearms", [7] = "One-Handed Swords", [8] = "Two-Handed Swords",
    [10] = "Staves", [13] = "Fist Weapons", [14] = "Miscellaneous", [15] = "Daggers", [16] = "Thrown",
    [18] = "Crossbows", [19] = "Wands", [20] = "Fishing Poles",
}

-- Objeto de prueba. cls 4 = Armadura, 2 = Arma, 0 = consumible (sin hueco de equipo).
local function gearItem(name, cls, sub, loc, minLevel, ilvl, extra)
    local it = {
        name = name, classID = cls, subclassID = sub, equipLoc = loc, minLevel = minLevel, ilvl = ilvl,
        quality = 2, stats = { ITEM_MOD_STAMINA_SHORT = 2 },
        className = cls == 4 and "Armor" or (cls == 2 and "Weapon" or "Consumable"),
        subName = (cls == 4 and SUB_NAMES_ARMOR[sub]) or (cls == 2 and SUB_NAMES_WEAPON[sub]) or "Other",
    }
    for k, v in pairs(extra or {}) do it[k] = v end
    return it
end

local function gearItems()
    return {
        [4101] = gearItem("Túnica de la Sabiduría", 4, 1, "INVTYPE_CHEST", 14, 22, { stats = { ITEM_MOD_INTELLECT_SHORT = 5 } }),
        [4102] = gearItem("Coraza de placas", 4, 4, "INVTYPE_CHEST", 15, 24),
        [4103] = gearItem("Varita de fuego", 2, 19, "INVTYPE_RANGEDRIGHT", 12, 18),
        [4104] = gearItem("Hacha de guerra", 2, 0, "INVTYPE_WEAPONMAINHAND", 12, 18),
        [4105] = gearItem("Bastón de sabio", 2, 10, "INVTYPE_2HWEAPON", 16, 20, { uncached = true }),
        [4106] = gearItem("Capa de viajero", 4, 0, "INVTYPE_CLOAK", 5, 8),
        [4107] = gearItem("Yelmo pesado", 4, 4, "INVTYPE_HEAD", 30, 40),
        [4108] = gearItem("Pócima de vida", 0, 0, "", 5, 0),
        [4109] = gearItem("Daga corta", 2, 15, "INVTYPE_WEAPON", 10, 12),
        [4110] = gearItem("Escudo de madera", 4, 6, "INVTYPE_SHIELD", 12, 14),
        [4111] = gearItem("Daga de mano izquierda", 2, 15, "INVTYPE_WEAPONOFFHAND", 10, 11),
        [4112] = gearItem("Maza de guerra", 2, 4, "INVTYPE_WEAPONMAINHAND", 17, 19, { uncached = true }),
        -- Recompensas por el indice inverso de Questie (item.questRewards), para misiones sin datos de FDJ.
        [4113] = gearItem("Anillo del vigía", 4, 0, "INVTYPE_FINGER", 12, 16, { questRewards = { 3003 } }),
        [4114] = gearItem("Espada de la guardia", 2, 7, "INVTYPE_WEAPONMAINHAND", 12, 16, { questRewards = { 3003 } }),
        [4115] = gearItem("Pergamino de la guardia", 0, 0, "", 12, 0, { questRewards = { 3003 } }),
        [4116] = gearItem("Yelmo de la Corte", 4, 4, "INVTYPE_HEAD", 30, 40, { questRewards = { 3003 } }),
        [4117] = gearItem("Capa de la guardia", 4, 0, "INVTYPE_CLOAK", 22, 24, { questRewards = { 3013 } }),
        -- Sin requiredLevel en la ficha de Questie: se conserva en la ventana y el filtro mira GetItemInfo.
        [4118] = gearItem("Medalla sin ficha", 4, 0, "INVTYPE_TRINKET", nil, 10, { questRewards = { 3012 } }),
    }
end

-- Misiones base (Elwynn, area 12). Por defecto: inicio y fin en el PNJ 5001.
local function baseQuests()
    local q = {
        [3001] = { name = "Básica", requiredLevel = 12, questLevel = 15 },
        [3002] = { name = "Encadenada", requiredLevel = 13, questLevel = 16, preQuestSingle = { 3001 } },
        [3003] = { name = "Grupo", requiredLevel = 14, questLevel = 17, preQuestGroup = { 3001, 3100 } },
        [3004] = { name = "Alternativa", requiredLevel = 15, questLevel = 18, preQuestSingle = { 3100, 3101 } },
        [3005] = { name = "Solo guerreros", requiredLevel = 15, questLevel = 18, requiredClasses = 1 },
        [3006] = { name = "Solo Alianza", requiredLevel = 15, questLevel = 17, requiredRaces = 77 },
        [3007] = { name = "Solo Horda", requiredLevel = 15, questLevel = 17, requiredRaces = 178 },
        [3008] = { name = "Nivel alto", requiredLevel = 30, questLevel = 32 },
        [3009] = { name = "Muy baja", requiredLevel = 1, questLevel = 2 },
        [3010] = { name = "Exclusiva", requiredLevel = 15, questLevel = 18, exclusiveTo = { 3011 } },
        [3011] = { name = "Ya hecha", requiredLevel = 15, questLevel = 18 },
        [3012] = { name = "Prerrequisito inexistente", requiredLevel = 16, questLevel = 18, preQuestSingle = { 3200 } },
        [3013] = { name = "Nivel justo", requiredLevel = 22, questLevel = 19 },
        [3014] = { name = "Sin inicio", requiredLevel = 15, questLevel = 18, startedBy = { {}, {}, {} } },
    }
    for id, row in pairs(q) do
        row.startedBy = row.startedBy or { { 5001 }, {}, {} }
        row.finishedBy = row.finishedBy or { { 5001 }, {}, {} }
        row.id = id
    end
    return q
end

-- Estado de los datos de prueba. Se rehace en cada installGear.
M.GEAR = nil

-- opts: bulkQuests = N misiones extra usables (tope de 400); bulkItems = N objetos de cofre (tope de 300);
-- bulkDungeons = N mazmorras extra en el rango (tope de 6).
function M.installGear(opts)
    opts = opts or {}
    local S = M.state
    S.level = 19
    S.faction = "Alliance"
    S.inCombat = false
    local quests = baseQuests()
    local items = gearItems()
    local G = {
        quests = quests, items = items, loaded = {}, requested = {}, infoCalls = {},
        equipped = { [5] = 4101, [15] = 4106, [16] = 4109 },
        skills = { { "Armas", true }, { "Cloth", false }, { "Daggers", false }, { "Staves", false },
                   { "One-Handed Maces", false }, { "Wands", false } },
        dualWield = false, race = { id = 1, name = "Human" }, class = { id = 5, name = "Priest", token = "PRIEST" },
    }
    M.GEAR = G
    for id, it in pairs(items) do
        if not it.uncached then G.loaded[id] = true end
    end
    -- Misiones masivas usables (nivel 15-19, requisito 10): el tope de 400 debe cortar.
    for i = 1, (opts.bulkQuests or 0) do
        quests[6000 + i] = { name = "Relleno " .. i, requiredLevel = 10, questLevel = 15 + (i % 5),
            startedBy = { { 5001 }, {}, {} }, finishedBy = { { 5001 }, {}, {} } }
    end
    -- Objetos masivos de cofre (tope de 300 candidatos).
    local bulkLoot = {}
    for i = 1, (opts.bulkItems or 0) do
        local id = 7000 + i
        items[id] = gearItem("Tela masiva " .. i, 4, 1, "INVTYPE_CHEST", 10, 15 + (i % 10))
        G.loaded[id] = true
        bulkLoot[#bulkLoot + 1] = { id, "Tela masiva " .. i, "Chest, Cloth", 3 }
    end

    local function loot(list)
        local out = {}
        for _, id in ipairs(list) do out[#out + 1] = { id, items[id].name, "x", 3 } end
        return out
    end

    local dungeonDB = {
        ["Mina Prueba"] = {
            level = "15-24", location = "Bosque de Elwynn", description = "Prueba", icon = "x",
            entrance = { mapID = 1429, x = 0.3, y = 0.4, label = "Entrada", detail = "d", markerType = "dungeon" },
            quests = {
                { id = 3001, name = "Básica", rewardItems = { { 4101, "Túnica", 1 }, { 4102, "Coraza", 1 } }, rewardChoice = true },
                { id = 3002, name = "Encadenada", rewardItems = { { 4103, "Varita", 1 } } },
            },
            bosses = {
                { name = "Jefe A", loot = loot({ 4105, 4106 }) },
                { name = "Jefe B", loot = loot({ 4112, 4107, 4104 }) },
            },
        },
        ["Cueva Baja"] = {
            level = "13-18", location = "Bosque de Elwynn", description = "Prueba", icon = "x",
            quests = {},
            bosses = { { name = "Guardián", loot = loot({ 4109, 4110, 4111, 4108 }) } },
        },
        ["Cripta Alta"] = {
            level = "30-40", location = "Lejos", description = "Prueba", icon = "x", quests = {},
            bosses = { { name = "Lejano", loot = loot({ 4107 }) } },
        },
    }
    local order = { "Mina Prueba", "Cueva Baja", "Cripta Alta" }
    for i = 1, (opts.bulkDungeons or 0) do
        local name = "Masiva " .. i
        dungeonDB[name] = {
            level = "18-22", location = "Lugar " .. i, description = "Prueba", icon = "x", quests = {},
            bosses = { { name = "Jefe " .. i, loot = (i == 1) and bulkLoot or {} } },
        }
        order[#order + 1] = name
    end
    G.dungeonDB, G.order = dungeonDB, order
    _G.ForeverDungeonJournal_NS = { DB = dungeonDB, ORDER = order }

    -- QuestieDB y ZoneDB de prueba (solo misiones de G.quests).
    local QDB = { npcFlags = NPC_FLAGS, NPCPointers = {} }
    QDB.QueryQuestSingle = function(id, key)
        local q = G.quests[id]
        if type(q) ~= "table" then return nil end
        return q[key]
    end
    QDB.QueryNPCSingle = function(id, key)
        local n = GEAR_NPC[id]
        if type(n) ~= "table" then return nil end
        return n[key]
    end
    QDB.QueryObjectSingle = function() return nil end
    -- ItemPointers: todos los objetos (como QuestieDB.ItemPointers, hashmap id -> true).
    QDB.ItemPointers = {}
    for id in pairs(items) do QDB.ItemPointers[id] = true end
    QDB.QueryItemSingle = function(id, key)
        local it = G.items[id]
        if key == "questRewards" then G.questRewardReads = (G.questRewardReads or 0) + 1 end
        if type(it) ~= "table" then return nil end
        if key == "class" then return it.classID end
        if key == "subClass" then return it.subclassID end
        if key == "requiredLevel" then return it.minLevel end
        if key == "name" then return it.name end
        if key == "questRewards" then return it.questRewards end
        return nil
    end
    local ZDB = {}
    ZDB.GetUiMapIdByAreaId = function(_, areaId) return AREA_TO_UI[areaId] end
    ZDB.GetAreaIdByUiMapId = function(_, uiMapId) return UI_TO_AREA[uiMapId] end
    ZDB.GetZonesWithQuests = function(yield)
        assert(yield == false, "GetZonesWithQuests debe llamarse sin yield")
        local set = {}
        for id in pairs(G.quests) do set[id] = true end
        return { [12] = set, [40] = {} }
    end
    _G.QuestieLoader = {
        ImportModule = function(_, name)
            if name == "QuestieDB" then return QDB end
            if name == "ZoneDB" then return ZDB end
            return { private = {} }
        end,
    }

    -- Objetos: C_Item con carga asincrona (uncached -> nil hasta M.loadItem).
    local function idOf(x)
        if type(x) == "number" then return x end
        return tonumber(string.match(tostring(x), "item:(%d+)"))
    end
    _G.C_Item = _G.C_Item or {}
    _G.C_Item.GetItemInfo = function(x)
        local id = idOf(x)
        if not id then return nil end
        G.infoCalls[id] = (G.infoCalls[id] or 0) + 1
        local it = G.items[id]
        if not it or not G.loaded[id] then return nil end
        local link = "|cffffffff|Hitem:" .. id .. "|h[" .. it.name .. "]|h|r"
        return it.name, link, it.quality, it.ilvl, it.minLevel, it.className, it.subName, 1, it.equipLoc, nil, 0,
            it.classID, it.subclassID
    end
    _G.C_Item.GetItemStats = function(link)
        local id = idOf(link)
        local it = id and G.items[id]
        if not it or not G.loaded[id] then return nil end
        local out = {}
        for k, v in pairs(it.stats) do out[k] = v end
        return out
    end
    _G.C_Item.RequestLoadItemDataByID = function(id)
        rec("C_Item.RequestLoadItemDataByID", id)
        G.requested[id] = true
    end
    _G.GetItemSubClassInfo = function(cls, sub)
        if cls == 4 then return SUB_NAMES_ARMOR[sub] end
        if cls == 2 then return SUB_NAMES_WEAPON[sub] end
        return nil
    end
    _G.GetNumSkillLines = function() return #G.skills end
    _G.GetSkillLineInfo = function(i)
        local s = G.skills[i]
        if not s then return nil end
        return s[1], s[2], false, s[2] and 0 or 1
    end
    _G.C_SpellBook = { IsSpellKnown = function(id)
        rec("C_SpellBook.IsSpellKnown", id)
        return id == 674 and G.dualWield or false
    end }
    _G.UnitRace = function() return G.race.name, G.race.name, G.race.id end
    _G.UnitClass = function() return G.class.name, G.class.token, G.class.id end
    _G.GetInventoryItemLink = function(_, slot)
        local id = G.equipped[slot]
        if not id then return nil end
        return "|cffffffff|Hitem:" .. id .. "|h[" .. items[id].name .. "]|h|r"
    end
    _G.GetInventoryItemID = function(_, slot) return G.equipped[slot] end
    _G.C_QuestLog.GetAllCompletedQuestIDs = function()
        local out = {}
        for id, v in pairs(S.flagged) do
            if v == true then out[#out + 1] = id end
        end
        table.sort(out)
        return out
    end
end

function M.loadItem(id)
    M.GEAR.loaded[id] = true
end

function M.uninstallGear()
    _G.QuestieLoader = nil
    _G.ForeverDungeonJournal_NS = nil
    _G.UnitRace, _G.UnitClass = nil, nil
    _G.GetInventoryItemLink, _G.GetInventoryItemID = nil, nil
    _G.GetItemSubClassInfo, _G.GetNumSkillLines, _G.GetSkillLineInfo = nil, nil, nil
    _G.C_SpellBook = nil
    if _G.C_Item then
        _G.C_Item.GetItemInfo, _G.C_Item.GetItemStats, _G.C_Item.RequestLoadItemDataByID = nil, nil, nil
    end
    M.GEAR = nil
end

-- ---------------------------------------------------------------------------
-- Combate (Combat.lua): C_DamageMeter de prueba, objetivo, zona y recursos.
-- Los valores secretos (issecretvalue) son cajas flaggeadas: aritmetica sobre ellas daria error.
-- M.DM.secretInCombat = true -> las lecturas de sesion dentro de combate devuelven valores secretos.
-- Todas las lecturas de C_DamageMeter quedan registradas en M.calls con prefijo "C_DamageMeter.".
-- ---------------------------------------------------------------------------
local SECRET = setmetatable({}, { __mode = "k" })

function M.secret(v)
    local box = { v = v }
    SECRET[box] = true
    return box
end

-- Enumeraciones como las de Blizzard (valores de la wiki 12.1.5).
M.ENUM = {
    DamageMeterSessionType = { Overall = 0, Current = 1, Expired = 2 },
    DamageMeterType = { DamageDone = 0, Dps = 1, HealingDone = 2, Hps = 3, Absorbs = 4, Interrupts = 5,
                        Dispels = 6, DamageTaken = 7, AvoidableDamageTaken = 8, Deaths = 9, EnemyDamageTaken = 10 },
}

-- Sesion de prueba: f = { duration, guid, damageDone = {total, spells}, healingDone = {...}, damageTaken = {...} }
-- spells = { {spellID, amount, creature?} }. Se sirve como sesion Current.
M.DM = { fight = nil, secretInCombat = false, available = true }
M.SPELL_NAMES = { [585] = "Castigo", [2050] = "Curación", [589] = "Dolor abrasador", [1752] = "Golpe furtivo",
                  [9999] = "Explosión" }

function M.setFight(f)
    M.DM.fight = f
end

local function dmVal(v)
    if M.DM.secretInCombat and M.state.inCombat then return M.secret(v) end
    return v
end

local function dmMeterBlock(f, meter)
    local E = M.ENUM.DamageMeterType
    if meter == E.DamageDone then return f.damageDone, f.guid end
    if meter == E.HealingDone then return f.healingDone, f.guid end
    if meter == E.DamageTaken then return f.damageTaken, f.guid end
    return nil, nil
end

local function dmSpellList(block)
    local out = {}
    for _, sp in ipairs(block and block.spells or {}) do
        -- Formato: { spellID, amount, creature? } (posicional) o { spellID = , amount = , creature = }.
        local id = sp.spellID or sp[1]
        local amount = sp.amount or sp[2]
        local creature = sp.creature or sp[3]
        out[#out + 1] = { spellID = id, totalAmount = dmVal(amount), amountPerSecond = 0,
                          creatureName = creature or "Lobo", overkillAmount = 0 }
    end
    return out
end

function M.installCombat()
    local S = M.state
    local E = M.ENUM
    _G.Enum = M.ENUM
    _G.issecretvalue = function(v) return type(v) == "table" and SECRET[v] == true end
    _G.InCombatLockdown = function() return S.inCombat == true end
    _G.UnitName = function(unit)
        if unit == "target" then return S.target and S.target.name or nil end
        return "Tester"
    end
    _G.UnitExists = function(unit)
        if unit == "target" then return S.target ~= nil end
        return unit == "player"
    end
    _G.UnitGUID = function(unit)
        if unit == "target" then return S.target and S.target.guid or nil end
        return "Player-1-0000AAAA"
    end
    _G.UnitLevel = function(unit)
        if unit == "target" then return S.target and S.target.level or nil end
        return S.level
    end
    _G.UnitClassification = function(unit)
        if unit == "target" and S.target then return S.target.classification end
        return nil
    end
    _G.UnitCanAttack = function(_, unit) return unit == "target" and S.target ~= nil and S.target.canAttack == true end
    _G.UnitHealth = function() return dmVal(S.hp) end
    _G.UnitHealthMax = function() return 100 end
    _G.UnitPower = function() return dmVal(S.power) end
    _G.UnitPowerMax = function() return 100 end
    _G.IsInInstance = function() return S.inInstance == true, "none" end
    _G.GetRealZoneText = function() return S.zone end
    _G.C_Spell = _G.C_Spell or {}
    _G.C_Spell.GetSpellName = function(id) rec("C_Spell.GetSpellName", id); return M.SPELL_NAMES[id] end
    _G.C_DamageMeter = {
        IsDamageMeterAvailable = function() return M.DM.available, nil end,
        GetSessionDurationSeconds = function(sessionType)
            rec("C_DamageMeter.GetSessionDurationSeconds", sessionType)
            local f = M.DM.fight
            return f and dmVal(f.duration) or nil
        end,
        GetCombatSessionFromType = function(sessionType, meter)
            rec("C_DamageMeter.GetCombatSessionFromType", sessionType, meter)
            local f = M.DM.fight
            if f == nil or sessionType ~= E.DamageMeterSessionType.Current then return nil end
            return {
                totalAmount = 0,
                durationSeconds = dmVal(f.duration),
                combatSources = {
                    { name = "Tester", sourceGUID = f.guid, isLocalPlayer = true, totalAmount = 0 },
                },
            }
        end,
        GetCombatSessionSourceFromType = function(sessionType, meter, guid)
            rec("C_DamageMeter.GetCombatSessionSourceFromType", sessionType, meter, guid)
            local f = M.DM.fight
            if f == nil or sessionType ~= E.DamageMeterSessionType.Current then return nil end
            local block, localGuid = dmMeterBlock(f, meter)
            if block == nil or guid ~= localGuid then return nil end
            local spells = dmSpellList(block)
            if M.DM.secretInCombat and S.inCombat then spells = M.secret(spells) end
            return { totalAmount = dmVal(block.total), maxAmount = 0, combatSpells = spells }
        end,
    }
end

function M.uninstallCombat()
    for _, name in ipairs({ "Enum", "issecretvalue", "C_DamageMeter", "UnitGUID", "UnitClassification",
                            "UnitCanAttack", "UnitHealth", "UnitHealthMax", "UnitPower", "UnitPowerMax",
                            "IsInInstance", "GetRealZoneText" }) do
        _G[name] = nil
    end
    M.DM = { fight = nil, secretInCombat = false, available = true }
end

-- ---------------------------------------------------------------------------
-- Barras de accion (Bars.lua): GetActionInfo, bindings, Pickup*/PlaceAction, cursor y hechizos conocidos.
-- M.BARS.actions[slot] = { type, id } (slot ocupado); M.BARS.bindings[nombre] = tecla; M.BARS.known[spellID].
-- M.BARS.log = recorder de Pickup*/PlaceAction/ClearCursor (ademas de M.calls).
-- ---------------------------------------------------------------------------
M.BARS = nil

function M.installBars()
    local B = {
        actions = {}, bindings = {}, known = {}, cursor = nil, macros = {}, items = {},
        spellNames = {}, numGlobal = 0, numChar = 0,
        shown = { MultiBarBottomLeft = false, MultiBarBottomRight = false, MultiBarRight = false, MultiBarLeft = false },
        noPickupSpell = false, noPlaceAction = false,
    }
    M.BARS = B
    _G.GetActionInfo = function(slot)
        local a = B.actions[slot]
        if not a then return nil end
        return a.type, a.id, a.subType
    end
    -- Texto del boton: el nombre de la macro (a.text si el id del slot no es el indice de la macro).
    _G.GetActionText = function(slot)
        local a = B.actions[slot]
        if not a or a.type ~= "macro" then return nil end
        if a.text then return a.text end
        local m = B.macros[a.id]
        return m and m.name or nil
    end
    _G.GetBindingKey = function(name)
        rec("GetBindingKey", name)
        return B.bindings[name]
    end
    _G.GetMacroInfo = function(i)
        local m = B.macros[i]
        if not m then return nil end
        return m.name, 134400, m.body
    end
    _G.GetNumMacros = function() return B.numGlobal, B.numChar end
    _G.GetCursorInfo = function()
        if B.cursor then return B.cursor.type, B.cursor.id end
        return nil
    end
    _G.ClearCursor = function() rec("ClearCursor"); B.cursor = nil end
    _G.PickupAction = function(slot)
        rec("PickupAction", slot)
        local a = B.actions[slot]
        B.cursor = a and { type = a.type, id = a.id } or nil
        B.actions[slot] = nil
    end
    _G.PlaceAction = function(slot)
        rec("PlaceAction", slot)
        if B.noPlaceAction then return end
        if B.cursor then B.actions[slot] = { type = B.cursor.type, id = B.cursor.id } end
        B.cursor = nil
    end
    _G.PickupMacro = function(index)
        rec("PickupMacro", index)
        B.cursor = { type = "macro", id = index }
    end
    _G.PickupItem = nil
    _G.C_Item = _G.C_Item or {}
    _G.C_Item.GetItemNameByID = function(id) return B.items[id] end
    _G.C_Item.PickupItem = function(id) rec("C_Item.PickupItem", id); B.cursor = { type = "item", id = id } end
    _G.C_Spell = _G.C_Spell or {}
    _G.C_Spell.GetSpellName = function(id) rec("C_Spell.GetSpellName", id); return B.spellNames[id] or M.SPELL_NAMES[id] end
    _G.C_Spell.PickupSpell = function(id)
        rec("C_Spell.PickupSpell", id)
        if B.noPickupSpell then return end
        B.cursor = { type = "spell", id = id }
    end
    _G.C_SpellBook = { IsSpellKnown = function(id) rec("C_SpellBook.IsSpellKnown", id); return B.known[id] == true end }
    _G.MultiBarBottomLeft = { IsShown = function() return B.shown.MultiBarBottomLeft end }
    _G.MultiBarBottomRight = { IsShown = function() return B.shown.MultiBarBottomRight end }
    _G.MultiBarRight = { IsShown = function() return B.shown.MultiBarRight end }
    _G.MultiBarLeft = { IsShown = function() return B.shown.MultiBarLeft end }
    return B
end

function M.uninstallBars()
    for _, name in ipairs({ "GetActionInfo", "GetActionText", "GetBindingKey", "GetMacroInfo", "GetNumMacros", "GetCursorInfo",
                            "ClearCursor", "PickupAction", "PlaceAction", "PickupMacro", "C_SpellBook",
                            "MultiBarBottomLeft", "MultiBarBottomRight", "MultiBarRight", "MultiBarLeft" }) do
        _G[name] = nil
    end
    M.BARS = nil
end

-- ---------------------------------------------------------------------------
-- Macros (Macros.lua): GetNumMacros, GetMacroInfo, CreateMacro, EditMacro, DeleteMacro.
-- M.MACROS.global = lista de cuenta (indices 1..120), M.MACROS.char = lista de personaje (121..138).
-- Igual que el juego: borrar desplaza los indices siguientes. M.MACROS.noCreate = CreateMacro no crea.
-- Requiere installBars() para PickupMacro/PlaceAction (la copia de barras es la misma).
-- ---------------------------------------------------------------------------
M.MACROS = nil

function M.installMacros()
    local S = { global = {}, char = {}, maxGlobal = 120, maxChar = 18, noCreate = false }
    M.MACROS = S
    _G.MAX_ACCOUNT_MACROS = S.maxGlobal
    _G.MAX_CHARACTER_MACROS = S.maxChar
    -- Lista y posicion (dentro de la lista) de un indice de macro; nil si no existe.
    local function locate(index)
        if index >= 1 and index <= #S.global then return S.global, index end
        local ci = index - S.maxGlobal
        if ci >= 1 and ci <= #S.char then return S.char, ci end
        return nil
    end
    _G.GetNumMacros = function() rec("GetNumMacros"); return #S.global, #S.char end
    _G.GetMacroInfo = function(index)
        local list, pos = locate(index)
        if not list then return nil end
        local m = list[pos]
        return m.name, m.icon, m.body
    end
    _G.CreateMacro = function(name, icon, body, perChar)
        rec("CreateMacro", name, icon, body, perChar)
        if S.noCreate then return nil end
        local list = perChar and S.char or S.global
        local max = perChar and S.maxChar or S.maxGlobal
        if #list >= max then return nil end
        list[#list + 1] = { name = name, icon = icon, body = body }
        return (perChar and S.maxGlobal or 0) + #list
    end
    _G.EditMacro = function(index, name, icon, body)
        rec("EditMacro", index, name, icon, body)
        local list, pos = locate(index)
        if not list then return nil end
        list[pos] = { name = name, icon = icon, body = body }
        return index
    end
    _G.DeleteMacro = function(index)
        rec("DeleteMacro", index)
        local list, pos = locate(index)
        if list then table.remove(list, pos) end
    end
    return S
end

function M.uninstallMacros()
    for _, name in ipairs({ "GetNumMacros", "GetMacroInfo", "CreateMacro", "EditMacro", "DeleteMacro",
                            "MAX_ACCOUNT_MACROS", "MAX_CHARACTER_MACROS" }) do
        _G[name] = nil
    end
    M.MACROS = nil
end

-- ---------------------------------------------------------------------------
-- APIs nativas de misiones (Native.lua, QuestWindow.lua, Gear.lua): C_QuestLine, C_QuestLog de Blizzard,
-- C_Map.GetMapInfo con padres, servicios de entrenador y Encounter Journal (opcional).
-- Opt-in: M.installNative(opts). Sin llamarla, las APIs no existen (caso "juego incompleto").
-- opts (todas opcionales): lines[mapID] = QuestLineInfo[], waypoints[qid] = {mapID,x,y},
-- pois[mapID] = QuestPOIMapInfo[], levels[qid], titles[qid], parents[mapID], mapNames[mapID],
-- rewards[qid] = {choice = {itemID...}, fixed = {itemID...}} (solo para misiones en el registro),
-- services = { {name, rank, category}, ... }, journal = { {instanceID, name}, ... } (nil = sin Encounter Journal).
-- ---------------------------------------------------------------------------
function M.installNative(opts)
    opts = opts or {}
    local S = M.state
    local nat = {
        lines = opts.lines or {}, waypoints = opts.waypoints or {}, pois = opts.pois or {},
        levels = opts.levels or {}, titles = opts.titles or {}, parents = opts.parents or {},
        mapNames = opts.mapNames or {}, rewards = opts.rewards or {}, services = opts.services or {},
        journal = opts.journal,
    }
    S.native = nat
    M._saved = { mapInfo = _G.C_Map.GetMapInfo }
    _G.C_Map.GetMapInfo = function(id)
        local base = M._saved.mapInfo and M._saved.mapInfo(id) or nil
        if nat.mapNames[id] then
            base = base or {}
            base.name = nat.mapNames[id]
        end
        if base then
            base.mapID = id
            base.parentMapID = nat.parents[id] or 0
        end
        return base
    end
    _G.C_QuestLine = {
        GetAvailableQuestLines = function(m)
            rec("C_QuestLine.GetAvailableQuestLines", m)
            return nat.lines[m] or {}
        end,
    }
    local L = _G.C_QuestLog
    L.GetNextWaypoint = function(qid)
        local wp = nat.waypoints[qid]
        if wp then return wp.mapID, wp.x, wp.y end
        return nil
    end
    L.GetQuestsOnMap = function(m) return nat.pois[m] or {} end
    L.GetQuestDifficultyLevel = function(qid) return nat.levels[qid] end
    L.GetTitleForQuestID = function(qid) return nat.titles[qid] end
    _G.GetNumQuestLogChoices = function(qid)
        local r = nat.rewards[qid]
        return r and #r.choice or 0
    end
    _G.GetQuestLogChoiceInfo = function(i, qid)
        local r = nat.rewards[qid]
        local id = r and r.choice[i]
        return "Choice" .. tostring(i), nil, 1, 3, true, id
    end
    _G.GetNumQuestLogRewards = function(qid)
        local r = nat.rewards[qid]
        return r and #r.fixed or 0
    end
    _G.GetQuestLogRewardInfo = function(i, qid)
        local r = nat.rewards[qid]
        local id = r and r.fixed[i]
        return "Reward" .. tostring(i), nil, 1, 1, true, id
    end
    _G.GetNumTrainerServices = function() return #nat.services end
    _G.GetTrainerServiceInfo = function(i)
        local s = nat.services[i]
        if not s then return nil end
        return s.name, s.rank or "", s.category
    end
    if opts.journal then
        _G.EJ_GetInstanceByIndex = function(i)
            local j = nat.journal[i]
            if j then return j.instanceID, j.name end
            return nil
        end
    else
        _G.EJ_GetInstanceByIndex = nil
    end
end

function M.uninstallNative()
    if M._saved then
        _G.C_Map.GetMapInfo = M._saved.mapInfo
        M._saved = nil
    end
    _G.C_QuestLine = nil
    if _G.C_QuestLog then
        for _, k in ipairs({ "GetNextWaypoint", "GetQuestsOnMap", "GetQuestDifficultyLevel", "GetTitleForQuestID" }) do
            _G.C_QuestLog[k] = nil
        end
    end
    for _, k in ipairs({ "GetNumQuestLogChoices", "GetQuestLogChoiceInfo", "GetNumQuestLogRewards",
                         "GetQuestLogRewardInfo", "GetNumTrainerServices", "GetTrainerServiceInfo",
                         "EJ_GetInstanceByIndex" }) do
        _G[k] = nil
    end
    M.state.native = nil
end

-- ---------------------------------------------------------------------------
-- Hoja de personaje (Sheet.lua): stats, armadura, combate, resistencias, forma, talentos
-- (C_ClassTalents / C_Traits), hechizos (C_Spell) y tooltip oculto de escaneo.
-- M.SHEET guarda los valores (M.resetSheet() los restaura). M.SHEET.secret[API] = true hace que la
-- API devuelva valores secretos. M.SHEET.descSecret[spellID] = true: descripcion secreta.
-- M.sheetFrames: frames creados con CreateFrame("Frame") (para comprobar eventos registrados).
-- ---------------------------------------------------------------------------
M.SHEET = nil
M.sheetFrames = {}

local SHEET_SPELL_NAMES = {
    [772] = "Rend", [100] = "Charge", [2457] = "Battle Stance", [12294] = "Mortal Strike",
    [12286] = "Improved Rend", [12328] = "Sweeping Strikes", [12320] = "Cruelty", [12975] = "Last Stand",
}
local SHEET_SUBCLASS = { [4] = "One-Handed Maces", [7] = "One-Handed Swords", [8] = "Two-Handed Swords" }

local function newSheetState()
    return {
        level = 19,
        className = "Warrior", classFile = "WARRIOR", raceName = "Human", raceFile = "Human",
        base = { 60, 40, 55, 20, 30 }, total = { 78, 52, 70, 20, 30 },
        health = 900, healthMax = 900, power = 0, powerMax = 100, powerToken = "RAGE",
        armor = { 620, 620 },                      -- UnitArmor: base, effective (real y bonus = 620, 0)
        ap = { 180, 10, 0 }, rap = { 30, 0, 0 },   -- base, posBuff, negBuff
        crit = 6.2, rcrit = 4.0, hit = 0, expertise = 0,
        mh = { 30.1, 45.4 }, oh = { 0, 0 }, speed = 2.6, offSpeed = nil,
        ranged = { speed = 2.9, min = 10, max = 20 },
        dodge = 5.1, parry = 5.0, block = 0, shieldBlock = 0,
        spellCrit = { [2] = 0, [3] = 0, [4] = 0, [5] = 0, [6] = 0, [7] = 0 },
        spellPower = { [3] = 45 }, spellHit = 0, haste = 0,
        regen = { 0, 0 }, resist = {},
        form = 2, formSpell = 2457,
        skills = { { name = "Two-Handed Swords", rank = 95, max = 95 },
                   { name = "Weapon Skills", header = true },
                   { name = "Defense", rank = 95, max = 95 } },
        mhItem = "item:19351", ohItem = nil, weaponSub = 8,
        configID = 999, trees = { 101, 102, 103 }, treeNames = { [101] = "Arms", [102] = "Fury", [103] = "Protection" },
        treeNodes = { [101] = { 1, 2, 3 }, [102] = { 10 }, [103] = { 20 } },
        -- posX/posY en pixeles; ranks = ranksPurchased; active = entryID activo (nil -> sin activeEntry)
        nodes = {
            [1] = { posX = 0, posY = 0, ranks = 1, max = 1, entries = { 1 }, active = 1 },
            [2] = { posX = 80, posY = 0, ranks = 2, max = 3, entries = { 2 }, active = 2 },
            [3] = { posX = 0, posY = 40, ranks = 0, max = 1, entries = { 3 }, active = nil },
            [10] = { posX = 0, posY = 0, ranks = 0, max = 1, entries = { 10 }, active = nil },
            [20] = { posX = 0, posY = 0, ranks = 2, max = 2, entries = { 20 }, active = 20 },
        },
        entries = { [1] = 1, [2] = 2, [3] = 3, [10] = 10, [20] = 20 },  -- entryID -> definitionID
        defs = { [1] = { spellID = 12294 }, [2] = { spellID = 12286 }, [3] = { spellID = 12328 },
                 [10] = { spellID = 12320 }, [20] = { spellID = 12975 } },
        unspent = 1,
        spellInfo = { [772] = { castTime = 0, maxRange = 5 }, [100] = { castTime = 0, maxRange = 25 },
                      [12294] = { castTime = 0, maxRange = 5 } },
        powerCost = { [772] = { { name = "RAGE", cost = 10 } }, [12294] = { { name = "RAGE", cost = 30 } } },
        cooldownMs = { [772] = 0, [100] = 15000, [12294] = 6000 },
        desc = { [772] = "Wounds the target causing them to bleed for 63 damage over 12 sec." },
        descSecret = {},
        tipLines = { [12294] = { "Mortal Strike", "Deals 160% weapon damage.", "Cost: 30 Rage" } },
        tipCalls = {},
        secret = {},
    }
end

-- Restaura los datos de la hoja (en la misma tabla: las APIs instaladas la referencian).
function M.resetSheet()
    local cur = M.SHEET
    for k in pairs(cur) do cur[k] = nil end
    for k, v in pairs(newSheetState()) do cur[k] = v end
end

local sheetSaved = nil

-- Crea el tooltip oculto "name" con SetSpellByID que expone las lineas de M.SHEET.tipLines.
local function makeTip(name)
    local tip = {}
    function tip:SetOwner() end
    function tip:ClearLines() end
    function tip:SetSpellByID(id)
        local SH = M.SHEET
        SH.tipCalls[#SH.tipCalls + 1] = id
        local lines = SH.tipLines[id] or {}
        for i = 1, 40 do
            local t = lines[i]
            if t then
                _G[name .. "TextLeft" .. i] = { GetText = function() return t end }
            else
                _G[name .. "TextLeft" .. i] = nil
            end
        end
    end
    return tip
end

function M.installSheet()
    if M.SHEET == nil then M.SHEET = newSheetState() end
    local SH = M.SHEET
    sheetSaved = sheetSaved or { CreateFrame = _G.CreateFrame, C_Spell = _G.C_Spell }
    _G.DEFENSE = "Defense"
    _G.issecretvalue = function(v) return type(v) == "table" and SECRET[v] == true end

    local function val(api, v)
        if SH.secret[api] then return M.secret(v) end
        return v
    end

    _G.CreateFrame = function(kind, name)
        if kind == "GameTooltip" then return makeTip(name) end
        local f = {}
        f.events = {}
        function f:RegisterEvent(ev) self.events[#self.events + 1] = ev end
        function f:SetScript(n, fn) if n == "OnEvent" then self.onEvent = fn end end
        M.sheetFrames[#M.sheetFrames + 1] = f
        return f
    end

    _G.UnitClass = function() return SH.className, SH.classFile end
    _G.UnitRace = function() return SH.raceName, SH.raceFile end
    _G.UnitStat = function(_, i)
        local b, t = SH.base[i], SH.total[i]
        return val("UnitStat", b), val("UnitStat", t)
    end
    _G.UnitHealth = function() return val("UnitHealth", SH.health) end
    _G.UnitHealthMax = function() return SH.healthMax end
    _G.UnitPower = function() return val("UnitPower", SH.power) end
    _G.UnitPowerMax = function() return SH.powerMax end
    _G.UnitPowerType = function() return 1, SH.powerToken end
    _G.UnitArmor = function()
        return val("UnitArmor", SH.armor[1]), val("UnitArmor", SH.armor[2]), SH.armor[1], 0
    end
    _G.UnitAttackPower = function() return SH.ap[1], SH.ap[2], SH.ap[3] end
    _G.UnitRangedAttackPower = function() return SH.rap[1], SH.rap[2], SH.rap[3] end
    _G.GetCritChance = function() return SH.crit end
    _G.GetRangedCritChance = function() return SH.rcrit end
    _G.GetHitModifier = function() return SH.hit end
    _G.GetExpertisePercent = function() return SH.expertise end
    _G.UnitDamage = function() return SH.mh[1], SH.mh[2], SH.oh[1], SH.oh[2], 0, 0, 1 end
    _G.UnitAttackSpeed = function() return SH.speed, SH.offSpeed end
    _G.UnitRangedDamage = function() return SH.ranged.speed, SH.ranged.min, SH.ranged.max, 0, 0, 1 end
    _G.GetDodgeChance = function() return SH.dodge end
    _G.GetParryChance = function() return SH.parry end
    _G.GetBlockChance = function() return SH.block end
    _G.GetShieldBlock = function() return SH.shieldBlock end
    _G.GetSpellCritChance = function(school) rec("GetSpellCritChance", school); return SH.spellCrit[school] end
    _G.GetSpellBonusDamage = function(school) rec("GetSpellBonusDamage", school); return SH.spellPower[school] end
    _G.GetSpellHitModifier = function() return SH.spellHit end
    _G.GetHaste = function() return SH.haste end
    _G.GetManaRegen = function() return SH.regen[1], SH.regen[2] end
    _G.UnitResistance = function(_, idx) return SH.resist[idx] or 0 end
    _G.GetShapeshiftForm = function() return SH.form end
    _G.GetShapeshiftFormInfo = function(i)
        if i == SH.form and i ~= 0 then return "icon", true, true, SH.formSpell end
        return nil
    end
    _G.GetNumSkillLines = function() return #SH.skills end
    _G.GetSkillLineInfo = function(i)
        local s = SH.skills[i]
        if not s then return nil end
        return s.name, s.header == true, false, s.rank, 0, 0, s.max
    end
    _G.GetItemSubClassInfo = function(cls, sub)
        if cls ~= 2 then return nil end
        return SHEET_SUBCLASS[sub]
    end
    _G.GetInventoryItemLink = function(_, slot)
        if slot == 16 then return SH.mhItem end
        if slot == 17 then return SH.ohItem end
        return nil
    end
    _G.C_Item = {
        GetItemInfoInstant = function(link)
            if link == SH.mhItem then
                return 19351, "Weapon", SHEET_SUBCLASS[SH.weaponSub], "INVTYPE_2HWEAPON", 0, 2, SH.weaponSub
            end
            return nil
        end,
    }

    _G.C_ClassTalents = { GetActiveConfigID = function() return SH.configID end }
    _G.C_Traits = {
        GetConfigInfo = function(id)
            if id == SH.configID then return { treeIDs = SH.trees } end
            return nil
        end,
        GetTreeNodes = function(tid) return SH.treeNodes[tid] end,
        GetTreeInfo = function(_, tid)
            local n = SH.treeNames[tid]
            if n then return { name = n } end
            return nil
        end,
        GetNodeInfo = function(_, nid)
            local n = SH.nodes[nid]
            if not n then return nil end
            return {
                posX = n.posX, posY = n.posY, ranksPurchased = n.ranks, maxRanks = n.max,
                entryIDs = n.entries,
                activeEntry = n.active and { entryID = n.active, rank = n.ranks } or nil,
            }
        end,
        GetEntryInfo = function(_, eid)
            local d = SH.entries[eid]
            if d then return { definitionID = d } end
            return nil
        end,
        GetDefinitionInfo = function(did) return SH.defs[did] end,
    }
    _G.UnitCharacterPoints = function() return SH.unspent end

    _G.C_Spell = _G.C_Spell or {}
    _G.C_Spell.GetSpellName = function(id) return SHEET_SPELL_NAMES[id] or M.SPELL_NAMES[id] end
    _G.C_Spell.GetSpellInfo = function(id)
        local i = SH.spellInfo[id]
        if not i then return nil end
        return { name = SHEET_SPELL_NAMES[id], castTime = i.castTime, maxRange = i.maxRange, spellID = id }
    end
    _G.C_Spell.GetSpellPowerCost = function(id) return SH.powerCost[id] end
    _G.C_Spell.GetSpellBaseCooldown = function(id)
        local c = SH.cooldownMs[id]
        if c == nil then return nil end
        return c, 1500
    end
    _G.C_Spell.GetSpellDescription = function(id)
        local d = SH.desc[id]
        if SH.descSecret[id] then return M.secret(d) end
        return d
    end
end

function M.uninstallSheet()
    local saved = sheetSaved or {}
    _G.CreateFrame = saved.CreateFrame
    _G.C_Spell = saved.C_Spell
    sheetSaved = nil
    for _, name in ipairs({ "UnitClass", "UnitRace", "UnitStat", "UnitHealth", "UnitHealthMax", "UnitPower",
                            "UnitPowerMax", "UnitPowerType", "UnitArmor", "UnitAttackPower",
                            "UnitRangedAttackPower", "GetCritChance", "GetRangedCritChance", "GetHitModifier",
                            "GetExpertisePercent", "UnitDamage", "UnitAttackSpeed", "UnitRangedDamage",
                            "GetDodgeChance", "GetParryChance", "GetBlockChance", "GetShieldBlock",
                            "GetSpellCritChance", "GetSpellBonusDamage", "GetSpellHitModifier", "GetHaste",
                            "GetManaRegen", "UnitResistance", "GetShapeshiftForm", "GetShapeshiftFormInfo",
                            "GetNumSkillLines", "GetSkillLineInfo", "GetItemSubClassInfo", "GetInventoryItemLink",
                            "C_Item", "C_ClassTalents", "C_Traits", "UnitCharacterPoints", "DEFENSE",
                            "issecretvalue" }) do
        _G[name] = nil
    end
    M.SHEET = nil
end

return M
