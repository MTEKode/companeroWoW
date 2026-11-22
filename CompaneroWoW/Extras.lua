-- Extras.lua: datos del personaje que el estado guardado no llevaba (0.2.0). Bloques de memoria del
-- personaje que Storage.SaveState pasa a characters[clave].state:
--   bank         banco: objetos (id, nombre, cantidad, calidad). Solo se lee con el banco abierto.
--   recipes      recetas conocidas por profesion (solo nombres). Se leen al abrir la ventana de la profesion.
--   currencies   monedas y fichas (nombre, cantidad).
--   guildRoster  lista de la hermandad: nombre, nivel, clase, rango, zona de los conectados. Sin notas.
--   info         piedra de hogar, rango en la hermandad, JcJ, mascota, bloqueos de mazmorra, tiempo jugado.
-- (bags y reputations salen de lo que ya recoge Collectors.lua: ver STATE_VIEWS en Storage.lua.)
--
-- Solo LEE: ninguna llamada cambia nada en el juego ni pide datos al servidor (no llama a GuildRoster,
-- RequestTimePlayed ni abre o expande ventanas). Toda la API pasa por ns.Call: una funcion que falte o
-- falle deja el bloque sin dato (se conserva el anterior), nunca un error de Lua. Fuera de combate, por
-- eventos, con una espera comun (FLUSH_DELAY) y un intervalo minimo por bloque (INTERVAL).
-- Privacidad: de la hermandad no se guardan notas publicas ni de oficial; nada de chat ni de correo.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call, Get, Num, Str, Clean = ns.Call, ns.Get, ns.Num, ns.Str, ns.Clean
local floor, sort = math.floor, table.sort

ns.Extras = ns.Extras or {}
local E = ns.Extras

E.RECIPES_MAX = 250      -- recetas por profesion
E.PROFESSIONS_MAX = 8    -- profesiones con recetas
E.ROSTER_MAX = 150       -- miembros de la hermandad (conectados primero, luego los de mas nivel)
E.CURRENCIES_MAX = 40
E.LOCKOUTS_MAX = 20
E.PET_SPELLS_MAX = 10
E.SCAN_MAX = 2000        -- tope de filas que se recorren en una lista del juego
E.NAME_MAX = 80          -- longitud maxima de un nombre guardado
E.FLUSH_DELAY = 2        -- segundos desde el evento hasta la lectura (los eventos llegan en rafaga)
E.BANK_EMPTY_GRACE = 5   -- segundos tras abrir el banco en los que una lectura vacia no borra el banco guardado
-- Segundos minimos entre dos lecturas del mismo bloque.
E.INTERVAL = { bank = 2, recipes = 2, crafts = 2, currencies = 10, info = 30, guildRoster = 120 }

local ORDER = { "bank", "recipes", "crafts", "currencies", "info", "guildRoster" }

local dirty = {}            -- [bloque] = hay una lectura pedida
local lastRun = {}          -- [bloque] = GetTime de su ultima lectura
local timerPending = false
local combatPending = false
local bankOpen = false      -- entre BANKFRAME_OPENED y BANKFRAME_CLOSED
local bankOpenedAt = nil    -- GetTime del ultimo BANKFRAME_OPENED
local played = nil          -- { total, level, at } del ultimo TIME_PLAYED_MSG

E.stats = { flushes = 0, writes = 0 }

local function nowSec()
    local t = Num(Call("GetTime"))
    if t then return t end
    return ns.Now()
end

-- Nombre limpio y acotado. nil si no es un texto con contenido.
local function nameOf(v)
    v = Str(v)
    if not v or v == "" then return nil end
    if #v > E.NAME_MAX then v = string.sub(v, 1, E.NAME_MAX) end
    return v
end

-- Entero no negativo acotado, o nil. Un max que no es numero no cuenta (ojo: count(Call(...)) pasaria como
-- max el segundo valor que devuelva la API; para un solo valor, count((Call(...)))).
local function count(v, max)
    v = Num(v)
    if not v or v ~= v or v < 0 then return nil end
    if type(max) ~= "number" or max ~= max then max = 2147483647 end
    return ns.ClampInt(v, 0, max, 0)
