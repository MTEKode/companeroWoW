-- test_storage.lua: datos guardados (esquema 2) y bandeja de envio (Storage.lua). Cubre: codificacion
-- compacta (ida y vuelta), altas con dedupe, migracion desde un esquema 1 de prueba (bloques grandes
-- a la bandeja y fuera de SavedVariables), baja tras 3 ventanas, topes de items y de bytes, limpieza,
-- enrutado de campos (lo que no es persistente no se guarda), y tamano estimado de un personaje
-- realista (< 150 KB) frente al esquema 1.
-- Ejecutar desde la raiz del repo:  luajit tools/lua/test_storage.lua
package.path = "tools/lua/?.lua;" .. package.path
require("wowstub")

local ns = {}
local function load(path)
    local chunk, err = loadfile(path)
    if not chunk then error(err) end
    chunk("CompaneroWoW", ns)
end

load("CompaneroWoW/Util.lua")
load("CompaneroWoW/Json.lua")
load("CompaneroWoW/Storage.lua")

local St = ns.Storage
-- Reloj de prueba: CLOCK (0 por defecto: los items con hora 0 o anterior no caducan). Las pruebas de
-- caducidad lo mueven.
local CLOCK = 0
ns.Now = function() return CLOCK end
local failures = 0
local function outln(s) io.write(s, "\n") end
local function check(name, cond, detail)
    if cond then
        outln("  ok    " .. name)
    else
        failures = failures + 1
        outln("  FAIL  " .. name .. (detail and (" -> " .. detail) or ""))
    end
end
local function eq(name, got, want)
    check(name, got == want, string.format("got %s, want %s", tostring(got), tostring(want)))
end

-- Lee la base de SavedVariables de forma cruda (lo que el cliente escribiria).
local function rawOf(t, k) return rawget(t, k) end

