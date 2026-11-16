/*
 * Chatbot widget. Plain script; everything is rendered as text, never as markup.
 *
 *   <script src="https://app.example/chat.js" data-bot="cb_..." defer></script>
 *
 * The conversation token lives in sessionStorage only after the visitor opens the chat.
 */
(function () {
  "use strict";
  var script = document.currentScript;
  if (!script || window.mspChat) return;
  var key = script.getAttribute("data-bot");
  if (!key) return;
  var base = (script.getAttribute("data-api") || new URL("/api/v1", script.src).toString()) + "/public/chat/" + encodeURIComponent(key);
  var TOKEN = "msp_chat_" + key;
  var config = null, token = null, timer = null, shown = {};

  function el(tag, attrs, text) {
    var e = document.createElement(tag);
    for (var k in attrs || {}) e.setAttribute(k, attrs[k]);
    if (text != null) e.textContent = text;
    return e;
  }
  function call(method, path, body) {
    return fetch(base + path, { method: method, credentials: "omit", headers: { "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined })
      .then(function (r) { return r.status === 204 ? {} : r.json().then(function (j) { if (!r.ok) throw j; return j; }); });
  }

  var root = el("div", { "data-msp-chat": "", style: "position:fixed;right:16px;bottom:16px;z-index:2147483000;font:14px system-ui,sans-serif" });
  var button = el("button", { type: "button", "aria-expanded": "false", style: "border-radius:999px;padding:10px 16px;border:0;background:#1d4ed8;color:#fff;cursor:pointer" }, "Chat with us");
  var panel = el("section", { role: "dialog", "aria-label": "Chat", hidden: "", style: "width:320px;max-height:480px;display:flex;flex-direction:column;background:#fff;color:#111;border:1px solid #ccc;border-radius:8px;box-shadow:0 8px 24px rgba(0,0,0,.15);margin-bottom:8px" });
  var header = el("header", { style: "padding:8px 12px;border-bottom:1px solid #eee;font-weight:600" });
  var notice = el("p", { "data-msp-disclosure": "", style: "margin:0;padding:6px 12px;font-size:12px;color:#555;background:#f6f6f6" });
  var log = el("ol", { "aria-live": "polite", style: "list-style:none;margin:0;padding:8px 12px;overflow-y:auto;flex:1" });
  var form = el("form", { style: "display:flex;gap:6px;padding:8px;border-top:1px solid #eee" });
  var input = el("input", { type: "text", maxlength: "1000", "aria-label": "Your message", style: "flex:1;padding:6px" });
  var send = el("button", { type: "submit" }, "Send");
  var leadLink = el("button", { type: "button", style: "margin:0 8px 8px;font-size:12px;background:none;border:0;color:#1d4ed8;cursor:pointer;text-align:left" }, "Leave your details for a call back");
  var lead = el("form", { hidden: "", style: "padding:8px;border-top:1px solid #eee;display:grid;gap:6px" });
  form.append(input, send);
  panel.append(header, notice, log, form, leadLink, lead);
  root.append(panel, button);

  function render(messages) {
    (messages || []).forEach(function (m) {
      if (shown[m.id]) return;
      shown[m.id] = true;
      var who = m.role === "visitor" ? "You" : m.role === "human_agent" ? "Team" : m.role === "assistant" ? "Assistant" : "";
      var li = el("li", { "data-role": m.role, style: "margin:4px 0;" + (m.role === "visitor" ? "text-align:right" : "") });
      if (who) li.append(el("strong", { style: "display:block;font-size:11px;color:#666" }, who));
      li.append(el("span", { style: "white-space:pre-wrap" }, m.content));
      log.append(li);
    });
    log.scrollTop = log.scrollHeight;
  }
  function start() {
    token = sessionStorage.getItem(TOKEN);
    if (token) return call("POST", "/poll", { token: token }).then(function (r) { render(r.messages); }, function () { sessionStorage.removeItem(TOKEN); token = null; return start(); });
    return call("POST", "/conversations").then(function (r) { token = r.token; sessionStorage.setItem(TOKEN, token); render(r.messages); });
  }
  function buildLead() {
    lead.textContent = "";
    var fields = [["name", "Name", "text"], ["email", "Email", "email"], ["company", "Company", "text"], ["preferred_time", "Preferred time for a call", "text"]];
    (config.questions || []).forEach(function (q) { fields.push(["answer:" + q.field_key, q.question, "text"]); });
    fields.forEach(function (f) {
      var label = el("label", { style: "display:grid;font-size:12px" }, f[1]);
      var attrs = { name: f[0], type: f[2], style: "padding:4px" };
      if (f[0] === "name" || f[0] === "email") attrs.required = "";
      label.append(el("input", attrs));
      lead.append(label);
    });
    var c = el("label", { style: "font-size:12px;display:flex;gap:6px" });
    c.append(el("input", { type: "checkbox", name: "marketing_consent" }), el("span", {}, config.consent_text));
    lead.append(c, el("button", { type: "submit" }, "Send details"));
  }

  button.addEventListener("click", function () {
    var open = panel.hasAttribute("hidden");
    if (open) panel.removeAttribute("hidden"); else panel.setAttribute("hidden", "");
    button.setAttribute("aria-expanded", String(open));
    if (open && !token) start();
    clearInterval(timer);
    if (open) timer = setInterval(function () { if (token) call("POST", "/poll", { token: token }).then(function (r) { render(r.messages); }, function () {}); }, 5000);
  });
  form.addEventListener("submit", function (e) {
    e.preventDefault();
    var text = input.value.trim();
    if (!text || !token) return;
    input.value = "";
    send.disabled = true;
    call("POST", "/messages", { token: token, message: text }).then(function (r) { render(r.messages); }, function (err) { render([{ id: "err" + Date.now(), role: "system", content: (err && err.title) || "Message not sent." }]); })
      .then(function () { send.disabled = false; });
  });
  leadLink.addEventListener("click", function () { lead.toggleAttribute("hidden"); });
  lead.addEventListener("submit", function (e) {
    e.preventDefault();
    var d = new FormData(lead), answers = {};
    d.forEach(function (v, k) { if (k.indexOf("answer:") === 0 && v) answers[k.slice(7)] = String(v); });
    var body = { token: token, name: String(d.get("name") || ""), email: String(d.get("email") || ""), marketing_consent: d.get("marketing_consent") === "on", answers: answers };
    if (d.get("company")) body.company = String(d.get("company"));
    if (d.get("preferred_time")) body.preferred_time = String(d.get("preferred_time"));
    call("POST", "/lead", body).then(function () { lead.setAttribute("hidden", ""); render([{ id: "lead" + Date.now(), role: "system", content: "Thanks, we will be in touch." }]); },
      function (err) { render([{ id: "lerr" + Date.now(), role: "system", content: (err && err.title) || "Details not sent." }]); });
  });

  call("GET", "/config").then(function (c) {
    config = c;
    header.textContent = c.name;
    notice.textContent = c.disclosure;
    if (c.greeting) render([{ id: "greeting", role: "assistant", content: c.greeting }]);
    buildLead();
    document.body.append(root);
  }, function () { /* Not available on this page: show nothing. */ });
  window.mspChat = { open: function () { if (panel.hasAttribute("hidden")) button.click(); } };
})();
