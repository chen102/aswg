package server

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-session-web-gateway/backend/internal/adapter"
	"agent-session-web-gateway/backend/internal/model"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const frontendAppRuntimePatch = `
;(() => {
  const PATCH_MARK = "ASWG Runtime Patch v17";
  if (window.__aswgRuntimePatchApplied === PATCH_MARK) {
    return;
  }
  window.__aswgRuntimePatchApplied = PATCH_MARK;

  function normalizeSessionStatus(value) {
    return String(value || "").trim().toLowerCase() === "running" ? "running" : "idle";
  }

  function sessionStatusLabel(status) {
    return status === "running" ? "进行中" : "空闲";
  }

  function iconSVG(name) {
    const icons = {
      settings:
        '<path d="M12 3v2"/><path d="M12 19v2"/><path d="M3 12h2"/><path d="M19 12h2"/><path d="m5.64 5.64 1.42 1.42"/><path d="m16.94 16.94 1.42 1.42"/><path d="m18.36 5.64-1.42 1.42"/><path d="m7.06 16.94-1.42 1.42"/><circle cx="12" cy="12" r="3.2"/>',
      list: '<path d="M8 6h12"/><path d="M8 12h12"/><path d="M8 18h12"/><path d="M4 6h.01"/><path d="M4 12h.01"/><path d="M4 18h.01"/>',
      refresh:
        '<path d="M20 4v6h-6"/><path d="M4 20v-6h6"/><path d="M20 10a8 8 0 0 0-13.66-4.66L4 8"/><path d="M4 14a8 8 0 0 0 13.66 4.66L20 16"/>',
      plug: '<path d="M8 3v6"/><path d="M16 3v6"/><path d="M7 9h10v3a5 5 0 0 1-10 0z"/><path d="M12 17v4"/>',
      send: '<path d="m22 2-7 20-4-9-9-4z"/><path d="M22 2 11 13"/>',
      trash: '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6"/><path d="M14 11v6"/>',
      up: '<path d="m6 15 6-6 6 6"/><path d="M12 9v10"/>',
      down: '<path d="m6 9 6 6 6-6"/><path d="M12 5v10"/>',
    };
    const paths = icons[name] || icons.list;
    return (
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      paths +
      "</svg>"
    );
  }

  function setIconButton(button, iconName, label) {
    if (!button) {
      return;
    }
    const text = String(label || "").trim();
    if (!text) {
      return;
    }
    if (button.dataset.imIconName === iconName && button.dataset.imIconText === text) {
      button.setAttribute("title", text);
      button.setAttribute("aria-label", text);
      return;
    }
    button.dataset.imIconName = iconName;
    button.dataset.imIconText = text;
    button.setAttribute("title", text);
    button.setAttribute("aria-label", text);
    button.classList.add("im-btn-with-icon");
    button.textContent = "";

    const icon = document.createElement("span");
    icon.className = "im-btn-icon";
    icon.innerHTML = iconSVG(iconName);

    const title = document.createElement("span");
    title.className = "im-btn-label";
    title.textContent = text;

    button.appendChild(icon);
    button.appendChild(title);
  }

  function applyButtonIcons() {
    setIconButton(document.getElementById("im-settings-toggle"), "settings", "连接设置");
    setIconButton(document.getElementById("im-sessions-toggle"), "list", "会话列表");
    setIconButton(document.getElementById("reload-sessions"), "refresh", "刷新列表");
    setIconButton(document.getElementById("reconnect-stream"), "plug", "重连流");
    setIconButton(document.getElementById("continue-submit"), "send", "发送");
    setIconButton(document.getElementById("delete-session-button"), "trash", "删除会话");
    setIconButton(document.getElementById("chat-jump-top"), "up", "顶部");
    setIconButton(document.getElementById("chat-jump-bottom"), "down", "底部");
  }

  function ensurePatchStyles() {
    if (document.getElementById("aswg-runtime-patch-style")) {
      return;
    }
    const style = document.createElement("style");
    style.id = "aswg-runtime-patch-style";
    style.textContent = [
      ".session-card-status.is-running{color:#0a7a5c;font-weight:700;}",
      ".session-card-status.is-idle{color:#6d6558;}",
      ".session-detail-title-row{display:flex;align-items:center;justify-content:space-between;gap:8px;min-width:0;}",
      ".session-detail-title-row h3{margin:0;min-width:0;flex:1;display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:1;overflow:hidden;text-overflow:ellipsis;line-height:1.25;}",
      ".delete-session-btn{border:1px solid #d8b0aa;background:#fff6f5;color:#8c1f1f;border-radius:10px;padding:4px 10px;font-size:12px;line-height:1.2;cursor:pointer;transition:all .16s ease;}",
      ".delete-session-btn:hover:not(:disabled){background:#fdeaea;border-color:#c97a70;}",
      ".delete-session-btn:disabled{opacity:.5;cursor:not-allowed;}",
      "body.aswg-im-mode,body.aswg-chat-pin{height:100dvh;overflow:hidden;}",
      "body.aswg-im-mode main.layout,body.aswg-chat-pin main.layout{height:100dvh;overflow:hidden;}",
      "body.aswg-im-mode .layout{max-width:1400px;padding:10px 12px 12px;gap:10px;height:100dvh;overflow:hidden;}",
      "body.aswg-im-mode .hero{margin:0;background:rgba(255,253,248,.92);backdrop-filter:blur(6px);border:1px solid var(--line);border-radius:12px;padding:10px 12px;align-items:center;}",
      "body.aswg-im-mode .hero h1{font-size:clamp(1.2rem,2vw,1.6rem);}",
      "body.aswg-im-mode .hero p{font-size:.82rem;}",
      "body.aswg-im-mode #settings-panel{display:none;}",
      "body.aswg-im-mode.im-settings-open #settings-panel{display:block;position:fixed;top:64px;right:12px;z-index:80;width:min(620px,calc(100vw - 24px));max-height:72vh;overflow:auto;}",
      "body.aswg-im-mode .im-header-tools{display:flex;gap:8px;align-items:center;}",
      "body.aswg-im-mode .im-ctrl-btn{border:1px solid var(--line);background:#fff;border-radius:10px;padding:6px 10px;font-size:12px;line-height:1.2;color:var(--ink);cursor:pointer;}",
      "body.aswg-im-mode .im-ctrl-btn:hover{background:#f7f2e8;}",
      "body.aswg-im-mode .panel-sessions{height:calc(100dvh - 78px);display:flex;flex-direction:column;padding:8px 10px;margin:0;}",
      "body.aswg-im-mode .panel-sessions>.panel-title-row{margin:0 0 10px;}",
      "body.aswg-im-mode .panel-sessions .session-body{flex:1;min-height:0;gap:10px;grid-template-columns:300px minmax(0,1fr);}",
      "body.aswg-im-mode .panel-sessions .session-body>aside{min-height:0;border:1px solid var(--line);border-radius:12px;padding:10px;background:#fff;}",
      "body.aswg-im-mode .session-list{max-height:none;min-height:0;}",
      "body.aswg-im-mode .create-session-form{display:none;}",
      "body.aswg-chat-pin .panel-sessions{height:calc(100dvh - 78px);display:flex;flex-direction:column;}",
      "body.aswg-chat-pin .panel-sessions .session-body{flex:1;min-height:0;}",
      "body.aswg-im-mode .session-detail,body.aswg-chat-pin .session-detail{position:relative;display:grid;grid-template-rows:auto minmax(0,1fr) auto !important;min-height:0;height:100%;border:1px solid var(--line);border-radius:12px;background:#fff;padding:12px;overflow:hidden;}",
      "body.aswg-im-mode .session-detail .aswg-chat-head,body.aswg-chat-pin .session-detail .aswg-chat-head{position:static;z-index:4;background:#fff;padding-bottom:4px;border-bottom:1px solid rgba(194,184,163,.46);}",
      "body.aswg-im-mode .session-detail>#aswg-chat-head,body.aswg-chat-pin .session-detail>#aswg-chat-head{grid-row:1;}",
      "body.aswg-im-mode .session-detail>#chat-thread,body.aswg-chat-pin .session-detail>#chat-thread{grid-row:2;min-height:0;height:auto;max-height:none;overflow:auto;}",
      "body.aswg-im-mode .session-detail>#continue-form,body.aswg-chat-pin .session-detail>#continue-form{grid-row:3;align-self:end;}",
      "body.aswg-im-mode .session-detail .aswg-chat-head .stream-row,body.aswg-chat-pin .session-detail .aswg-chat-head .stream-row{margin-top:4px;}",
      "body.aswg-im-mode .chat-thread{height:auto;max-height:none;min-height:0;overflow:auto;scroll-behavior:smooth;grid-auto-rows:max-content;align-content:start;padding-right:12px;padding-bottom:calc(16px + env(safe-area-inset-bottom,0px));scroll-padding-bottom:calc(26px + env(safe-area-inset-bottom,0px));}",
      "body.aswg-chat-pin .chat-thread{height:auto;max-height:none;min-height:0;overflow:auto;scroll-behavior:smooth;grid-auto-rows:max-content;align-content:start;padding-right:12px;padding-bottom:calc(16px + env(safe-area-inset-bottom,0px));scroll-padding-bottom:calc(26px + env(safe-area-inset-bottom,0px));}",
      "body.aswg-im-mode .chat-tail-spacer,body.aswg-chat-pin .chat-tail-spacer{height:14px;min-height:calc(14px + env(safe-area-inset-bottom,0px));pointer-events:none;}",
      "body.aswg-im-mode .chat-bubble,body.aswg-chat-pin .chat-bubble{overflow:visible !important;max-height:none !important;}",
      "body.aswg-im-mode .chat-body,body.aswg-chat-pin .chat-body{display:block;white-space:pre-wrap !important;word-break:break-word !important;overflow:visible !important;text-overflow:clip;}",
      "body.aswg-im-mode .aswg-history-hint,body.aswg-chat-pin .aswg-history-hint{margin:0 auto 8px;padding:4px 10px;border-radius:999px;font-size:11px;line-height:1.2;color:var(--muted);background:rgba(194,184,163,.24);width:max-content;max-width:100%;}",
      "body.aswg-im-mode .aswg-history-hint.is-loading,body.aswg-chat-pin .aswg-history-hint.is-loading{color:var(--accent-ink);background:rgba(10,122,92,.12);}",
      "body.aswg-im-mode .aswg-chat-scroll-nav{position:absolute;right:12px;bottom:78px;display:flex;flex-direction:column;gap:8px;z-index:35;}",
      "body.aswg-im-mode .im-jump-btn{border:1px solid var(--line);background:rgba(255,255,255,.96);color:var(--ink);border-radius:999px;padding:6px 12px;font-size:12px;line-height:1;cursor:pointer;box-shadow:0 6px 14px rgba(0,0,0,.12);transition:opacity .16s ease,transform .16s ease,background .16s ease;}",
      "body.aswg-im-mode .im-jump-btn:hover{background:#f7f2e8;}",
      "body.aswg-im-mode .im-jump-btn.is-hidden{opacity:0;transform:translateY(4px);pointer-events:none;}",
      "body.aswg-im-mode .continue-form,body.aswg-chat-pin .continue-form{position:static !important;bottom:auto !important;z-index:3;background:#fff;padding-top:3px;padding-bottom:calc(3px + env(safe-area-inset-bottom,0px));margin-top:3px;border-top:1px solid rgba(194,184,163,.46);}",
      "body.aswg-im-mode .continue-form textarea,body.aswg-chat-pin .continue-form textarea{min-height:42px;max-height:14dvh;line-height:1.35;padding:7px 10px;}",
      "body.aswg-im-mode .continue-form .continue-actions,body.aswg-chat-pin .continue-form .continue-actions{margin-top:3px;display:flex;justify-content:flex-end;}",
      "body.aswg-im-mode .session-detail .meta-line,body.aswg-chat-pin .session-detail .meta-line{margin:2px 0 0;font-size:12px;line-height:1.3;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;}",
      "body.aswg-im-mode .session-detail .stream-row,body.aswg-chat-pin .session-detail .stream-row{display:flex;align-items:center;justify-content:space-between;gap:8px;}",
      "body.aswg-im-mode .session-detail .stream-row .status-text,body.aswg-chat-pin .session-detail .stream-row .status-text{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;}",
      "body.aswg-im-mode .session-actions,body.aswg-chat-pin .session-actions{align-items:center;gap:8px;flex-wrap:wrap;}",
      "body.aswg-im-mode .session-actions .btn,body.aswg-chat-pin .session-actions .btn,body.aswg-im-mode .stream-row .btn,body.aswg-chat-pin .stream-row .btn,body.aswg-im-mode .im-ctrl-btn,body.aswg-chat-pin .im-ctrl-btn,.delete-session-btn{display:inline-flex;align-items:center;gap:6px;padding:6px 10px;border-radius:10px;font-size:12px;line-height:1.15;}",
      ".im-btn-with-icon .im-btn-icon{display:inline-flex;align-items:center;justify-content:center;width:14px;height:14px;flex:0 0 14px;}",
      ".im-btn-with-icon .im-btn-icon svg{width:14px;height:14px;display:block;}",
      ".im-btn-with-icon .im-btn-label{display:inline-block;white-space:nowrap;}",
      "body.aswg-im-mode .im-sidebar-backdrop{display:none;}",
      "@media (max-width:900px){body.aswg-im-mode .layout,body.aswg-chat-pin .layout{height:100dvh;overflow:hidden;padding:8px;gap:8px;}body.aswg-im-mode .hero,body.aswg-chat-pin .hero{padding:8px 10px;}body.aswg-im-mode .hero p,body.aswg-chat-pin .hero p{display:none;}body.aswg-im-mode .panel-sessions,body.aswg-chat-pin .panel-sessions{height:calc(100dvh - 56px);padding:8px;display:flex;flex-direction:column;}body.aswg-im-mode .panel-sessions .session-body,body.aswg-chat-pin .panel-sessions .session-body{grid-template-columns:1fr;flex:1;min-height:0;}body.aswg-im-mode .panel-sessions .session-body>aside,body.aswg-chat-pin .panel-sessions .session-body>aside{position:fixed;top:58px;left:8px;bottom:8px;width:min(84vw,320px);z-index:85;transform:translateX(-115%);transition:transform .22s ease;box-shadow:0 14px 32px rgba(0,0,0,.22);}body.aswg-im-mode.im-sidebar-open .panel-sessions .session-body>aside,body.aswg-chat-pin.im-sidebar-open .panel-sessions .session-body>aside{transform:translateX(0);}body.aswg-im-mode .im-sidebar-backdrop,body.aswg-chat-pin .im-sidebar-backdrop{position:fixed;inset:0;background:rgba(12,10,8,.26);z-index:80;display:none;}body.aswg-im-mode.im-sidebar-open .im-sidebar-backdrop,body.aswg-chat-pin.im-sidebar-open .im-sidebar-backdrop{display:block;}body.aswg-im-mode #im-sessions-toggle,body.aswg-chat-pin #im-sessions-toggle{display:inline-flex;}body.aswg-im-mode .session-actions,body.aswg-chat-pin .session-actions{width:100%;align-items:center;gap:8px;}body.aswg-im-mode .session-actions label,body.aswg-chat-pin .session-actions label{flex:1;}body.aswg-im-mode .session-actions #reload-sessions,body.aswg-chat-pin .session-actions #reload-sessions{width:auto;}body.aswg-im-mode .session-detail,body.aswg-chat-pin .session-detail{height:100%;padding:8px 8px 6px;grid-template-rows:auto minmax(0,1fr) auto !important;}body.aswg-im-mode .session-detail .meta-line,body.aswg-chat-pin .session-detail .meta-line{display:none;}body.aswg-im-mode .session-detail>#chat-thread,body.aswg-chat-pin .session-detail>#chat-thread{min-height:0 !important;height:auto !important;max-height:none !important;overflow:auto !important;}body.aswg-im-mode .session-detail>#continue-form,body.aswg-chat-pin .session-detail>#continue-form{align-self:end;}body.aswg-im-mode .continue-form,body.aswg-chat-pin .continue-form{padding-top:2px;padding-bottom:calc(2px + env(safe-area-inset-bottom,0px));margin-top:2px;}body.aswg-im-mode .continue-form textarea,body.aswg-chat-pin .continue-form textarea{min-height:34px;max-height:9dvh;}body.aswg-im-mode .aswg-chat-scroll-nav,body.aswg-chat-pin .aswg-chat-scroll-nav{display:none !important;}body.aswg-im-mode .chat-bubble,body.aswg-chat-pin .chat-bubble{max-width:94%;}body.aswg-im-mode.im-settings-open #settings-panel,body.aswg-chat-pin.im-settings-open #settings-panel{left:8px;right:8px;top:58px;width:auto;max-height:78vh;}body.aswg-im-mode .im-btn-with-icon,body.aswg-chat-pin .im-btn-with-icon{min-width:32px;height:32px;padding:6px !important;justify-content:center;}body.aswg-im-mode .im-btn-with-icon .im-btn-label,body.aswg-chat-pin .im-btn-with-icon .im-btn-label{display:none;}}",
      "@media (min-width:901px){body.aswg-im-mode #im-sessions-toggle{display:none;}}",
    ].join("");
    document.head.appendChild(style);
  }

  function toggleElementVisible(node, visible) {
    if (!node) {
      return;
    }
    node.classList.toggle("is-hidden", !visible);
  }

  function ensureChatQuickNav() {
    const detail = document.querySelector(".session-detail");
    const thread = el.chatThread;
    if (!detail || !thread) {
      return null;
    }

    let nav = document.getElementById("chat-scroll-nav");
    if (!nav) {
      nav = document.createElement("div");
      nav.id = "chat-scroll-nav";
      nav.className = "aswg-chat-scroll-nav";
      detail.appendChild(nav);
    } else if (!detail.contains(nav)) {
      detail.appendChild(nav);
    }

    let topBtn = document.getElementById("chat-jump-top");
    if (!topBtn) {
      topBtn = document.createElement("button");
      topBtn.id = "chat-jump-top";
      topBtn.type = "button";
      topBtn.className = "im-jump-btn is-hidden";
      topBtn.textContent = "到顶部";
      nav.appendChild(topBtn);
    } else if (!nav.contains(topBtn)) {
      nav.appendChild(topBtn);
    }

    let bottomBtn = document.getElementById("chat-jump-bottom");
    if (!bottomBtn) {
      bottomBtn = document.createElement("button");
      bottomBtn.id = "chat-jump-bottom";
      bottomBtn.type = "button";
      bottomBtn.className = "im-jump-btn is-hidden";
      bottomBtn.textContent = "回到底部";
      nav.appendChild(bottomBtn);
    } else if (!nav.contains(bottomBtn)) {
      nav.appendChild(bottomBtn);
    }

    if (topBtn.dataset.boundClick !== "1") {
      topBtn.dataset.boundClick = "1";
      topBtn.addEventListener("click", () => {
        state.__aswgAutoFollowLatest = false;
        thread.scrollTo({ top: 0, behavior: "smooth" });
      });
    }
    if (bottomBtn.dataset.boundClick !== "1") {
      bottomBtn.dataset.boundClick = "1";
      bottomBtn.addEventListener("click", () => {
        state.__aswgAutoFollowLatest = true;
        const win = ensureChatWindowState();
        win.forceLatest = true;
        thread.scrollTo({ top: thread.scrollHeight, behavior: "smooth" });
      });
    }
    if (thread.dataset.boundQuickNav !== "1") {
      thread.dataset.boundQuickNav = "1";
      thread.addEventListener("scroll", updateChatQuickNav, { passive: true });
    }

    applyButtonIcons();

    return { topBtn, bottomBtn, thread };
  }

  function syncAutoFollowState(thread) {
    const target = thread || el.chatThread;
    if (!target) {
      return false;
    }
    const distanceToBottom = Math.max(0, target.scrollHeight - target.clientHeight - target.scrollTop);
    const followLatest = distanceToBottom <= 120;
    state.__aswgAutoFollowLatest = followLatest;
    return followLatest;
  }

  function updateChatQuickNav() {
    const result = ensureChatQuickNav();
    if (!result) {
      return;
    }
    const thread = result.thread;
    syncAutoFollowState(thread);
    const scrollable = thread.scrollHeight - thread.clientHeight > 160;
    const distanceToBottom = Math.max(0, thread.scrollHeight - thread.clientHeight - thread.scrollTop);
    const showTop = scrollable && thread.scrollTop > 24;
    const showBottom = scrollable && distanceToBottom > 24;
    toggleElementVisible(result.topBtn, showTop);
    toggleElementVisible(result.bottomBtn, showBottom);
  }

  function moveNode(node, parent) {
    if (!node || !parent || node.parentElement === parent) {
      return;
    }
    parent.appendChild(node);
  }

  function ensureChatScaffold() {
    const detail = document.querySelector(".session-detail");
    if (!detail) {
      return;
    }

    let head = document.getElementById("aswg-chat-head");
    if (!head) {
      head = document.createElement("div");
      head.id = "aswg-chat-head";
      head.className = "aswg-chat-head";
      detail.insertBefore(head, detail.firstChild);
    } else if (!detail.contains(head)) {
      detail.insertBefore(head, detail.firstChild);
    }

    const titleRow = document.getElementById("session-title-row");
    const title = el.sessionTitle || document.getElementById("session-title");
    const meta = el.sessionMeta || document.getElementById("session-meta");
    const stream = document.querySelector(".stream-row");
    if (titleRow) {
      moveNode(titleRow, head);
    } else {
      moveNode(title, head);
    }
    moveNode(meta, head);
    moveNode(stream, head);
    moveNode(el.chatThread, detail);
    moveNode(el.continueForm, detail);
  }

  function ensureChatWindowState() {
    if (!state.__aswgChatWindow || typeof state.__aswgChatWindow !== "object") {
      state.__aswgChatWindow = {
        sessionID: "",
        visibleStart: 0,
        chunkSize: 80,
        loadingOlder: false,
        forceLatest: false,
      };
    }
    if (typeof state.__aswgAutoFollowLatest !== "boolean") {
      state.__aswgAutoFollowLatest = true;
    }
    return state.__aswgChatWindow;
  }

  function resetVisibleWindowToLatest(sessionID) {
    const win = ensureChatWindowState();
    const total = Array.isArray(state.messages) ? state.messages.length : 0;
    win.sessionID = String(sessionID || state.selectedSessionID || "");
    win.visibleStart = Math.max(0, total - win.chunkSize);
    win.loadingOlder = false;
    win.forceLatest = true;
    state.__aswgAutoFollowLatest = true;
  }

  function renderHistoryHint() {
    const thread = el.chatThread;
    if (!thread) {
      return;
    }
    let hint = thread.querySelector(".aswg-history-hint");
    if (!hint) {
      hint = document.createElement("p");
      hint.className = "aswg-history-hint";
      thread.prepend(hint);
    }
    const win = ensureChatWindowState();
    const hiddenCount = Math.max(0, Number(win.visibleStart || 0));
    if (hiddenCount > 0) {
      hint.style.display = "block";
      hint.textContent = win.loadingOlder ? "正在加载更早消息..." : "上滑加载更早消息";
      hint.classList.toggle("is-loading", win.loadingOlder);
      return;
    }
    hint.classList.remove("is-loading");
    if (Array.isArray(state.messages) && state.messages.length > 0) {
      hint.style.display = "block";
      hint.textContent = "已显示全部历史消息";
      return;
    }
    hint.style.display = "none";
  }

  function maybeLoadOlderMessages() {
    const thread = el.chatThread;
    if (!thread) {
      return;
    }
    const win = ensureChatWindowState();
    if (win.loadingOlder || win.visibleStart <= 0) {
      return;
    }
    if (thread.scrollTop > 28) {
      return;
    }

    win.loadingOlder = true;
    win.visibleStart = Math.max(0, win.visibleStart - win.chunkSize);
    renderChatThread();
    requestAnimationFrame(() => {
      win.loadingOlder = false;
      renderHistoryHint();
      updateChatQuickNav();
    });
  }

  function bindThreadScrollLoader() {
    const thread = el.chatThread;
    if (!thread || thread.dataset.boundHistoryScroll === "1") {
      return;
    }
    thread.dataset.boundHistoryScroll = "1";
    thread.addEventListener(
      "scroll",
      () => {
        syncAutoFollowState(thread);
        updateChatQuickNav();
        maybeLoadOlderMessages();
      },
      { passive: true },
    );
  }

  async function fetchAllSessionEvents(adapter, sessionID) {
    const items = [];
    const seenSeq = new Set();
    let cursor = "";
    const pageLimit = 500;
    const maxPages = 80;

    for (let i = 0; i < maxPages; i += 1) {
      const query = new URLSearchParams({ limit: String(pageLimit) });
      if (cursor) {
        query.set("cursor", cursor);
      }
      const result = await fetchAPI(
        "/api/v1/adapters/" +
          encodeURIComponent(adapter) +
          "/sessions/" +
          encodeURIComponent(sessionID) +
          "/events?" +
          query.toString(),
        { method: "GET" },
      );
      const pageItems = Array.isArray(result && result.data && result.data.items) ? result.data.items : [];
      for (const event of pageItems) {
        const seq = Number(event && event.seq ? event.seq : 0);
        if (seq > 0) {
          if (seenSeq.has(seq)) {
            continue;
          }
          seenSeq.add(seq);
        }
        items.push(event);
      }

      const hasMore = Boolean(result && result.data && result.data.has_more);
      const nextCursor = String((result && result.data && result.data.next_cursor) || "");
      if (!hasMore || !nextCursor || nextCursor === cursor || pageItems.length === 0) {
        break;
      }
      cursor = nextCursor;
    }

    return items;
  }

  function ensureRunningSummaryElement() {
    const actions = document.querySelector(".session-actions");
    if (!actions) {
      return null;
    }
    let summary = document.getElementById("running-sessions-summary");
    if (!summary) {
      summary = document.createElement("p");
      summary.id = "running-sessions-summary";
      summary.className = "status-text";
      actions.insertBefore(summary, actions.firstChild);
    }
    return summary;
  }

  function decorateSessionUI() {
    applyIMLayout();
    if (!Array.isArray(state.sessions) || !el.sessionList) {
      return;
    }

    ensurePatchStyles();
    ensureChatScaffold();
    ensureDeleteSessionButton();
    applyButtonIcons();

    const summary = ensureRunningSummaryElement();
    const runningCount = state.sessions.reduce((count, item) => {
      return count + (normalizeSessionStatus(item && item.status) === "running" ? 1 : 0);
    }, 0);
    if (summary) {
      summary.textContent = "进行中: " + String(runningCount);
    }

    const cards = el.sessionList.querySelectorAll("button.session-card");
    cards.forEach((card, idx) => {
      const session = state.sessions[idx];
      if (!session) {
        return;
      }
      const status = normalizeSessionStatus(session.status);
      let line = card.querySelector(".session-card-status");
      if (!line) {
        line = document.createElement("p");
        line.className = "session-card-meta session-card-status";
        card.appendChild(line);
      }
      line.textContent = "状态: " + sessionStatusLabel(status);
      line.classList.toggle("is-running", status === "running");
      line.classList.toggle("is-idle", status !== "running");
    });

    if (!el.sessionMeta || !state.selectedSessionID) {
      return;
    }
    const selected = state.sessions.find((item) => item && item.id === state.selectedSessionID);
    if (!selected) {
      return;
    }
    const base = String(el.sessionMeta.textContent || "").replace(/ · 状态: .+$/, "");
    el.sessionMeta.textContent = base + " · 状态: " + sessionStatusLabel(normalizeSessionStatus(selected.status));
    updateDeleteSessionButtonState();
  }

  function updateLocalSessionStatus(sessionID, nextStatus) {
    if (!sessionID || !Array.isArray(state.sessions)) {
      return;
    }
    const targetStatus = normalizeSessionStatus(nextStatus);
    let changed = false;
    state.sessions = state.sessions.map((item) => {
      if (!item || item.id !== sessionID) {
        return item;
      }
      if (normalizeSessionStatus(item.status) === targetStatus) {
        return item;
      }
      changed = true;
      return { ...item, status: targetStatus };
    });
    if (changed && typeof renderSessionList === "function") {
      renderSessionList();
    }
    decorateSessionUI();
  }

  function bindEnterToSend() {
    if (!el.continuePrompt || !el.continueForm) {
      return;
    }
    if (el.continuePrompt.dataset.enterToSendBound === "1") {
      return;
    }
    el.continuePrompt.dataset.enterToSendBound = "1";
    el.continuePrompt.addEventListener("keydown", (event) => {
      if (event.key !== "Enter") {
        return;
      }
      if (event.shiftKey || event.ctrlKey || event.altKey || event.metaKey || event.isComposing) {
        return;
      }
      event.preventDefault();
      el.continueForm.requestSubmit();
    });

    if (!String(el.continuePrompt.placeholder || "").includes("Enter")) {
      el.continuePrompt.placeholder = "输入继续会话的提示词（Enter 发送，Shift+Enter 换行）";
    }
  }

  function ensureSettingsToggle() {
    const hero = document.querySelector(".hero");
    if (!hero) {
      return;
    }
    let tools = document.getElementById("im-header-tools");
    if (!tools) {
      tools = document.createElement("div");
      tools.id = "im-header-tools";
      tools.className = "im-header-tools";
      hero.appendChild(tools);
    }

    let btn = document.getElementById("im-settings-toggle");
    if (!btn) {
      btn = document.createElement("button");
      btn.id = "im-settings-toggle";
      btn.type = "button";
      btn.className = "im-ctrl-btn";
      btn.textContent = "连接设置";
      tools.appendChild(btn);
    }
    if (btn.dataset.boundSettings === "1") {
      return;
    }
    btn.dataset.boundSettings = "1";
    btn.addEventListener("click", () => {
      document.body.classList.toggle("im-settings-open");
    });
    applyButtonIcons();
  }

  function closeIMSidebar() {
    document.body.classList.remove("im-sidebar-open");
  }

  function ensureSidebarToggle() {
    const actions = document.querySelector(".session-actions");
    if (!actions) {
      return;
    }
    let btn = document.getElementById("im-sessions-toggle");
    if (!btn) {
      btn = document.createElement("button");
      btn.id = "im-sessions-toggle";
      btn.type = "button";
      btn.className = "btn btn-ghost";
      btn.textContent = "会话列表";
      actions.insertBefore(btn, actions.firstChild);
    }
    if (btn.dataset.boundSidebar !== "1") {
      btn.dataset.boundSidebar = "1";
      btn.addEventListener("click", () => {
        document.body.classList.toggle("im-sidebar-open");
      });
    }
    applyButtonIcons();

    let backdrop = document.getElementById("im-sidebar-backdrop");
    if (!backdrop) {
      backdrop = document.createElement("div");
      backdrop.id = "im-sidebar-backdrop";
      backdrop.className = "im-sidebar-backdrop";
      document.body.appendChild(backdrop);
    }
    if (backdrop.dataset.boundSidebar !== "1") {
      backdrop.dataset.boundSidebar = "1";
      backdrop.addEventListener("click", closeIMSidebar);
    }
  }

  function applyIMLayout() {
    document.body.classList.add("aswg-im-mode");
    ensureSettingsToggle();
    ensureSidebarToggle();
    ensureChatQuickNav();
    applyButtonIcons();
    updateChatQuickNav();
  }

  function applyComposerPinLayout() {
    document.body.classList.add("aswg-chat-pin");
    ensureChatScaffold();
    const detail = document.querySelector(".session-detail");
    const thread = document.getElementById("chat-thread");
    const form = document.getElementById("continue-form");
    const prompt = document.getElementById("continue-prompt");
    if (detail) {
      detail.style.minHeight = "0";
      detail.style.height = "100%";
      detail.style.overflow = "hidden";
      detail.style.display = "grid";
      detail.style.gridTemplateRows = "auto minmax(0,1fr) auto";
    }
    if (thread) {
      thread.style.gridRow = "2";
      thread.style.minHeight = "0";
      thread.style.height = "auto";
      thread.style.maxHeight = "none";
      thread.style.overflow = "auto";
      thread.style.overscrollBehavior = "contain";
    }
    if (form) {
      form.style.gridRow = "3";
      form.style.alignSelf = "end";
      form.style.position = "static";
      form.style.bottom = "auto";
      form.style.zIndex = "3";
    }
    if (prompt) {
      prompt.rows = 2;
      prompt.style.minHeight = "34px";
      prompt.style.maxHeight = "9dvh";
      prompt.style.height = "auto";
    }
    applyButtonIcons();
  }

  function ensureDeleteSessionButton() {
    const titleNode = el.sessionTitle || document.getElementById("session-title");
    if (!titleNode) {
      return null;
    }

    const detail = titleNode.closest(".session-detail");
    if (!detail) {
      return null;
    }
    const head = document.getElementById("aswg-chat-head");
    const host = head && detail.contains(head) ? head : detail;

    let titleRow = document.getElementById("session-title-row");
    if (!titleRow) {
      titleRow = document.createElement("div");
      titleRow.id = "session-title-row";
      titleRow.className = "session-detail-title-row";
      if (host.firstChild) {
        host.insertBefore(titleRow, host.firstChild);
      } else {
        host.appendChild(titleRow);
      }
      titleRow.appendChild(titleNode);
    } else if (!titleRow.contains(titleNode)) {
      titleRow.insertBefore(titleNode, titleRow.firstChild || null);
    }
    if (!host.contains(titleRow)) {
      host.insertBefore(titleRow, host.firstChild || null);
    }

    let btn = document.getElementById("delete-session-button");
    if (!btn) {
      btn = document.createElement("button");
      btn.id = "delete-session-button";
      btn.type = "button";
      btn.className = "delete-session-btn";
      btn.textContent = "删除会话";
      titleRow.appendChild(btn);
    } else if (!titleRow.contains(btn)) {
      titleRow.appendChild(btn);
    }
    setIconButton(btn, "trash", "删除会话");
    return btn;
  }

  function updateDeleteSessionButtonState() {
    const btn = document.getElementById("delete-session-button");
    if (!btn) {
      return;
    }
    const visible = !!state.selectedSessionID;
    btn.style.visibility = visible ? "visible" : "hidden";
    btn.disabled = !state.selectedSessionID || !!state.continuePending;
  }

  function clearSelectedSessionLocally() {
    if (typeof closeStream === "function") {
      closeStream();
    }
    state.selectedSessionID = "";
    state.lastSeq = 0;
    state.messages = [];
    state.assistantDraftId = "";
    if (typeof renderChatThread === "function") {
      renderChatThread();
    }
  }

  function bindDeleteSessionAction() {
    const btn = ensureDeleteSessionButton();
    if (!btn) {
      return;
    }
    if (btn.dataset.boundDeleteAction === "1") {
      updateDeleteSessionButtonState();
      return;
    }
    btn.dataset.boundDeleteAction = "1";
    btn.addEventListener("click", async () => {
      const adapter = typeof currentAdapter === "function" ? currentAdapter() : "";
      const sessionID = String(state.selectedSessionID || "").trim();
      if (!adapter || !sessionID) {
        return;
      }
      if (state.continuePending) {
        if (typeof setStreamStatus === "function") {
          setStreamStatus("正在处理消息，暂不能删除会话", true);
        }
        return;
      }

      const ok = window.confirm("确认删除当前会话？删除后将从列表中移除。");
      if (!ok) {
        return;
      }

      btn.disabled = true;
      try {
        await fetchAPI("/api/v1/adapters/" + encodeURIComponent(adapter) + "/sessions/" + encodeURIComponent(sessionID), {
          method: "DELETE",
        });
        clearSelectedSessionLocally();
        await refreshSessions();
        await restoreSelectedSession();
        if (typeof setStreamStatus === "function") {
          setStreamStatus("会话已删除", false);
        }
      } catch (err) {
        if (typeof setStreamStatus === "function") {
          setStreamStatus("删除失败: " + err.message, true);
        }
      } finally {
        updateDeleteSessionButtonState();
      }
    });
    updateDeleteSessionButtonState();
  }

  try {
    if (typeof renderChatThread === "function") {
      const originalRenderChatThread = renderChatThread;
      renderChatThread = function patchedRenderChatThread() {
        ensurePatchStyles();
        applyComposerPinLayout();
        ensureChatScaffold();
        bindThreadScrollLoader();

        const win = ensureChatWindowState();
        const selected = String(state.selectedSessionID || "");
        const allMessages = Array.isArray(state.messages) ? state.messages : [];
        if (win.sessionID !== selected) {
          resetVisibleWindowToLatest(selected);
        }
        if (win.visibleStart > allMessages.length) {
          win.visibleStart = Math.max(0, allMessages.length - win.chunkSize);
        }

        const thread = el.chatThread;
        const distanceToBottom = thread ? Math.max(0, thread.scrollHeight - thread.clientHeight - thread.scrollTop) : 0;
        const autoFollowLatest = state.__aswgAutoFollowLatest !== false;
        const shouldStickToLatest = Boolean(win.forceLatest || autoFollowLatest);
        const keepGapFromBottom = thread && !shouldStickToLatest && distanceToBottom > 0 ? distanceToBottom : null;

        const visibleMessages = allMessages.slice(Math.max(0, Number(win.visibleStart || 0)));
        state.messages = visibleMessages;
        try {
          originalRenderChatThread();
        } finally {
          state.messages = allMessages;
        }

        const nextThread = el.chatThread;
        if (nextThread && shouldStickToLatest) {
          nextThread.scrollTop = nextThread.scrollHeight;
          state.__aswgAutoFollowLatest = true;
        } else if (nextThread && keepGapFromBottom !== null) {
          const maxTop = Math.max(0, nextThread.scrollHeight - nextThread.clientHeight);
          nextThread.scrollTop = Math.max(0, maxTop - keepGapFromBottom);
        }
        win.forceLatest = false;
        renderHistoryHint();
        updateChatQuickNav();
      };
    }

    if (typeof renderSessionList === "function") {
      const originalRenderSessionList = renderSessionList;
      renderSessionList = function patchedRenderSessionList() {
        originalRenderSessionList();
        decorateSessionUI();
      };
    }

    if (typeof refreshSessions === "function") {
      const originalRefreshSessions = refreshSessions;
      refreshSessions = async function patchedRefreshSessions() {
        await originalRefreshSessions();
        decorateSessionUI();
        bindDeleteSessionAction();
      };
    }

    if (typeof loadSession === "function") {
      loadSession = async function patchedLoadSession(sessionID) {
        const adapter = typeof currentAdapter === "function" ? currentAdapter() : "";
        if (!adapter || !sessionID) {
          return;
        }

        const loadToken = String(Date.now()) + ":" + String(sessionID);
        state.__aswgLoadToken = loadToken;
        if (typeof setStreamStatus === "function") {
          setStreamStatus("加载会话中...", false);
        }

        try {
          const detail = await fetchAPI(
            "/api/v1/adapters/" + encodeURIComponent(adapter) + "/sessions/" + encodeURIComponent(sessionID),
            { method: "GET" },
          );
          const eventItems = await fetchAllSessionEvents(adapter, sessionID);
          if (state.__aswgLoadToken !== loadToken || String(state.selectedSessionID || "") !== String(sessionID)) {
            return;
          }

          const sessionDetail = (detail && detail.data) || {};
          if (el.sessionTitle) {
            el.sessionTitle.textContent = sessionDetail.title || sessionID;
          }
          if (el.sessionMeta) {
            el.sessionMeta.textContent =
              "workspace: " +
              String(sessionDetail.workspace || "-") +
              " · updated_at: " +
              String(sessionDetail.updated_at || "-");
          }

          const rebuilt = buildMessagesFromEvents(eventItems);
          state.messages = rebuilt.messages;
          state.assistantDraftId = rebuilt.assistantDraftId;
          state.lastSeq =
            eventItems.length > 0 ? Number((eventItems[eventItems.length - 1] && eventItems[eventItems.length - 1].seq) || 0) : 0;
          resetVisibleWindowToLatest(sessionID);
          renderChatThread();
          await connectStream();
          closeIMSidebar();
          decorateSessionUI();
          bindDeleteSessionAction();
        } catch (err) {
          if (typeof setStreamStatus === "function") {
            setStreamStatus("加载会话详情失败: " + err.message, true);
          }
        }
      };
    }

    if (typeof handleWSFrame === "function") {
      const originalHandleWSFrame = handleWSFrame;
      handleWSFrame = function patchedHandleWSFrame(frame) {
        originalHandleWSFrame(frame);

        if (!frame || frame.frame_type !== "event") {
          return;
        }

        const event = frame.data || {};
        const role = String((event.normalized && event.normalized.role) || "");
        const done = Boolean(event.normalized && event.normalized.done);

        if (role === "assistant" && event.type === "message.done" && done) {
          updateLocalSessionStatus(state.selectedSessionID, "idle");
          return;
        }

        if (event.type === "message.user" || event.type === "message.delta") {
          updateLocalSessionStatus(state.selectedSessionID, "running");
        }
      };
    }

    if (el.continueForm && el.continueForm.dataset.statusPatchBound !== "1") {
      el.continueForm.dataset.statusPatchBound = "1";
      el.continueForm.addEventListener("submit", () => {
        state.__aswgAutoFollowLatest = true;
        const win = ensureChatWindowState();
        win.forceLatest = true;
        setTimeout(() => {
          if (state.continuePending) {
            updateLocalSessionStatus(state.selectedSessionID, "running");
          }
          updateDeleteSessionButtonState();
        }, 0);
      });
    }

    bindEnterToSend();
    applyIMLayout();
    applyComposerPinLayout();
    decorateSessionUI();
    bindDeleteSessionAction();
    window.addEventListener("resize", updateChatQuickNav, { passive: true });
    setInterval(decorateSessionUI, 5000);
    setInterval(applyComposerPinLayout, 2000);
    setInterval(updateDeleteSessionButtonState, 1500);
    setInterval(updateChatQuickNav, 1500);
  } catch (err) {
    console.warn("ASWG runtime patch failed", err);
  }
})();
`

