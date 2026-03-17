const STORAGE_KEY = "aswg_runtime_config_v1";
const THEME_STORAGE_KEY = "aswg_theme";
const SESSION_EDITOR_COLLAPSE_KEY = "aswg_session_editor_collapsed";
const APP_BRAND_TITLE_DEFAULT = "Agent Session Gateway";
const APP_BRAND_SUBTITLE_DEFAULT = "AI-Native Control Surface";
const SESSION_POLL_INTERVAL_MS = 3000;
const DEFAULT_SESSION_TYPES = ["项目", "需求", "bug修复", "问题拆解"];
const MOBILE_VIEWS = new Set(["chat", "sessions", "create", "editor"]);
const MOBILE_WIDTH_QUERY = "(max-width: 1024px)";
const MOBILE_POINTER_QUERY = "(hover: none) and (pointer: coarse)";

const state = {
  defaults: null,
  config: null,
  adapters: [],
  sessions: [],
  selectedSessionID: "",
  ws: null,
  lastSeq: 0,
  messages: [],
  assistantDraftId: "",
  continuePending: false,
  wsReconnectTimer: null,
  wsReconnectAttempts: 0,
  sessionsPollTimer: null,
  sessionsPollInFlight: false,
  sessionMetaStore: {},
  sessionSearch: "",
  sessionTypeFilter: "",
  streamStatusText: "WS 未连接",
  streamStatusError: false,
  theme: "dark",
  sessionEditorCollapsed: true,
  mobileView: "chat",
  toolInsightsOpen: false,
  viewportSyncScheduled: false,
};

const el = {
  brandTitle: document.getElementById("app-brand-title"),
  brandSubtitle: document.getElementById("app-brand-subtitle"),
  mobileMoreToggle: document.getElementById("mobile-more-toggle"),
  mobileQuickMenu: document.getElementById("mobile-quick-menu"),
  mobileActionSessions: document.getElementById("mobile-action-sessions"),
  mobileActionCreate: document.getElementById("mobile-action-create"),
  mobileActionEditor: document.getElementById("mobile-action-editor"),
  mobileActionReconnect: document.getElementById("mobile-action-reconnect"),
  mobileActionDelete: document.getElementById("mobile-action-delete"),
  mobileActionSettings: document.getElementById("mobile-action-settings"),
  themeToggle: document.getElementById("theme-toggle"),
  settingsPanel: document.getElementById("settings-panel"),
  settingsToggle: document.getElementById("im-settings-toggle"),
  settingsClose: document.getElementById("settings-close"),
  settingsBackdrop: document.getElementById("settings-backdrop"),
  settingsForm: document.getElementById("settings-form"),
  settingsStatus: document.getElementById("settings-status"),
  settingsTabs: Array.from(document.querySelectorAll("[data-settings-tab]")),
  settingsPanels: Array.from(document.querySelectorAll("[data-settings-panel]")),
  settingsSelectedAdapter: document.getElementById("settings-selected-adapter"),
  settingsSelectedSession: document.getElementById("settings-selected-session"),
  settingsStreamMode: document.getElementById("settings-stream-mode"),
  settingsStreamAttempts: document.getElementById("settings-stream-attempts"),
  apiBaseURL: document.getElementById("api-base-url"),
  wsBaseURL: document.getElementById("ws-base-url"),
  defaultAdapter: document.getElementById("default-adapter"),
  testConnection: document.getElementById("test-connection"),
  resetDefaults: document.getElementById("reset-defaults"),
  adapterSelect: document.getElementById("adapter-select"),
  reloadSessions: document.getElementById("reload-sessions"),
  createSessionForm: document.getElementById("create-session-form"),
  createSessionTitle: document.getElementById("create-session-title"),
  createSessionWorkspace: document.getElementById("create-session-workspace"),
  createSessionSeed: document.getElementById("create-session-seed"),
  createSessionStatus: document.getElementById("create-session-status"),
  sessionsToggle: document.getElementById("im-sessions-toggle"),
  sessionSidebar: document.getElementById("session-sidebar"),
  sidebarBackdrop: document.getElementById("sidebar-backdrop"),
  runningSummary: document.getElementById("running-sessions-summary"),
  sessionSearch: document.getElementById("session-search"),
  sessionTypeFilter: document.getElementById("session-type-filter"),
  sessionList: document.getElementById("session-list"),
  sessionTitle: document.getElementById("session-title"),
  sessionMeta: document.getElementById("session-meta"),
  sessionIDLine: document.getElementById("session-id-line"),
  sessionEditorForm: document.getElementById("session-editor-form"),
  sessionEditTitle: document.getElementById("session-edit-title"),
  sessionEditType: document.getElementById("session-edit-type"),
  sessionEditNote: document.getElementById("session-edit-note"),
  sessionTypeOptions: document.getElementById("session-type-options"),
  sessionEditorToggle: document.getElementById("session-editor-toggle"),
  sessionEditorBody: document.getElementById("session-editor-body"),
  sessionEditorSave: document.getElementById("session-editor-save"),
  sessionEditorReset: document.getElementById("session-editor-reset"),
  sessionEditorStatus: document.getElementById("session-editor-status"),
  conversation: document.querySelector(".conversation"),
  conversationHead: document.querySelector(".conversation-head"),
  chatThread: document.getElementById("chat-thread"),
  toolInsights: document.querySelector(".tool-insights"),
  toolEventList: document.getElementById("tool-event-list"),
  toggleToolInsights: document.getElementById("toggle-tool-insights"),
  toolInsightsClose: document.getElementById("tool-insights-close"),
  continueForm: document.getElementById("continue-form"),
  continuePrompt: document.getElementById("continue-prompt"),
  continueSubmit: document.getElementById("continue-submit"),
  streamStatus: document.getElementById("stream-status"),
  reconnectStream: document.getElementById("reconnect-stream"),
  deleteSessionButton: document.getElementById("delete-session-button"),
  chatJumpTop: document.getElementById("chat-jump-top"),
  chatJumpBottom: document.getElementById("chat-jump-bottom"),
};

bootstrap().catch((err) => {
  setSettingsStatus(`初始化失败: ${err.message}`, true);
});

async function bootstrap() {
  const defaults = await loadDefaults();
  const saved = loadSavedConfig();
  state.defaults = defaults;
  state.config = { ...defaults, ...saved };

  applyConfigToForm(state.config);
  initTheme();
  syncViewportModeClass();
  initSessionEditorCollapse();
  refreshSessionTypeControls();
  switchSettingsTab("connection");
  bindEvents();
  bindChatNavigation();
  bindViewportEvents();
  toggleSettingsPanel(false);
  toggleSidebar(false);
  setStreamStatus("WS 未连接", false);

  await refreshAdapters();
  await refreshSessions();
  await restoreSelectedSession();
  ensureSessionPolling();
  updateSettingsContext();
  updateDeleteSessionButtonState();
}

function normalizeSessionStatus(value) {
  return String(value || "").trim().toLowerCase() === "running" ? "running" : "idle";
}

function sessionStatusLabel(status) {
  return status === "running" ? "进行中" : "空闲";
}

function isMobileViewport() {
  const widthMatch = window.matchMedia(MOBILE_WIDTH_QUERY).matches;
  const pointerMatch = window.matchMedia(MOBILE_POINTER_QUERY).matches;
  return widthMatch || pointerMatch;
}

function scheduleViewportSync() {
  if (state.viewportSyncScheduled) {
    return;
  }
  state.viewportSyncScheduled = true;
  window.requestAnimationFrame(() => {
    state.viewportSyncScheduled = false;
    syncViewportModeClass();
    if (!isMobileViewport()) {
      toggleSidebar(false);
      toggleSettingsPanel(false);
    }
  });
}

function bindViewportEvents() {
  window.addEventListener("resize", scheduleViewportSync, { passive: true });
  window.addEventListener("orientationchange", scheduleViewportSync, { passive: true });

  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", scheduleViewportSync, { passive: true });
  }

  const widthMQL = window.matchMedia(MOBILE_WIDTH_QUERY);
  const pointerMQL = window.matchMedia(MOBILE_POINTER_QUERY);
  if (typeof widthMQL.addEventListener === "function") {
    widthMQL.addEventListener("change", scheduleViewportSync);
    pointerMQL.addEventListener("change", scheduleViewportSync);
    return;
  }
  if (typeof widthMQL.addListener === "function") {
    widthMQL.addListener(scheduleViewportSync);
    pointerMQL.addListener(scheduleViewportSync);
  }
}

function syncViewportModeClass() {
  const mobile = isMobileViewport();
  document.body.classList.toggle("viewport-mobile", mobile);
  document.body.classList.toggle("viewport-desktop", !mobile);
  if (!mobile) {
    document.body.classList.remove("sidebar-open");
    toggleMobileQuickMenu(false);
    toggleMobileSubpageBackdrop(false);
    document.body.classList.remove("mobile-view-chat", "mobile-view-sessions", "mobile-view-create", "mobile-view-editor");
    state.mobileView = "chat";
  } else {
    if (!MOBILE_VIEWS.has(state.mobileView)) {
      state.mobileView = "chat";
    }
    applyMobileViewClass();
  }
  applyToolInsightsVisibility();
  updateMobileBrand();
}