end

-- Verdadero de la API: true en los clientes modernos, 1 en los antiguos (que dan 1/nil).
local function yes(v)
    v = Clean(v)
    return v == true or v == 1
end

-- Llama a una funcion de la API distinguiendo "no existe o fallo" de "devolvio nil".
-- Devuelve ok (existe y no fallo) y su primer valor (nil si es secreto).
local function probe(path, ...)
    local fn = Get(path)
    if not fn then return false, nil end
    local ok, v = ns.Pcall(fn, ...)
    if not ok or ns.IsSecret(v) then return false, nil end
    return true, v
end

-- Falso de la API dicho por ella: false, nil o 0 de una funcion que existe y no fallo.
local function saysNo(path, ...)
    local ok, v = probe(path, ...)
    return ok and (v == nil or v == false or v == 0)
end

local function copy(v, depth)
    if type(v) ~= "table" then return v end
    depth = depth or 0
    if depth > 8 then return nil end
    local out = {}
    for k, x in pairs(v) do out[k] = copy(x, depth + 1) end
    return out
end

-- Igualdad profunda. En el primer nivel no cuenta la hora "at".
local function same(a, b, depth)
    if a == b then return true end
    if type(a) ~= "table" or type(b) ~= "table" then return false end
    depth = depth or 0
    if depth > 8 then return false end
    for k, v in pairs(a) do
        if not (depth == 0 and k == "at") and not same(v, b[k], depth + 1) then return false end
    end
    for k in pairs(b) do
        if not (depth == 0 and k == "at") and a[k] == nil then return false end
    end
    return true
end

-- Bloque anterior de un personaje: el de memoria o, tras una recarga, una copia del guardado en el estado
-- (solo para bloques que el estado guarda tal cual, con claves largas: recipes, info).
local function prior(c, name)
    if type(c[name]) == "table" then return c[name] end
    local st = c.state
    if type(st) == "table" and type(st[name]) == "table" then return copy(st[name]) end
    return nil
end

-- ---------------------------------------------------------------------------
-- Banco
-- ---------------------------------------------------------------------------

