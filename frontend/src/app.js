const STORAGE_KEY = "aswg_runtime_config_v1";

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
};

const el = {
  settingsForm: document.getElementById("settings-form"),
  settingsStatus: document.getElementById("settings-status"),
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
  sessionList: document.getElementById("session-list"),
  sessionTitle: document.getElementById("session-title"),
  sessionMeta: document.getElementById("session-meta"),
  chatThread: document.getElementById("chat-thread"),
  continueForm: document.getElementById("continue-form"),
  continuePrompt: document.getElementById("continue-prompt"),
  continueSubmit: document.getElementById("continue-submit"),
  streamStatus: document.getElementById("stream-status"),
  reconnectStream: document.getElementById("reconnect-stream"),
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
  bindEvents();

  await refreshAdapters();
  await refreshSessions();
  await restoreSelectedSession();
}

function bindEvents() {
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
  });

  el.reloadSessions.addEventListener("click", async () => {
    await refreshSessions();
    await restoreSelectedSession();
  });

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
}

async function refreshSessions() {
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
      const query = new URLSearchParams({ limit: String(pageLimit) });
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
        items.push(session);
      });

      const hasMore = Boolean(result?.data?.has_more);
      const nextCursor = result?.data?.next_cursor || "";
      if (!hasMore || !nextCursor || nextCursor === cursor) {
        break;
      }
      cursor = nextCursor;
    }

    state.sessions = items;
    renderSessionList();
  } catch (err) {
    state.sessions = [];
    renderSessionList();
    setStreamStatus(`加载会话失败: ${err.message}`, true);
  }
}

function renderSessionList() {
  el.sessionList.innerHTML = "";
  if (state.sessions.length === 0) {
    const li = document.createElement("li");
    li.textContent = "无可用会话";
    el.sessionList.appendChild(li);
    return;
  }

  state.sessions.forEach((session) => {
    const li = document.createElement("li");
    const button = document.createElement("button");
    button.type = "button";
    button.classList.add("session-card");
    if (session.id === state.selectedSessionID) {
      button.classList.add("active");
    }
    const title = document.createElement("p");
    title.className = "session-card-title";
    title.textContent = session.title || session.id;

    const meta = document.createElement("p");
    meta.className = "session-card-meta";
    meta.textContent = `${session.workspace || "-"} · ${session.updated_at || "-"}`;

    button.append(title, meta);
    button.addEventListener("click", async () => {
      state.selectedSessionID = session.id;
      renderSessionList();
      await loadSession(session.id);
    });
    li.appendChild(button);
    el.sessionList.appendChild(li);
  });
}

async function restoreSelectedSession() {
  if (state.sessions.length === 0) {
    clearSessionView();
    return;
  }

  const exists = state.sessions.some((s) => s.id === state.selectedSessionID);
  if (!exists) {
    state.selectedSessionID = state.sessions[0].id;
  }
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
  renderChatThread();
  setStreamStatus("WS 未连接", false);
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
    const events = await fetchAPI(
      `/api/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}/events?limit=500`,
      { method: "GET" },
    );

    const sessionDetail = detail?.data || {};
    const eventItems = events?.data?.items || [];

    el.sessionTitle.textContent = sessionDetail.title || sessionID;
    el.sessionMeta.textContent = `workspace: ${sessionDetail.workspace || "-"} · updated_at: ${sessionDetail.updated_at || "-"}`;

    const rebuilt = buildMessagesFromEvents(eventItems);
    state.messages = rebuilt.messages;
    state.assistantDraftId = rebuilt.assistantDraftId;
    state.lastSeq = eventItems.length > 0 ? Number(eventItems[eventItems.length - 1].seq || 0) : 0;
    renderChatThread();

    await connectStream();
  } catch (err) {
    setStreamStatus(`加载会话详情失败: ${err.message}`, true);
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

function renderChatThread() {
  el.chatThread.innerHTML = "";
  if (state.messages.length === 0) {
    const empty = document.createElement("p");
    empty.className = "chat-empty";
    empty.textContent = "当前会话暂无消息，先输入一条 continue 开始对话。";
    el.chatThread.appendChild(empty);
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
    body.textContent = message.text || "";

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

  el.chatThread.scrollTop = el.chatThread.scrollHeight;
}

async function connectStream() {
  const adapter = currentAdapter();
  const sessionID = state.selectedSessionID;
  if (!adapter || !sessionID) {
    return;
  }

  closeStream();
  const wsURL = `${state.config.ws_base_url}/ws/v1/adapters/${encodeURIComponent(adapter)}/sessions/${encodeURIComponent(sessionID)}?last_seq=${state.lastSeq}`;
  const ws = new WebSocket(wsURL);
  state.ws = ws;

  ws.addEventListener("open", () => {
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
      setStreamStatus("WS 已断开", true);
    }
  });

  ws.addEventListener("error", () => {
    if (state.ws === ws) {
      setStreamStatus("WS 连接错误", true);
    }
  });
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
    setStreamStatus("收到 done 帧", false);
    return;
  }
  if (frame.frame_type !== "event") {
    return;
  }

  const event = frame.data || {};
  const seq = Number(event.seq || 0);
  if (seq <= state.lastSeq) {
    return;
  }
  state.lastSeq = seq;

  const draftRef = { id: state.assistantDraftId };
  applyEventToMessages(state.messages, draftRef, event);
  state.assistantDraftId = draftRef.id;
  renderChatThread();
}

function closeStream() {
  if (state.ws) {
    state.ws.close();
    state.ws = null;
  }
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
  el.streamStatus.textContent = text;
  el.streamStatus.classList.toggle("text-danger", Boolean(isError));
}

function setCreateSessionStatus(text, isError) {
  el.createSessionStatus.textContent = text;
  el.createSessionStatus.classList.toggle("text-danger", Boolean(isError));
}

function setContinuePending(pending) {
  state.continuePending = Boolean(pending);
  el.continuePrompt.disabled = state.continuePending;
  el.continueSubmit.disabled = state.continuePending;
  el.continueSubmit.textContent = state.continuePending ? "发送中..." : "Continue";
}
