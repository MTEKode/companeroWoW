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
for i = 1, 400 do ch.profPlan.steps[i] = { text = string.rep("paso " .. i .. " ", 20) } end
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
outln("(i) Estado y serializacion")
CompaneroWoWDB = { schema = 2, settings = {}, characters = {} }
St.Sync()
St.Learn(9, { "npcs", 1 }, { npcID = 1, name = "uno" })
local line = St.StatusLine()
check("estado: linea 'Datos guardados: N KB, bandeja: N elementos (N KB)'",
    string.match(line, "^Datos guardados: [%d%.]+ KB, bandeja: %d+ elementos %([%d%.]+ KB%)$") ~= nil, line)
local ser = St.Serialize({ a = 1, b = "x\ny", c = { true, false } })
check("serializa: tabla tipo SavedVariables", string.sub(ser, 1, 1) == "{" and string.find(ser, "[\"a\"]=1", 1, true) ~= nil)

outln(failures == 0 and "ALL TESTS PASSED" or ("FAILED: " .. failures .. " check(s)"))
if failures > 0 then os.exit(1) end