-- Contenedores del banco: el principal y sus bolsas.
local function bankContainers()
    local e = _G.Enum and _G.Enum.BagIndex
    e = type(e) == "table" and e or {}
    local list = { Num(e.Bank) or Num(_G.BANK_CONTAINER) or -1 }
    local first = Num(e.BankBag_1) or ((Num(_G.NUM_BAG_SLOTS) or 4) + 1)
    local n = ns.ClampInt(Num(_G.NUM_BANKBAGSLOTS) or 6, 0, 12, 6)
    for b = first, first + n - 1 do list[#list + 1] = b end
    return list
end

-- { items = { {bag, slot, itemID, link, count, quality}, ... }, totalSlots, freeSlots }.
-- nil si el banco no esta abierto o no se pudo leer ningun hueco.
function E.CollectBank()
    if not bankOpen or not ns.ContainerSlots or not ns.ContainerItem then return nil end
    local items, total = {}, 0
    for _, bag in ipairs(bankContainers()) do
        local n = count(ns.ContainerSlots(bag), 200) or 0
        total = total + n
        for slot = 1, n do
            local item = ns.ContainerItem(bag, slot)
            if item then items[#items + 1] = item end
        end
    end
    if total == 0 then return nil end
    return { items = items, totalSlots = total, freeSlots = total - #items }
end

-- ---------------------------------------------------------------------------
-- Recetas
-- ---------------------------------------------------------------------------

-- Ventana de profesion abierta: nombre, nivel, maximo y nombres de sus recetas. nil si no hay ventana.
function E.ReadTradeSkill()
    if Get("GetNumTradeSkills") then
        local name, rank, maxRank = Call("GetTradeSkillLine")
        name = nameOf(name)
        if not name or name == "UNKNOWN" then return nil end
        -- Sin ventana abierta la lista esta vacia (y el nombre puede ser cualquier texto): no hay profesion.
        local n = count(Call("GetNumTradeSkills"), E.SCAN_MAX) or 0
        if n == 0 then return nil end
        local names = {}
        for i = 1, n do
            local rname, rtype = Call("GetTradeSkillInfo", i)
            rname, rtype = nameOf(rname), Str(rtype)
            if rname and rtype ~= "header" and rtype ~= "subheader" then names[#names + 1] = rname end
        end
        return name, Num(rank), Num(maxRank), names
    end
    if Get("C_TradeSkillUI.GetAllRecipeIDs") then
        local info = Clean(Call("C_TradeSkillUI.GetBaseProfessionInfo"))
        local name = type(info) == "table" and nameOf(info.professionName) or nil
        if not name then return nil end
        local ids = Clean(Call("C_TradeSkillUI.GetAllRecipeIDs"))
        if type(ids) ~= "table" then return nil end
        local names = {}
        for i, id in ipairs(ids) do
            if i > E.SCAN_MAX then break end
            local r = Num(id) and Clean(Call("C_TradeSkillUI.GetRecipeInfo", Num(id))) or nil
            local rname = type(r) == "table" and yes(r.learned) and nameOf(r.name) or nil
            if rname then names[#names + 1] = rname end
        end
        return name, Num(info.skillLevel), Num(info.maxSkillLevel), names
    end
    return nil
end

-- Ventana de "craft" (encantamiento en la API clasica). nil si no hay ventana o no es una profesion.
function E.ReadCraft()
    if not Get("GetNumCrafts") then return nil end
    local name, rank, maxRank = Call("GetCraftDisplaySkillLine")
    name = nameOf(name)
    if not name then return nil end
    local n = count(Call("GetNumCrafts"), E.SCAN_MAX) or 0
    if n == 0 then return nil end -- sin ventana abierta
    local names = {}
    for i = 1, n do
        local rname, _, rtype = Call("GetCraftInfo", i)
        rname = nameOf(rname)
        if rname and Str(rtype) ~= "header" then names[#names + 1] = rname end
    end
    return name, Num(rank), Num(maxRank), names
end

-- Une una lectura a las recetas guardadas: una receta conocida no se olvida (la ventana puede tener
-- filtros o categorias plegadas). old no cambia. Devuelve la lista nueva de profesiones.
function E.MergeRecipes(old, name, rank, maxRank, names, now)
    local out, entry = {}, nil
    for _, p in ipairs(type(old) == "table" and old or {}) do
        if type(p) == "table" and type(p.name) == "string" then
            local cp = copy(p)
            if cp.name == name then entry = cp end
            out[#out + 1] = cp
        end
    end
    if not entry then
        entry = { name = name, list = {} }
        out[#out + 1] = entry
    end
    local set, list = {}, {}
    for _, src in ipairs({ type(entry.list) == "table" and entry.list or {}, names }) do
        for _, n in ipairs(src) do
            if type(n) == "string" and not set[n] then
                set[n] = true
                list[#list + 1] = n
            end
        end
    end
    sort(list)
    while #list > E.RECIPES_MAX do list[#list] = nil end
    local fresh = { name = name, rank = rank or entry.rank, max = maxRank or entry.max, list = list }
    if not same(entry, fresh) then
        entry.rank, entry.max, entry.list, entry.at = fresh.rank, fresh.max, list, now
    end
    -- Mas profesiones de la cuenta: sale la que lleva mas tiempo sin leerse.
    while #out > E.PROFESSIONS_MAX do
        local oldest = 1
        for i, p in ipairs(out) do
            if (tonumber(p.at) or 0) < (tonumber(out[oldest].at) or 0) then oldest = i end
        end
        table.remove(out, oldest)
    end
    return out
end

-- ---------------------------------------------------------------------------
-- Monedas
-- ---------------------------------------------------------------------------

-- { { name, count, id? }, ... }. nil si el juego no tiene lista de monedas.
function E.CollectCurrencies()
    local out = {}
    if Get("C_CurrencyInfo.GetCurrencyListSize") then
        for i = 1, count(Call("C_CurrencyInfo.GetCurrencyListSize"), 500) or 0 do
            local d = Clean(Call("C_CurrencyInfo.GetCurrencyListInfo", i))
            if type(d) == "table" and not yes(d.isHeader) then
                local name = nameOf(d.name)
                if name and #out < E.CURRENCIES_MAX then
                    out[#out + 1] = { name = name, count = count(d.quantity) or 0, id = Num(d.currencyID) }
                end
            end
        end
        return out
    end
    if Get("GetCurrencyListSize") then
        for i = 1, count(Call("GetCurrencyListSize"), 500) or 0 do
            local name, isHeader, _, _, _, n = Call("GetCurrencyListInfo", i)
            name = nameOf(name)
            if name and not yes(isHeader) and #out < E.CURRENCIES_MAX then
                out[#out + 1] = { name = name, count = count(n) or 0 }
            end
        end
        return out
    end
    return nil
end

-- ---------------------------------------------------------------------------
-- Hermandad
-- ---------------------------------------------------------------------------

-- { guild, total, online, members = { { name, level, class, rank, zone?, online? }, ... } }.
-- De GetGuildRosterInfo solo se leen nombre, rango, nivel, clase, zona y si esta conectado: las notas
-- (publica y de oficial) no se guardan. nil sin hermandad o sin lista cargada.
function E.CollectRoster()
    local total = count(Call("GetNumGuildMembers"), E.SCAN_MAX)
    if not total or total == 0 then return nil end
    local members, online = {}, 0
    for i = 1, total do
        local name, rank, _, level, class, zone, _, _, isOnline = Call("GetGuildRosterInfo", i)
        name = nameOf(name)
        if name then
            name = string.match(name, "^([^%-]+)") or name -- sin el reino
            local on = yes(isOnline)
            if on then online = online + 1 end
            members[#members + 1] = {
                name = name, level = count(level, 255), class = nameOf(class), rank = nameOf(rank),
                zone = on and nameOf(zone) or nil, online = on or nil,
            }
        end
    end
    if #members == 0 then return nil end
    sort(members, function(a, b)
        if (a.online == true) ~= (b.online == true) then return a.online == true end
        local la, lb = a.level or 0, b.level or 0
        if la ~= lb then return la > lb end
        return a.name < b.name
    end)
    while #members > E.ROSTER_MAX do members[#members] = nil end
    return { guild = nameOf((Call("GetGuildInfo", "player"))), total = total, online = online, members = members }
end

-- ---------------------------------------------------------------------------
-- Info: hogar, rango, JcJ, mascota, bloqueos, tiempo jugado
-- ---------------------------------------------------------------------------

local function collectPvp()
    local hk, dk, highest = Call("GetPVPLifetimeStats")
    if not Num(hk) then return nil end
    local pvp = { kills = count(hk), dishonorable = count(dk), highestRank = count(highest, 100) }
    local rank = Num(Call("UnitPVPRank", "player"))
    if rank and rank > 0 then
        local rankName, rankNumber = Call("GetPVPRankInfo", rank, "player")
        pvp.rankName, pvp.rank = nameOf(rankName), count(rankNumber, 100)
    end
    pvp.honor = count((Call("GetHonorCurrency"))) -- solo el primer valor
    return pvp
end

-- Mascota invocada (cazador, brujo): nombre, nivel, familia y habilidades de su barra.
local function collectPet()
    if not yes(Call("UnitExists", "pet")) then return nil end
    local name = nameOf(Call("UnitName", "pet"))
    if not name then return nil end
    local pet = { name = name, level = count(Call("UnitLevel", "pet"), 255), family = nameOf(Call("UnitCreatureFamily", "pet")) }
    local abilities = {}
    for i = 1, E.PET_SPELLS_MAX do
        local ability = nameOf(Call("GetPetActionInfo", i))
        -- Las ordenes (atacar, seguir, pasivo...) llegan como claves "PET_...": no son habilidades.
        if ability and string.sub(ability, 1, 4) ~= "PET_" then abilities[#abilities + 1] = ability end
    end
    if #abilities > 0 then pet.abilities = abilities end
    return pet
end

-- Bloqueos de mazmorra y banda: nombre y hora de reinicio (redondeada a 10 minutos). nil si no hay API.
local function collectLockouts()
    local n = count(Call("GetNumSavedInstances"), 100)
    if not n then return nil end
    local now, list = ns.Now(), {}
    for i = 1, n do
        local name, _, reset = Call("GetSavedInstanceInfo", i)
        name, reset = nameOf(name), Num(reset)
        if name and reset and reset > 0 and #list < E.LOCKOUTS_MAX then
            list[#list + 1] = { name = name, resetAt = floor((now + reset + 300) / 600) * 600 }
        end
    end
    return list
end

-- true solo si el juego dice que el personaje no tiene hermandad: IsInGuild existe, no falla y devuelve
-- falso, y GetGuildInfo tampoco da nombre. Una funcion que falta o falla no dice nada (false).
local function noGuild()
    return saysNo("IsInGuild") and saysNo("GetGuildInfo", "player")
end

-- Clases con mascota propia: la suya se conserva aunque no este invocada.
local PET_CLASS = { HUNTER = true, WARLOCK = true }

-- true solo si se sabe que no hay mascota que conservar: la clase (conocida) no tiene mascota propia y el
-- juego dice que ahora no hay ninguna. Sin UnitClass o sin UnitExists no se sabe (false).
local function noPet()
    local _, token = Call("UnitClass", "player")
    token = Str(token)
    if not token or PET_CLASS[token] then return false end
    return saysNo("UnitExists", "pet")
end

-- Lectura de ahora: cada campo sin dato queda fuera (nil).
function E.CollectInfo()
    local info = {}
    info.hearth = nameOf(Call("GetBindLocation"))
    local _, rankName, rankIndex = Call("GetGuildInfo", "player")
    info.guildRank, info.guildRankIndex = nameOf(rankName), count(rankIndex, 100)
    info.pvp = collectPvp()
    info.pet = collectPet()
    info.lockouts = collectLockouts()
    if played then info.played = copy(played) end
    return info
end

-- ---------------------------------------------------------------------------
-- Lectura y guardado de cada bloque. Devuelven true si el bloque cambio.
-- ---------------------------------------------------------------------------

local function setIfChanged(c, name, block)
    if type(block) ~= "table" or next(block) == nil or same(c[name], block) then return false end
    block.at = ns.Now()
    c[name] = block
    return true
end

local function runRecipes(c, read)
    local name, rank, maxRank, names = read()
    if not name or type(names) ~= "table" then return false end
    local old = prior(c, "recipes")
    local merged = E.MergeRecipes(old, name, rank, maxRank, names, ns.Now())
    if type(c.recipes) == "table" and same(old, merged, 1) then return false end
    c.recipes = merged
    return true
end

-- Objetos del banco guardado: el de memoria o, tras una recarga, el del estado (forma compacta: it).
local function savedBankItems(c)
    if type(c.bank) == "table" and type(c.bank.items) == "table" then return #c.bank.items end
    local st = type(c.state) == "table" and c.state.bank or nil
    local list = type(st) == "table" and (st.it or st.items) or nil
    return type(list) == "table" and #list or 0
end

-- Las funciones de RUN devuelven true si el bloque cambio y, opcionalmente, los segundos tras los que
-- hay que repetir la lectura.
local RUN = {
    bank = function(c)
        local block = E.CollectBank()
        -- Recien abierto el banco, los objetos pueden no estar cargados: una lectura vacia no sustituye a
        -- un banco guardado con objetos. Se repite; pasada la espera, un banco vacio si se guarda.
        if type(block) == "table" and #block.items == 0 and bankOpenedAt and savedBankItems(c) > 0 then
            local since = nowSec() - bankOpenedAt
            if since >= 0 and since < E.BANK_EMPTY_GRACE then
                local again = E.INTERVAL.bank or 0
                return false, again > 0 and again or 1
            end
        end
        return setIfChanged(c, "bank", block)
    end,
    recipes = function(c) return runRecipes(c, E.ReadTradeSkill) end,
    crafts = function(c) return runRecipes(c, E.ReadCraft) end,
    currencies = function(c)
        local list = E.CollectCurrencies()
        if type(list) ~= "table" or #list == 0 or same(c.currencies, list, 1) then return false end
        c.currencies = list
        return true
    end,
    guildRoster = function(c)
        local block = E.CollectRoster()
        -- Sin hermandad (dicho por el juego): la lista guardada se sustituye por una vacia.
        if block == nil and noGuild() then
            local st = type(c.state) == "table" and c.state.guildRoster or nil
            if type(c.guildRoster) == "table" or type(st) == "table" then
                block = { total = 0, online = 0, members = {} }
            end
        end
        return setIfChanged(c, "guildRoster", block)
    end,
    info = function(c)
        local old = prior(c, "info")
        local info = E.CollectInfo()
        -- Lo que ahora no se puede leer (mascota guardada, tiempo jugado de otra sesion) conserva lo anterior.
        -- Salvo lo que el juego dice que ya no existe: el rango sin hermandad, la mascota de una clase sin ella.
        local gone = {}
        if noGuild() then gone.guildRank, gone.guildRankIndex = true, true end
        if noPet() then gone.pet = true end
        for k, v in pairs(type(old) == "table" and old or {}) do
            if info[k] == nil and not gone[k] then info[k] = v end
        end
        if next(info) == nil or (type(c.info) == "table" and same(c.info, info, 1)) then return false end
        c.info = info
        return true
    end,
}

local function currentRecord()
    local db = CompaneroWoWDB
    local key = ns.CharKey and ns.CharKey()
    if type(db) ~= "table" or not key then return nil end
    if type(db.characters) ~= "table" then db.characters = {} end
    local c = db.characters[key]
    if type(c) ~= "table" then
        c = {}
        db.characters[key] = c
    end
    return c
end

-- Programa una lectura. Devuelve true si queda un temporizador en marcha (este u otro anterior).
-- Si C_Timer.After falta o falla no queda nada pendiente: timerPending solo sigue puesto si se programo.
local function schedule(sec)
    if timerPending then return true end
    local after = Get("C_Timer.After")
    if not after then return false end
    timerPending = true
    local ok = ns.Pcall(after, sec, function()
        timerPending = false
        ns.SafeCall(E.Flush)
    end)
    if not ok then timerPending = false end
    return ok == true
end

-- Lee los bloques pedidos cuyo intervalo ha pasado. Fuera de combate y dentro del mundo.
-- noSave: quien llama guarda el estado despues (Core.SnapshotAll). Devuelve true si algun bloque cambio.
function E.Flush(noSave)
    E.stats.flushes = E.stats.flushes + 1
    if ns.InCombat and ns.InCombat() then
        combatPending = true
        return false
    end
    if ns.IsFullyInWorld and not ns.IsFullyInWorld() then return false end
    local c = currentRecord()
    if not c then return false end
    local now, changed, wait = nowSec(), false, nil
    for _, kind in ipairs(ORDER) do
        if dirty[kind] then
            local last, interval = lastRun[kind], E.INTERVAL[kind] or 0
            if last == nil or now - last >= interval or now < last then
                dirty[kind] = nil
                lastRun[kind] = now
                local did, again = ns.SafeCall(RUN[kind], c)
                if did == true then changed = true end
                again = Num(again)
                if again and again > 0 then -- la lectura pide repetirse
                    dirty[kind] = true
                    if not wait or again < wait then wait = again end
                end
            else
                local w = interval - (now - last)
                if not wait or w < wait then wait = w end
            end
        end
    end
    if changed then
        E.stats.writes = E.stats.writes + 1
        if not noSave and ns.Storage then ns.Storage.SaveState(c) end
    end
    if wait then schedule(wait) end
    return changed
end

-- Pide la lectura de un bloque: se hace tras FLUSH_DELAY (o ya, si el cliente no tiene temporizadores).
local function request(kind)
    dirty[kind] = true
    if not schedule(E.FLUSH_DELAY) then ns.SafeCall(E.Flush) end
end
E.Request = request

-- Desde Core.SnapshotAll (cada 2 minutos como mucho): lo que no tiene evento fiable se vuelve a leer.
function ns.RequestExtrasExport(noSave)
    dirty.info, dirty.currencies, dirty.guildRoster = true, true, true
    return E.Flush(noSave)
end

-- Para /compa sync: lectura inmediata de todo (sin intervalos), fuera de combate.
function ns.ExportExtrasNow()
    for _, kind in ipairs(ORDER) do
        dirty[kind] = true
        lastRun[kind] = nil
    end
    return E.Flush()
end

-- Linea para /compa estado.
function ns.ExtrasStatusLine()
    local c = currentRecord() or {}
    local st = type(c.state) == "table" and c.state or {}
    local function n(block, field)
        local list = type(block) == "table" and (field and block[field] or block) or nil
        return type(list) == "table" and #list or 0
    end
    local recipes = 0
    for _, p in ipairs(type(st.recipes) == "table" and st.recipes or {}) do recipes = recipes + n(p, "list") end
    return string.format("Extras: bolsas %d, banco %d, reputaciones %d, monedas %d, recetas %d, hermandad %d",
        n(st.bags, "it"), n(st.bank, "it"), n(st.reputations), n(st.currencies), recipes, n(st.guildRoster, "members"))
end

-- ---------------------------------------------------------------------------
-- Eventos
-- ---------------------------------------------------------------------------

local function ask(kind) return function() request(kind) end end
local function bankChanged() if bankOpen then request("bank") end end

local handlers = {
    BANKFRAME_OPENED = function()
        bankOpen = true
        bankOpenedAt = nowSec()
        dirty.bank, lastRun.bank = true, nil
        ns.SafeCall(E.Flush) -- ya: el banco puede cerrarse antes de que pase la espera
        request("bank")      -- y otra vez tras la espera, con los objetos ya cargados
    end,
    BANKFRAME_CLOSED = function() bankOpen = false end,
    PLAYERBANKSLOTS_CHANGED = bankChanged,
    BAG_UPDATE_DELAYED = bankChanged,
    TRADE_SKILL_SHOW = ask("recipes"),
    TRADE_SKILL_UPDATE = ask("recipes"),
    TRADE_SKILL_LIST_UPDATE = ask("recipes"),
    CRAFT_SHOW = ask("crafts"),
    CRAFT_UPDATE = ask("crafts"),
    CURRENCY_DISPLAY_UPDATE = ask("currencies"),
    GUILD_ROSTER_UPDATE = ask("guildRoster"),
    PLAYER_GUILD_UPDATE = ask("info"),
    PLAYER_ENTERING_WORLD = function()
        request("info")
        request("currencies")
    end,
    HEARTHSTONE_BOUND = ask("info"),
    UPDATE_INSTANCE_INFO = ask("info"),
    PLAYER_PVP_KILLS_CHANGED = ask("info"),
    UNIT_PET = function(unit)
        if unit == nil or unit == "player" then request("info") end
    end,
    -- Solo se escucha: lo provoca /played (o la interfaz), nunca este addon.
    TIME_PLAYED_MSG = function(total, level)
        total, level = count(total), count(level)
        if not total then return end
        played = { total = total, level = level, at = ns.Now() }
        request("info")
    end,
    PLAYER_REGEN_ENABLED = function()
        if combatPending then
            combatPending = false
            if not schedule(E.FLUSH_DELAY) then ns.SafeCall(E.Flush) end
        end
    end,
}

function E.OnEvent(event, ...)
    local h = handlers[event]
    if h then h(...) end
end

local frame = ns.NewFrame and ns.NewFrame("Frame")
if frame then
    for event in pairs(handlers) do
        ns.Method(frame, "RegisterEvent", event)
    end
    ns.Method(frame, "SetScript", "OnEvent", function(_, event, ...)
        ns.SafeCall(E.OnEvent, event, ...)
    end)
end

-- Internos expuestos solo para tools/lua/test_extras.lua.
function E.ResetForTests()
    dirty, lastRun = {}, {}
    timerPending, combatPending, bankOpen, bankOpenedAt, played = false, false, false, nil, nil
    E.stats.flushes, E.stats.writes = 0, 0
end

return E