-- ---------------------------------------------------------------------------
outln("(a) Codificacion compacta: ida y vuelta")
local npc = {
    npcID = 1234, name = "Guardia de Puerta", mapID = 1429, x = 0.123456, y = 0.5, reaction = 4,
    isQuestGiver = true, services = { "Instruccion", "Aprender" }, firstSeen = 1700000000,
    lastSeen = 1700000999, availableCount = 2,
}
local cnp = St.Compact(npc)
eq("compacta: npcID -> pn", cnp.pn, 1234)
eq("compacta: name -> n", cnp.n, "Guardia de Puerta")
eq("compacta: mapID -> m", cnp.m, 1429)
eq("compacta: x a 3 decimales", cnp.x, 0.123)
eq("compacta: isQuestGiver -> qg", cnp.qg, true)
eq("compacta: services -> sv (array intacto)", cnp.sv[2], "Aprender")
eq("compacta: lastSeen no se guarda", cnp.lastSeen, nil)
eq("compacta: la clave larga ya no aparece", cnp.npcID, nil)
local enp = St.Expand(cnp)
eq("expande: npcID", enp.npcID, 1234)
eq("expande: name", enp.name, "Guardia de Puerta")
eq("expande: x redondeada", enp.x, 0.123)
eq("expande: services", enp.services[1], "Instruccion")
eq("expande: lastSeen vuelve vacio", enp.lastSeen, nil)
local quest = {
    questID = 9001, title = "Peligro en la granja", level = 5, giverX = 0.2514, giverY = 0.6631,
    rewards = { money = 1200, xp = 450, items = { { itemID = 2901, link = "|Hitem:2901|h|r", count = 1, choice = true } } },
    descriptionText = "Texto de la mision.", objectivesText = "Mata lobos.",
}
local rq = St.Expand(St.Compact(quest))
eq("ida y vuelta: titulo", rq.title, "Peligro en la granja")
eq("ida y vuelta: giverX redondeada a 3 decimales", rq.giverX, 0.251)
eq("ida y vuelta: rewards.money", rq.rewards.money, 1200)
eq("ida y vuelta: rewards.items[1].itemID", rq.rewards.items[1].itemID, 2901)
eq("ida y vuelta: rewards.items[1].choice", rq.rewards.items[1].choice, true)
eq("ida y vuelta: descriptionText", rq.descriptionText, "Texto de la mision.")
local longJSON = ns.Json.Encode(npc)
local shortJSON = ns.Json.Encode(cnp)
check("compacta: el JSON es mas corto que el registro largo", #shortJSON < #longJSON, #shortJSON .. " vs " .. #longJSON)
local rawCopy = { name = "Sin tocar", x = 1 }
St.Compact(rawCopy)
eq("compacta no modifica el original", rawCopy.name, "Sin tocar")
-- Claves sin codigo corto que coinciden con uno (n, c, ~...) se escapan: la ida y vuelta es exacta.
local tricky = { n = "a", c = 2, ["~x"] = 3, id = 4, name = "b", deep = { m = 5, pn = 6 }, arr = { { r = 7 } } }
local back = St.Expand(St.Compact(tricky))
eq("escape: clave corta sin traducir (n) vuelve igual", back.n, "a")
eq("escape: clave corta sin traducir (c) vuelve igual", back.c, 2)
eq("escape: clave con ~ vuelve igual", back["~x"], 3)
eq("escape: id (sin codigo) vuelve igual", back.id, 4)
eq("escape: name (traducida) vuelve igual", back.name, "b")
eq("escape: anidado con claves cortas", back.deep.m, 5)
eq("escape: anidado pn", back.deep.pn, 6)
eq("escape: dentro de arrays", back.arr[1].r, 7)

-- ---------------------------------------------------------------------------
outln("(b) Altas: dedupe por contenido y sin volatiles")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
local s1 = St.Learn(9, { "npcs", 1234 }, npc, 100)
check("alta: devuelve seq", type(s1) == "number")
local s2 = St.Learn(9, { "npcs", 1234 }, { npcID = 1234, name = "Guardia de Puerta", mapID = 1429, x = 0.123456, y = 0.5, reaction = 4, isQuestGiver = true, services = { "Instruccion", "Aprender" }, firstSeen = 1700000000, lastSeen = 1700005000, availableCount = 2 }, 200)
eq("mismo contenido (solo cambia lastSeen): sin alta", s2, nil)
local s3 = St.Learn(9, { "npcs", 1234 }, { npcID = 1234, name = "Guardia nueva" }, 300)
check("contenido cambiado: alta nueva", type(s3) == "number" and s3 > s1)
local items = St.Items()
eq("bandeja: 2 items", #items, 2)
eq("item: msgType 9", items[1].msgType, 9)
eq("item: payload.oseq = seq", items[1].payload.oseq, items[1].seq)
eq("item: ruta en payload.p", items[1].payload.p[1], "npcs")
eq("item: registro compacto en payload.v", items[1].payload.v.pn, 1234)
eq("alta sin base: nil", (function()
    local saved = CompaneroWoWDB
    CompaneroWoWDB = nil
    local r = St.Learn(9, { "npcs", 1 }, { npcID = 1 })
    CompaneroWoWDB = saved
    return r
end)(), nil)
St.Sync()

-- ---------------------------------------------------------------------------
outln("(c) Migracion desde el esquema 1")
local legacy = {
    settings = { strip = true },
    maps = { [1429] = "Elwynn" },
    mark = { mapID = 1429, x = 40, y = 50 },
    knowledge = {
        quests = { [9001] = { questID = 9001, title = "Peligro", level = 5, lastSeen = 10,
            rewards = { money = 1200, items = {} } } },
        npcs = { [1234] = { npcID = 1234, name = "PNJ", mapID = 1429, x = 0.5, y = 0.25, lastSeen = 20 } },
    },
    world = {
        flight = {
            Alliance = {
                nodes = { [5] = { nodeID = 5, name = "Puerto", mapID = 1429, x = 0.4, y = 0.4, known = true, lastSeen = 30 } },
                costs = { ["5>6"] = { copper = 100, hops = 1, seconds = 60 } },
            },
            known = { ["Tester-Realm"] = { 5, 6 } },
        },
        vendors = { [1000] = { npcID = 1000, name = "Vendedor", items = { { itemID = 2001 } }, lastSeen = 40 } },
        trainers = {},
        questChains = { [9001] = { questID = 9001, nextID = 9002 } },
        consumables = { [2001] = { itemID = 2001, kind = "food", requiredLevel = 5 } },
    },
    characters = {
        ["Tester-Realm"] = {
            character = { name = "Tester", level = 60, class = "WARRIOR" },
            updatedAt = 1700000000,
            quests = { { id = 1, title = "x" } },
            inventory = { items = { { id = 1, n = 5 } } },
            equipment = { { slot = 1 } },
            combats = { { id = 2, t = 20, zone = "Elwynn" }, { id = 1, t = 10, zone = "Westfall" } },
            ask = { lastId = 2, lastShown = 2, questions = {}, answers = {} },
            session = { started = 1, xp = 10 },
            sessionBase = {},
            actionBarsBackup = { { at = 1, slots = {} } },
        },
    },
}
local charsBefore = #St.Serialize(legacy.characters)
CompaneroWoWDB = legacy
St.Sync()
local db = CompaneroWoWDB
eq("migracion: esquema 2", db.schema, 2)
check("migracion: knowledge fuera de SavedVariables", rawOf(db, "knowledge") == nil)
check("migracion: world fuera de SavedVariables", rawOf(db, "world") == nil)
local byType = {}
for _, it in ipairs(St.Items()) do byType[it.msgType] = (byType[it.msgType] or 0) + 1 end
eq("migracion: 2 registros de conocimiento (mision y PNJ)", byType[9], 2)
eq("migracion: 6 registros de mundo (nodo, coste, conocidos, vendedor, cadena, consumible)", byType[16], 6)
eq("migracion: 2 combates", byType[7], 2)
local function indexOfItem(pred)
    for i, it in ipairs(St.Items()) do
        if pred(it) then return i end
    end
    return nil
end
local iQuest = indexOfItem(function(it) return it.msgType == 9 and it.payload.p[2] == 9001 end)
local iNpc = indexOfItem(function(it) return it.msgType == 9 and it.payload.p[2] == 1234 end)
check("migracion: la mision (lastSeen 10) va antes que el PNJ (lastSeen 20)", iQuest ~= nil and iNpc ~= nil and iQuest < iNpc)
local c = db.characters["Tester-Realm"]
check("migracion: el personaje queda en SavedVariables con core", rawOf(c, "character") ~= nil and rawOf(c, "character").name == "Tester")
check("migracion: los bloques de estado no quedan en SavedVariables", rawOf(c, "quests") == nil and rawOf(c, "inventory") == nil and rawOf(c, "equipment") == nil)
check("migracion: combates fuera de SavedVariables", rawOf(c, "combats") == nil)
check("migracion: las respuestas (ask) siguen guardadas", rawOf(c, "ask") ~= nil and rawOf(c, "ask").lastId == 2)
check("migracion: la sesion sigue guardada", rawOf(c, "session") ~= nil)
check("migracion: las copias de barras siguen guardadas", rawOf(c, "actionBarsBackup") ~= nil)
eq("migracion: el estado de memoria sigue accesible", c.quests and c.quests[1].id, 1)
local replayed = { knowledge = 0, world = 0 }
St.Replay(function(msgType) if msgType == 9 then replayed.knowledge = replayed.knowledge + 1 elseif msgType == 16 then replayed.world = replayed.world + 1 end end)
eq("reproduccion: 2 de conocimiento", replayed.knowledge, 2)
eq("reproduccion: 6 de mundo", replayed.world, 6)
local n1 = #St.Items()
St.Sync()
eq("segunda carga: no migra otra vez (misma bandeja)", #St.Items(), n1)
check("migracion: los bloques de estado salen de SavedVariables (characters pesa menos)", #St.Serialize(CompaneroWoWDB.characters) < charsBefore, string.format("%d -> %d", charsBefore, #St.Serialize(CompaneroWoWDB.characters)))

-- ---------------------------------------------------------------------------
outln("(d) Retencion: los items no salen de la bandeja por ventanas; caducan a los 7 dias")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
local seq = St.Learn(9, { "npcs", 77 }, { npcID = 77, name = "Dos veces" })
check("ventana 1: cuenta", St.MarkShared(seq, 1) == true)
check("misma ventana otra vez: no cuenta", St.MarkShared(seq, 1) == false)
check("ventana 2: cuenta", St.MarkShared(seq, 2) == true)
eq("ventana 2: sigue en la bandeja", #St.Items(), 1)
check("ventana 3: cuenta", St.MarkShared(seq, 3) == true)
eq("tras la tercera ventana: sigue en la bandeja (sin tope de envios)", #St.Items(), 1)
eq("tras la tercera ventana: cuenta 3", St.Items()[1].shares, 3)
eq("seq desconocido: sin efecto", (St.MarkShared(999999, 1)), false)
eq("sin tope de envios: MAX_SHARES ya no existe", St.MAX_SHARES, nil)
-- Caducidad: un item de hace mas de 7 dias sale al siguiente alta; uno con hora 0 no caduca.
CLOCK = 1000
St.Learn(9, { "npcs", 78 }, { npcID = 78, name = "Viejo" }, 1000)
St.Learn(9, { "npcs", 79 }, { npcID = 79, name = "Sin hora" }, 0)
CLOCK = 1000 + St.OUTBOX_MAX_AGE + 1
St.Learn(9, { "npcs", 80 }, { npcID = 80, name = "Nuevo" }, CLOCK)
local ids = {}
for _, it in ipairs(St.Items()) do ids[#ids + 1] = it.payload.p[2] end
check("caducidad: el item de hace 7 dias y 1 s sale", (function()
    for _, id in ipairs(ids) do if id == 78 then return false end end
    return true
end)(), table.concat(ids, ","))
check("caducidad: el item sin hora no caduca", (function()
    for _, id in ipairs(ids) do if id == 79 then return true end end
    return false
end)(), table.concat(ids, ","))
check("caducidad: el item nuevo entra", ids[#ids] == 80)
CLOCK = 0

-- ---------------------------------------------------------------------------
outln("(e) Topes: items y bytes (se descarta lo mas antiguo)")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
local savedMax = St.MAX_ITEMS
St.MAX_ITEMS = 5
for i = 1, 8 do St.Learn(9, { "npcs", 100 + i }, { npcID = 100 + i, name = "PNJ " .. i }) end
eq("tope de items: 5 en la bandeja", #St.Items(), 5)
eq("tope de items: se quedan los 5 mas nuevos", St.Items()[1].payload.p[2], 104)
St.MAX_ITEMS = savedMax
-- Tope de bytes (serializados): tres items caben; el cuarto, de igual tamano, expulsa al mas antiguo.
local savedBytes = St.MAX_SV_BYTES
St.Clear()
for i = 1, 3 do St.Learn(9, { "npcs", 800 + i }, { npcID = 800 + i, name = string.rep("n", 200) }) end
local _, three = St.Stats()
St.MAX_SV_BYTES = three + 100
eq("tope de bytes: tres items caben", #St.Items(), 3)
St.Learn(9, { "npcs", 900 }, { npcID = 900, name = string.rep("n", 200) })
local n2, bytes2 = St.Stats()
check("tope de bytes: no se supera", bytes2 <= St.MAX_SV_BYTES, string.format("%d > %d", bytes2, St.MAX_SV_BYTES))
eq("tope de bytes: sale solo el mas antiguo", n2, 3)
check("tope de bytes: el registro nuevo entra", St.Items()[n2].payload.p[2] == 900)
check("tope de bytes: el primero ya no esta", St.Items()[1].payload.p[2] == 802)
-- El total es la suma de los tamanos serializados de los items (tambien tras una baja por ventanas).
local function sumSerialized()
    local s = 0
    for _, it in ipairs(St.Items()) do s = s + #St.Serialize(it) end
    return s
end
eq("bytes = suma de los items serializados", select(2, St.Stats()), sumSerialized())
St.MarkShared(St.Items()[1].seq, 1)
eq("tras contar una ventana, el total sigue cuadrando", select(2, St.Stats()), sumSerialized())
St.MAX_SV_BYTES = savedBytes

-- ---------------------------------------------------------------------------
outln("(f) Limpieza: /compa limpiar si")
local before9 = St.Stats()
local removed = St.Clear()
eq("limpiar: devuelve cuantos habia", removed, before9)
eq("limpiar: bandeja vacia", #St.Items(), 0)
eq("limpiar: el contador de registros sigue creciendo", (St.Learn(9, { "npcs", 1 }, { npcID = 1, name = "nuevo" }) or 0) > 0, true)
local calls = 0
St.OnClear = function() calls = calls + 1 end
St.Clear()
eq("limpiar: avisa al canal (OnClear)", calls, 1)
St.OnClear = nil

-- ---------------------------------------------------------------------------
outln("(g) Enrutado: solo lo persistente va a SavedVariables")
CompaneroWoWDB = { settings = {}, characters = {} }
St.Attach()
local key = "Tester-Realm"
CompaneroWoWDB.characters[key] = {}
local ch = CompaneroWoWDB.characters[key]
ch.quests = { 1, 2, 3 }
ch.ask = { lastId = 5 }
ch.character = { name = "Tester" }
eq("enrutado: el estado de juego se lee", ch.quests[2], 2)
check("enrutado: el estado de juego no es raw", rawOf(ch, "quests") == nil)
check("enrutado: ask es raw (persistente)", rawOf(ch, "ask") ~= nil and rawOf(ch, "ask").lastId == 5)
check("enrutado: character es raw (persistente)", rawOf(ch, "character") ~= nil)
ch.quests = nil
eq("enrutado: borrar un campo de memoria", ch.quests, nil)
ch.flag = false
eq("enrutado: un false de memoria se conserva", ch.flag, false)
check("enrutado: knowledge es memoria (no raw)", rawOf(CompaneroWoWDB, "knowledge") == nil and type(CompaneroWoWDB.knowledge) == "table")
check("enrutado: world es memoria (no raw)", rawOf(CompaneroWoWDB, "world") == nil)
local sizeMem = #St.Serialize(CompaneroWoWDB)
check("enrutado: lo de memoria no cuenta en el tamano guardado", sizeMem < 400, tostring(sizeMem))

-- ---------------------------------------------------------------------------
outln("(h) Personaje realista: tamano guardado < 150 KB")
-- Datos de un personaje con uso normal: 10 respuestas de ~1,5 KB, sesion con 5 anteriores,
-- copias de barras y macros (3 cada una), y una bandeja con un mes de conocimiento, mundo,
-- combates y hojas. Los valores son estimaciones de tamano, no datos reales del juego.
local function text(n, seed)
    local t = {}
    for i = 1, n do t[i] = string.char(97 + ((seed + i * 7) % 26)) end
    return table.concat(t)
end
local function sessionSummary(i)
    local samples, quests, zones = {}, {}, {}
    for k = 1, 60 do samples[k] = { t = 1700000000 + k * 60, xp = k * 120, mapID = 1429 } end
    for k = 1, 40 do quests[k] = 9000 + k end
    for k = 1, 12 do zones[k] = { mapID = 1400 + k, name = "Zona " .. k, secs = 300 * k } end
    return { id = i, startedAt = 1700000000 + i, endedAt = 1700003600 + i, xp = 50000, kills = 120,
        deaths = 1, money = 12345, quests = quests, zones = zones, samples = samples, recentKills = {} }
end
local function realisticDB(withOutbox)
    local answers = {}
    for i = 1, 10 do
        answers[i] = { id = i, provider = "modelo", at = 1700000000 + i, blocks = {
            { kind = "para", segs = { { text = text(1500, i), bold = false } } } } }
    end
    local previous = {}
    for i = 1, 5 do previous[i] = sessionSummary(i) end
    local bars = {}
    for i = 1, 3 do
        local slots = {}
        for s = 1, 72 do slots[s] = { kind = "spell", id = 2000 + s, name = "Hechizo " .. s } end
        bars[i] = { at = 1700000000 + i, slots = slots }
    end
    local macroBackup = {}
    for i = 1, 3 do
        macroBackup[i] = { at = 1700000000 + i, list = {} }
        for m = 1, 10 do macroBackup[i].list[m] = { name = "Macro " .. m, icon = "INV_Misc_QuestionMark", body = text(200, m) } end
    end
    local db = {
        schema = 2, settings = { strip = true }, maps = {},
        characters = { ["Tester-Realm"] = {
            character = { name = "Tester", realm = "Realm", race = "Human", class = "WARRIOR", level = 60,
                faction = "Alliance", money = 123456 },
            updatedAt = 1700000000, loggedOutAt = 1700000000,
            ask = { lastId = 10, lastShown = 10, questions = {}, answers = answers },
            session = sessionSummary(6), sessionBase = previous,
            actionBarsBackup = bars, macroBackup = macroBackup, combatSeq = 10,
            sent = { [1] = "1:2", [12] = "9:9" },
        } },
    }
    -- Estado de juego del personaje: en memoria (no se guarda). Su tamano no cuenta.
    db.characters["Tester-Realm"].quests = { { id = 1, text = text(4000, 3) } }
    db.characters["Tester-Realm"].inventory = { items = {} }
    db.characters["Tester-Realm"].sheet = { text = text(6000, 5) }
    return db
end
local function addOutbox(db, n)
    CompaneroWoWDB = db
    St.Sync()
    for i = 1, n do
        St.Learn(9, { "npcs", 1000 + i }, { npcID = 1000 + i, name = "PNJ " .. i, mapID = 1429, x = 0.3, y = 0.4,
            reaction = 4, isQuestGiver = true, services = { "Instruccion" }, firstSeen = 1700000000 })
    end
    for i = 1, math.floor(n / 4) do
        St.Learn(16, { "vendors", 2000 + i }, { npcID = 2000 + i, name = "Vendedor " .. i,
            items = { { itemID = 3000 + i, name = "Articulo", price = 150, stackCount = 1, numAvailable = -1 } } })
    end
    for i = 1, 10 do
        St.Learn(7, { "combats", i }, { id = i, t = 1700000000 + i, zone = "Elwynn", duration = 40,
            damageDone = { total = 3000 + i, targets = { { name = "Lobo", dmg = 3000 } } }, died = false })
    end
    return db
end
local typical = addOutbox(realisticDB(), 250)
CompaneroWoWDB = typical
St.Sync()
local nItems, bytesItems = St.Stats()
local typicalSize = St.SavedBytes()
outln(string.format("  info  esquema 2: %d bytes guardados (%.1f KB), bandeja %d elementos (%.1f KB)",
    typicalSize, typicalSize / 1024, nItems, bytesItems / 1024))
do
    -- Desglose: lo del personaje (respuestas, sesion, copias), la bandeja (serializada) y el resto.
    local charBytes = #St.Serialize(CompaneroWoWDB.characters)
    local outboxBytes = #St.Serialize(CompaneroWoWDB.outbox)
    outln(string.format("  info  desglose: personaje %.1f KB, bandeja serializada %.1f KB, resto %.1f KB",
        charBytes / 1024, outboxBytes / 1024, (typicalSize - charBytes - outboxBytes) / 1024))
end
check("personaje realista: menos de 150 KB guardados", typicalSize < 150 * 1024, string.format("%.1f KB", typicalSize / 1024))
-- Lo mismo con el esquema 1 (bloques de estado y conocimiento sin bandeja), para comparar.
local legacySize = 0
do
    local db1 = realisticDB()
    local ch1 = db1.characters["Tester-Realm"]
    ch1.quests = { { id = 1, text = text(4000, 3) } }
    ch1.inventory = { items = {} }
    ch1.sheet = { text = text(6000, 5) }
    ch1.questie = { data = text(30000, 9) }
    ch1.gearCandidates = { text(9000, 4) }
    ch1.combats = {}
    for i = 1, 10 do ch1.combats[i] = { id = i, t = 1700000000 + i, zone = "Elwynn", duration = 40, damageDone = { total = 3000 } } end
    db1.knowledge = { quests = {}, npcs = {} }
    for i = 1, 2000 do
        db1.knowledge.npcs[1000 + i] = { npcID = 1000 + i, name = "PNJ " .. i, mapID = 1429, x = 0.3, y = 0.4,
            reaction = 4, isQuestGiver = true, services = { "Instruccion" }, firstSeen = 1700000000, lastSeen = 1700000000 }
    end
    db1.world = { vendors = {} }
    for i = 1, 800 do
        db1.world.vendors[2000 + i] = { npcID = 2000 + i, name = "Vendedor " .. i,
            items = { { itemID = 3000 + i, name = "Articulo", price = 150, stackCount = 1, numAvailable = -1 } } }
    end
    legacySize = #St.Serialize(db1)
    outln(string.format("  info  esquema 1 (referencia): %.1f KB con 2000 PNJ y 800 vendedores", legacySize / 1024))
end
check("personaje realista: el esquema 2 pesa menos que el esquema 1", typicalSize < legacySize)

-- Tope de la bandeja (60 KB serializados) incluso con mucho aprendizaje: no pasa de 2000 items ni de 60 KB.
CompaneroWoWDB = realisticDB()
St.Sync()
for i = 1, 2500 do
    St.Learn(9, { "npcs", 5000 + i }, { npcID = 5000 + i, name = "PNJ " .. i, mapID = 1429, x = 0.3, y = 0.4,
        reaction = 4, isQuestGiver = true, services = { "Instruccion" }, firstSeen = 1700000000 })
end
local nMax, bMax = St.Stats()
check("tope: como mucho 2000 items", nMax <= St.MAX_ITEMS, tostring(nMax))
check("tope: como mucho 60 KB serializados de bandeja", bMax <= St.MAX_SV_BYTES, tostring(bMax))
check("tope: lo mas nuevo se conserva", St.Items()[nMax].payload.p[2] == 7500)

-- ---------------------------------------------------------------------------
outln("(j) Estado actual (characters[k].state, esquema 2.1)")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
local function attachedChar(key, fields)
    local c = {}
    for k, v in pairs(fields) do c[k] = v end
    CompaneroWoWDB.characters[key] = c
    return St.AttachChar(c)
end
local sheetWithTooltips = {
    level = 25, class = "WARRIOR", race = "HUMAN", exportedAt = 99, total = { str = 150, crit = 12.5 },
    talents = { tabs = { { name = "Armas", points = 31 } } },
    spells = {
        { spellID = 100, name = "Golpe heroico", castTime = 0, cost = 15, powerType = "RAGE",
          tooltip = string.rep("Texto largo del hechizo. ", 40) },
        { spellID = 101, name = "Carga", cooldown = 15, range = 25, tooltip = "Carga al enemigo: 12 de dano." },
    },
}
local ch = attachedChar("Tester-Realm", { character = { name = "Tester", level = 25 }, updatedAt = 1 })
ch.sheet = sheetWithTooltips
ch.equipment = { [1] = { slotID = 1, itemID = 2, link = "|Hitem:2|h|r", itemLevel = 40 } }
ch.location = { mapID = 1429, zone = "Elwynn" }
ch.training = { available = true, source = "WhatsTraining", spells = { 3 } }
local wrote = St.SaveState(ch)
check("estado: se escribe al cambiar", wrote == true)
check("estado: va a SavedVariables (raw)", rawOf(ch, "state") ~= nil)
check("estado: la hoja esta", ch.state.sheet ~= nil and ch.state.sheet.level == 25)
check("estado: la hoja no lleva exportedAt", ch.state.sheet.exportedAt == nil)
check("estado: los hechizos no llevan tooltip", ch.state.sheet.spells[1].tooltip == nil and ch.state.sheet.spells[2].tooltip == nil)
eq("estado: los numeros del hechizo se quedan (coste)", ch.state.sheet.spells[1].cost, 15)
eq("estado: los numeros del hechizo se quedan (enfriamiento)", ch.state.sheet.spells[2].cooldown, 15)
eq("estado: el nombre del hechizo (espanol) se queda", ch.state.sheet.spells[2].name, "Carga")
eq("estado: el equipo", ch.state.equipment[1].itemID, 2)
eq("estado: la ubicacion", ch.state.location.zone, "Elwynn")
eq("estado: entrenamiento", ch.state.training.spells[1], 3)
check("estado: la hoja de memoria no cambia (copia)", sheetWithTooltips.spells[1].tooltip ~= nil)
check("estado: sello de hora", type(ch.state.at) == "number")
check("estado: sin cambios no escribe", St.SaveState(ch) == false)
-- Una lectura vacia o ausente no borra lo guardado.
ch.equipment = {}
ch.location = nil
St.SaveState(ch)
eq("estado: equipo vacio no sustituye al anterior", ch.state.equipment[1].itemID, 2)
eq("estado: ubicacion ausente conserva la anterior", ch.state.location.zone, "Elwynn")
-- Presupuesto: un bloque enorme no se guarda entero (se quitan secciones y, al final, hechizos).
ch.profPlan = { steps = {} }
for i = 1, 800 do ch.profPlan.steps[i] = { text = string.rep("paso " .. i .. " ", 20) } end
St.SaveState(ch)
check("presupuesto: el estado cabe en STATE_MAX_BYTES", #St.Serialize(ch.state) <= St.STATE_MAX_BYTES, tostring(#St.Serialize(ch.state)))
check("presupuesto: se quita antes el plan de profesion que la hoja", ch.state.profPlan == nil and ch.state.sheet ~= nil)
-- Otros personajes: con el fichero por encima de FILE_TARGET_BYTES, su estado queda en el nucleo.
local other = attachedChar("Otro-Realm", { character = { name = "Otro" }, updatedAt = 0 })
other.sheet = sheetWithTooltips
St.SaveState(other)
check("otros: antes de recortar, el estado tiene la hoja completa", other.state.sheet.spells ~= nil)
local savedTarget = St.FILE_TARGET_BYTES
St.FILE_TARGET_BYTES = 1000
ch.location = { mapID = 1429, zone = "Elwynn", x = 0.5 }
St.SaveState(ch)
St.FILE_TARGET_BYTES = savedTarget
check("otros: el personaje recortado queda en el nucleo", other.state.sheet ~= nil and other.state.sheet.spells == nil and other.state.sheet.level == 25)
check("otros: el personaje activo no se recorta", ch.state.sheet.spells ~= nil)
-- Migracion desde schema 2 (sin schemaMinor): la hoja de la bandeja pasa al personaje activo.
CompaneroWoWDB = { schema = 2, settings = {}, outbox = { seq = 3, win = 4, items = {
    { seq = 1, msgType = 12, at = 500, shares = 1, payload = { oseq = 1, p = { "sheet", 1 }, v = { lv = 20, class = "WARRIOR", spells = { { spellID = 5, n = "Vieja", tooltip = "x" } } } } },
    { seq = 2, msgType = 12, at = 900, shares = 1, payload = { oseq = 2, p = { "sheet", 2 }, v = { lv = 21, class = "WARRIOR", spells = { { spellID = 6, n = "Nueva" } } } } },
    { seq = 3, msgType = 9, at = 901, shares = 0, payload = { oseq = 3, p = { "npcs", 7 }, v = { pn = 7, n = "Guardia" } } },
} }, characters = {
    Viejo = { character = { name = "Viejo" }, updatedAt = 100 },
    Activo = { character = { name = "Activo" }, updatedAt = 800, loggedOutAt = 850 },
} }
St.Sync()
local dbm = CompaneroWoWDB
check("migracion 2.1: schemaMinor = 1", dbm.schemaMinor == 1)
check("migracion 2.1: sin hojas en la bandeja", (function()
    for _, it in ipairs(St.Items()) do if it.msgType == 12 then return false end end
    return true
end)())
check("migracion 2.1: el conocimiento se queda en la bandeja", #St.Items() == 1 and St.Items()[1].msgType == 9)
local act = dbm.characters.Activo
check("migracion 2.1: la hoja mas reciente pasa al personaje activo", act.state ~= nil and act.state.sheet ~= nil and act.state.sheet.level == 21)
check("migracion 2.1: la hoja pasa con claves largas", act.state.sheet.spells ~= nil and act.state.sheet.spells[1].name == "Nueva")
check("migracion 2.1: el personaje antiguo no recibe hoja", dbm.characters.Viejo.state == nil)
St.Sync()
check("migracion 2.1: no se repite al volver a cargar", dbm.schemaMinor == 1 and act.state.sheet.level == 21)

-- ---------------------------------------------------------------------------
outln("(k) Estado: ventana de misiones, candidatos, mazmorras y resumen de Questie")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
local function countKeys(t)
    local n = 0
    for _ in pairs(t or {}) do n = n + 1 end
    return n
end
-- Un personaje con la ventana completa: 300 misiones (200 accesibles), 200 candidatos y 6 mazmorras.
local questsIn = {}
for i = 1, 300 do
    local id = 10000 + i
    questsIn[id] = {
        questID = id, name = "Mision " .. i, level = 20 + (i % 9), requiredLevel = 18 + (i % 5),
        zone = "Zona " .. (i % 12), accessibleNow = (i % 3 ~= 0), source = "questie", inLog = false,
        missingPrereqs = (i % 3 == 0) and { 9000 + i } or nil,
        starter = { kind = "npc", id = 1000 + i, name = "PNJ " .. i, mapID = 1429, x = 40.1234, y = 55.5 },
        finisher = { kind = "object", id = 2000 + i, name = "Objeto " .. i, mapID = 1429, x = 41.1, y = 56.2 },
        rewards = { choice = { 5000 + i }, fixed = { 6000 + i }, rewardsKnownAs = "questie-reverse" },
    }
end
local itemsIn = {}
for i = 1, 200 do
    itemsIn[3000 + i] = {
        itemID = 3000 + i, name = "Objeto de prueba " .. i, itemLevel = 20 + (i % 40), requiredLevel = 20,
        quality = 3, equipLoc = "INVTYPE_CHEST", class = "Armor", subclass = "Mail",
        stats = { ITEM_MOD_STRENGTH_SHORT = 5 }, sources = { { kind = "quest", questID = 10000 + i, choice = true, source = "questie" } },
    }
end
local dungeonsIn = { available = true, exportedAt = 7, sources = { fdj = true }, list = {} }
for d = 1, 6 do
    dungeonsIn.list[d] = { key = "d" .. d, name = "Mazmorra " .. d, minLevel = 20, maxLevel = 30, source = "fdj", levelsKnown = true, quests = { 1, 2 }, bosses = { { name = "Jefe", loot = { 3001 } } } }
end
local qch = attachedChar("Quest-Realm", { character = { name = "Quest", level = 25 }, updatedAt = 1 })
qch.questWindow = { available = true, exportedAt = 7, level = 25, minLevel = 20, maxLevel = 28, quests = questsIn, sources = { questie = true } }
qch.gearCandidates = { available = true, exportedAt = 7, items = itemsIn, sources = { questie = true } }
qch.dungeons = dungeonsIn
qch.sheet = { level = 25, class = "WARRIOR", spells = { { spellID = 1, name = "Golpe", cost = 15 } } }
qch.completedQuests = "10001,10002,10010"
qch.questie = { available = true, version = "12.10.0", exportedAt = 7, quests = { [1] = { questID = 1 } }, zoneQuests = { list = { 1, 2 } }, npcs = { 1, 2, 3 }, sources = { questie = true } }
St.SaveState(qch)
check("ventana: va al estado", qch.state.questWindow ~= nil and qch.state.questWindow.available == true)
local qw = St.Expand(qch.state.questWindow)
eq("ventana: 120 misiones como mucho (STATE_QUESTS)", countKeys(qw.quests), 120)
eq("ventana: las 120 son accesibles ahora (hay 200)", (function()
    local n = 0
    for _, q in pairs(qw.quests) do if q.accessibleNow == true then n = n + 1 end end
    return n
end)(), 120)
check("ventana: la mision del mismo nivel entra", qw.quests[10005] ~= nil and qw.quests[10005].level == 25)
eq("ventana: nombre expandido", qw.quests[10005].name, "Mision 5")
eq("ventana: id del PNJ inicial", qw.quests[10005].starter.id, 1005)
eq("ventana: recompensa elegible (ids)", qw.quests[10005].rewards.choice[1], 5005)
check("ventana: coordenada a 3 decimales", math.abs(qw.quests[10005].starter.x - 40.123) < 1e-9, tostring(qw.quests[10005].starter.x))
check("ventana: el PNJ npc no lleva tipo", qw.quests[10005].starter.kind == nil)
eq("ventana: el objeto lleva su tipo", qw.quests[10005].finisher.kind, "object")
check("ventana: la clave corta va en el estado (qd)", qch.state.questWindow.quests[10005].qd == 10005)
local cd = St.Expand(qch.state.gearCandidates)
eq("candidatos: 80 como mucho (STATE_CANDIDATES), los de mayor nivel", countKeys(cd.items), 80)
local minLv = 999
for _, it in pairs(cd.items) do if it.itemLevel < minLv then minLv = it.itemLevel end end
eq("candidatos: el mas bajo de los 80 es el nivel 44", minLv, 44)
eq("candidatos: stats sin cambios", cd.items[3064].stats.ITEM_MOD_STRENGTH_SHORT, 5)
eq("candidatos: origen expandido (questID)", cd.items[3064].sources[1].questID, 10064)
eq("mazmorras: se guardan tal cual", qch.state.dungeons.list[1].name, "Mazmorra 1")
check("questieStatus: instalado, con version y conteos", qch.state.questieStatus.available == true
    and qch.state.questieStatus.version == "12.10.0" and qch.state.questieStatus.quests == 1
    and qch.state.questieStatus.zoneQuests == 2 and qch.state.questieStatus.npcs == 3)
check("questieStatus: solo el resumen, sin la exportacion completa", qch.state.questieStatus.quests == 1 and qch.state.questieStatus.zoneQuests ~= nil and qch.state.questieStatus.exportedAt == 7)
check("presupuesto: el estado con ventana, candidatos, mazmorras y hoja cabe", #St.Serialize(qch.state) <= St.STATE_MAX_BYTES,
    tostring(#St.Serialize(qch.state)))
check("presupuesto: con todo cabiendo, la hoja y la ventana se quedan", qch.state.sheet ~= nil and qch.state.questWindow ~= nil)
eq("completadas: el texto de ids va al estado", qch.state.completedQuests, "10001,10002,10010")
qch.completedQuests = ""
St.SaveState(qch)
eq("completadas: un texto vacio no sustituye al anterior", qch.state.completedQuests, "10001,10002,10010")
-- Questie no instalado: el dato dice available = false (la app solo sugiere instalarlo entonces).
qch.questie = { available = false, reason = "Questie no está instalado (opcional)", sources = { juego = false } }
St.SaveState(qch)
check("questieStatus: no instalado lo dice", qch.state.questieStatus.available == false and qch.state.questieStatus.version == nil)
-- Una exportacion que fallo (sin motivo) no dice que Questie falte: la seccion no cambia.
local failed = attachedChar("Failed-Realm", { character = { name = "Failed", level = 10 }, updatedAt = 1 })
failed.questie = { available = false }
St.SaveState(failed)
check("questieStatus: una exportacion fallida no dice que falte", failed.state == nil or failed.state.questieStatus == nil)
-- Orden de recorte: con poco sitio caen primero las secciones de la izquierda (mazmorras, candidatos, ventana),
-- y el resumen de Questie y la hoja se quedan.
local savedMax = St.STATE_MAX_BYTES
St.STATE_MAX_BYTES = 12 * 1024
local small = attachedChar("Small-Realm", { character = { name = "Small", level = 25 }, updatedAt = 1 })
small.questWindow = qch.questWindow
small.gearCandidates = qch.gearCandidates
small.dungeons = qch.dungeons
small.questie = { available = true, version = "12.10.0", quests = {}, zoneQuests = { list = {} }, npcs = {} }
small.sheet = { level = 25, class = "WARRIOR", spells = { { spellID = 1, name = "Golpe" } } }
St.SaveState(small)
St.STATE_MAX_BYTES = savedMax
check("recorte: la hoja se queda", small.state.sheet ~= nil and small.state.sheet.level == 25)
check("recorte: las mazmorras caen primero", small.state.dungeons == nil)
check("recorte: los candidatos caen antes que la ventana", small.state.gearCandidates == nil)
check("recorte: el resumen de Questie se queda", small.state.questieStatus ~= nil and small.state.questieStatus.available == true)
-- Lo que no ha cambiado no se reescribe; una exportacion de Questie no disponible no tira la buena.
local before = St.Serialize(qch.state)
check("estado: sin cambios no escribe (con vistas)", St.SaveState(qch) == false and St.Serialize(qch.state) == before)
local secs = St.STATE_SECTIONS
eq("orden: la hoja es la ultima", secs[#secs], "sheet")
check("orden: la ventana de misiones va antes que la hoja", (function()
    local at = {}
    for i, n in ipairs(secs) do at[n] = i end
    return at.questWindow < at.sheet and at.gearCandidates < at.sheet and at.dungeons < at.sheet and at.questieStatus < at.sheet
end)())

-- ---------------------------------------------------------------------------
outln("(i) Estado y serializacion")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
St.Learn(9, { "npcs", 1 }, { npcID = 1, name = "uno" })
local line = St.StatusLine()
check("estado: linea 'Datos guardados: N KB, bandeja: N elementos (N KB)'",
    string.match(line, "^Datos guardados: [%d%.]+ KB, bandeja: %d+ elementos %([%d%.]+ KB%)$") ~= nil, line)
local ser = St.Serialize({ a = 1, b = "x\ny", c = { true, false } })
check("serializa: tabla tipo SavedVariables", string.sub(ser, 1, 1) == "{" and string.find(ser, "[\"a\"]=1", 1, true) ~= nil)

outln("(j) Estado: barras y macros")
CompaneroWoWDB = { schema = 2, schemaMinor = 1, settings = {}, characters = {} }
St.Sync()
CompaneroWoWDB.characters["Bar-Realm"] = { updatedAt = 1 }
local bch = CompaneroWoWDB.characters["Bar-Realm"]
bch.actionBars = { exportedAt = 5, visibleBars = { 1 }, slots = { [52] = { slot = 52, kind = "macro", id = 121, name = "abrumar", key = "F" } } }
bch.macros = { exportedAt = 5, account = { used = 0, max = 120, list = {} },
    character = { used = 1, max = 18, list = { { index = 121, name = "abrumar", body = "/cast Abrumar" } } } }
St.SaveState(bch)
eq("estado: las barras se guardan", rawOf(bch, "state").actionBars.slots[52].name, "abrumar")
eq("estado: las macros se guardan con su cuerpo", rawOf(bch, "state").macros.character.list[1].body, "/cast Abrumar")
check("estado: el bloque de memoria de barras no va suelto a SavedVariables", rawOf(bch, "actionBars") == nil)
bch.actionBars = { available = false, reason = "sin API" }
St.SaveState(bch)
eq("estado: un bloque no disponible no sustituye a las barras guardadas", bch.state.actionBars.slots[52].id, 121)

-- Cuerpo de macro: tope de 255 bytes en el estado, sin partir un caracter UTF-8.
bch.actionBars = nil
bch.macros = { exportedAt = 6, account = { used = 1, max = 120, list = { { index = 1, name = "larga", body = string.rep("x", 400) } } },
    character = { used = 2, max = 18, list = { { index = 121, name = "abrumar", body = "/cast Abrumar" },
                                               { index = 122, name = "acentos", body = string.rep("x", 254) .. "ñandú" } } } }
St.SaveState(bch)
eq("macros: el cuerpo se acota a 255", #bch.state.macros.account.list[1].body, 255)
eq("macros: un cuerpo corto no cambia", bch.state.macros.character.list[1].body, "/cast Abrumar")
eq("macros: no se parte un caracter UTF-8", bch.state.macros.character.list[2].body, string.rep("x", 254))
eq("macros: el bloque de memoria no cambia", #bch.macros.account.list[1].body, 400)
-- Recorte: muchas macros largas no echan a las misiones ni al equipo; lo nuevo (0.2.0) cae antes.
local at = {}
for i, n in ipairs(St.STATE_SECTIONS) do at[n] = i end
check("orden: macros y barras se recortan antes que misiones, equipo y ventana de misiones",
    at.macros < at.quests and at.macros < at.equipment and at.macros < at.questWindow and at.macros < at.training
    and at.actionBars < at.quests and at.actionBars < at.equipment and at.actionBars < at.questWindow)
check("orden: lo nuevo (bolsas, banco, recetas, monedas, hermandad, info) se recorta antes que las macros",
    at.bags < at.macros and at.bank < at.macros and at.recipes < at.macros and at.currencies < at.macros
    and at.guildRoster < at.macros and at.info < at.macros)
CompaneroWoWDB.characters["Mac-Realm"] = { updatedAt = 1 }
local mch = CompaneroWoWDB.characters["Mac-Realm"]
mch.sheet = { level = 30, class = "WARRIOR", spells = { { spellID = 1, name = "Golpe" } } }
mch.quests = { [7] = { questID = 7, title = "La mision" } }
mch.equipment = { [1] = { slotID = 1, itemID = 2 } }
mch.questWindow = { available = true, level = 30, quests = { [9] = { questID = 9, name = "Cercana", level = 30, accessibleNow = true } } }
mch.info = { hearth = "Ventormenta" }
mch.macros = { exportedAt = 7, account = { used = 100, max = 120, list = {} }, character = { used = 0, max = 18, list = {} } }
for i = 1, 100 do mch.macros.account.list[i] = { index = i, name = "m" .. i, body = string.rep("/cast Algo largo\n", 15) } end
local macroMax = St.STATE_MAX_BYTES
St.STATE_MAX_BYTES = 4000
St.SaveState(mch)
check("recorte: el estado cabe", #St.Serialize(mch.state) <= St.STATE_MAX_BYTES)
check("recorte: las macros largas caen", mch.state.macros == nil)
check("recorte: misiones, equipo, ventana y hoja se quedan", mch.state.quests ~= nil and mch.state.equipment ~= nil
    and mch.state.questWindow ~= nil and mch.state.sheet ~= nil)
St.STATE_MAX_BYTES = macroMax
-- Otros personajes recortados al nucleo: el banco y las recetas se quedan (solo se releen en el banco o
-- con la ventana de la profesion); el resto cae. Una seccion desmedida no se queda.
local function altChar(key, recipeNames)
    CompaneroWoWDB.characters[key] = { updatedAt = 1 }
    local a = CompaneroWoWDB.characters[key]
    a.sheet = { level = 20, class = "MAGE", spells = { { spellID = 1, name = "Bola" } } }
    a.location = { zone = "Elwynn" }
    a.inventory = { items = { { itemID = 2589, count = 3, name = "Tela de lino" } }, totalSlots = 16, freeSlots = 15 }
    a.bank = { items = { { itemID = 4306, count = 40, name = "Tela de seda", quality = 1 } }, totalSlots = 24, freeSlots = 23, at = 5 }
    local list = {}
    for i = 1, recipeNames do list[i] = string.format("Receta de nombre bastante largo %04d", i) end
    a.recipes = { { name = "Sastrería", rank = 150, max = 225, list = list, at = 5 } }
    St.SaveState(a)
    return a
end
local alt, altBig = altChar("Alt-Realm", 3), altChar("AltGrande-Realm", 250)
check("otros: antes de recortar tienen banco, recetas y bolsas", alt.state.bank ~= nil and alt.state.recipes ~= nil and alt.state.bags ~= nil)
local fileTarget, coreMax = St.FILE_TARGET_BYTES, St.CORE_SECTION_BYTES
St.FILE_TARGET_BYTES, St.CORE_SECTION_BYTES = 1000, 2000
mch.location = { zone = "Tanaris" }
St.SaveState(mch)
St.FILE_TARGET_BYTES, St.CORE_SECTION_BYTES = fileTarget, coreMax
check("otros: recortado al nucleo (sin bolsas, ubicacion ni hechizos)", alt.state.bags == nil and alt.state.location == nil
    and alt.state.sheet ~= nil and alt.state.sheet.spells == nil and alt.state.sheet.level == 20)
check("otros: el banco se queda", alt.state.bank ~= nil and St.Expand(alt.state.bank).items[1].itemID == 4306)
check("otros: las recetas se quedan", alt.state.recipes ~= nil and #alt.state.recipes[1].list == 3)
check("otros: unas recetas desmedidas no se quedan", altBig.state.recipes == nil and altBig.state.bank ~= nil)
check("otros: el activo no se recorta", mch.state.quests ~= nil and mch.state.location.zone == "Tanaris")

outln("(k) Personaje guardado sin reino")
local savedNorm, savedRealm, savedUnit = _G.GetNormalizedRealmName, _G.GetRealmName, _G.UnitName
_G.UnitName = function() return "Sinreino" end
_G.GetNormalizedRealmName = function() return "" end
_G.GetRealmName = function() return "Mi Reino" end
eq("reino: \"\" cae a GetRealmName (sin espacios)", ns.CharKey(), "Sinreino-MiReino")
_G.GetRealmName = function() return nil end
CompaneroWoWDB = { schema = 2, schemaMinor = 1, settings = {}, characters = {} }
St.Sync()
eq("sin reino y sin clave guardada: solo el nombre", ns.CharKey(), "Sinreino")
CompaneroWoWDB.characters["Sinreino-MiReino"] = { updatedAt = 10, combatSeq = 79, actionBarsBackup = { { at = 1 } },
    state = { at = 10, sheet = { level = 26 } } }
eq("sin reino: se usa la clave ya guardada de ese nombre", ns.CharKey(), "Sinreino-MiReino")
CompaneroWoWDB.characters["Sinreino"] = { updatedAt = 20, combatSeq = 3, state = { at = 20, sheet = { level = 27 } } }
check("union: se une el registro sin reino", St.AdoptRealmless(CompaneroWoWDB, "Sinreino", "Sinreino-MiReino") == true)
local merged = CompaneroWoWDB.characters["Sinreino-MiReino"]
check("union: el registro sin reino desaparece", rawOf(CompaneroWoWDB.characters, "Sinreino") == nil)
eq("union: queda el estado mas reciente", merged.state.sheet.level, 27)
eq("union: lo que solo tenia el antiguo se conserva", merged.actionBarsBackup[1].at, 1)
eq("union: el contador de combates mas alto", merged.combatSeq, 79)
check("union: sin registro sin reino no hace nada", St.AdoptRealmless(CompaneroWoWDB, "Sinreino", "Sinreino-MiReino") == false)
_G.GetNormalizedRealmName, _G.GetRealmName, _G.UnitName = savedNorm, savedRealm, savedUnit

outln(failures == 0 and "ALL TESTS PASSED" or ("FAILED: " .. failures .. " check(s)"))
if failures > 0 then os.exit(1) end
