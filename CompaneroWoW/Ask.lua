-- Ask.lua: preguntas a Compañero desde el marco del juego (T40). Contrato in-game (ver Markup.lua):
--   - Enviar: mensaje 15 "question" { id, text, at, mapID, target } por el canal de píxeles, con la
--     prioridad máxima (Channel.lua, C.SendQuestion). El id crece por personaje y se guarda en
--     CompaneroWoWDB.characters[clave].ask. Abre la ventana de envío (los datos viajan también).
--   - Recibir: la respuesta llega por el portapapeles (Ctrl+V en el marco); Markup.Parse la lee y
--     Ask.Receive la guarda (últimas 10, con su id). Una respuesta con id anterior a la última mostrada se ignora.
--   - Acciones de la respuesta: marca (Marks.Set), barras (Bars.Apply) y macro (Macros.Create), las mismas
--     funciones que los comandos /compa. Fuera de combate; en combate no hacen nada.
local ADDON_NAME, ns = ...
ns = ns or {}

local Call = ns.Call

local Ask = {}
ns.Ask = Ask

Ask.MAX_QUESTION = 500   -- caracteres de una pregunta (contrato)
Ask.KEEP = 10            -- preguntas y respuestas guardadas por personaje
Ask.COMBAT_MSG = "En combate no se puede hacer esto. Inténtalo al salir de combate."
Ask.pending = nil        -- id de la pregunta enviada y aun sin respuesta (para el aviso «Pensando…»)

local function say(msg)
    if ns.Say then ns.Say(msg) end
end

-- Almacen de la clave del personaje actual. create: lo crea si falta. nil sin personaje.
local function store(create)
    if type(CompaneroWoWDB) ~= "table" then return nil end
    local key = ns.CharKey and ns.CharKey()
    if not key then return nil end
    if type(CompaneroWoWDB.characters) ~= "table" then
        if not create then return nil end
        CompaneroWoWDB.characters = {}
    end
    local c = CompaneroWoWDB.characters[key]
    if type(c) ~= "table" then
        if not create then return nil end
        c = {}
        CompaneroWoWDB.characters[key] = c
    end
    if type(c.ask) ~= "table" then
        if not create then return nil end
        c.ask = {}
    end
    local a = c.ask
    a.lastId = tonumber(a.lastId) or 0
    a.lastShown = tonumber(a.lastShown) or 0
    if type(a.questions) ~= "table" then a.questions = {} end
    if type(a.answers) ~= "table" then a.answers = {} end
    return a
end
Ask.Store = store

-- Quita saltos de linea y caracteres de control, y los espacios de los extremos.
function Ask.Clean(text)
    local s = string.gsub(tostring(text or ""), "%c", " ")
    return (string.match(s, "^%s*(.-)%s*$"))
end

-- Objetivo actual (nombre) o nil.
local function targetName()
    if Call("UnitExists", "target") ~= true then return nil end
    return ns.Str(Call("UnitName", "target"))
end