function applyMobileViewClass() {
  const active = MOBILE_VIEWS.has(state.mobileView) ? state.mobileView : "chat";
  document.body.classList.toggle("mobile-view-chat", active === "chat");
  document.body.classList.toggle("mobile-view-sessions", active === "sessions");
  document.body.classList.toggle("mobile-view-create", active === "create");
  document.body.classList.toggle("mobile-view-editor", active === "editor");
}

function toggleMobileSubpageBackdrop(forceOpen) {
  if (!el.sidebarBackdrop) {
    return;
  }
  if (!isMobileViewport()) {
    const nextOpen = Boolean(forceOpen);
    el.sidebarBackdrop.classList.toggle("is-hidden", !nextOpen);
    el.sidebarBackdrop.setAttribute("aria-hidden", nextOpen ? "false" : "true");
    return;
  }
  // Mobile subpages are rendered as full-screen sheets inside .app-shell.
  // Keep external backdrop hidden; otherwise it sits above .app-shell and blocks taps.
  el.sidebarBackdrop.classList.add("is-hidden");
  el.sidebarBackdrop.setAttribute("aria-hidden", "true");
}

function setMobileView(nextView) {
  if (!isMobileViewport()) {
    return;
  }
  state.mobileView = MOBILE_VIEWS.has(nextView) ? nextView : "chat";
  document.body.classList.remove("sidebar-open");
  if (el.sessionsToggle) {
    el.sessionsToggle.setAttribute("aria-expanded", state.mobileView === "sessions" ? "true" : "false");
  }
  if (state.mobileView === "editor") {
    setSessionEditorCollapsed(false, false);
  } else {
    setSessionEditorCollapsed(true, false);
  }
  applyMobileViewClass();
  toggleMobileSubpageBackdrop(state.mobileView !== "chat");
  updateMobileBrand();
}

function toggleMobileQuickMenu(forceOpen) {
  if (!el.mobileQuickMenu) {
    return;
  }
  const nextOpen = typeof forceOpen === "boolean" ? forceOpen : el.mobileQuickMenu.classList.contains("is-hidden");
  el.mobileQuickMenu.classList.toggle("is-hidden", !nextOpen);
  el.mobileQuickMenu.setAttribute("aria-hidden", nextOpen ? "false" : "true");
  if (el.mobileMoreToggle) {
    el.mobileMoreToggle.setAttribute("aria-expanded", nextOpen ? "true" : "false");
  }
}

function syncMobileTopbarHeight() {
  if (!document.body.classList.contains("viewport-mobile")) {
    document.body.style.removeProperty("--mobile-topbar-height");
    return;
  }
  const topbar = document.querySelector(".topbar");
  if (!topbar) {
    return;
  }
  const height = Math.ceil(topbar.getBoundingClientRect().height);
  if (height > 0) {
    document.body.style.setProperty("--mobile-topbar-height", `${height}px`);
  }
}

function updateMobileBrand() {
  if (!el.brandTitle || !el.brandSubtitle) {
    return;
  }
  if (!document.body.classList.contains("viewport-mobile")) {
    el.brandTitle.textContent = APP_BRAND_TITLE_DEFAULT;
    el.brandSubtitle.textContent = APP_BRAND_SUBTITLE_DEFAULT;
    syncMobileTopbarHeight();
    return;
  }

  if (state.mobileView === "sessions") {
    const adapter = currentAdapter();
    el.brandTitle.textContent = "会话列表";
    el.brandSubtitle.textContent = `${adapter || "-"} · ${state.sessions.length} 个会话`;
    syncMobileTopbarHeight();
    return;
  }

  if (state.mobileView === "create") {
    const adapter = currentAdapter();
    el.brandTitle.textContent = "创建会话";
    el.brandSubtitle.textContent = adapter ? `${adapter} · 新会话` : "新会话";
    syncMobileTopbarHeight();
    return;
  }

  if (state.mobileView === "editor") {
    const adapter = currentAdapter();
    const sessionID = String(state.selectedSessionID || "").trim();
    const session = sessionID ? state.sessions.find((item) => item.id === sessionID) : null;
    const meta = sessionID ? getSessionMeta(adapter, sessionID) : { title: "" };
    const title = String(meta.title || "").trim() || String(session?.title || "").trim() || sessionID || "未选择会话";
    el.brandTitle.textContent = "编辑会话";
    el.brandSubtitle.textContent = title;
    syncMobileTopbarHeight();
    return;
  }

  const adapter = currentAdapter();
  const sessionID = String(state.selectedSessionID || "").trim();
  if (!sessionID) {
    el.brandTitle.textContent = "未选择会话";
    el.brandSubtitle.textContent = adapter ? `${adapter} · 请选择会话` : "请选择会话";
    syncMobileTopbarHeight();
    return;
  }

  const session = state.sessions.find((item) => item.id === sessionID);
  const meta = getSessionMeta(adapter, sessionID);
  const title = String(meta.title || "").trim() || String(session?.title || "").trim() || sessionID;
  const status = normalizeSessionStatus(session?.status);

  el.brandTitle.textContent = title;
  el.brandSubtitle.textContent = `${sessionStatusLabel(status)} · ${adapter || "-"}`;
  syncMobileTopbarHeight();
}

function applyToolInsightsVisibility(forceOpen) {
  if (!el.toolInsights) {
    return;
  }
  const desktop = !isMobileViewport();
  let nextOpen = state.toolInsightsOpen;
  if (typeof forceOpen === "boolean") {
    nextOpen = forceOpen;
  } else if (!desktop) {
    nextOpen = false;
  }
  if (!desktop) {
    nextOpen = false;
  }
  state.toolInsightsOpen = Boolean(nextOpen);
  el.toolInsights.classList.toggle("is-collapsed", !state.toolInsightsOpen);
  if (el.toggleToolInsights) {
    el.toggleToolInsights.setAttribute("aria-expanded", state.toolInsightsOpen ? "true" : "false");
    el.toggleToolInsights.textContent = state.toolInsightsOpen ? "收起轨迹" : "工具轨迹";
  }
}

function toggleSettingsPanel(forceOpen) {
  if (!el.settingsPanel) {
    return;
  }
  const nextOpen = typeof forceOpen === "boolean" ? forceOpen : el.settingsPanel.classList.contains("is-collapsed");
  if (nextOpen) {
    toggleMobileQuickMenu(false);
  }
  if (nextOpen && isMobileViewport()) {
    if (state.mobileView !== "chat") {
      setMobileView("chat");
    }
    document.body.classList.remove("sidebar-open");
    toggleMobileSubpageBackdrop(false);
    if (el.sessionsToggle) {
      el.sessionsToggle.setAttribute("aria-expanded", "false");
    }
  }
  el.settingsPanel.classList.toggle("is-collapsed", !nextOpen);
  el.settingsPanel.setAttribute("aria-hidden", nextOpen ? "false" : "true");
  if (el.settingsToggle) {
    el.settingsToggle.setAttribute("aria-expanded", nextOpen ? "true" : "false");
  }
  if (el.settingsBackdrop) {
    el.settingsBackdrop.classList.toggle("is-hidden", !nextOpen);
    el.settingsBackdrop.setAttribute("aria-hidden", nextOpen ? "false" : "true");
  }
  if (!nextOpen && isMobileViewport()) {
    toggleMobileSubpageBackdrop(state.mobileView !== "chat");
  }
}

function toggleSidebar(forceOpen) {
  if (!el.sessionSidebar) {
    return;
  }
  if (!isMobileViewport()) {
    document.body.classList.remove("sidebar-open");
    toggleMobileSubpageBackdrop(false);
    return;
  }

  const currentlyOpen = state.mobileView === "sessions";
  const nextOpen = typeof forceOpen === "boolean" ? forceOpen : !currentlyOpen;
  if (nextOpen && el.settingsPanel && !el.settingsPanel.classList.contains("is-collapsed")) {
    toggleSettingsPanel(false);
  }
  toggleMobileQuickMenu(false);
  setMobileView(nextOpen ? "sessions" : "chat");
  document.body.classList.remove("sidebar-open");
  if (el.sessionsToggle) {
    el.sessionsToggle.setAttribute("aria-expanded", nextOpen ? "true" : "false");
  }
}