type contextKey string

const requestIDKey contextKey = "request_id"

type Server struct {
	cfg            Config
	registry       *adapter.Registry
	httpSrv        *http.Server
	static         http.Handler
	sessionLimiter *fixedWindowRateLimiter
}

type apiErrorBody struct {
	Type      string         `json:"type"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

type apiEnvelope struct {
	Code      int           `json:"code"`
	Message   string        `json:"message"`
	Data      any           `json:"data"`
	RequestID string        `json:"request_id"`
	Error     *apiErrorBody `json:"error,omitempty"`
}

type wsFrame struct {
	FrameType string    `json:"frame_type"`
	RequestID string    `json:"request_id"`
	Seq       int64     `json:"seq,omitempty"`
	Ts        time.Time `json:"ts"`
	Data      any       `json:"data,omitempty"`
}

type wsConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func New(cfg Config, registry *adapter.Registry) *Server {
	if cfg.FrontendDir == "" {
		cfg.FrontendDir = "frontend/src"
	}
	return &Server{
		cfg:            cfg,
		registry:       registry,
		static:         http.FileServer(http.Dir(cfg.FrontendDir)),
		sessionLimiter: newFixedWindowRateLimiter(time.Second),
	}
}

func (s *Server) Run(ctx context.Context) error {
	s.httpSrv = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("server started on %s", s.httpSrv.Addr)
	err := s.httpSrv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/adapters", s.handleAdapters)
	mux.HandleFunc("/api/v1/adapters/", s.handleAdapterRoutes)
	mux.HandleFunc("/ws/v1/adapters/", s.handleWSRoutes)
	mux.HandleFunc("/", s.handleFrontend)
	return s.withRequestID(s.withRecover(s.withLogging(mux)))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeMethodNotAllowed(w, r)
		return
	}

	type adapterHealth struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		LatencyMS int64  `json:"latency_ms"`
	}

	requestID := getRequestID(r)
	items := s.registry.List()
	head := make([]adapterHealth, 0, len(items))
	overallStatus := "ok"
	for _, info := range items {
		a, _ := s.registry.Get(info.Name)
		hcCtx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
		latency, err := a.HealthCheck(hcCtx)
		cancel()
		status := "ok"
		if err != nil {
			status = "degraded"
			overallStatus = "degraded"
		}
		head = append(head, adapterHealth{Name: info.Name, Status: status, LatencyMS: latency})
	}

	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"status":   overallStatus,
			"version":  s.cfg.Version,
			"time":     time.Now().UTC(),
			"adapters": head,
		},
	})
}

func (s *Server) handleAdapters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeMethodNotAllowed(w, r)
		return
	}
	if !s.requireAuth(w, r, false) {
		return
	}
	requestID := getRequestID(r)
	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"items": s.registry.List(),
		},
	})
}

func (s *Server) handleAdapterRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r, false) {
		return
	}
	requestID := getRequestID(r)

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/adapters/")
	parts := splitPath(path)
	if len(parts) < 2 {
		s.writeNotFound(w, requestID)
		return
	}

	adapterName := parts[0]
	a, ok := s.registry.Get(adapterName)
	if !ok {
		s.writeBusinessError(w, http.StatusNotFound, 4002, "adapter not found", requestID, "not_found", false, map[string]any{"adapter": adapterName})
		return
	}

	if parts[1] != "sessions" {
		s.writeNotFound(w, requestID)
		return
	}

	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		s.handleSessionsList(w, r, requestID, a)
	case len(parts) == 2 && r.Method == http.MethodPost:
		s.handleCreateSession(w, r, requestID, a)
	case len(parts) == 3 && r.Method == http.MethodGet:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleSessionDetail(w, r, requestID, a, sessionID)
	case len(parts) == 3 && r.Method == http.MethodDelete:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleDeleteSession(w, r, requestID, a, sessionID)
	case len(parts) == 4 && parts[3] == "events" && r.Method == http.MethodGet:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleSessionEvents(w, r, requestID, a, sessionID)
	case len(parts) == 4 && parts[3] == "continue" && r.Method == http.MethodPost:
		sessionID, err := url.PathUnescape(parts[2])
		if err != nil {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
			return
		}
		s.handleContinue(w, r, requestID, a, sessionID)
	default:
		s.writeMethodNotAllowed(w, r)
	}
}

func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter) {
	if !s.allowSessionListRate(w, r, requestID) {
		return
	}

	q := r.URL.Query()
	limit, err := parseInt(q.Get("limit"), model.DefaultSessionsLimit)
	if err != nil || limit < 1 || limit > model.MaxSessionsLimit {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: limit", requestID, "validation_error", false, map[string]any{"field": "limit", "reason": "must be between 1 and 100"})
		return
	}

	updatedAfter, err := parseRFC3339Ptr(q.Get("updated_after"))
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: updated_after", requestID, "validation_error", false, map[string]any{"field": "updated_after", "reason": "must be RFC3339"})
		return
	}
	updatedBefore, err := parseRFC3339Ptr(q.Get("updated_before"))
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: updated_before", requestID, "validation_error", false, map[string]any{"field": "updated_before", "reason": "must be RFC3339"})
		return
	}

	resp, err := a.DiscoverSessions(r.Context(), model.DiscoverRequest{
		Query:         q.Get("query"),
		Workspace:     q.Get("workspace"),
		UpdatedAfter:  updatedAfter,
		UpdatedBefore: updatedBefore,
		Limit:         limit,
		Cursor:        q.Get("cursor"),
		SortBy:        q.Get("sort_by"),
		SortOrder:     q.Get("sort_order"),
	})
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}

	s.writeJSON(w, http.StatusOK, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: resp})
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, sessionID string) {
	detail, err := a.GetSession(r.Context(), sessionID)
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	s.writeJSON(w, http.StatusOK, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: detail})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, sessionID string) {
	if err := a.DeleteSession(r.Context(), sessionID); err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}
	s.writeJSON(w, http.StatusOK, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data: map[string]any{
			"session_id": sessionID,
			"deleted":    true,
		},
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter) {
	defer r.Body.Close()
	var body model.CreateSessionRequest

	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		if !errors.Is(err, io.EOF) {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{
				"field":  "body",
				"reason": err.Error(),
			})
			return
		}
	}

	detail, err := a.CreateSession(r.Context(), model.CreateSessionInput{
		Title:      body.Title,
		Workspace:  body.Workspace,
		SeedPrompt: body.SeedPrompt,
	})
	if err != nil {
		if errors.Is(err, model.ErrInvalidParam) {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter", requestID, "validation_error", false, nil)
			return
		}
		s.writeMappedError(w, requestID, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, apiEnvelope{
		Code:      0,
		Message:   "ok",
		RequestID: requestID,
		Data:      detail,
	})
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, sessionID string) {
	q := r.URL.Query()
	limit, err := parseInt(q.Get("limit"), model.DefaultEventsLimit)
	if err != nil || limit < 1 || limit > model.MaxEventsLimit {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: limit", requestID, "validation_error", false, map[string]any{"field": "limit", "reason": "must be between 1 and 500"})
		return
	}

	resp, err := a.GetSessionEvents(r.Context(), model.EventsRequest{
		SessionID: sessionID,
		Limit:     limit,
		Cursor:    q.Get("cursor"),
	})
	if err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}

	s.writeJSON(w, http.StatusOK, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: resp})
}

func (s *Server) handleContinue(w http.ResponseWriter, r *http.Request, requestID string, a adapter.AgentAdapter, sessionID string) {
	defer r.Body.Close()
	var body model.ContinueRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid request body", requestID, "validation_error", false, map[string]any{"field": "body", "reason": err.Error()})
		return
	}

	job, err := a.ContinueSession(context.Background(), model.ContinueInput{
		SessionID:      sessionID,
		Prompt:         body.Prompt,
		Cwd:            body.Cwd,
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	})
	if err != nil {
		if errors.Is(err, model.ErrSessionNotFound) {
			s.writeBusinessError(w, http.StatusNotFound, 4003, "session not found", requestID, "not_found", false, map[string]any{"session_id": sessionID})
			return
		}
		if errors.Is(err, model.ErrInvalidParam) {
			s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: prompt", requestID, "validation_error", false, map[string]any{"field": "prompt", "reason": "must be between 1 and 8000"})
			return
		}
		s.writeBusinessError(w, http.StatusInternalServerError, 4004, "continue start failed", requestID, "adapter_error", true, map[string]any{"reason": err.Error()})
		return
	}

	s.writeJSON(w, http.StatusAccepted, apiEnvelope{Code: 0, Message: "ok", RequestID: requestID, Data: job})
}

func (s *Server) allowSessionListRate(w http.ResponseWriter, r *http.Request, requestID string) bool {
	if s.cfg.RateLimitSessionsPerSec <= 0 {
		return true
	}
	key := "sessions:" + clientIP(r)
	if s.sessionLimiter.allow(key, s.cfg.RateLimitSessionsPerSec, time.Now().UTC()) {
		return true
	}
	s.writeBusinessError(w, http.StatusTooManyRequests, 4290, "too many requests", requestID, "rate_limited", true, map[string]any{
		"limit_per_sec": s.cfg.RateLimitSessionsPerSec,
	})
	return false
}

func (s *Server) handleWSRoutes(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestID(r)
	if r.Method != http.MethodGet {
		s.writeMethodNotAllowed(w, r)
		return
	}
	if !s.requireAuth(w, r, true) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/ws/v1/adapters/")
	parts := splitPath(path)
	if len(parts) != 3 || parts[1] != "sessions" {
		s.writeNotFound(w, requestID)
		return
	}

	adapterName := parts[0]
	a, ok := s.registry.Get(adapterName)
	if !ok {
		s.writeBusinessError(w, http.StatusNotFound, 4002, "adapter not found", requestID, "not_found", false, map[string]any{"adapter": adapterName})
		return
	}

	sessionID, err := url.PathUnescape(parts[2])
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid session id", requestID, "validation_error", false, nil)
		return
	}
	if _, err := a.GetSession(r.Context(), sessionID); err != nil {
		s.writeMappedError(w, requestID, err)
		return
	}

	fromSeq, err := parseSeqFromQuery(r.URL.Query())
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter: cursor/last_seq", requestID, "validation_error", false, map[string]any{"field": "cursor", "reason": err.Error()})
		return
	}

	conn, err := upgradeToWebSocket(w, r)
	if err != nil {
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "websocket upgrade failed", requestID, "validation_error", false, map[string]any{"reason": err.Error()})
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		buf := make([]byte, 1024)
		for {
			if _, readErr := conn.conn.Read(buf); readErr != nil {
				cancel()
				return
			}
		}
	}()

	eventCh, unsubscribe, err := a.Subscribe(ctx, sessionID, fromSeq)
	if err != nil {
		_ = conn.WriteJSON(wsFrame{FrameType: "error", RequestID: requestID, Ts: time.Now().UTC(), Data: map[string]any{"code": 4003, "message": "session not found"}})
		return
	}
	defer unsubscribe()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	lastSeq := fromSeq
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-eventCh:
			if !ok {
				return
			}
			lastSeq = ev.Seq
			if err := conn.WriteJSON(wsFrame{
				FrameType: "event",
				RequestID: requestID,
				Seq:       ev.Seq,
				Ts:        time.Now().UTC(),
				Data:      ev,
			}); err != nil {
				return
			}
			if done, _ := ev.Normalized["done"].(bool); done {
				if err := conn.WriteJSON(wsFrame{FrameType: "done", RequestID: requestID, Seq: ev.Seq, Ts: time.Now().UTC(), Data: map[string]any{"session_id": sessionID}}); err != nil {
					return
				}
			}
		case <-ticker.C:
			if err := conn.WriteJSON(wsFrame{FrameType: "heartbeat", RequestID: requestID, Seq: lastSeq, Ts: time.Now().UTC(), Data: map[string]any{"session_id": sessionID}}); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}

	if r.URL.Path == "/" {
		http.ServeFile(w, r, filepath.Join(s.cfg.FrontendDir, "index.html"))
		return
	}

	candidate := filepath.Clean(filepath.Join(s.cfg.FrontendDir, r.URL.Path))
	if strings.HasSuffix(strings.ToLower(r.URL.Path), ".js") && s.servePatchedJS(w, r, candidate) {
		return
	}
	if _, err := os.Stat(candidate); err == nil {
		http.ServeFile(w, r, candidate)
		return
	}

	http.ServeFile(w, r, filepath.Join(s.cfg.FrontendDir, "index.html"))
}

func (s *Server) servePatchedJS(w http.ResponseWriter, r *http.Request, candidate string) bool {
	src, err := os.ReadFile(candidate)
	if err != nil {
		return false
	}

	content := string(src)
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	// Avoid duplicate append when file already embeds a runtime patch marker.
	if strings.Contains(content, "ASWG Runtime Patch v") {
		_, _ = w.Write(src)
		return true
	}

	// Primary app script gets runtime patch appended.
	lowerPath := strings.ToLower(r.URL.Path)
	shouldInject := lowerPath == "/app.js" ||
		(strings.Contains(content, "aswg_runtime_config_v1") && strings.Contains(content, "chat-thread")) ||
		(strings.Contains(content, "function renderChatThread") && strings.Contains(content, "continue-form"))

	_, _ = w.Write(src)
	if shouldInject {
		_, _ = io.WriteString(w, frontendAppRuntimePatch)
	}
	return true
}

func (s *Server) writeMappedError(w http.ResponseWriter, requestID string, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidParam):
		s.writeBusinessError(w, http.StatusBadRequest, 4001, "invalid parameter", requestID, "validation_error", false, nil)
	case errors.Is(err, model.ErrAdapterNotFound):
		s.writeBusinessError(w, http.StatusNotFound, 4002, "adapter not found", requestID, "not_found", false, nil)
	case errors.Is(err, model.ErrSessionNotFound):
		s.writeBusinessError(w, http.StatusNotFound, 4003, "session not found", requestID, "not_found", false, nil)
	default:
		s.writeBusinessError(w, http.StatusInternalServerError, 5000, "internal error", requestID, "internal_error", true, map[string]any{"reason": err.Error()})
	}
}

func (s *Server) writeNotFound(w http.ResponseWriter, requestID string) {
	s.writeBusinessError(w, http.StatusNotFound, 5000, "not found", requestID, "not_found", false, nil)
}

func (s *Server) writeMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestID(r)
	s.writeBusinessError(w, http.StatusMethodNotAllowed, 4001, "method not allowed", requestID, "validation_error", false, nil)
}

func (s *Server) writeBusinessError(w http.ResponseWriter, httpStatus, code int, msg, requestID, typ string, retryable bool, details map[string]any) {
	s.writeJSON(w, httpStatus, apiEnvelope{
		Code:      code,
		Message:   msg,
		Data:      nil,
		RequestID: requestID,
		Error: &apiErrorBody{
			Type:      typ,
			Retryable: retryable,
			Details:   details,
		},
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v apiEnvelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request, allowQueryToken bool) bool {
	if s.cfg.AuthToken == "" {
		return true
	}
	token := extractBearerToken(r.Header.Get("Authorization"))
	if token == "" && allowQueryToken {
		token = strings.TrimSpace(r.URL.Query().Get("access_token"))
	}
	if token != s.cfg.AuthToken {
		s.writeBusinessError(w, http.StatusUnauthorized, 4010, "unauthorized", getRequestID(r), "auth_error", false, nil)
		return false
	}
	return true
}

func extractBearerToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func parseInt(raw string, fallback int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	return v, nil
}

func parseRFC3339Ptr(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	ut := t.UTC()
	return &ut, nil
}

func parseSeqFromQuery(values url.Values) (int64, error) {
	if cursor := strings.TrimSpace(values.Get("cursor")); cursor != "" {
		return model.DecodeSeqCursor(cursor)
	}
	if rawLast := strings.TrimSpace(values.Get("last_seq")); rawLast != "" {
		v, err := strconv.ParseInt(rawLast, 10, 64)
		if err != nil {
			return 0, err
		}
		if v < 0 {
			return 0, fmt.Errorf("last_seq must be >= 0")
		}
		return v, nil
	}
	return 0, nil
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-Id")
		if strings.TrimSpace(reqID) == "" {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-Id", reqID)
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered request_id=%s err=%v", getRequestID(r), rec)
				s.writeBusinessError(w, http.StatusInternalServerError, 5000, "internal error", getRequestID(r), "internal_error", true, nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("request_id=%s method=%s path=%s latency_ms=%d", getRequestID(r), r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}

func getRequestID(r *http.Request) string {
	if v, ok := r.Context().Value(requestIDKey).(string); ok && v != "" {
		return v
	}
	return "req_unknown"
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return "req_" + hex.EncodeToString(buf)
}

func upgradeToWebSocket(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if !headerContainsToken(r.Header, "Connection", "upgrade") {
		return nil, fmt.Errorf("missing Connection: upgrade")
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return nil, fmt.Errorf("missing Upgrade: websocket")
	}
	key := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if key == "" {
		return nil, fmt.Errorf("missing Sec-WebSocket-Key")
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("server does not support websocket")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	hasher := sha1.New() // #nosec G401: protocol requires SHA-1 for websocket handshake.
	_, _ = hasher.Write([]byte(key + wsGUID))
	accept := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

	response := []string{
		"HTTP/1.1 101 Switching Protocols",
		"Upgrade: websocket",
		"Connection: Upgrade",
		"Sec-WebSocket-Accept: " + accept,
		"",
		"",
	}
	if _, err := rw.WriteString(strings.Join(response, "\r\n")); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &wsConn{conn: conn}, nil
}

func headerContainsToken(h http.Header, key, want string) bool {
	vals := h.Values(key)
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), want) {
				return true
			}
		}
	}
	return false
}

func (c *wsConn) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(0x1, payload)
}

func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode)
	l := len(payload)
	switch {
	case l < 126:
		header = append(header, byte(l))
	case l <= 65535:
		header = append(header, 126)
		tmp := make([]byte, 2)
		binary.BigEndian.PutUint16(tmp, uint16(l))
		header = append(header, tmp...)
	default:
		header = append(header, 127)
		tmp := make([]byte, 8)
		binary.BigEndian.PutUint64(tmp, uint64(l))
		header = append(header, tmp...)
	}

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if _, err := c.conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func (c *wsConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

func ensureFile(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findFrontendDir(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if ensureFile(filepath.Join(p, "index.html")) {
			return p
		}
	}
	return "frontend/src"
}

func ResolveFrontendDir(configured string) string {
	return findFrontendDir(
		configured,
		"frontend/src",
		"../frontend/src",
	)
}

func upgradeReader(conn net.Conn) *bufio.Reader {
	return bufio.NewReader(conn)
}