-- Envia una pregunta. Devuelve true y el id, o false y nada (con el motivo en el chat).
function Ask.Send(text)
    local s = Ask.Clean(text)
    if s == "" then
        say("Escribe una pregunta primero: /compa ? <texto>")
        return false
    end
    local len = ns.Markup and ns.Markup.CharLen(s) or #s
    if len > Ask.MAX_QUESTION then
        say(string.format("La pregunta tiene %d caracteres (máximo %d).", len, Ask.MAX_QUESTION))
        return false
    end
    if not ns.IsStripOn() then
        say("Compartir datos está desactivado. Usa /compa strip on para preguntar.")
        return false
    end
    local a = store(true)
    if not a then
        say("Sin personaje activo: no se puede enviar la pregunta.")
        return false
    end
    local C, Sh = ns.Channel, ns.Share
    if not (C and Sh and ns.Json) then
        say("Canal no disponible: no se puede enviar la pregunta.")
        return false
    end
    local mapID = ns.GetPlayerPosition and ns.GetPlayerPosition() or nil
    local msg = {
        id = a.lastId + 1,
        text = s,
        at = ns.Now(),
        mapID = ns.Num(mapID),
        target = targetName(),
    }
    local json = ns.Json.Encode(msg)
    if not json then
        say("La pregunta no se pudo codificar.")
        return false
    end
    -- Primero la ventana (Begin reinicia el plan); luego la pregunta, que va delante de todo.
    if not Sh.Activate(false) then
        say("No se puede enviar: no hay franja de píxeles.")
        return false
    end
    local now = ns.Num(Call("GetTime")) or ns.Now()
    C.SendQuestion(json, now)
    a.lastId = msg.id
    a.questions[#a.questions + 1] = s
    while #a.questions > Ask.KEEP do table.remove(a.questions, 1) end
    Ask.pending = msg.id
    if ns.UI and ns.UI.OnQuestionSent then ns.SafeCall(ns.UI.OnQuestionSent, msg.id) end
    return true, msg.id
end

-- Preguntas guardadas, la mas reciente primero (para Arriba/Abajo en el campo).
function Ask.History()
    local a = store(false)
    local out = {}
    if not a then return out end
    for i = #a.questions, 1, -1 do out[#out + 1] = a.questions[i] end
    return out
end

-- Respuestas guardadas, de la mas antigua a la mas reciente.
function Ask.Answers()
    local a = store(false)
    return a and a.answers or {}
end

-- Id de la ultima respuesta mostrada (0 si ninguna).
function Ask.LastShown()
    local a = store(false)
    return a and a.lastShown or 0
end

-- Copia de la respuesta que se guarda en SavedVariables: bloques hasta ANSWER_MAX_BYTES de texto
-- (si no hay bloques, el cuerpo cortado a ese tope). La respuesta mostrada no cambia.
function Ask.StoredCopy(ans)
    local limit = (ns.Storage and ns.Storage.ANSWER_MAX_BYTES) or 2048
    local out = { id = ans.id, provider = ans.provider, at = ans.at, truncated = ans.truncated }
    local used, blocks = 0, {}
    for _, b in ipairs(ans.blocks or {}) do
        local size = #(b.text or "") + #(b.args or "") + #(b.name or "")
        for _, s in ipairs(b.segs or {}) do size = size + #(s.text or "") end
        if used + size > limit then
            out.truncated = true
            break
        end
        used = used + size
        blocks[#blocks + 1] = b
    end
    if #blocks > 0 then
        out.blocks = blocks
    elseif type(ans.body) == "string" then
        out.body = string.sub(ans.body, 1, limit)
    end
    return out
end

-- Pega en el marco: parsea y guarda la respuesta. Devuelve la respuesta, o nil y el motivo.
function Ask.Receive(text)
    local ans, err = ns.Markup.Parse(text)
    if not ans then return nil, err end
    local a = store(true)
    if not a then return nil, "Sin personaje activo: no se puede guardar la respuesta." end
    if ans.id < a.lastShown then
        return nil, string.format("Esa respuesta es de una pregunta anterior (#%d). Se ignora.", ans.id)
    end
    ans.at = ns.Now()
    local n = #a.answers
    local saved = Ask.StoredCopy(ans)
    if ans.id == a.lastShown and n > 0 and a.answers[n].id == ans.id then
        a.answers[n] = saved -- la misma pregunta otra vez: reemplaza, no duplica
    else
        a.answers[n + 1] = saved
    end
    while #a.answers > Ask.KEEP do table.remove(a.answers, 1) end
    a.lastShown = ans.id
    if Ask.pending and ans.id >= Ask.pending then Ask.pending = nil end
    return ans
end

-- Ejecuta una accion de una respuesta. action: bloque { action = "marca"|"barras"|"macro", ... }.
-- En combate no hace nada y lo dice. Devuelve true si llamo a la funcion de la accion.
function Ask.Run(action)
    if type(action) ~= "table" then return false end
    if ns.InCombat() then
        say(Ask.COMBAT_MSG)
        return false
    end
    if action.action == "marca" then
        if not (ns.Marks and ns.Marks.Set) then
            say("Marcas no disponibles.")
            return false
        end
        -- Mismo camino que /compa marca <mapID> <x> <y> <texto> (x e y de 0 a 100 -> fraccion).
        ns.Marks.Set(action.mapID, action.x / 100, action.y / 100, action.text or "")
        return true
    elseif action.action == "barras" then
        if not (ns.Bars and ns.Bars.Apply) then
            say("Barras no disponibles.")
            return false
        end
        ns.Bars.Apply(action.args or "")
        return true
    elseif action.action == "macro" then
        if not (ns.Macros and ns.Macros.Create) then
            say("Macros no disponibles.")
            return false
        end
        ns.Macros.Create(action.args or "")
        return true
    end
    return false
end

-- Lineas de estado para /compa estado.
function Ask.StatusLine()
    local a = store(false)
    local last = a and a.lastId or 0
    local shown = a and a.lastShown or 0
    return string.format("Preguntas: última #%d, respuesta mostrada #%d", last, shown)
end

return Ask