function bindEvents() {
  if (el.themeToggle) {
    el.themeToggle.addEventListener("click", () => {
      const next = state.theme === "light" ? "dark" : "light";
      applyTheme(next);
      saveTheme(next);
    });
  }

  if (el.toggleToolInsights) {
    el.toggleToolInsights.addEventListener("click", () => {
      applyToolInsightsVisibility(!state.toolInsightsOpen);
    });
  }

  if (el.toolInsightsClose) {
    el.toolInsightsClose.addEventListener("click", () => {
      applyToolInsightsVisibility(false);
    });
  }

  if (el.mobileMoreToggle) {
    el.mobileMoreToggle.addEventListener("click", (event) => {
      event.stopPropagation();
      toggleMobileQuickMenu();
    });
  }

  if (el.mobileActionSessions) {
    el.mobileActionSessions.addEventListener("click", () => {
      toggleMobileQuickMenu(false);
      toggleSettingsPanel(false);
      setMobileView("sessions");
    });
  }

  if (el.mobileActionCreate) {
    el.mobileActionCreate.addEventListener("click", () => {
      toggleMobileQuickMenu(false);
      toggleSettingsPanel(false);
      setMobileView("create");
    });
  }

  if (el.mobileActionEditor) {
    el.mobileActionEditor.addEventListener("click", () => {
      toggleMobileQuickMenu(false);
      toggleSettingsPanel(false);
      if (!state.selectedSessionID) {
        setStreamStatus("请先选择会话", true);
        return;
      }
      setMobileView("editor");
    });
  }

  if (el.mobileActionReconnect) {
    el.mobileActionReconnect.addEventListener("click", async () => {
      toggleMobileQuickMenu(false);
      setMobileView("chat");
      await connectStream();
    });
  }

  if (el.mobileActionDelete) {
    el.mobileActionDelete.addEventListener("click", async () => {
      toggleMobileQuickMenu(false);
      setMobileView("chat");
      await deleteCurrentSession();
    });
  }

  if (el.mobileActionSettings) {
    el.mobileActionSettings.addEventListener("click", () => {
      toggleMobileQuickMenu(false);
      if (isMobileViewport()) {
        setMobileView("chat");
      }
      toggleSettingsPanel(true);
    });
  }

  if (el.settingsToggle) {
    el.settingsToggle.addEventListener("click", () => {
      toggleSettingsPanel();
    });
  }

  if (el.settingsClose) {
    el.settingsClose.addEventListener("click", () => {
      toggleSettingsPanel(false);
    });
  }

  if (el.settingsBackdrop) {
    el.settingsBackdrop.addEventListener("click", () => {
      toggleSettingsPanel(false);
    });
  }

  el.settingsTabs.forEach((tabButton) => {
    tabButton.addEventListener("click", () => {
      switchSettingsTab(tabButton.dataset.settingsTab || "connection");
    });
  });

  if (el.sessionsToggle) {
    el.sessionsToggle.addEventListener("click", () => {
      toggleSidebar();
    });
  }

  if (el.sidebarBackdrop) {
    el.sidebarBackdrop.addEventListener("click", () => {
      if (isMobileViewport()) {
        setMobileView("chat");
        return;
      }
      toggleSidebar(false);
    });
  }

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") {
      return;
    }
    toggleMobileQuickMenu(false);
    toggleSettingsPanel(false);
    toggleSidebar(false);
  });

  document.addEventListener("click", (event) => {
    if (!el.mobileQuickMenu || !el.mobileMoreToggle) {
      return;
    }
    if (el.mobileQuickMenu.classList.contains("is-hidden")) {
      return;
    }
    const target = event.target;
    if (target instanceof Node && (el.mobileQuickMenu.contains(target) || el.mobileMoreToggle.contains(target))) {
      return;
    }
    toggleMobileQuickMenu(false);
  });

  el.settingsForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    try {
      const next = readConfigFromForm();
      state.config = next;
      saveConfig(next);
      setSettingsStatus("配置已保存", false);
      await refreshAdapters();
      await refreshSessions();
      await restoreSelectedSession();
    } catch (err) {
      setSettingsStatus(err.message, true);
    }
  });

  el.testConnection.addEventListener("click", async () => {
    try {
      const health = await fetchAPI("/api/v1/health", { method: "GET" });
      const adapters = await fetchAPI("/api/v1/adapters", { method: "GET" });
      const adapterCount = adapters?.data?.items?.length ?? 0;
      setSettingsStatus(
        `连接成功: health=${health?.data?.status ?? "unknown"}, adapters=${adapterCount}`,
        false,
      );
    } catch (err) {
      setSettingsStatus(`连接失败: ${err.message}`, true);
    }
  });

  el.resetDefaults.addEventListener("click", async () => {
    state.config = { ...state.defaults };
    saveConfig(state.config);
    applyConfigToForm(state.config);
    setSettingsStatus("已恢复默认配置", false);
    await refreshAdapters();
    await refreshSessions();
    await restoreSelectedSession();
  });

  el.adapterSelect.addEventListener("change", async () => {
    await refreshSessions();
    await restoreSelectedSession();
    updateSettingsContext();
  });

  el.reloadSessions.addEventListener("click", async () => {
    await refreshSessions();
    await restoreSelectedSession();
    updateSettingsContext();
  });

  if (el.sessionSearch) {
    el.sessionSearch.addEventListener("input", async () => {
      state.sessionSearch = String(el.sessionSearch.value || "").trim();
      await ensureSelectedSessionVisible();
      renderSessionList();
    });
  }

  if (el.sessionTypeFilter) {
    el.sessionTypeFilter.addEventListener("change", async () => {
      state.sessionTypeFilter = String(el.sessionTypeFilter.value || "").trim();
      await ensureSelectedSessionVisible();
      renderSessionList();
    });
  }

  if (el.sessionEditorToggle) {
    el.sessionEditorToggle.addEventListener("click", () => {
      setSessionEditorCollapsed(!state.sessionEditorCollapsed, true);
    });
  }

  el.createSessionForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const adapter = currentAdapter();
    if (!adapter) {
      setCreateSessionStatus("请先选择 adapter", true);
      return;
    }

    const payload = {
      title: el.createSessionTitle.value.trim(),
      workspace: el.createSessionWorkspace.value.trim(),
      seed_prompt: el.createSessionSeed.value.trim(),
    };
    if (!payload.title) delete payload.title;
    if (!payload.workspace) delete payload.workspace;
    if (!payload.seed_prompt) delete payload.seed_prompt;

    try {
      const created = await fetchAPI(`/api/v1/adapters/${encodeURIComponent(adapter)}/sessions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      const session = created?.data || {};
      if (!session.id) {
        throw new Error("创建会话返回缺少 id");
      }

      el.createSessionForm.reset();
      setCreateSessionStatus(`创建成功: ${session.id}`, false);
      await refreshSessions();
      state.selectedSessionID = session.id;
      renderSessionList();
      await loadSession(session.id);
      if (isMobileViewport()) {
        setMobileView("chat");
      }
      updateSettingsContext();
    } catch (err) {
      setCreateSessionStatus(`创建失败: ${err.message}`, true);
    }
  });

  el.continueForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (state.continuePending) {
      return;
    }
    const adapter = currentAdapter();
    const sessionID = state.selectedSessionID;
    const prompt = el.continuePrompt.value.trim();
    if (!adapter || !sessionID) {
      setStreamStatus("请先选择会话", true);
      return;
    }
    if (!prompt) {
      setStreamStatus("prompt 不能为空", true);
      return;
    }

    // Defensive boundary: close stale assistant draft before a new user turn.
    const submitDraftRef = { id: state.assistantDraftId };
    finalizeAssistantDraft(state.messages, submitDraftRef);
    state.assistantDraftId = submitDraftRef.id;

    const localMessage = {
      id: `local-user-${Date.now()}`,
      role: "user",
      text: prompt,
      pending: true,
      done: true,
      seq: null,
    };
    state.messages.push(localMessage);
    renderChatThread();
    setContinuePending(true);

    try {
      const payload = { prompt };
      const result = await fetchAPI(
        `/api/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}/continue`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        },
      );
      el.continuePrompt.value = "";
      setStreamStatus(`continue 已提交, job=${result?.data?.job_id ?? "n/a"}`, false);
      updateLocalSessionStatus(sessionID, "running");
      if (!state.ws || state.ws.readyState !== WebSocket.OPEN) {
        await connectStream();
      }
    } catch (err) {
      state.messages = state.messages.filter((item) => item.id !== localMessage.id);
      renderChatThread();
      setStreamStatus(`continue 失败: ${err.message}`, true);
    } finally {
      setContinuePending(false);
    }
  });

  el.reconnectStream.addEventListener("click", async () => {
    await connectStream();
  });

  if (el.continuePrompt) {
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
  }

  if (el.deleteSessionButton) {
    el.deleteSessionButton.addEventListener("click", async () => {
      await deleteCurrentSession();
    });
  }

  if (el.sessionEditorForm) {
    el.sessionEditorForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const sessionID = state.selectedSessionID;
      const adapter = currentAdapter();
      if (!sessionID || !adapter) {
        setSessionEditorStatus("请先选择会话", true);
        return;
      }

      const nextMeta = {
        title: String(el.sessionEditTitle?.value || "").trim(),
        note: String(el.sessionEditNote?.value || "").trim(),
        type: String(el.sessionEditType?.value || "").trim(),
      };
      try {
        await setSessionMeta(adapter, sessionID, nextMeta);
        refreshSessionTypeControls();
        renderSessionList();
        applySessionHeader(sessionID);
        setSessionEditorStatus("已保存会话信息", false);
        if (isMobileViewport() && state.mobileView === "editor") {
          setMobileView("chat");
        }
        updateSettingsContext();
      } catch (err) {
        setSessionEditorStatus(`保存失败: ${err.message}`, true);
      }
    });
  }

  if (el.sessionEditorReset) {
    el.sessionEditorReset.addEventListener("click", async () => {
      const sessionID = state.selectedSessionID;
      const adapter = currentAdapter();
      if (!sessionID || !adapter) {
        setSessionEditorStatus("请先选择会话", true);
        return;
      }
      try {
        await setSessionMeta(adapter, sessionID, { title: "", note: "", type: "" });
        refreshSessionTypeControls();
        populateSessionEditor(sessionID);
        renderSessionList();
        applySessionHeader(sessionID);
        setSessionEditorStatus("已清空自定义信息", false);
        updateSettingsContext();
      } catch (err) {
        setSessionEditorStatus(`清空失败: ${err.message}`, true);
      }
    });
  }
}

async function deleteCurrentSession() {
  const adapter = currentAdapter();
  const sessionID = state.selectedSessionID;
  if (!adapter || !sessionID) {
    setStreamStatus("请先选择会话", true);
    return;
  }
  if (state.continuePending) {
    setStreamStatus("正在发送消息，暂不能删除会话", true);
    return;
  }
  if (!window.confirm("确认删除当前会话？此操作不可撤销。")) {
    return;
  }
  try {
    await fetchAPI(`/api/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}`, {
      method: "DELETE",
    });
    setStreamStatus("会话已删除", false);
    await refreshSessions();
    await restoreSelectedSession();
    if (isMobileViewport()) {
      setMobileView("chat");
    }
    updateSettingsContext();
  } catch (err) {
    setStreamStatus(`删除失败: ${err.message}`, true);
  } finally {
    updateDeleteSessionButtonState();
  }
}

async function loadDefaults() {
  const res = await fetch("/runtime-config.json", { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`runtime-config.json 加载失败 (${res.status})`);
  }
  return await res.json();
}

function loadSavedConfig() {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) {
    return {};
  }
  try {
    return JSON.parse(raw);
  } catch {
    return {};
  }
}

function saveConfig(config) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
}

function loadSavedTheme() {
  try {
    const value = String(localStorage.getItem(THEME_STORAGE_KEY) || "").trim().toLowerCase();
    if (value === "light" || value === "dark") {
      return value;
    }
  } catch {
    // Ignore localStorage access failures and fallback to system preference.
  }
  if (window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches) {
    return "light";
  }
  return "dark";
}

function saveTheme(theme) {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Ignore localStorage failures.
  }
}

function applyTheme(theme) {
  const nextTheme = theme === "light" ? "light" : "dark";
  state.theme = nextTheme;
  document.body.classList.toggle("theme-light", nextTheme === "light");
  if (el.themeToggle) {
    el.themeToggle.textContent = nextTheme === "light" ? "暗色模式" : "亮色模式";
  }
}

function initTheme() {
  applyTheme(loadSavedTheme());
}

function loadSessionEditorCollapsed() {
  try {
    const raw = String(localStorage.getItem(SESSION_EDITOR_COLLAPSE_KEY) || "").trim().toLowerCase();
    if (raw === "0" || raw === "false") {
      return false;
    }
    if (raw === "1" || raw === "true") {
      return true;
    }
  } catch {
    // Ignore localStorage failures.
  }
  return true;
}

function saveSessionEditorCollapsed(collapsed) {
  try {
    localStorage.setItem(SESSION_EDITOR_COLLAPSE_KEY, collapsed ? "1" : "0");
  } catch {
    // Ignore localStorage failures.
  }
}

function setSessionEditorCollapsed(collapsed, persist = false) {
  const next = Boolean(collapsed);
  state.sessionEditorCollapsed = next;
  if (el.sessionEditorBody) {
    el.sessionEditorBody.classList.toggle("is-collapsed", next);
  }
  if (el.conversation) {
    el.conversation.classList.toggle("editor-open", !next);
  }
  if (el.conversationHead) {
    el.conversationHead.classList.toggle("editor-open", !next);
  }
  if (el.sessionEditorToggle) {
    el.sessionEditorToggle.setAttribute("aria-expanded", next ? "false" : "true");
    el.sessionEditorToggle.setAttribute("data-icon", next ? "▾" : "▴");
    el.sessionEditorToggle.textContent = next ? "编辑会话信息" : "收起会话编辑";
  }
  if (!next) {
    // Expanded state should start from top to avoid appearing "stuck" at the bottom.
    if (el.conversationHead) {
      el.conversationHead.scrollTop = 0;
    }
    if (el.sessionEditorBody) {
      el.sessionEditorBody.scrollTop = 0;
    }
  }
  if (persist) {
    saveSessionEditorCollapsed(next);
  }
}

function initSessionEditorCollapse() {
  setSessionEditorCollapsed(loadSessionEditorCollapsed(), false);
}

function switchSettingsTab(tabName) {
  const selected = String(tabName || "connection").trim() || "connection";
  el.settingsTabs.forEach((tabButton) => {
    const active = tabButton.dataset.settingsTab === selected;
    tabButton.classList.toggle("is-active", active);
    tabButton.setAttribute("aria-selected", active ? "true" : "false");
  });
  el.settingsPanels.forEach((panel) => {
    const active = panel.dataset.settingsPanel === selected;
    panel.classList.toggle("is-active", active);
  });
}

function updateSettingsContext() {
  const adapter = currentAdapter();
  if (el.settingsSelectedAdapter) {
    el.settingsSelectedAdapter.textContent = `adapter: ${adapter || "-"}`;
  }

  if (el.settingsSelectedSession) {
    const sessionID = String(state.selectedSessionID || "").trim();
    if (!sessionID) {
      el.settingsSelectedSession.textContent = "session: -";
    } else {
      const session = state.sessions.find((item) => item.id === sessionID);
      const meta = getSessionMeta(adapter, sessionID);
      const displayName = String(meta.title || "").trim() || String(session?.title || "").trim() || sessionID;
      el.settingsSelectedSession.textContent = `session: ${displayName} (${sessionID})`;
    }
  }
  updateSettingsStreamContext();
  updateMobileBrand();
}

function updateSettingsStreamContext() {
  if (el.settingsStreamMode) {
    el.settingsStreamMode.textContent = `状态: ${state.streamStatusText || "-"}`;
    el.settingsStreamMode.classList.toggle("text-danger", Boolean(state.streamStatusError));
  }
  if (el.settingsStreamAttempts) {
    el.settingsStreamAttempts.textContent = `重连次数: ${Number(state.wsReconnectAttempts || 0)}`;
  }
}

function sessionMetaKey(adapter, sessionID) {
  return `${String(adapter || "").trim()}::${String(sessionID || "").trim()}`;
}

function getSessionMeta(adapter, sessionID) {
  const key = sessionMetaKey(adapter, sessionID);
  const item = state.sessionMetaStore[key];
  if (!item || typeof item !== "object") {
    return { title: "", note: "", type: "" };
  }
  return {
    title: String(item.title || "").trim(),
    note: String(item.note || "").trim(),
    type: String(item.type || "").trim(),
  };
}

function cacheSessionMeta(adapter, sessionID, rawMeta) {
  const key = sessionMetaKey(adapter, sessionID);
  const normalized = {
    title: String(rawMeta?.title ?? rawMeta?.name ?? "").trim(),
    note: String(rawMeta?.note || "").trim(),
    type: String(rawMeta?.type || "").trim(),
  };
  if (!normalized.title && !normalized.note && !normalized.type) {
    delete state.sessionMetaStore[key];
  } else {
    state.sessionMetaStore[key] = normalized;
  }
}

function syncSessionMetaFromSession(adapter, session) {
  if (!adapter || !session?.id) {
    return;
  }
  cacheSessionMeta(adapter, session.id, session.session_meta);
}

async function setSessionMeta(adapter, sessionID, next) {
  const payload = {
    name: String(next?.title || "").trim(),
    note: String(next?.note || "").trim(),
    type: String(next?.type || "").trim(),
  };

  const result = await fetchAPI(
    `/api/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}/meta`,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    },
  );
  cacheSessionMeta(adapter, sessionID, result?.data?.meta || payload);
}

function getKnownSessionTypes(adapter) {
  const set = new Set(DEFAULT_SESSION_TYPES.map((item) => String(item).trim()).filter(Boolean));
  Object.entries(state.sessionMetaStore).forEach(([key, item]) => {
    if (adapter && !key.startsWith(`${adapter}::`)) {
      return;
    }
    const sessionType = String(item?.type || "").trim();
    if (sessionType) {
      set.add(sessionType);
    }
  });
  return Array.from(set).sort((a, b) => a.localeCompare(b));
}

function refreshSessionTypeControls() {
  const selected = String(el.sessionTypeFilter?.value || state.sessionTypeFilter || "").trim();
  const types = getKnownSessionTypes(currentAdapter());

  if (el.sessionTypeFilter) {
    el.sessionTypeFilter.innerHTML = "";
    const all = document.createElement("option");
    all.value = "";
    all.textContent = "全部类型";
    el.sessionTypeFilter.appendChild(all);

    types.forEach((sessionType) => {
      const option = document.createElement("option");
      option.value = sessionType;
      option.textContent = sessionType;
      el.sessionTypeFilter.appendChild(option);
    });

    if (selected && types.includes(selected)) {
      el.sessionTypeFilter.value = selected;
      state.sessionTypeFilter = selected;
    } else {
      el.sessionTypeFilter.value = "";
      if (!selected || !types.includes(selected)) {
        state.sessionTypeFilter = "";
      }
    }
  }

  if (el.sessionTypeOptions) {
    el.sessionTypeOptions.innerHTML = "";
    types.forEach((sessionType) => {
      const option = document.createElement("option");
      option.value = sessionType;
      el.sessionTypeOptions.appendChild(option);
    });
  }
}

function applyConfigToForm(config) {
  el.apiBaseURL.value = config.api_base_url ?? "";
  el.wsBaseURL.value = config.ws_base_url ?? "";
  el.defaultAdapter.value = config.default_adapter ?? "codex";
}

function readConfigFromForm() {
  const config = {
    api_base_url: trimTrailingSlash(el.apiBaseURL.value.trim()),
    ws_base_url: trimTrailingSlash(el.wsBaseURL.value.trim()),
    default_adapter: el.defaultAdapter.value.trim() || "codex",
    request_timeout_ms: Number(state.config?.request_timeout_ms || 30000),
  };

  if (!isValidHTTPBaseURL(config.api_base_url)) {
    throw new Error("api_base_url 必须是 http/https 地址");
  }
  if (!isValidWSBaseURL(config.ws_base_url)) {
    throw new Error("ws_base_url 必须是 ws/wss 地址");
  }
  return config;
}

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function isValidHTTPBaseURL(value) {
  try {
    const u = new URL(value);
    return u.protocol === "http:" || u.protocol === "https:";
  } catch {
    return false;
  }
}

function isValidWSBaseURL(value) {
  try {
    const u = new URL(value);
    return u.protocol === "ws:" || u.protocol === "wss:";
  } catch {
    return false;
  }
}

function currentAdapter() {
  const selected = el.adapterSelect.value;
  if (selected) {
    return selected;
  }
  return state.config?.default_adapter || "codex";
}

async function refreshAdapters() {
  try {
    const result = await fetchAPI("/api/v1/adapters", { method: "GET" });
    const items = result?.data?.items ?? [];
    state.adapters = items;
    renderAdapterOptions(items);
  } catch (err) {
    state.adapters = [];
    renderAdapterOptions([]);
    setSettingsStatus(`加载 adapters 失败: ${err.message}`, true);
  }
}

function renderAdapterOptions(items) {
  const fallback = state.config?.default_adapter || "codex";
  const list = items.length > 0 ? items.map((item) => item.name) : [fallback];

  el.adapterSelect.innerHTML = "";
  list.forEach((name) => {
    const option = document.createElement("option");
    option.value = name;
    option.textContent = name;
    if (name === fallback) {
      option.selected = true;
    }
    el.adapterSelect.appendChild(option);
  });
  updateSettingsContext();
}

async function refreshSessions(options = {}) {
  const silent = Boolean(options?.silent);
  const adapter = currentAdapter();
  if (!adapter) {
    state.sessions = [];
    renderSessionList();
    return;
  }
  try {
    const items = [];
    const seen = new Set();
    let cursor = "";
    const pageLimit = 100;
    const maxPages = 100;

    for (let i = 0; i < maxPages; i += 1) {
      const query = new URLSearchParams({
        limit: String(pageLimit),
        sort_by: "updated_at",
        sort_order: "desc",
      });
      if (cursor) {
        query.set("cursor", cursor);
      }

      const result = await fetchAPI(
        `/api/v1/adapters/${encodeURIComponent(adapter)}/sessions?${query.toString()}`,
        { method: "GET" },
      );
      const pageItems = result?.data?.items ?? [];
      pageItems.forEach((session) => {
        if (!session?.id || seen.has(session.id)) {
          return;
        }
        seen.add(session.id);
        syncSessionMetaFromSession(adapter, session);
        items.push(session);
      });

      const hasMore = Boolean(result?.data?.has_more);
      const nextCursor = result?.data?.next_cursor || "";
      if (!hasMore || !nextCursor || nextCursor === cursor) {
        break;
      }
      cursor = nextCursor;
    }

    items.sort(compareSessionUpdatedAtDesc);
    pruneSessionMetaForAdapter(adapter, items);
    refreshSessionTypeControls();
    state.sessions = items;
    renderSessionList();
    updateRunningSummary();
    updateDeleteSessionButtonState();
    if (state.selectedSessionID) {
      applySessionHeader(state.selectedSessionID);
    }
    updateSettingsContext();
  } catch (err) {
    if (silent) {
      return;
    }
    state.sessions = [];
    renderSessionList();
    updateRunningSummary();
    updateDeleteSessionButtonState();
    setStreamStatus(`加载会话失败: ${err.message}`, true);
    updateSettingsContext();
  }
}

function compareSessionUpdatedAtDesc(a, b) {
  const left = Date.parse(String(a?.updated_at || "")) || 0;
  const right = Date.parse(String(b?.updated_at || "")) || 0;
  if (left !== right) {
    return right - left;
  }
  return String(a?.id || "").localeCompare(String(b?.id || ""));
}

function pruneSessionMetaForAdapter(adapter, sessions) {
  if (!adapter) {
    return;
  }
  const keep = new Set((sessions || []).map((item) => sessionMetaKey(adapter, item?.id)));
  Object.keys(state.sessionMetaStore).forEach((key) => {
    if (!key.startsWith(`${adapter}::`)) {
      return;
    }
    if (keep.has(key)) {
      return;
    }
    delete state.sessionMetaStore[key];
  });
}

function getVisibleSessions() {
  const adapter = currentAdapter();
  const search = String(state.sessionSearch || "").trim().toLowerCase();
  const typeFilter = String(state.sessionTypeFilter || "").trim().toLowerCase();
  const visible = [];

  state.sessions.forEach((session) => {
    const meta = getSessionMeta(adapter, session.id);
    const sessionType = String(meta.type || "").trim();
    const aliasTitle = String(meta.title || "").trim();
    const note = String(meta.note || "").trim();

    if (typeFilter && sessionType.toLowerCase() !== typeFilter) {
      return;
    }

    const displayTitle = aliasTitle || session.title || session.id;
    if (search) {
      const corpus = [
        session.id,
        session.title,
        displayTitle,
        session.workspace,
        session.source,
        session.status,
        sessionType,
        note,
      ]
        .map((item) => String(item || "").toLowerCase())
        .join(" ");
      if (!corpus.includes(search)) {
        return;
      }
    }

    visible.push({
      ...session,
      displayTitle,
      sessionType,
      note,
    });
  });

  return visible;
}

function ensureSessionPolling() {
  if (state.sessionsPollTimer) {
    return;
  }
  state.sessionsPollTimer = setInterval(() => {
    void pollSessionsSilently();
  }, SESSION_POLL_INTERVAL_MS);
}

async function pollSessionsSilently() {
  if (state.sessionsPollInFlight) {
    return;
  }
  state.sessionsPollInFlight = true;
  try {
    await refreshSessions({ silent: true });
    if (state.selectedSessionID) {
      renderSessionList();
      applySessionHeader(state.selectedSessionID);
    }
  } finally {
    state.sessionsPollInFlight = false;
  }
}

function updateRunningSummary() {
  if (!el.runningSummary) {
    return;
  }
  const running = (state.sessions || []).filter((item) => normalizeSessionStatus(item?.status) === "running").length;
  el.runningSummary.textContent = `进行中: ${running}`;
}

function updateDeleteSessionButtonState() {
  if (!el.deleteSessionButton) {
    return;
  }
  const visible = Boolean(state.selectedSessionID);
  el.deleteSessionButton.disabled = !visible || state.continuePending;
  el.deleteSessionButton.style.visibility = visible ? "visible" : "hidden";
}

function updateLocalSessionStatus(sessionID, nextStatus) {
  if (!sessionID || !Array.isArray(state.sessions)) {
    return;
  }
  const status = normalizeSessionStatus(nextStatus);
  let changed = false;
  state.sessions = state.sessions.map((item) => {
    if (!item || item.id !== sessionID) {
      return item;
    }
    if (normalizeSessionStatus(item.status) === status) {
      return item;
    }
    changed = true;
    return {
      ...item,
      status,
    };
  });
  if (!changed) {
    return;
  }
  state.sessions.sort(compareSessionUpdatedAtDesc);
  renderSessionList();
  applySessionHeader(sessionID);
}

function renderSessionList() {
  el.sessionList.innerHTML = "";
  updateRunningSummary();
  const visibleSessions = getVisibleSessions();
  if (visibleSessions.length === 0) {
    const li = document.createElement("li");
    li.textContent = state.sessions.length === 0 ? "无可用会话" : "当前筛选条件下无会话";
    el.sessionList.appendChild(li);
    updateDeleteSessionButtonState();
    return;
  }

  visibleSessions.forEach((session) => {
    const li = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.classList.add("session-card");
    if (session.id === state.selectedSessionID) {
      button.classList.add("active");
    }
    const title = document.createElement("p");
    title.className = "session-card-title";
    title.textContent = session.displayTitle || session.id;

    const meta = document.createElement("p");
    meta.className = "session-card-meta";
    meta.textContent = `${session.workspace || "-"} · ${session.updated_at || "-"}`;

    const idLine = document.createElement("p");
    idLine.className = "session-card-id";
    idLine.textContent = `ID: ${session.id}`;

    const status = normalizeSessionStatus(session.status);
    const statusLine = document.createElement("p");
    statusLine.className = "session-card-status";
    statusLine.textContent = `状态: ${sessionStatusLabel(status)}`;
    statusLine.classList.toggle("is-running", status === "running");
    statusLine.classList.toggle("is-idle", status !== "running");

    button.append(title, meta, idLine, statusLine);
    if (session.sessionType) {
      const tags = document.createElement("div");
      tags.className = "session-card-tags";
      const typeChip = document.createElement("span");
      typeChip.className = "session-type-chip";
      typeChip.textContent = session.sessionType;
      tags.appendChild(typeChip);
      button.appendChild(tags);
    }
    if (session.note) {
      const noteLine = document.createElement("p");
      noteLine.className = "session-note-line";
      noteLine.textContent = session.note;
      button.appendChild(noteLine);
    }

    button.addEventListener("click", async () => {
      state.selectedSessionID = session.id;
      renderSessionList();
      updateSettingsContext();
      toggleSidebar(false);
      await loadSession(session.id);
    });
    li.appendChild(button);
    el.sessionList.appendChild(li);
  });
  updateDeleteSessionButtonState();
}

async function restoreSelectedSession() {
  const visibleSessions = getVisibleSessions();
  if (visibleSessions.length === 0) {
    clearSessionView();
    return;
  }

  const exists = visibleSessions.some((s) => s.id === state.selectedSessionID);
  if (!exists) {
    state.selectedSessionID = visibleSessions[0].id;
  }
  renderSessionList();
  await loadSession(state.selectedSessionID);
}

async function ensureSelectedSessionVisible() {
  if (!state.selectedSessionID) {
    return;
  }
  const visibleSessions = getVisibleSessions();
  if (visibleSessions.length === 0) {
    clearSessionView();
    return;
  }
  const exists = visibleSessions.some((item) => item.id === state.selectedSessionID);
  if (exists) {
    return;
  }
  state.selectedSessionID = visibleSessions[0].id;
  renderSessionList();
  await loadSession(state.selectedSessionID);
}

function clearSessionView() {
  state.selectedSessionID = "";
  state.lastSeq = 0;
  state.messages = [];
  state.assistantDraftId = "";
  setContinuePending(false);
  closeStream();
  el.sessionTitle.textContent = "未选择会话";
  el.sessionMeta.textContent = "请选择左侧会话";
  if (el.sessionIDLine) {
    el.sessionIDLine.textContent = "session_id: -";
  }
  populateSessionEditor("");
  setSessionEditorStatus("未编辑", false);
  renderChatThread();
  updateChatNavigation();
  updateDeleteSessionButtonState();
  setStreamStatus("WS 未连接", false);
  updateSettingsContext();
}

async function fetchAllSessionEvents(adapter, sessionID) {
  const items = [];
  const seen = new Set();
  let cursor = "";
  const pageLimit = 500;
  const maxPages = 80;

  for (let i = 0; i < maxPages; i += 1) {
    const query = new URLSearchParams({ limit: String(pageLimit) });
    if (cursor) {
      query.set("cursor", cursor);
    }
    const result = await fetchAPI(
      `/api/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}/events?${query.toString()}`,
      { method: "GET" },
    );

    const pageItems = result?.data?.items || [];
    pageItems.forEach((event) => {
      const seq = Number(event?.seq || 0);
      if (seq > 0) {
        if (seen.has(seq)) {
          return;
        }
        seen.add(seq);
      }
      items.push(event);
    });

    const hasMore = Boolean(result?.data?.has_more);
    const nextCursor = String(result?.data?.next_cursor || "");
    if (!hasMore || !nextCursor || nextCursor === cursor || pageItems.length === 0) {
      break;
    }
    cursor = nextCursor;
  }

  return items;
}

async function loadSession(sessionID) {
  const adapter = currentAdapter();
  if (!adapter || !sessionID) {
    return;
  }

  try {
    const detail = await fetchAPI(
      `/api/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}`,
      { method: "GET" },
    );
    const eventItems = await fetchAllSessionEvents(adapter, sessionID);

    const sessionDetail = detail?.data || {};

    syncSessionMetaFromSession(adapter, { id: sessionID, session_meta: sessionDetail?.session_meta });
    applySessionHeader(sessionID, sessionDetail);
    populateSessionEditor(sessionID);
    setSessionEditorStatus("未编辑", false);

    const rebuilt = buildMessagesFromEvents(eventItems);
    state.messages = rebuilt.messages;
    state.assistantDraftId = rebuilt.assistantDraftId;
    state.lastSeq = eventItems.length > 0 ? Number(eventItems[eventItems.length - 1].seq || 0) : 0;
    renderChatThread();
    if (el.chatThread) {
      el.chatThread.scrollTop = el.chatThread.scrollHeight;
    }
    updateDeleteSessionButtonState();
    updateSettingsContext();

    await connectStream();
  } catch (err) {
    setStreamStatus(`加载会话详情失败: ${err.message}`, true);
  }
}

function applySessionHeader(sessionID, sessionDetail) {
  const adapter = currentAdapter();
  const detail = sessionDetail || state.sessions.find((item) => item.id === sessionID) || {};
  const meta = getSessionMeta(adapter, sessionID);
  const title = String(meta.title || "").trim() || detail.title || sessionID;
  const workspace = detail.workspace || "-";
  const updatedAt = detail.updated_at || "-";
  const source = detail.source ? ` · source: ${detail.source}` : "";
  const status = normalizeSessionStatus(detail.status);
  const sessionType = String(meta.type || "").trim();
  const note = String(meta.note || "").trim();

  el.sessionTitle.textContent = title;
  el.sessionMeta.textContent = `workspace: ${workspace} · updated_at: ${updatedAt}${source} · 状态: ${sessionStatusLabel(status)}`;
  if (el.sessionIDLine) {
    let idLine = `session_id: ${sessionID}`;
    if (sessionType) {
      idLine += ` · type: ${sessionType}`;
    }
    if (note) {
      idLine += ` · note: ${note}`;
    }
    el.sessionIDLine.textContent = idLine;
  }
  updateDeleteSessionButtonState();
  updateSettingsContext();
}

function populateSessionEditor(sessionID) {
  const adapter = currentAdapter();
  const hasSession = Boolean(adapter && sessionID);
  const meta = hasSession ? getSessionMeta(adapter, sessionID) : { title: "", note: "", type: "" };
  const fields = [el.sessionEditTitle, el.sessionEditType, el.sessionEditNote, el.sessionEditorSave, el.sessionEditorReset];
  fields.forEach((node) => {
    if (node) {
      node.disabled = !hasSession;
    }
  });
  if (el.sessionEditTitle) {
    el.sessionEditTitle.value = meta.title || "";
  }
  if (el.sessionEditType) {
    el.sessionEditType.value = meta.type || "";
  }
  if (el.sessionEditNote) {
    el.sessionEditNote.value = meta.note || "";
  }
}

function buildMessagesFromEvents(events) {
  const messages = [];
  const draftRef = { id: "" };
  events.forEach((event) => {
    applyEventToMessages(messages, draftRef, event);
  });
  return { messages, assistantDraftId: draftRef.id };
}

function applyEventToMessages(messages, draftRef, event) {
  const seq = Number(event?.seq || 0);
  const role = String(event?.normalized?.role || "");
  const text = String(event?.normalized?.text ?? event?.payload?.text ?? "");
  const done = Boolean(event?.normalized?.done);
  const action = extractActionText(event);

  if (role === "user") {
    // New user turn means previous assistant draft must be closed.
    finalizeAssistantDraft(messages, draftRef, seq);

    const pendingIndex = messages.findIndex((item) => item.pending && item.role === "user" && item.text === text);
    if (pendingIndex >= 0) {
      messages[pendingIndex] = {
        ...messages[pendingIndex],
        id: `evt-${seq}`,
        seq,
        pending: false,
        done: true,
      };
      return;
    }
    messages.push({ id: `evt-${seq}`, role: "user", text, done: true, pending: false, seq });
    return;
  }

  if (role !== "assistant") {
    return;
  }

  // Stream mode: render each assistant delta chunk as an individual message.
  if (event.type === "message.delta" && text) {
    finalizeAssistantDraft(messages, draftRef, seq);
    const msg = {
      id: `evt-${seq}-${messages.length}`,
      role: "assistant",
      text,
      actions: action ? [action] : [],
      done: false,
      pending: true,
      seq,
    };
    messages.push(msg);
    draftRef.id = msg.id;
    return;
  }

  // message.done is treated as turn boundary; if it carries no text,
  // only finalize the active draft and avoid inserting an empty bubble.
  if ((event.type === "message.done" || done) && !text && !action) {
    finalizeAssistantDraft(messages, draftRef, seq);
    return;
  }

  if ((event.type === "message.done" || done) && text) {
    finalizeAssistantDraft(messages, draftRef, seq);
    messages.push({
      id: `evt-${seq}-${messages.length}`,
      role: "assistant",
      text,
      actions: action ? [action] : [],
      done: true,
      pending: false,
      seq,
    });
    draftRef.id = "";
    return;
  }

  if (draftRef.id) {
    const draft = messages.find((item) => item.id === draftRef.id);
    if (draft) {
      if (action) {
        appendActionToMessage(draft, action);
      }
      draft.seq = seq || draft.seq;
      if (done || event.type === "message.done") {
        draft.done = true;
        draft.pending = false;
        draftRef.id = "";
      }
      return;
    }
    draftRef.id = "";
  }

  if (action && !done) {
    const msg = {
      id: `evt-${seq}`,
      role: "assistant",
      text: "",
      actions: [action],
      done: false,
      pending: true,
      seq,
    };
    messages.push(msg);
    draftRef.id = msg.id;
    return;
  }

  if (event.type === "message.done" || done) {
    messages.push({
      id: `evt-${seq}`,
      role: "assistant",
      text: text || "",
      actions: action ? [action] : [],
      done: true,
      pending: false,
      seq,
    });
  }
}

function extractActionText(event) {
  return String(event?.normalized?.action ?? event?.payload?.action ?? "").trim();
}

function appendActionToMessage(message, actionText) {
  const action = String(actionText || "").trim();
  if (!action) {
    return;
  }
  if (!Array.isArray(message.actions)) {
    message.actions = [];
  }
  if (message.actions[message.actions.length - 1] === action) {
    return;
  }
  message.actions.push(action);
  if (message.actions.length > 20) {
    message.actions = message.actions.slice(-20);
  }
}

function bindChatNavigation() {
  if (el.chatThread) {
    el.chatThread.addEventListener("scroll", () => {
      updateChatNavigation();
    });
  }
  if (el.chatJumpTop) {
    el.chatJumpTop.addEventListener("click", () => {
      el.chatThread.scrollTo({ top: 0, behavior: "smooth" });
    });
  }
  if (el.chatJumpBottom) {
    el.chatJumpBottom.addEventListener("click", () => {
      el.chatThread.scrollTo({ top: el.chatThread.scrollHeight, behavior: "smooth" });
    });
  }
  updateChatNavigation();
}

function updateChatNavigation() {
  if (!el.chatThread) {
    return;
  }
  const scrollable = el.chatThread.scrollHeight - el.chatThread.clientHeight > 80;
  const atTop = el.chatThread.scrollTop <= 12;
  const distanceToBottom = Math.max(0, el.chatThread.scrollHeight - el.chatThread.clientHeight - el.chatThread.scrollTop);
  const atBottom = distanceToBottom <= 12;

  if (el.chatJumpTop) {
    el.chatJumpTop.classList.toggle("is-hidden", !scrollable || atTop);
  }
  if (el.chatJumpBottom) {
    el.chatJumpBottom.classList.toggle("is-hidden", !scrollable || atBottom);
  }
}

function isChatNearBottom(threshold = 56) {
  if (!el.chatThread) {
    return true;
  }
  const distanceToBottom = Math.max(0, el.chatThread.scrollHeight - el.chatThread.clientHeight - el.chatThread.scrollTop);
  return distanceToBottom <= threshold;
}

function escapeHTML(raw) {
  return String(raw ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function escapeAttr(raw) {
  return escapeHTML(raw).replace(/`/g, "&#96;");
}

