-- test_weapons.lua: pruebas offline de las armas y del equipo de la bolsa (Gear.lua y Collectors.lua).
-- Cubre: el daño y la velocidad leidos con las cadenas del juego en español e inglés, sin cadenas (respaldo
-- por numeros), miles y decimales («1.200» frente a «2,80»), lineas que no son de un arma, la API de tooltip
-- y el tooltip oculto (con cache), y la info de equipo de la bolsa con el tope de 60 equipables.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_weapons.lua
package.path = "tools/lua/?.lua;" .. package.path
local W = require("wowstub")

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
local function near(name, got, want)
    check(name, got ~= nil and math.abs(got - want) < 1e-6, string.format("got %s, want %s", tostring(got), tostring(want)))
end

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end

-- Entorno: la API del juego, un marco que acepta registros de eventos, y los objetos de la bolsa.
W.installGameAPI()
_G.CreateFrame = function()
    local f = {}
    function f:RegisterEvent() end
    function f:SetScript() end
    return f
end
_G.UIParent = {}
SlashCmdList = SlashCmdList or {}
load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Gear.lua")
local G = ns.Gear

local SPANISH = { DAMAGE_TEMPLATE = "Daño %s - %s", SPEED_TEMPLATE = "Velocidad %s" }
local ENGLISH = { DAMAGE_TEMPLATE = "%s - %s Damage", SPEED_TEMPLATE = "Speed %s" }

print("== lectura de lineas (funcion pura)")
do
    local w = G.ParseWeaponLines({ "Daño 44 - 62", "Velocidad 3,50", "(15,1 de daño por segundo)" }, SPANISH)
    check("español: daño y velocidad con coma decimal", w ~= nil)
    if w then
        eq("español: mínimo", w.min, 44)
        eq("español: máximo", w.max, 62)
        near("español: velocidad 3,50", w.speed, 3.5)
        near("español: daño por segundo", w.dps, 53 / 3.5)
    end

    local e = G.ParseWeaponLines({ "44 - 62 Damage", "Speed 3.50" }, ENGLISH)
    check("inglés: con las cadenas inglesas", e ~= nil and e.min == 44 and e.max == 62)
    if e then near("inglés: velocidad 3.50", e.speed, 3.5) end

    local r = G.ParseWeaponLines({ "Daño 44 - 62", "Velocidad 3,50" }, nil)
    check("sin cadenas del juego: respaldo por numeros", r ~= nil and r.min == 44 and r.max == 62)
    if r then near("sin cadenas: velocidad", r.speed, 3.5) end

    local big = G.ParseWeaponLines({ "Daño 1.200 - 1.500", "Velocidad 2,80" }, SPANISH)
    check("miles con punto: 1.200 y 1.500", big ~= nil and big.min == 1200 and big.max == 1500)
    if big then near("miles: velocidad 2,80", big.speed, 2.8) end

    eq("decimal con punto: 2.5", G.ToNumber("2.5"), 2.5)
    eq("decimal con coma: 2,5", G.ToNumber("2,5"), 2.5)
    eq("miles: 1.234", G.ToNumber("1.234"), 1234)
    eq("miles: 1,234", G.ToNumber("1,234"), 1234)

    eq("sin daño ni velocidad: nil", G.ParseWeaponLines({ "+5 de fuerza", "Requiere nivel 20" }, SPANISH), nil)
    eq("solo daño: nil", G.ParseWeaponLines({ "Daño 44 - 62" }, SPANISH), nil)
    eq("tabla vacia: nil", G.ParseWeaponLines({}, SPANISH), nil)
end

