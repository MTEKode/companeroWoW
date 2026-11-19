-- UI.lua: marco "Compañero" dentro del juego (T40). Sin ventanas externas: el jugador escribe la pregunta
-- aqui, Compañero responde por el portapapeles y el jugador pulsa Ctrl+V en el campo de respuesta.
-- Partes: titulo, navegacion entre las ultimas respuestas ("‹ ›"), area de respuesta (texto con
-- negrita, viñetas, botones de accion y linea de fuente), campo de pregunta (Enter envia; Arriba/Abajo
-- recorren las ultimas 10 preguntas), campo de pegado oculto (captura Ctrl+V) y aviso «Pensando…».
-- Posicion y tamaño por personaje (characters[clave].ui). Escape cierra (UISpecialFrames).
-- La tecla de compartir (Share.lua) llama a UI.OnShareKey. /compa abre el marco (Core.lua).
local ADDON_NAME, ns = ...
ns = ns or {}

local Num, Method = ns.Num, ns.Method

local UI = {}
ns.UI = UI

local FRAME_NAME = "CompaneroWoWFrame"
local ANSWER_SCROLL = "CompaneroWoWAnswerScroll"
local QUESTION_NAME = "CompaneroWoWQuestion"
local PASTE_NAME = "CompaneroWoWPaste"
UI.MIN_W, UI.MIN_H = 320, 260
UI.DEF_W, UI.DEF_H = 420, 460
UI.PASTE_HINT = "Pensando… cuando oigas «Respuesta lista», pulsa Ctrl+V"
UI.EMPTY_TEXT = "Aún no hay respuestas. Escribe una pregunta abajo y pulsa Enter."

local ICONS = {
    marca = "Interface\\Icons\\INV_Misc_Map_01",
    barras = "Interface\\Icons\\INV_Misc_Book_09",
    macro = "Interface\\Icons\\INV_Misc_Note_01",
}
local LABELS = { marca = "Marcar", barras = "Colocar barras", macro = "Crear macro" }
local BACKDROP = {
    bgFile = "Interface\\DialogFrame\\UI-DialogBox-Background-Dark",
    edgeFile = "Interface\\DialogFrame\\UI-DialogBox-Border",
    tile = true, tileSize = 32, edgeSize = 32,
    insets = { left = 11, right = 12, top = 12, bottom = 11 },
}

UI.frame = nil     -- marco (nil hasta la primera vez que se abre)
UI.w = {}          -- widgets: title, scroll, content, counter, prev, next, status, hint, qbox, paste, grip
UI.index = nil     -- respuesta mostrada (indice en Ask.Answers)
local rows = {}    -- filas reutilizables del area de respuesta { fs, btn, icon, action }
local histPos = 0  -- posicion en el historial de preguntas (0 = campo vacio)

local function say(msg)
    if ns.Say then ns.Say(msg) end
end

local function create(kind, name, parent, template)
    return ns.Call("CreateFrame", kind, name, parent, template)
end

-- Registro del personaje actual (para ui: posicion y tamaño). nil sin personaje.
local function charRecord()
    if type(CompaneroWoWDB) ~= "table" or type(CompaneroWoWDB.characters) ~= "table" then return nil end
    local key = ns.CharKey and ns.CharKey()
    local c = key and CompaneroWoWDB.characters[key]
    return type(c) == "table" and c or nil
end

local function esc(s)
    return (string.gsub(s, "|", "||"))
end