function renderMarkdownHTML(source) {
  const tokenMap = [];
  const pushToken = (html) => {
    const key = `__MD_TOKEN_${tokenMap.length}__`;
    tokenMap.push({ key, html });
    return key;
  };

  let content = String(source || "").replace(/\r\n?/g, "\n");

  // Fenced code blocks first to avoid being touched by inline formatting.
  content = content.replace(/```([a-zA-Z0-9_-]+)?\n([\s\S]*?)```/g, (_, language, code) => {
    const lang = String(language || "").trim().replace(/[^a-zA-Z0-9_-]/g, "");
    const className = lang ? ` class="language-${lang}"` : "";
    return pushToken(`<pre class="md-code"><code${className}>${escapeHTML(code)}</code></pre>`);
  });

  content = content.replace(/`([^`\n]+)`/g, (_, code) => {
    return pushToken(`<code>${escapeHTML(code)}</code>`);
  });

  content = content.replace(/\[([^\]\n]+)\]\(([^)\s]+)\)/g, (match, label, url) => {
    const href = String(url || "").trim();
    if (!/^https?:\/\//i.test(href)) {
      return match;
    }
    return pushToken(
      `<a href="${escapeAttr(href)}" target="_blank" rel="noopener noreferrer">${escapeHTML(label)}</a>`,
    );
  });

  let html = escapeHTML(content);

  // Block-level Markdown.
  html = html
    .replace(/^######\s+(.*)$/gm, "<h6>$1</h6>")
    .replace(/^#####\s+(.*)$/gm, "<h5>$1</h5>")
    .replace(/^####\s+(.*)$/gm, "<h4>$1</h4>")
    .replace(/^###\s+(.*)$/gm, "<h3>$1</h3>")
    .replace(/^##\s+(.*)$/gm, "<h2>$1</h2>")
    .replace(/^#\s+(.*)$/gm, "<h1>$1</h1>")
    .replace(/^&gt;\s?(.*)$/gm, "<blockquote>$1</blockquote>");

  html = html.replace(/(?:^(?:\s*[-*+]\s+.+)\n?)+/gm, (block) => {
    const items = block
      .trim()
      .split("\n")
      .map((line) => line.replace(/^\s*[-*+]\s+/, "").trim())
      .filter(Boolean);
    if (items.length === 0) {
      return block;
    }
    return `<ul>${items.map((item) => `<li>${item}</li>`).join("")}</ul>`;
  });

  html = html.replace(/(?:^(?:\s*\d+\.\s+.+)\n?)+/gm, (block) => {
    const items = block
      .trim()
      .split("\n")
      .map((line) => line.replace(/^\s*\d+\.\s+/, "").trim())
      .filter(Boolean);
    if (items.length === 0) {
      return block;
    }
    return `<ol>${items.map((item) => `<li>${item}</li>`).join("")}</ol>`;
  });

  // Inline Markdown.
  html = html.replace(/\*\*([^*\n][\s\S]*?)\*\*/g, "<strong>$1</strong>");
  html = html.replace(/\n/g, "<br />");

  tokenMap.forEach((item) => {
    html = html.replaceAll(item.key, item.html);
  });
  return html;
}

function renderChatThread() {
  const shouldStickToBottom = isChatNearBottom();
  el.chatThread.innerHTML = "";
  if (state.messages.length === 0) {
    const empty = document.createElement("p");
    empty.className = "chat-empty";
    empty.textContent = "当前会话暂无消息，先输入一条 continue 开始对话。";
    el.chatThread.appendChild(empty);
    renderToolInsights();
    updateChatNavigation();
    return;
  }

  state.messages.forEach((message) => {
    const bubble = document.createElement("div");
    bubble.classList.add("chat-bubble", message.role === "user" ? "user" : "assistant");
    if (message.pending) {
      bubble.classList.add("pending");
    }
    const meta = document.createElement("p");
    meta.className = "chat-meta";
    meta.textContent = message.role === "user" ? "You" : "Assistant";

    const body = document.createElement("div");
    body.className = "chat-body";
    body.innerHTML = renderMarkdownHTML(message.text || "");

    bubble.append(meta, body);
    if (Array.isArray(message.actions) && message.actions.length > 0) {
      const actions = document.createElement("ul");
      actions.className = "chat-actions";
      message.actions.forEach((item) => {
        const li = document.createElement("li");
        li.textContent = item;
        actions.appendChild(li);
      });
      bubble.appendChild(actions);
    }
    el.chatThread.appendChild(bubble);
  });

  const tailSpacer = document.createElement("div");
  tailSpacer.className = "chat-tail-spacer";
  tailSpacer.setAttribute("aria-hidden", "true");
  el.chatThread.appendChild(tailSpacer);

  if (shouldStickToBottom) {
    el.chatThread.scrollTop = el.chatThread.scrollHeight;
  }
  renderToolInsights();
  updateChatNavigation();
}

function renderToolInsights() {
  if (!el.toolEventList) {
    return;
  }
  el.toolEventList.innerHTML = "";

  const recentActions = [];
  for (let i = state.messages.length - 1; i >= 0 && recentActions.length < 24; i -= 1) {
    const message = state.messages[i];
    if (!Array.isArray(message?.actions) || message.actions.length === 0) {
      continue;
    }
    for (let j = message.actions.length - 1; j >= 0 && recentActions.length < 24; j -= 1) {
      recentActions.push({
        role: message.role === "user" ? "用户" : "助手",
        seq: Number(message.seq || 0),
        text: String(message.actions[j] || "").trim(),
      });
    }
  }

  if (recentActions.length === 0) {
    const empty = document.createElement("li");
    empty.className = "tool-event-empty";
    empty.textContent = "暂无工具动作";
    el.toolEventList.appendChild(empty);
    return;
  }

  recentActions.forEach((action, idx) => {
    const li = document.createElement("li");
    li.className = "tool-event-item";

    const meta = document.createElement("p");
    meta.className = "tool-event-meta";
    meta.textContent = `#${recentActions.length - idx} · ${action.role}${action.seq > 0 ? ` · seq ${action.seq}` : ""}`;

    const body = document.createElement("p");
    body.className = "tool-event-body";
    body.textContent = action.text || "-";

    li.append(meta, body);
    el.toolEventList.appendChild(li);
  });
}