print("== API de tooltip y tooltip oculto")
do
    local calls = 0
    _G.C_TooltipInfo = {
        GetItemByID = function(id)
            calls = calls + 1
            if id ~= 501 then return nil end
            return { lines = { { leftText = "Daño 44 - 62" }, { leftText = "Velocidad 3,50" } } }
        end,
    }
    _G.DAMAGE_TEMPLATE, _G.SPEED_TEMPLATE = SPANISH.DAMAGE_TEMPLATE, SPANISH.SPEED_TEMPLATE
    local w = G.WeaponStats(501, "|Hitem:501|h[Espada]|h")
    check("API de tooltip: daño leido", w ~= nil and w.min == 44 and w.max == 62)
    local again = G.WeaponStats(501, "|Hitem:501|h[Espada]|h")
    check("la lectura se guarda: la segunda vez no llama a la API", again == w and calls == 1,
        string.format("llamadas %d", calls))

    _G.C_TooltipInfo = nil
    local hyperlinks = {}
    local tip = {}
    function tip:ClearLines() end
    function tip:SetOwner() end
    function tip:SetHyperlink(link) hyperlinks[#hyperlinks + 1] = link end
    _G.CreateFrame = function(kind, name)
        if kind == "GameTooltip" and name == "CompaneroItemTooltip" then return tip end
        local f = {}
        function f:RegisterEvent() end
        function f:SetScript() end
        return f
    end
    _G.CompaneroItemTooltipTextLeft1 = { GetText = function() return "Hacha de prueba" end }
    _G.CompaneroItemTooltipTextLeft2 = { GetText = function() return "Daño 10 - 20" end }
    _G.CompaneroItemTooltipTextLeft3 = { GetText = function() return "Velocidad 2,00" end }
    _G.CompaneroItemTooltipTextLeft4 = nil
    local h = G.WeaponStats(502, "|Hitem:502|h[Hacha]|h")
    check("tooltip oculto: daño leido", h ~= nil and h.min == 10 and h.max == 20)
    if h then near("tooltip oculto: velocidad", h.speed, 2.0) end
    eq("tooltip oculto: se usa el enlace del objeto", hyperlinks[1], "|Hitem:502|h[Hacha]|h")
    _G.CompaneroItemTooltipTextLeft2 = { GetText = function() return "Requiere nivel 20" end }
    _G.CompaneroItemTooltipTextLeft3 = nil
    eq("un objeto sin lectura no se guarda", G.WeaponStats(503, nil), nil)
end

print("== equipo de la bolsa (Collectors.lua, tope de 60)")
do
    -- Objetos de prueba: armaduras con fuerza, un arma, una pocion (no es equipo) y 65 pecheras.
    local items = {}
    local function item(id, name, classID, subclassID, equipLoc, stats)
        items[id] = { name = name, classID = classID, subclassID = subclassID, equipLoc = equipLoc, stats = stats or {} }
    end
    item(900, "Pechera de prueba", 4, 3, "INVTYPE_CHEST", { ITEM_MOD_STRENGTH_SHORT = 9 })
    item(901, "Espada de prueba", 2, 7, "INVTYPE_2HWEAPON", {})
    item(902, "Poción de prueba", 0, 1, "", {})
    for i = 1, 65 do item(1000 + i, "Pechera " .. i, 4, 3, "INVTYPE_CHEST", { ITEM_MOD_STRENGTH_SHORT = i }) end

    local function idOf(x)
        if type(x) == "number" then return x end
        return tonumber(string.match(tostring(x), "item:(%d+)"))
    end
    _G.C_Item = {
        GetItemInfo = function(x)
            local id = idOf(x)
            local it = id and items[id]
            if not it then return nil end
            local link = "|cffffffff|Hitem:" .. id .. "|h[" .. it.name .. "]|h|r"
            return it.name, link, 1, 20, 1, "Armor", "Mail", 1, it.equipLoc, nil, 0, it.classID, it.subclassID
        end,
        GetItemStats = function(link)
            local id = idOf(link)
            local it = id and items[id]
            if not it then return nil end
            local out = {}
            for k, v in pairs(it.stats) do out[k] = v end
            return out
        end,
        RequestLoadItemDataByID = function() end,
        GetItemQualityByID = function() return 1 end,
    }
    _G.C_TooltipInfo = {
        GetItemByID = function(id)
            if id == 901 then return { lines = { { leftText = "Daño 60 - 80" }, { leftText = "Velocidad 2,60" } } } end
            return nil
        end,
    }

    -- Una bolsa con las 3 primeras y 65 pecheras: 1 + 1 + 65 equipables, y la pocion.
    local bagItems = { { 900, 901, 902 } }
    for i = 1, 65 do bagItems[1][#bagItems[1] + 1] = 1000 + i end
    local slots = {}
    for slot, id in ipairs(bagItems[1]) do
        slots[slot] = { itemID = id, hyperlink = "|cffffffff|Hitem:" .. id .. "|h[x]|h|r", stackCount = 1, quality = 1 }
    end
    _G.NUM_BAG_SLOTS = 0
    _G.C_Container = {
        GetContainerNumSlots = function(bag) return bag == 0 and #slots or 0 end,
        GetContainerNumFreeSlots = function() return 0 end,
        GetContainerItemInfo = function(bag, slot) return slots[slot] end,
    }
    load("CompaneroWoW/Collectors.lua")

    local inv = ns.CollectInventory()
    local withStats, withWeapon, potion = 0, nil, nil
    for _, it in ipairs(inv.items) do
        if it.equipLoc and it.equipLoc ~= "" and it.stats then withStats = withStats + 1 end
        if it.itemID == 901 then withWeapon = it end
        if it.itemID == 902 then potion = it end
    end
    eq("la bolsa: 60 equipables con stats (tope)", withStats, 60)
    check("la bolsa: el arma lleva daño y velocidad", withWeapon ~= nil and withWeapon.weapon ~= nil
        and withWeapon.weapon.min == 60 and withWeapon.weapon.max == 80 and math.abs(withWeapon.weapon.speed - 2.6) < 1e-6)
    check("la bolsa: la pocion no es equipo", potion ~= nil and (potion.equipLoc == nil or potion.equipLoc == ""))
    local first = inv.items[1]
    check("la bolsa: la pechera conserva sus stats", first.itemID == 900 and first.stats and first.stats.ITEM_MOD_STRENGTH_SHORT == 9)
end

print()
if failures == 0 then
    print("ALL TESTS PASSED")
else
    print(failures .. " FAILED")
    os.exit(1)
end