-- Texto de un bloque con sus segmentos: negrita en dorado, o todo en gris (fuente).
local function styled(segs, grey)
    local out = {}
    for _, s in ipairs(segs or {}) do
        local t = esc(s.text)
        if grey then
            t = "|cff9d9d9d" .. t .. "|r"
        elseif s.bold then
            t = "|cffffd100" .. t .. "|r"
        end
        out[#out + 1] = t
    end
    return table.concat(out)
end

local function plain(segs)
    local out = {}
    for _, s in ipairs(segs or {}) do out[#out + 1] = s.text end
    return table.concat(out)
end

-- ---------------------------------------------------------------------------
-- Estado guardado (posicion y tamaño por personaje)
-- ---------------------------------------------------------------------------

function UI.SaveState()
    local f, c = UI.frame, charRecord()
    if not (f and c) then return false end
    local point, _, relPoint, x, y = Method(f, "GetPoint")
    local w, h = Num(Method(f, "GetWidth")), Num(Method(f, "GetHeight"))
    if not point then return false end
    c.ui = { point = point, relPoint = relPoint or point, x = Num(x) or 0, y = Num(y) or 0, w = w, h = h }
    return true
end

-- Coloca y dimensiona el marco segun el estado guardado (o el de por defecto).
local function restoreState(f)
    local c = charRecord()
    local st = c and c.ui
    local w, h = UI.DEF_W, UI.DEF_H
    if type(st) == "table" then
        w = Num(st.w) or w
        h = Num(st.h) or h
    end
    w, h = math.max(UI.MIN_W, w), math.max(UI.MIN_H, h)
    Method(f, "SetSize", w, h)
    Method(f, "ClearAllPoints")
    if type(st) == "table" and st.point then
        Method(f, "SetPoint", st.point, _G.UIParent, st.relPoint or st.point, Num(st.x) or 0, Num(st.y) or 0)
    else
        Method(f, "SetPoint", "CENTER", _G.UIParent, "CENTER", 0, 0)
    end
end

-- ---------------------------------------------------------------------------
-- Construccion (la primera vez que se abre)
-- ---------------------------------------------------------------------------

local function nextRow(content)
    local i = (UI.rowUsed or 0) + 1
    UI.rowUsed = i
    local r = rows[i]
    if not r then
        r = {}
        rows[i] = r
    end
    return r, content
end

local function setStatus(text)
    if UI.w.status then Method(UI.w.status, "SetText", text or "") end
end

local function updateHint()
    if not UI.w.hint then return end
    local pending = ns.Ask and ns.Ask.pending
    Method(UI.w.hint, "SetText", pending and UI.PASTE_HINT or "")
end

-- Campo de pregunta: Enter envia; Arriba/Abajo recorren las ultimas preguntas.
local function onQuestionEnter(self)
    local text = self:GetText()
    if ns.Ask.Send(text) then
        self:SetText("")
        histPos = 0
    end
end

local function onQuestionArrow(self, key)
    local hist = ns.Ask.History()
    if #hist == 0 then return end
    if key == "UP" then
        histPos = math.min(histPos + 1, #hist)
    elseif key == "DOWN" then
        histPos = math.max(histPos - 1, 0)
    else
        return
    end
    self:SetText(histPos == 0 and "" or hist[histPos])
end

-- Campo de pegado: cualquier texto que entre por Ctrl+V se procesa como respuesta y se vacia.
local function onPasteChanged(self, userInput)
    if not userInput then return end
    local text = self:GetText()
    if not text or text == "" then return end
    self:SetText("")
    UI.Receive(text)
end

-- Pinta el texto de la respuesta recibida (o el aviso de error) y muestra la respuesta nueva.
function UI.Receive(text)
    local ans, err = ns.Ask.Receive(text)
    if not ans then
        setStatus(err)
        return false
    end
    UI.index = #ns.Ask.Answers()
    setStatus(string.format("Respuesta #%d recibida.", ans.id))
    updateHint()
    UI.Render()
    return true
end

-- Llamado por Shot.lua al confirmarse una captura: estado y pista de Ctrl+V (si hay pregunta pendiente).
function UI.OnShotSent(id)
    setStatus(string.format("Enviado (captura %d).", id or 0))
    updateHint()
end

-- Llamado por Ask.Send al enviar una pregunta: foco al campo de pegado y aviso.
function UI.OnQuestionSent(id)
    setStatus(string.format("Pregunta #%d enviada.", id))
    updateHint()
    if UI.w.paste then Method(UI.w.paste, "SetFocus") end
end

local function build()
    if UI.frame then return UI.frame end
    local parent = _G.UIParent
    local f = create("Frame", FRAME_NAME, parent, "BackdropTemplate")
    if not f then return nil end
    UI.frame = f
    local w = UI.w
    Method(f, "SetFrameStrata", "DIALOG")
    Method(f, "SetClampedToScreen", true)
    Method(f, "SetMovable", true)
    Method(f, "SetResizable", true)
    if Method(f, "SetResizeBounds", UI.MIN_W, UI.MIN_H) == nil then
        Method(f, "SetMinResize", UI.MIN_W, UI.MIN_H)
    end
    Method(f, "SetBackdrop", BACKDROP)
    Method(f, "EnableMouse", true)
    Method(f, "RegisterForDrag", "LeftButton")
    Method(f, "RegisterEvent", "PLAYER_REGEN_DISABLED")
    Method(f, "RegisterEvent", "PLAYER_REGEN_ENABLED")
    local special = _G.UISpecialFrames
    if type(special) == "table" then
        local known = false
        for _, n in ipairs(special) do if n == FRAME_NAME then known = true end end
        if not known then table.insert(special, FRAME_NAME) end -- Escape lo cierra
    end

    w.title = Method(f, "CreateFontString", nil, "OVERLAY", "GameFontNormalLarge")
    Method(w.title, "SetPoint", "TOP", f, "TOP", 0, -14)
    Method(w.title, "SetText", "Compañero")

    local close = create("Button", nil, f, "UIPanelCloseButton")
    Method(close, "SetPoint", "TOPRIGHT", f, "TOPRIGHT", -4, -4)
    Method(close, "SetScript", "OnClick", function() ns.SafeCall(UI.Hide) end)

    w.prev = create("Button", nil, f, "UIPanelButtonTemplate")
    Method(w.prev, "SetSize", 28, 22)
    Method(w.prev, "SetPoint", "TOPLEFT", f, "TOPLEFT", 14, -40)
    Method(w.prev, "SetText", "‹")
    Method(w.prev, "SetScript", "OnClick", function() ns.SafeCall(UI.Step, -1) end)
    w.next = create("Button", nil, f, "UIPanelButtonTemplate")
    Method(w.next, "SetSize", 28, 22)
    Method(w.next, "SetPoint", "TOPRIGHT", f, "TOPRIGHT", -34, -40)
    Method(w.next, "SetText", "›")
    Method(w.next, "SetScript", "OnClick", function() ns.SafeCall(UI.Step, 1) end)
    w.counter = Method(f, "CreateFontString", nil, "OVERLAY", "GameFontHighlightSmall")
    Method(w.counter, "SetPoint", "TOP", f, "TOP", 0, -46)

    w.scroll = create("ScrollFrame", ANSWER_SCROLL, f, "UIPanelScrollFrameTemplate")
    Method(w.scroll, "SetPoint", "TOPLEFT", f, "TOPLEFT", 14, -68)
    Method(w.scroll, "SetPoint", "BOTTOMRIGHT", f, "BOTTOMRIGHT", -30, 74)
    w.content = create("Frame", nil, w.scroll)
    Method(w.content, "SetSize", UI.MIN_W - 40, 10)
    Method(w.scroll, "SetScrollChild", w.content)

    w.hint = Method(f, "CreateFontString", nil, "OVERLAY", "GameFontHighlightSmall")
    Method(w.hint, "SetPoint", "BOTTOMLEFT", f, "BOTTOMLEFT", 16, 8)
    w.status = Method(f, "CreateFontString", nil, "OVERLAY", "GameFontNormalSmall")
    Method(w.status, "SetPoint", "BOTTOMLEFT", f, "BOTTOMLEFT", 16, 26)

    w.qbox = create("EditBox", QUESTION_NAME, f, "InputBoxTemplate")
    Method(w.qbox, "SetAutoFocus", false)
    Method(w.qbox, "SetMaxLetters", ns.Ask.MAX_QUESTION)
    Method(w.qbox, "SetHeight", 22)
    Method(w.qbox, "SetPoint", "BOTTOMLEFT", f, "BOTTOMLEFT", 16, 46)
    Method(w.qbox, "SetPoint", "BOTTOMRIGHT", f, "BOTTOMRIGHT", -16, 46)
    Method(w.qbox, "SetScript", "OnEnterPressed", function(self) ns.SafeCall(onQuestionEnter, self) end)
    Method(w.qbox, "SetScript", "OnEscapePressed", function(self) Method(self, "ClearFocus") end)
    Method(w.qbox, "SetScript", "OnArrowPressed", function(self, key) ns.SafeCall(onQuestionArrow, self, key) end)

    -- Campo de pegado: enfocable pero invisible (alpha 0); recibe Ctrl+V.
    w.paste = create("EditBox", PASTE_NAME, f)
    Method(w.paste, "SetMultiLine", true)
    Method(w.paste, "SetAutoFocus", false)
    Method(w.paste, "SetMaxLetters", 8000)
    Method(w.paste, "SetSize", 20, 20)
    Method(w.paste, "SetPoint", "BOTTOMLEFT", f, "BOTTOMLEFT", 0, 0)
    Method(w.paste, "SetAlpha", 0)
    Method(w.paste, "EnableMouse", false)
    Method(w.paste, "SetScript", "OnTextChanged", function(self, userInput) ns.SafeCall(onPasteChanged, self, userInput) end)
    Method(w.paste, "SetScript", "OnEscapePressed", function(self) Method(self, "ClearFocus") end)

    w.grip = create("Button", nil, f)
    Method(w.grip, "SetSize", 16, 16)
    Method(w.grip, "SetPoint", "BOTTOMRIGHT", f, "BOTTOMRIGHT", -4, 4)
    Method(w.grip, "SetNormalTexture", "Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Up")
    Method(w.grip, "SetPushedTexture", "Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Down")
    Method(w.grip, "SetHighlightTexture", "Interface\\ChatFrame\\UI-ChatIM-SizeGrabber-Highlight")
    Method(w.grip, "SetScript", "OnMouseDown", function() Method(f, "StartSizing", "BOTTOMRIGHT") end)
    Method(w.grip, "SetScript", "OnMouseUp", function()
        Method(f, "StopMovingOrSizing")
        ns.SafeCall(UI.SaveState)
    end)

    Method(f, "SetScript", "OnDragStart", function(self) Method(self, "StartMoving") end)
    Method(f, "SetScript", "OnDragStop", function(self)
        Method(self, "StopMovingOrSizing")
        ns.SafeCall(UI.SaveState)
    end)
    Method(f, "SetScript", "OnSizeChanged", function() ns.SafeCall(UI.Render) end)
    Method(f, "SetScript", "OnHide", function() ns.SafeCall(UI.SaveState) end)
    Method(f, "SetScript", "OnEvent", function() ns.SafeCall(UI.SyncCombat) end)

    restoreState(f)
    Method(f, "Hide")
    return f
end
UI.Build = build

-- ---------------------------------------------------------------------------
-- Dibujo de la respuesta
-- ---------------------------------------------------------------------------

-- Altura de un texto: la del motor si la da, si no una estimacion por lineas.
local function textHeight(fs, text, width)
    local h = Num(Method(fs, "GetStringHeight"))
    if h and h > 0 then return h end
    local chars = ns.Markup and ns.Markup.CharLen(text) or #text
    local lines = math.max(1, math.ceil(chars * 7 / math.max(width, 1)))
    return lines * 14
end

local function textRow(content, y, width, segs, font, grey, indent, prefix)
    local r = nextRow(content)
    if not r.fs then
        r.fs = Method(content, "CreateFontString", nil, "OVERLAY", font)
    end
    local fs = r.fs
    local text = (prefix or "") .. styled(segs, grey)
    local shownWidth = width - indent
    Method(fs, "SetFontObject", font)
    Method(fs, "SetWidth", shownWidth)
    Method(fs, "SetJustifyH", "LEFT")
    Method(fs, "SetWordWrap", true)
    Method(fs, "SetText", text)
    Method(fs, "ClearAllPoints")
    Method(fs, "SetPoint", "TOPLEFT", content, "TOPLEFT", indent, y)
    Method(fs, "Show")
    if r.btn then Method(r.btn, "Hide") end
    local h = textHeight(fs, (prefix or "") .. plain(segs), shownWidth)
    return y - h - 6
end

local function tooltipFor(block)
    local tip = _G.GameTooltip
    if not tip then return end
    local detail
    if block.action == "marca" then
        detail = string.format("Mapa %d, coordenadas %.1f, %.1f%s", block.mapID, block.x, block.y,
            (block.text and block.text ~= "") and (": " .. block.text) or "")
    elseif block.action == "barras" then
        detail = "Coloca: " .. (block.args or "")
    else
        detail = "Crea la macro «" .. (block.name or "") .. "»."
    end
    Method(tip, "SetOwner", _G.UIParent, "ANCHOR_CURSOR")
    Method(tip, "SetText", LABELS[block.action] or "Acción", 1, 0.82, 0)
    Method(tip, "AddLine", detail, 1, 1, 1, true)
    if ns.InCombat() then
        Method(tip, "AddLine", ns.Ask.COMBAT_MSG, 1, 0.3, 0.3, true)
    end
    Method(tip, "Show")
end

-- Boton de accion con icono. Fuera de combate ejecuta la accion; en combate esta desactivado.
local function actionRow(content, y, width, block)
    local r = nextRow(content)
    if not r.btn then
        r.btn = create("Button", nil, content, "UIPanelButtonTemplate")
        r.icon = Method(r.btn, "CreateTexture", nil, "ARTWORK")
        Method(r.icon, "SetSize", 16, 16)
        Method(r.icon, "SetPoint", "LEFT", r.btn, "LEFT", 8, 0)
        Method(r.btn, "SetScript", "OnClick", function()
            ns.SafeCall(ns.Ask.Run, r.action)
        end)
        Method(r.btn, "SetScript", "OnEnter", function() ns.SafeCall(tooltipFor, r.action) end)
        Method(r.btn, "SetScript", "OnLeave", function()
            if _G.GameTooltip then Method(_G.GameTooltip, "Hide") end
        end)
    end
    r.action = block
    Method(r.icon, "SetTexture", ICONS[block.action] or ICONS.marca)
    Method(r.icon, "Show")
    Method(r.btn, "SetText", "      " .. (LABELS[block.action] or "Acción"))
    Method(r.btn, "SetSize", width - 8, 24)
    Method(r.btn, "ClearAllPoints")
    Method(r.btn, "SetPoint", "TOPLEFT", content, "TOPLEFT", 4, y)
    Method(r.btn, "SetEnabled", not ns.InCombat())
    Method(r.btn, "Show")
    if r.fs then Method(r.fs, "Hide") end
    return y - 30
end

local function hideRows()
    for i = 1, UI.rowUsed or 0 do
        local r = rows[i]
        if r.fs then Method(r.fs, "Hide") end
        if r.btn then Method(r.btn, "Hide") end
    end
    UI.rowUsed = 0
end

-- Pinta la respuesta UI.index (o el texto vacio) en el area de respuesta.
function UI.Render()
    local f, w = UI.frame, UI.w
    if not (f and w.content) then return end
    local list = ns.Ask.Answers()
    local n = #list
    hideRows()
    local scrollW = Num(Method(w.scroll, "GetWidth")) or (UI.MIN_W - 44)
    local width = math.max(200, scrollW - 8)
    local y = -4
    if n == 0 then
        y = textRow(w.content, y, width, { { text = UI.EMPTY_TEXT } }, "GameFontDisable", true, 4)
        UI.index = nil
    else
        if not UI.index or UI.index < 1 or UI.index > n then UI.index = n end
        local ans = list[UI.index]
        local head = "#" .. ans.id .. (ans.provider and (" · " .. ans.provider) or "")
        y = textRow(w.content, y, width, { { text = head } }, "GameFontDisableSmall", true, 4)
        for _, block in ipairs(ans.blocks or {}) do
            if block.kind == "space" then
                y = y - 8
            elseif block.kind == "action" then
                y = actionRow(w.content, y, width, block)
            elseif block.kind == "source" then
                y = textRow(w.content, y, width, block.segs, "GameFontDisableSmall", true, 4)
            elseif block.kind == "bullet" then
                y = textRow(w.content, y, width, block.segs, "GameFontHighlight", false, 16, "· ")
            else
                y = textRow(w.content, y, width, block.segs, "GameFontHighlight", false, 4)
            end
        end
        if ans.truncated then
            y = textRow(w.content, y, width, { { text = "(respuesta recortada)" } }, "GameFontDisableSmall", true, 4)
        end
    end
    Method(w.content, "SetSize", scrollW, math.max(20, -y + 8))
    Method(w.counter, "SetText", n == 0 and "" or string.format("%d / %d", UI.index or 0, n))
    Method(w.prev, "SetEnabled", n > 1 and (UI.index or 0) > 1)
    Method(w.next, "SetEnabled", n > 1 and (UI.index or 0) < n)
    updateHint()
    UI.SyncCombat()
end

-- Pasa a la respuesta anterior (-1) o siguiente (+1).
function UI.Step(d)
    local n = #ns.Ask.Answers()
    if n == 0 then return end
    UI.index = math.min(n, math.max(1, (UI.index or n) + d))
    UI.Render()
end

-- Botones de accion desactivados en combate (con su aviso); activados fuera.
function UI.SyncCombat()
    local inCombat = ns.InCombat()
    for i = 1, UI.rowUsed or 0 do
        local r = rows[i]
        if r.btn then Method(r.btn, "SetEnabled", not inCombat) end
    end
end

-- ---------------------------------------------------------------------------
-- Abrir, cerrar y enfocar
-- ---------------------------------------------------------------------------

-- Muestra el marco (lo crea la primera vez) y refresca el contenido.
function UI.Open()
    local f = build()
    if not f then return false end
    Method(f, "Show")
    UI.Render()
    return true
end

function UI.Hide()
    if UI.frame then Method(UI.frame, "Hide") end
end

function UI.IsShown()
    return UI.frame ~= nil and Method(UI.frame, "IsShown") == true
end

-- Pone el cursor en el campo de pregunta.
function UI.FocusQuestion()
    if not UI.w.qbox then return false end
    Method(UI.w.qbox, "SetFocus")
    return true
end

-- Tecla de compartir (Share.lua): si el campo de pregunta ya tiene el foco, la pulsacion cierra el
-- marco; si no, lo abre (o lo deja abierto) y pone el foco en la pregunta.
function UI.OnShareKey()
    if UI.IsShown() and UI.w.qbox and Method(UI.w.qbox, "HasFocus") == true then
        UI.Hide()
        return
    end
    UI.Open()
    UI.FocusQuestion()
end

return UI