async function connectStream() {
  const adapter = currentAdapter();
  const sessionID = state.selectedSessionID;
  if (!adapter || !sessionID) {
    closeStream();
    return;
  }

  cancelStreamReconnect();
  closeStream(false);
  const wsURL = `${state.config.ws_base_url}/ws/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}?last_seq=${state.lastSeq}`;
  let ws;
  try {
    ws = new WebSocket(wsURL);
  } catch (err) {
    setStreamStatus(`WS 连接失败: ${err.message}`, true);
    scheduleStreamReconnect(adapter, sessionID);
    return;
  }
  ws.__manualClose = false;
  ws.__adapter = adapter;
  ws.__sessionID = sessionID;
  state.ws = ws;

  ws.addEventListener("open", () => {
    if (state.ws !== ws) {
      return;
    }
    cancelStreamReconnect();
    state.wsReconnectAttempts = 0;
    setStreamStatus("WS 已连接", false);
  });

  ws.addEventListener("message", (event) => {
    try {
      const frame = JSON.parse(event.data);
      handleWSFrame(frame);
    } catch (err) {
      setStreamStatus(`WS 消息解析失败: ${err.message}`, true);
    }
  });

  ws.addEventListener("close", () => {
    if (state.ws === ws) {
      state.ws = null;
    }
    if (ws.__manualClose) {
      return;
    }
    if (state.selectedSessionID !== ws.__sessionID) {
      return;
    }
    if (currentAdapter() !== ws.__adapter) {
      return;
    }
    scheduleStreamReconnect(ws.__adapter, ws.__sessionID);
  });

  ws.addEventListener("error", () => {
    // close 事件统一处理重连；这里仅提示瞬时错误。
    setStreamStatus("WS 连接错误，等待重连...", true);
  });
}

