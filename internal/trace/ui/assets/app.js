// Minimal trace viewer — no build step, vanilla JS.
//
// State machine:
//   1. Fetch /api/sessions, populate sidebar.
//   2. On session click, open SSE /api/sessions/{id}/stream.
//   3. Each SSE event carries one JSONL record line; append to timeline.
//   4. On row click, render the full JSON in the detail pane.

const $ = (sel) => document.querySelector(sel);
const sessions = $("#session-list");
const timeline = $("#timeline");
const detail = $("#detail-body");
const sessionMeta = $("#session-meta");
const statusEl = $("#status");

const state = {
  currentSession: null,
  records: [],
  // seenIDs deduplicates records across SSE reconnects. The server starts
  // each stream from offset 0 and replays the whole file, so on transient
  // disconnects (laptop sleep, localhost blip) we'd otherwise accumulate
  // duplicates. Each JSONL record carries a unique id field — use that.
  seenIDs: new Set(),
  eventSource: null,
  filters: {
    message:   true,
    inference: true,
    tool:      true,
    system:    true,
    state:     true,
  },
};

function classify(type) {
  if (!type) return "unknown";
  if (type.startsWith("session."))         return "session";
  if (type.startsWith("message."))         return "message";
  if (type.startsWith("inference."))       return "inference";
  if (type.startsWith("tools."))           return "tool";
  if (type === "tool.invoked" || type === "tool.completed") return "tool";
  if (type.startsWith("system."))          return "system";
  if (type === "state.patched")            return "state";
  return "unknown";
}

function fmtTime(s) {
  if (!s) return "";
  const d = new Date(s);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleTimeString();
}

function labelFor(rec) {
  let label = rec.type;
  if (rec.message)   label += "  " + (rec.message.role || "");
  if (rec.system)    label += "  " + (rec.system.name || "");
  if (rec.tools && rec.tools.schema) label += "  " + rec.tools.schema.name;
  if (rec.tools && rec.tools.name)   label += "  " + rec.tools.name;
  if (rec.inference) label += "  turn " + (rec.inference.turn || "?");
  return label;
}

function setStatus(text, live) {
  statusEl.textContent = text;
  statusEl.classList.toggle("live", !!live);
}

async function loadSessions() {
  setStatus("loading…");
  try {
    const r = await fetch("/api/sessions");
    if (!r.ok) throw new Error(r.statusText);
    const list = await r.json();
    renderSessionList(list);
    setStatus("");
  } catch (e) {
    setStatus("error: " + e.message);
  }
}

function renderSessionList(list) {
  sessions.innerHTML = "";
  if (!list.length) {
    const li = document.createElement("li");
    li.textContent = "(no sessions in this project)";
    li.style.color = "var(--fg-dim)";
    sessions.appendChild(li);
    return;
  }
  for (const s of list) {
    const li = document.createElement("li");
    li.dataset.id = s.id;
    // s.id is derived from the transcripts/ filesystem listing. Today it
    // is always a UUID, but filename-shaped data flowing into innerHTML
    // shouldn't be trusted on principle. Escape consistently with title.
    li.innerHTML =
      '<div>' + escapeHTML(s.title || "(untitled)") + '</div>' +
      '<span class="id">' + escapeHTML(s.id.slice(0, 12)) + '… · ' + Math.round(s.size / 1024) + 'KB</span>';
    li.addEventListener("click", () => openSession(s.id, li));
    sessions.appendChild(li);
  }
}

function openSession(id, li) {
  for (const x of sessions.querySelectorAll("li.active")) x.classList.remove("active");
  if (li) li.classList.add("active");

  if (state.eventSource) {
    state.eventSource.close();
    state.eventSource = null;
  }
  state.currentSession = id;
  state.records = [];
  state.seenIDs.clear();
  timeline.innerHTML = "";
  detail.textContent = "Click a record to inspect its payload.";
  sessionMeta.textContent = id;

  const es = new EventSource("/api/sessions/" + encodeURIComponent(id) + "/stream");
  state.eventSource = es;
  setStatus("connecting…");

  es.onopen = () => setStatus("connected", true);
  es.onerror = () => setStatus("disconnected");
  es.onmessage = (ev) => {
    let rec;
    try {
      rec = JSON.parse(ev.data);
    } catch (e) {
      return;
    }
    // Browsers transparently reconnect EventSource on transient drops
    // (laptop sleep, localhost blip). The server starts each stream from
    // offset 0 and replays the whole file, so without dedup the timeline
    // would balloon across reconnects. Each record's id is unique on disk.
    if (rec.id && state.seenIDs.has(rec.id)) return;
    if (rec.id) state.seenIDs.add(rec.id);
    state.records.push(rec);
    appendRow(rec, state.records.length - 1);
  };
}

function appendRow(rec, idx) {
  const klass = classify(rec.type);
  if (!passesFilter(klass)) return;

  const row = document.createElement("div");
  row.className = "row " + klass;
  row.dataset.idx = idx;
  row.innerHTML =
    '<span class="time">' + fmtTime(rec.time) + '</span>' +
    '<span class="swatch"></span>' +
    '<span class="label">' + escapeHTML(labelFor(rec)) + '</span>';
  row.addEventListener("click", () => showDetail(idx, row));
  timeline.appendChild(row);

  // Auto-scroll if user is near the bottom.
  if (timeline.scrollHeight - timeline.scrollTop - timeline.clientHeight < 100) {
    timeline.scrollTop = timeline.scrollHeight;
  }
}

function showDetail(idx, row) {
  for (const x of timeline.querySelectorAll(".row.active")) x.classList.remove("active");
  row.classList.add("active");
  const rec = state.records[idx];
  detail.textContent = JSON.stringify(rec, null, 2);
}

function passesFilter(klass) {
  if (klass === "state")     return state.filters.state;
  if (klass === "tool")      return state.filters.tool;
  if (klass === "system")    return state.filters.system;
  if (klass === "inference") return state.filters.inference;
  if (klass === "message")   return state.filters.message;
  return true;
}

function wireFilters() {
  const map = {
    "filter-state":     "state",
    "filter-tools":     "tool",
    "filter-system":    "system",
    "filter-inference": "inference",
    "filter-message":   "message",
  };
  for (const [id, key] of Object.entries(map)) {
    const el = document.getElementById(id);
    el.addEventListener("change", () => {
      state.filters[key] = el.checked;
      // Re-render timeline against current filters.
      timeline.innerHTML = "";
      state.records.forEach((r, i) => appendRow(r, i));
    });
  }
}

function escapeHTML(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

wireFilters();
loadSessions();
