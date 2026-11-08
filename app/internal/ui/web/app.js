(function () {
  "use strict";

  // Go binds these functions (see window_windows.go). Each returns a Promise. The names
  // are prefixed so they never replace a browser global such as window.history.
  var go = {
    history: function () { return window.uiHistory(); },
    clearHistory: function () { return window.uiClearHistory(); },
    status: function () { return window.uiStatus(); },
    openURL: function (url) { return window.uiOpenURL(url); },
    quit: function () { return window.uiQuit(); },
    goAsk: function (id, text, shot) { return window.uiAsk(id, text, shot); }
  };

  var chat = document.getElementById("chat");
  var input = document.getElementById("input");
  var sendBtn = document.getElementById("send");
  var shot = document.getElementById("shot");
  var thinking = document.getElementById("thinking");
  var notice = document.getElementById("notice");
  var dot = document.getElementById("bridge-dot");
  var bridgeText = document.getElementById("bridge-text");
  var svText = document.getElementById("sv-text");
  var place = document.getElementById("place");
  var hints = document.getElementById("hints");

  var pending = {};
  var pendingCount = 0;
  var seq = 0;
  var introEl = null;

  // ---- escaping and minimal Markdown -----------------------------------

  function esc(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  // inline applies code spans, bold, links. Input must already be escaped.
  function inline(s) {
    var codes = [];
    s = s.replace(/`([^`]+)`/g, function (_, c) {
      codes.push("<code>" + c + "</code>");
      return "\u0000" + (codes.length - 1) + "\u0000";
    });
    s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, function (_, text, url) {
      return '<a href="#" data-url="' + url + '">' + text + "</a>";
    });
    s = s.replace(/(^|[\s(])(https?:\/\/[^\s<)]+)/g, function (_, pre, url) {
      return pre + '<a href="#" data-url="' + url + '">' + url + "</a>";
    });
    s = s.replace(/\u0000(\d+)\u0000/g, function (_, i) { return codes[+i]; });
    return s;
  }

  // md renders text as HTML. Everything is escaped first, so only the tags made here exist.
  function md(text) {
    var lines = esc(text).split("\n");
    var out = [];
    var para = [];
    var list = null;

    function flushPara() {
      if (para.length) {
        out.push("<p>" + para.map(inline).join("<br>") + "</p>");
        para = [];
      }
    }
    function closeList() {
      if (list) {
        out.push("</" + list + ">");
        list = null;
      }
    }

    lines.forEach(function (line) {
      var m;
      if ((m = line.match(/^\s*[-*]\s+(.*)$/))) {
        flushPara();
        if (list !== "ul") { closeList(); out.push("<ul>"); list = "ul"; }
        out.push("<li>" + inline(m[1]) + "</li>");
      } else if ((m = line.match(/^\s*\d+[.)]\s+(.*)$/))) {
        flushPara();
        if (list !== "ol") { closeList(); out.push("<ol>"); list = "ol"; }
        out.push("<li>" + inline(m[1]) + "</li>");
      } else if (line.trim() === "") {
        flushPara();
        closeList();
      } else if ((m = line.match(/^#{1,3}\s+(.*)$/))) {
        flushPara();
        closeList();
        out.push('<p class="h">' + inline(m[1]) + "</p>");
      } else {
        closeList();
        para.push(line);
      }
    });
    flushPara();
    closeList();
    return out.join("");
  }

  // ---- rendering --------------------------------------------------------

  function removeIntro() {
    if (introEl && introEl.parentNode) introEl.parentNode.removeChild(introEl);
    introEl = null;
  }

  function scrollDown() {
    chat.scrollTop = chat.scrollHeight;
  }

  function showIntro() {
    introEl = document.createElement("div");
    introEl.className = "msg intro";
    introEl.textContent = "Pregúntame por misiones, niveles, profesiones o cualquier cosa de WoW: Forever. Puedo mirar tu pantalla si activas «Incluir captura».";
    chat.appendChild(introEl);
  }

  function addBubble(role, text, provider) {
    removeIntro();
    var el = document.createElement("div");
    el.className = "msg " + role;
    if (role === "user") {
      el.textContent = text;
    } else {
      el.innerHTML = md(text);
      if (provider) {
        var meta = document.createElement("div");
        meta.className = "meta";
        meta.textContent = providerLabel(provider);
        el.appendChild(meta);
      }
    }
    chat.appendChild(el);
    scrollDown();
    return el;
  }

  function addError(text) {
    removeIntro();
    var el = document.createElement("div");
    el.className = "msg error";
    el.textContent = text;
    chat.appendChild(el);
    scrollDown();
  }

  function addTip(tip) {
    removeIntro();
    var el = document.createElement("div");
    el.className = "tip";
    var title = document.createElement("div");
    title.className = "tip-title";
    title.textContent = tip.title || "Consejo";
    var body = document.createElement("div");
    body.innerHTML = md(tip.text || "");
    var close = document.createElement("button");
    close.type = "button";
    close.className = "tip-close";
    close.setAttribute("aria-label", "Cerrar consejo");
    close.textContent = "×";
    close.addEventListener("click", function () { el.remove(); });
    el.appendChild(title);
    el.appendChild(body);
    el.appendChild(close);
    chat.appendChild(el);
    scrollDown();
  }

  function providerLabel(p) {
    if (p.indexOf("proactivo:") === 0) return "consejo · " + p.slice("proactivo:".length);
    return p;
  }

  function renderThinking() {
    thinking.hidden = pendingCount === 0;
    sendBtn.disabled = false;
  }

  function renderHistory(list) {
    chat.innerHTML = "";
    introEl = null;
    if (!list || list.length === 0) {
      showIntro();
      return;
    }
    list.forEach(function (m) {
      if (m.provider && m.provider.indexOf("proactivo:") === 0) {
        var parts = String(m.content).split(": ");
        var title = parts.shift();
        addTip({ title: title, text: parts.join(": ") });
      } else {
        addBubble(m.role === "user" ? "user" : "assistant", m.content, m.provider);
      }
    });
    scrollDown();
  }

  // ---- status -----------------------------------------------------------

  function ageText(sec) {
    if (sec < 60) return "hace " + sec + " s";
    if (sec < 3600) return "hace " + Math.floor(sec / 60) + " min";
    return "hace " + Math.floor(sec / 3600) + " h";
  }

  function renderStatus(s) {
    if (!s) return;
    var map = {
      connected: ["ok", "WoW conectado"],
      no_wow: ["off", "WoW no detectado"],
      no_strip: ["warn", "Franja de píxeles no encontrada"]
    };
    var b = map[s.bridge] || ["", "Comprobando…"];
    dot.className = "dot " + b[0];
    bridgeText.textContent = b[1];

    if (typeof s.savedVarsAgeSec === "number" && s.savedVarsAgeSec >= 0) {
      svText.textContent = "Datos " + ageText(s.savedVarsAgeSec);
    } else {
      svText.textContent = "Sin datos guardados";
    }

    var where = [];
    if (s.zone) where.push(s.zone);
    if (s.level) where.push("nivel " + s.level);
    place.textContent = where.join(" · ");
    place.hidden = where.length === 0;

    hints.innerHTML = "";
    (s.hints || []).forEach(function (h) {
      var d = document.createElement("div");
      d.textContent = h;
      hints.appendChild(d);
    });

    if (s.notice) {
      notice.textContent = s.notice;
      notice.hidden = false;
    } else {
      notice.hidden = true;
    }
  }

  // ---- actions ----------------------------------------------------------

  function send() {
    var text = input.value.trim();
    if (!text) return;
    input.value = "";
    autoSize();
    var withShot = shot.checked;
    addBubble("user", text);
    var id = ++seq;
    pendingCount++;
    renderThinking();
    new Promise(function (resolve, reject) {
      pending[id] = { resolve: resolve, reject: reject };
      Promise.resolve(go.goAsk(id, text, withShot)).catch(function (err) {
        delete pending[id];
        reject(err);
      });
    }).then(function (res) {
      addBubble("assistant", res.text, res.provider);
    }).catch(function (err) {
      addError(String((err && err.message) || err || "No pude responder."));
    }).then(function () {
      pendingCount--;
      renderThinking();
      input.focus();
    });
  }

  // Called by Go with the answer of goAsk.
  window.companeroAnswer = function (id, ok, payload) {
    var p = pending[id];
    if (!p) return;
    delete pending[id];
    if (ok) p.resolve(payload);
    else p.reject(new Error(payload));
  };

  window.companeroStatus = function (s) { renderStatus(s); };

  window.companeroTip = function (tip) { addTip(tip); };

  window.companeroFocus = function () { input.focus(); };

  function autoSize() {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 120) + "px";
  }

  // ---- events -----------------------------------------------------------

  input.addEventListener("keydown", function (e) {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    }
  });
  input.addEventListener("input", autoSize);
  sendBtn.addEventListener("click", send);

  document.getElementById("btn-clear").addEventListener("click", function () {
    if (!window.confirm("¿Borrar todo el historial del chat?")) return;
    Promise.resolve(go.clearHistory()).then(function () { renderHistory([]); }, function (err) {
      addError(String(err));
    });
  });

  document.getElementById("btn-quit").addEventListener("click", function () {
    go.quit();
  });

  // Links open in the default browser; the page itself never navigates.
  chat.addEventListener("click", function (e) {
    var a = e.target.closest ? e.target.closest("a[data-url]") : null;
    if (a) {
      e.preventDefault();
      go.openURL(a.getAttribute("data-url"));
    }
  });
  document.addEventListener("click", function (e) {
    if (e.target.closest && e.target.closest("a")) e.preventDefault();
  }, true);
  ["dragover", "drop"].forEach(function (ev) {
    document.addEventListener(ev, function (e) { e.preventDefault(); });
  });

  // ---- start ------------------------------------------------------------

  Promise.resolve(go.history()).then(renderHistory, function () { renderHistory([]); });
  Promise.resolve(go.status()).then(renderStatus, function () {});
  renderThinking();
  input.focus();
})();