function scheduleStreamReconnect(adapter, sessionID) {
  if (!adapter || !sessionID) {
    return;
  }
  if (state.wsReconnectTimer) {
    return;
  }
  const attempts = state.wsReconnectAttempts + 1;
  state.wsReconnectAttempts = attempts;
  const delay = Math.min(10000, 500 * 2 ** Math.max(0, attempts - 1));
  setStreamStatus(`WS 已断开，${delay}ms 后重连（第 ${attempts} 次）`, true);

  state.wsReconnectTimer = setTimeout(async () => {
    state.wsReconnectTimer = null;
    if (state.selectedSessionID !== sessionID) {
      return;
    }
    if (currentAdapter() !== adapter) {
      return;
    }
    await connectStream();
  }, delay);
}

function cancelStreamReconnect() {
  if (state.wsReconnectTimer) {
    clearTimeout(state.wsReconnectTimer);
    state.wsReconnectTimer = null;
  }
}

function handleWSFrame(frame) {
  if (frame.frame_type === "heartbeat") {
    setStreamStatus(`WS 心跳 seq=${frame.seq ?? 0}`, false);
    return;
  }
  if (frame.frame_type === "error") {
    setStreamStatus(`WS 错误: ${JSON.stringify(frame.data)}`, true);
    return;
  }
  if (frame.frame_type === "done") {
    // Some backends may emit done frame without a distinct message.done event.
    const draftRef = { id: state.assistantDraftId };
    finalizeAssistantDraft(state.messages, draftRef);
    state.assistantDraftId = draftRef.id;
    renderChatThread();
    updateLocalSessionStatus(state.selectedSessionID, "idle");
    setStreamStatus("收到 done 帧", false);
    return;
  }
  if (frame.frame_type !== "event") {
    return;
  }

  const event = frame.data || {};
  if (event?.session_id && state.selectedSessionID && event.session_id !== state.selectedSessionID) {
    return;
  }
  if (event?.adapter && currentAdapter() && event.adapter !== currentAdapter()) {
    return;
  }
  const seq = Number(event.seq || 0);
  if (seq <= state.lastSeq) {
    return;
  }
  state.lastSeq = seq;

  updateSelectedSessionByEvent(event);
  const draftRef = { id: state.assistantDraftId };
  applyEventToMessages(state.messages, draftRef, event);
  state.assistantDraftId = draftRef.id;
  if (event.type === "message.user" || event.type === "message.delta") {
    updateLocalSessionStatus(state.selectedSessionID, "running");
  }
  if (event.type === "message.done" || (event?.normalized?.role === "assistant" && event?.normalized?.done)) {
    updateLocalSessionStatus(state.selectedSessionID, "idle");
  }
  renderChatThread();
}

function updateSelectedSessionByEvent(event) {
  const sessionID = state.selectedSessionID;
  if (!sessionID) {
    return;
  }
  const idx = state.sessions.findIndex((item) => item.id === sessionID);
  if (idx < 0) {
    return;
  }
  const ts = String(event?.ts || "").trim() || new Date().toISOString();
  state.sessions[idx] = {
    ...state.sessions[idx],
    updated_at: ts,
  };
  state.sessions.sort(compareSessionUpdatedAtDesc);
  renderSessionList();
  applySessionHeader(sessionID);
}

function closeStream(resetRetry = true) {
  cancelStreamReconnect();
  if (state.ws) {
    state.ws.__manualClose = true;
    state.ws.close();
    state.ws = null;
  }
  if (resetRetry) {
    state.wsReconnectAttempts = 0;
  }
  updateSettingsStreamContext();
}

function finalizeAssistantDraft(messages, draftRef, fallbackSeq = 0) {
  if (!draftRef?.id) {
    return;
  }
  const draft = messages.find((item) => item.id === draftRef.id);
  if (draft) {
    draft.done = true;
    draft.pending = false;
    if (!draft.seq && fallbackSeq > 0) {
      draft.seq = fallbackSeq;
    }
  }
  draftRef.id = "";
}

async function fetchAPI(path, options = {}) {
  const baseURL = trimTrailingSlash(state.config.api_base_url || "");
  if (!baseURL) {
    throw new Error("api_base_url 未配置");
  }
  const url = `${baseURL}${path.startsWith("/") ? "" : "/"}${path}`;

  const timeout = Number(state.config.request_timeout_ms || 30000);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);

  try {
    const response = await fetch(url, {
      ...options,
      headers: {
        ...(options.headers || {}),
      },
      signal: controller.signal,
    });
    const text = await response.text();
    const payload = text ? JSON.parse(text) : null;

    if (!response.ok) {
      const msg = payload?.message || `HTTP ${response.status}`;
      const code = payload?.code ? ` (${payload.code})` : "";
      throw new Error(`${msg}${code}`);
    }
    return payload;
  } catch (err) {
    if (err.name === "AbortError") {
      throw new Error("请求超时");
    }
    throw err;
  } finally {
    clearTimeout(timer);
  }
}

function setSettingsStatus(text, isError) {
  el.settingsStatus.textContent = text;
  el.settingsStatus.classList.toggle("text-danger", Boolean(isError));
}

function setStreamStatus(text, isError) {
  state.streamStatusText = String(text || "");
  state.streamStatusError = Boolean(isError);
  el.streamStatus.textContent = text;
  el.streamStatus.classList.toggle("text-danger", Boolean(isError));
  updateSettingsStreamContext();
}

function setCreateSessionStatus(text, isError) {
  el.createSessionStatus.textContent = text;
  el.createSessionStatus.classList.toggle("text-danger", Boolean(isError));
}

function setSessionEditorStatus(text, isError) {
  if (!el.sessionEditorStatus) {
    return;
  }
  el.sessionEditorStatus.textContent = text;
  el.sessionEditorStatus.classList.toggle("text-danger", Boolean(isError));
}

function setContinuePending(pending) {
  state.continuePending = Boolean(pending);
  el.continuePrompt.disabled = state.continuePending;
  el.continueSubmit.disabled = state.continuePending;
  el.continueSubmit.textContent = state.continuePending ? "发送中..." : "Continue";
  updateDeleteSessionButtonState();
}
