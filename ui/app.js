/* ===== dock-util UI 로직 (vanilla, withGlobalTauri) ===== */
const T = window.__TAURI__;
const invoke = T.core.invoke;
const listen = T.event.listen;
const getWin = () => (T.window.getCurrentWindow ? T.window.getCurrentWindow() : T.window.getCurrent());

const label = getWin().label;
const isOrb = label === "orb";
const popWidget = label.startsWith("popover-") ? label.slice("popover-".length) : null;

/* ---------- i18n ---------- */
const STR = {
  ko: {
    form: "형태", orb: "⬤ 오브", bar: "▬ 바", language: "언어", korean: "한국어", english: "English",
    quit: "종료", auto_note: "독 옆 공간은 현재 화면에 맞춰 자동 배치됩니다.",
    notifications: "알림", feed_empty: "알림이 없습니다", feed_clear: "전체 지우기",
    pomodoro: "포모도로", terminal: "터미널", shelf: "선반", monitor: "시스템", toggles: "토글",
    ai_term: "AI 터미널", ai_apps: "AI 앱", not_installed: "미설치",
    launch_claude_app: "Claude 앱", launch_chatgpt_app: "ChatGPT 앱",
    run_codex: "Codex 실행", run_claude: "Claude Code 실행",
    agents: "에이전트", formatter: "포매터", settings: "설정",
    focus: "집중", brk: "휴식", focus_started: "집중 시작", break_started: "휴식 시작",
    round: "라운드", waiting: "대기 중", paused_txt: "일시정지 · ",
    presets: "프리셋", start: "시작", pause: "일시정지", resume: "재개", reset: "리셋",
    recent_cpu: "CPU 최근 기록", collecting: "수집 중…", disk_free: "디스크 여유",
    cpu: "CPU", memory: "메모리",
    appearance: "모양 전환", network: "네트워크", device_none: "장치 없음",
    system_volume: "시스템 볼륨", stay_awake: "잠금 방지", keep_display: "화면 꺼짐 방지",
    perm_note: "일부 토글은 첫 사용 시 자동화 권한을 요청합니다.",
    agents_title: "실행중 에이전트", no_agents: "동작중인 에이전트가 없습니다",
    agents_hint: "claude · codex · gemini · copilot · aider 등을 감지",
    agents_refresh: "10초마다 자동 갱신 · 종료 시 피드에 기록",
    shelf_title: "파일 선반", total: "합계", open: "열기", copy: "복사", move: "이동",
    move_all: "전체 이동…", copy_all: "모두 복사", clear: "비우기",
    shelf_empty: "파일을 바 또는 여기로 끌어다 놓으세요",
    shelf_empty_sub: "임시 보관 후 한 번에 이동할 수 있습니다",
    folder: "폴더",
    formatter: "포매터", input: "입력", auto: "자동", convert: "변환", pretty: "정리", minify: "압축",
    paste: "클립보드→", copy_out: "→복사", result: "결과",
    fmt_done_auto: " 자동 감지 완료", fmt_done: " 정리 완료", fmt_convert_done: " 변환 완료",
    term_fixed: "고정 · 가장자리 끌어 크기 조절", new_shell: "새 셸",
    term_hint: "⌘C 복사 · ⌘V 붙여넣기 · ⌘ +/-/0 글자크기 · ⌘W 닫기",
    term_exit_msg: "[셸 종료 — 새 셸 버튼으로 재시작]", new_shell_started: "새 셸 시작",
    drop_hint: "놓으면 선반에 올라갑니다",
    n_on: "개 켬", running_suffix: "대", zero_agents: "0대", items_suffix: "개",
    empty_shelf: "비었음", copied: "복사됨", copy_fail: "복사 실패",
    paste_fail: "클립보드 읽기 실패", xterm_fail: "xterm 로드 실패",
    widgets_visible: "위젯 표시", always_on: "설정은 항상 표시",
    installed: "설치됨 · 터미널에서 실행", term_note: "내장 터미널에서 실행되며 닫아도 계속 돌아갑니다.",
    apps_note: "기본 브라우저/앱으로 실행됩니다.", fmt_placeholder: "붙여넣기 (⌘V) 또는 직접 입력…",
    moved: "이동", failed: "실패", ready: "준비 중",
  },
  en: {
    form: "Form", orb: "⬤ Orb", bar: "▬ Bar", language: "Language", korean: "한국어", english: "English",
    quit: "Quit", auto_note: "Position adapts to the screen automatically.",
    notifications: "Alerts", feed_empty: "No alerts", feed_clear: "Clear all",
    pomodoro: "Pomodoro", terminal: "Terminal", shelf: "Shelf", monitor: "System", toggles: "Toggles",
    ai_term: "AI Terminal", ai_apps: "AI Apps", not_installed: "Not installed",
    launch_claude_app: "Claude app", launch_chatgpt_app: "ChatGPT app",
    run_codex: "Run Codex", run_claude: "Run Claude Code",
    agents: "Agents", formatter: "Formatter", settings: "Settings",
    focus: "Focus", brk: "Break", focus_started: "Focus started", break_started: "Break started",
    round: "rounds", waiting: "Idle", paused_txt: "Paused · ",
    presets: "Presets", start: "Start", pause: "Pause", resume: "Resume", reset: "Reset",
    recent_cpu: "CPU history", collecting: "Collecting…", disk_free: "Disk free",
    cpu: "CPU", memory: "Memory",
    appearance: "Appearance", network: "Network", device_none: "No device",
    system_volume: "System volume", stay_awake: "Stay awake", keep_display: "Keep display on",
    perm_note: "Some toggles ask for Automation permission on first use.",
    agents_title: "Running agents", no_agents: "No agents running",
    agents_hint: "detects claude · codex · gemini · copilot · aider and more",
    agents_refresh: "Auto-refresh every 10s · exits are logged to feed",
    shelf_title: "File shelf", total: "total", open: "Open", copy: "Copy", move: "Move",
    move_all: "Move all…", copy_all: "Copy all", clear: "Clear",
    shelf_empty: "Drop files onto the bar or here",
    shelf_empty_sub: "Keep them handy, then move them all at once",
    folder: "folder",
    formatter: "Formatter", input: "Input", auto: "Auto", convert: "Convert", pretty: "Pretty", minify: "Minify",
    paste: "Clipboard→", copy_out: "→Copy", result: "Result",
    fmt_done_auto: " auto-detected", fmt_done: " formatted", fmt_convert_done: " converted",
    term_fixed: "pinned · drag edge to resize", new_shell: "New shell",
    term_hint: "⌘C copy · ⌘V paste · ⌘ +/-/0 font · ⌘W close",
    term_exit_msg: "[shell exited — use New shell]", new_shell_started: "New shell started",
    drop_hint: "Drop files to stage them on the shelf",
    n_on: " on", running_suffix: " running", zero_agents: "0", items_suffix: "",
    empty_shelf: "Empty", copied: "Copied", copy_fail: "Copy failed",
    paste_fail: "Clipboard read failed", xterm_fail: "xterm load failed",
    widgets_visible: "Show widgets", always_on: "Settings always visible",
    installed: "Installed · runs in terminal", term_note: "Runs in the built-in terminal; keeps running when closed.",
    apps_note: "Launches the default app.", fmt_placeholder: "Paste (⌘V) or type…",
    moved: "Moved", failed: "failed", ready: "Loading…",
  },
};
const t = (k) => {
  const lang = S.config?.lang || "ko";
  return (STR[lang] && STR[lang][k]) || STR.ko[k] || k;
};

/* ---------- 공용 상태 ---------- */
const S = {
  pomodoro: { phase: "idle", running: false, paused: false, remaining_secs: 0, focus_min: 25, break_min: 5, rounds_done: 0 },
  stats: null,
  agents: [],
  exited: 0,
  shelf: [],
  feed: [],
  toggles: null,
  unread: 0,
  barRect: null,
  config: null,
};

/* ---------- 아이콘 (SF Symbols 느낌의 미니 SVG) ---------- */
const IC = {
  timer: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="13" r="8"/><path d="M12 13V9"/><path d="M10 2h4"/><path d="M12 2v3"/></svg>`,
  monitor: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12h4l2.5-6 4 12 2.5-6h5"/></svg>`,
  toggles: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 7h10M18 7h2M4 17h4M12 17h8"/><circle cx="16" cy="7" r="2.2"/><circle cx="10" cy="17" r="2.2"/></svg>`,
  agents: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="8" width="14" height="11" rx="3"/><path d="M12 8V4M9 4h6"/><circle cx="9.5" cy="13" r="1" fill="currentColor" stroke="none"/><circle cx="14.5" cy="13" r="1" fill="currentColor" stroke="none"/><path d="M9.5 16.2h5"/></svg>`,
  shelf: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 9.5 12 4l8 5.5V19a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19Z"/><path d="M4 12.5h16"/></svg>`,
  format: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M8 4c-2 0-3 1-3 3v2c0 1.5-.8 2.4-2 3 1.2.6 2 1.5 2 3v2c0 2 1 3 3 3"/><path d="M16 4c2 0 3 1 3 3v2c0 1.5.8 2.4 2 3-1.2.6-2 1.5-2 3v2c0 2-1 3-3 3"/></svg>`,
  feed: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9a6 6 0 0 1 12 0c0 5 2 6 2 6H4s2-1 2-6"/><path d="M10 19a2 2 0 0 0 4 0"/></svg>`,
  settings: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3.2"/><path d="M12 2.8v2.4M12 18.8v2.4M4.9 4.9l1.7 1.7M17.4 17.4l1.7 1.7M2.8 12h2.4M18.8 12h2.4M4.9 19.1l1.7-1.7M17.4 6.6l1.7-1.7"/></svg>`,
  terminal: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4.5" width="18" height="15" rx="2.5"/><path d="m7 9.5 3.2 2.7L7 14.9"/><path d="M12.6 15h4.4"/></svg>`,
  ai_term: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4.5" width="18" height="15" rx="2.5"/><path d="m6.8 9.5 3 2.5-3 2.5"/><path d="M12.5 15h4.5"/><path d="M17.5 4.5 18.3 6.3 20.1 7.1 18.3 7.9 17.5 9.7 16.7 7.9 14.9 7.1 16.7 6.3z" fill="currentColor" stroke="none"/></svg>`,
  ai_apps: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="3.5" width="7" height="7" rx="1.8"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.8"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.8"/><path d="M17 13.8v6.4M13.8 17h6.4"/></svg>`,
};

const AGENT_EMOJI = {
  claude: "🟠", codex: "⚙️", gemini: "💎", copilot: "🐙", aider: "🛠️",
  cline: "📎", "cursor-agent": "(cursor)", opencode: "📂", goose: "🪿",
  amp: "⚡", droid: "🤖", crush: "💥", windsurf: "🌊", qwen: "🔮",
};

const fmtTime = (s) => `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
const $ = (sel) => document.querySelector(sel);

/* ---------- 이벤트 구독 (바/팝오버/오브 공통) ---------- */
async function bindEvents() {
  await listen("pomodoro", (e) => {
    S.pomodoro = e.payload;
    if (popWidget === "pomodoro") renderPop();
    if (isOrb) {
      const p = S.pomodoro;
      const el = $("#ob-pomodoro");
      if (el) {
        const show = p.running && !p.paused;
        el.textContent = show ? fmtTime(p.remaining_secs) : "";
        el.className = "orb-badge amber" + (show ? "" : " hidden");
      }
    }
  });
  await listen("stats", (e) => {
    S.stats = e.payload;
    S.hist = [...(S.hist || []), e.payload.cpu].slice(-48);
    if (popWidget === "monitor") renderPop();
  });
  await listen("agents", (e) => { S.agents = e.payload.list; S.exited = e.payload.exited; if (popWidget === "agents") renderPop(); if (isOrb) renderOrbBadges(); });
  await listen("shelf", (e) => { S.shelf = e.payload; if (popWidget === "shelf") renderPop(); if (isOrb) renderOrbBadges(); });
  await listen("toggles", (e) => { S.toggles = e.payload; if (popWidget === "toggles") renderPop(); });
  await listen("feed", (e) => { S.feed = e.payload; if (popWidget === "feed") renderPop(); if (isOrb) renderOrbBadges(); });
  await listen("feed-new", (e) => {
    S.feed = [e.payload, ...S.feed].slice(0, 60);
    if (popWidget === "feed") renderPop();
    if (isOrb) { S.unread++; renderOrbBadges(); }
  });
  await listen("lang-changed", async (e) => {
    S.config = S.config || {};
    S.config.lang = e.payload;
    if (isOrb) { buildOrb(); }
    else if (popWidget && popWidget !== "terminal") renderPop();
  });
}

/* ---------- 오브 (동그라미 → 호버 시 2중 링 블룸) ---------- */
// 윈도우 380x300, 오브 중심 (336, 264) — Rust placement의 ORB_CX/CY와 일치해야 함
// 2중 링: 안쪽 4개(r=115, 25° 간격), 바깥 5개(r=185, ~19.75° 간격) — 최소 인접 간격 62px (원 46px, 겹침 없음)
const ORB_GEO = { cx: 336, cy: 264 };
// 배치 순서(아래 끝에서 시작): 안쪽 링을 반시계방향으로 채우고, 끝에 도달하면
// 바깥 링을 시계방향(위→아래)으로 이어가는 스네이크 배치
const ORB_ORDER = ["terminal", "ai_term", "pomodoro", "shelf", "monitor", "toggles", "agents", "ai_apps", "format", "feed", "settings"];
const ORB_RINGS = [
  { r: 115, aFrom: 97, aTo: 172, cap: 4 },    // 안쪽 링: 위 → 아래
  { r: 185, aFrom: 178, aTo: 99, cap: 5 },    // 바깥 링: 아래 → 위 (방향 반전)
  { r: 235, aFrom: 97, aTo: 172, cap: 6 },    // 세 번째 링: 위 → 아래
];
const ORB_BADGE = { agents: "agents", shelf: "shelf", feed: "feed", pomodoro: "pomodoro" };
const ORB_LABELS = () => ({
  terminal: t("terminal"), ai_term: t("ai_term"), pomodoro: t("pomodoro"), shelf: t("shelf"),
  monitor: t("monitor"), toggles: t("toggles"), agents: t("agents"), ai_apps: t("ai_apps"),
  format: t("formatter"), feed: t("notifications"), settings: t("settings"),
});

function visibleWidgets() {
  const hidden = new Set(S.config?.hidden_widgets || []);
  const list = ORB_ORDER.filter((w) => !hidden.has(w));
  if (!list.includes("settings")) list.push("settings"); // 설정은 항상 표시
  return list;
}

// 위젯 수에 맞춰 두 링에 배분하고 좌표를 계산한다.
// 안쪽 링은 아래 끝(172°)에서 시작해 위(97°)로, 바깥 링은 위(99°)에서 아래(178°)로 방향을 바꿔 이어진다.
function orbLayout(widgets) {
  let idx = 0;
  const out = new Array(widgets.length);
  for (const ring of ORB_RINGS) {
    if (idx >= widgets.length) break;
    const count = Math.min(ring.cap, widgets.length - idx);
    for (let i = 0; i < count; i++) {
      const deg = count === 1
        ? (ring.aFrom + ring.aTo) / 2
        : ring.aFrom + ((ring.aTo - ring.aFrom) * i) / (count - 1);
      const rad = (deg * Math.PI) / 180;
      out[idx++] = { x: ORB_GEO.cx + ring.r * Math.cos(rad), y: ORB_GEO.cy - ring.r * Math.sin(rad) };
    }
  }
  return out;
}

function buildOrb() {
  const root = $("#orb");
  const widgets = visibleWidgets();
  const positions = orbLayout(widgets);
  const items = widgets.map((w, i) => {
    const p = positions[i];
    const half = 23;
    const badge = ORB_BADGE[w]
      ? `<span class="orb-badge hidden" id="ob-${w}"></span>`
      : "";
    return `
    <div class="orb-item" data-w="${w}" data-i="${i}"
         style="left:${p.x - half}px; top:${p.y - half}px; transition-delay:${i * 26}ms;">
      <div class="orb-circle">${IC[w] ?? ""}${badge}</div>
      <div class="orb-label">${ORB_LABELS()[w]}</div>
    </div>`;
  }).join("");
  root.innerHTML = `
    ${items}
    <div class="orb-center" id="orb-center">
      <div class="orb-pulse"></div>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
        <circle cx="12" cy="12" r="3.4"/>
        <circle cx="12" cy="12" r="9" stroke-dasharray="3 4.6"/>
      </svg>
    </div>`;
  root.querySelectorAll(".orb-item").forEach((el) => {
    el.addEventListener("click", () => {
      const w = el.dataset.w;
      const i = Number(el.dataset.i);
      const p = orbLayout(visibleWidgets())[i];
      // 펼쳐진 팬이 팝오버를 가리지 않게 접은 뒤 연다
      invoke("orb_collapse");
      invoke("open_popover", { widget: w, tileX: p.x });
    });
  });
  // 오브 윈도우에도 파일 드롭 → 선반
  listen("tauri://drag-drop", (e) => {
    const paths = e.payload?.paths || [];
    if (paths.length) invoke("shelf_add", { paths });
  });
  // 확장 상태는 Rust 폴링 결과를 40ms마다 invoke로 확인 (이벤트 채널 이중화)
  let lastExpanded = null;
  setInterval(async () => {
    try {
      const st = await invoke("orb_state");
      if (st.expanded !== lastExpanded) {
        lastExpanded = st.expanded;
        document.body.classList.toggle("expanded", !!st.expanded);
      }
    } catch {}
  }, 40);
  listen("visibility-changed", async () => {
    S.config = await invoke("get_config");
    buildOrb();
  });
  renderOrbBadges();
}

function renderOrbBadges() {
  const set = (id, count, cls) => {
    const el = $(`#ob-${id}`);
    if (!el) return;
    el.textContent = count > 0 ? String(count) : "";
    el.classList.toggle("hidden", !(count > 0));
    if (cls) el.className = `orb-badge ${cls} ${count > 0 ? "" : "hidden"}`;
  };
  set("agents", S.agents.length, "green");
  set("shelf", S.shelf.length, "blue");
  set("feed", S.unread, "purple");
}

/* ---------- 팝오버 렌더 ---------- */
function renderPop() {
  const root = $("#pop");
  const R = {
    pomodoro: popPomodoro, monitor: popMonitor, toggles: popToggles, agents: popAgents,
    shelf: popShelf, format: popFormat, feed: popFeed, settings: popSettings, terminal: popTerminal,
    ai_term: popAiTerm, ai_apps: popAiApps,
  };
  root.innerHTML = `
    <button class="pop-close" data-act="pop_close" title="닫기">✕</button>
    <div class="pop-inner">${(R[popWidget] || popEmpty)()}</div>`;
  bindPopActions();
  if (popWidget === "monitor") drawSpark();
  if (popWidget === "terminal") initTerm();
}

/* --- AI 런처 --- */
function popAiTerm() {
  const st = S.aiStatus || {};
  const row = (p, label, ok) => `
    <div class="list-item">
      <div class="li-icon">${p === "codex" ? "⚙️" : "🟠"}</div>
      <div class="li-main"><div class="li-title">${label}</div>
      <div class="li-sub">${ok ? "설치됨 · 터미널에서 실행" : t("not_installed")}</div></div>
      <button class="small ${ok ? "primary" : ""}" data-act="ai_cli" data-p="${p}" ${ok ? "" : "disabled"}>▶</button>
    </div>`;
  return `
    <h2>${IC.ai_term} ${t("ai_term")}</h2>
    ${row("codex", "Codex", st.codex)}
    ${row("claude", "Claude Code", st.claude)}
    <div class="stat-sub" style="margin-top:4px;">내장 터미널에서 실행되며 닫아도 계속 돌아갑니다.</div>`;
}

function popAiApps() {
  const st = S.aiStatus || {};
  const row = (key, label, icon, ok) => `
    <div class="list-item">
      <div class="li-icon">${icon}</div>
      <div class="li-main"><div class="li-title">${label}</div>
      <div class="li-sub">${ok ? "/Applications" : t("not_installed")}</div></div>
      <button class="small ${ok ? "primary" : ""}" data-act="ai_app" data-n="${key}" ${ok ? "" : "disabled"}>▶</button>
    </div>`;
  return `
    <h2>${IC.ai_apps} ${t("ai_apps")}</h2>
    ${row("claude", "Claude", "🟠", st.claude_app)}
    ${row("chatgpt", "ChatGPT", "🟢", st.chatgpt_app)}
    <div class="stat-sub" style="margin-top:4px;">기본 브라우저/앱으로 실행됩니다.</div>`;
}

function popTerminal() {
  return `<h2>${IC.terminal} ${t("terminal")} <span class="rs" style="font-weight:400">· ${t("term_fixed")}</span></h2>
    <div class="btns" style="margin-bottom:8px;">
      <button class="small" data-act="term_new">${t("new_shell")}</button>
      <button class="small" data-act="term_font_down">A−</button>
      <button class="small" data-act="term_font_up">A+</button>
      <span class="rs" style="align-self:center">${t("term_hint")}</span>
    </div>
    <div id="term"></div>`;
}

let TERM = null;
async function loadXterm() {
  if (window.Terminal) return true;
  const ok = await new Promise((res) => {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    link.href = "vendor/xterm.css";
    document.head.appendChild(link);
    const sc = document.createElement("script");
    sc.src = "vendor/xterm.js";
    sc.onload = () => res(true);
    sc.onerror = () => res(false);
    document.head.appendChild(sc);
  });
  if (ok) document.dispatchEvent(new Event("xterm-loaded"));
  return ok;
}

async function initTerm() {
  const el = $("#term");
  if (!el) return;
  if (!(await loadXterm())) { toast(t("xterm_fail")); return; }
  const term = new window.Terminal({
    fontFamily: '"SF Mono", Menlo, monospace',
    fontSize: Number(localStorage.getItem("termFontSize")) || 12.5,
    cursorBlink: true,
    allowTransparency: true,
    scrollback: 5000,
    theme: {
      background: "rgba(0,0,0,0)",
      foreground: "#e8eaf0",
      cursor: "#7daaff",
      selectionBackground: "rgba(125,170,255,0.35)",
    },
  });
  TERM = term;
  term.open(el);
  term.onData((d) => invoke("term_write", { data: d }).catch(() => {}));
  term.onResize(({ cols, rows }) => invoke("term_resize", { cols, rows }).catch(() => {}));
  term.attachCustomKeyEventHandler((e) => {
    if (!(e.metaKey && e.type === "keydown")) return true;
    if (e.key === "v") { navigator.clipboard?.readText().then((t) => t && term.paste(t)).catch(() => {}); return false; }
    if (e.key === "c") { const sel = term.getSelection(); if (sel) { navigator.clipboard?.writeText(sel).catch(() => {}); return false; } }
    if (e.key === "=" || e.key === "+") { setTermFont(term, Math.min(20, term.options.fontSize + 1)); return false; }
    if (e.key === "-") { setTermFont(term, Math.max(9, term.options.fontSize - 1)); return false; }
    if (e.key === "0") { setTermFont(term, 12.5); return false; }
    if (e.key.toLowerCase() === "w") { invoke("close_popover"); return false; }
    return true;
  });
  let pendingOut = "";
  let rafId = null;
  await listen("term-out", (e) => {
    pendingOut += e.payload;
    if (!rafId) {
      rafId = requestAnimationFrame(() => {
        rafId = null;
        if (pendingOut) { term.write(pendingOut); pendingOut = ""; }
      });
    }
  });
  await listen("term-exit", () => term.write("\r\n\x1b[90m" + t("term_exit_msg") + "\r\n"));
  try { await invoke("term_init"); } catch (e) { term.write(`\r\n\x1b[31m${e}\r\n`); }
  const fit = () => {
    const fs = term.options.fontSize;
    const rows = Math.max(8, Math.floor(el.clientHeight / (fs * 1.35)));
    const cols = Math.max(20, Math.floor(el.clientWidth / (fs * 0.585)));
    try { term.resize(cols, rows); } catch {}
  };
  setTimeout(() => { fit(); term.focus(); }, 60);
  window.addEventListener("resize", fit);
}

function setTermFont(term, size) {
  term.options.fontSize = size;
  localStorage.setItem("termFontSize", String(size));
  try { term.refresh(0, term.rows - 1); } catch {}
  const el = $("#term");
  if (el) {
    const rows = Math.max(8, Math.floor(el.clientHeight / (size * 1.35)));
    const cols = Math.max(20, Math.floor(el.clientWidth / (size * 0.585)));
    try { term.resize(cols, rows); } catch {}
  }
}

function popEmpty() { return `<h2>${t("ready")}</h2>`; }

/* --- 포모도로 --- */
function popPomodoro() {
  const p = S.pomodoro;
  const phaseTxt = p.phase === "break" ? t("brk") : t("focus");
  const cls = p.phase === "break" ? "break" : "focus";
  return `
    <h2>${IC.timer} ${t("pomodoro")}</h2>
    <div class="sec" style="text-align:center; padding: 16px 10px;">
      <div class="big-num ${cls}">${p.running ? fmtTime(p.remaining_secs) : fmtTime(p.focus_min * 60)}</div>
      <div class="stat-sub" style="margin-top:6px;">
        ${p.running ? (p.paused ? t("paused_txt") : "") + phaseTxt : t("waiting")} · ${t("round")} ${p.rounds_done}
      </div>
    </div>
    <div class="sec">
      <div class="row"><span class="rl">${t("focus")}</span><span class="rs">${p.focus_min} min</span></div>
      <div class="row"><span class="rl">${t("brk")}</span><span class="rs">${p.break_min} min</span></div>
    </div>
    <div class="sec">
      <div class="lbl">${t("presets")}</div>
      <div class="btns" style="margin-top:2px;">
        <button class="small" data-act="pom_preset" data-f="25" data-b="5">25/5</button>
        <button class="small" data-act="pom_preset" data-f="50" data-b="10">50/10</button>
        <button class="small" data-act="pom_preset" data-f="15" data-b="5">15/5</button>
      </div>
    </div>
    <div class="btns">
      ${p.running
        ? (p.paused
            ? `<button class="primary" data-act="pom_resume">${t("resume")}</button>`
            : `<button data-act="pom_pause">${t("pause")}</button>`)
        : `<button class="primary" data-act="pom_start">${t("start")}</button>`}
      <button data-act="pom_reset">${t("reset")}</button>
    </div>`;
}

/* --- 모니터 --- */
function popMonitor() {
  const s = S.stats;
  if (!s) return `<h2>${IC.monitor} ${t("monitor")}</h2><div class="empty">${t("collecting")}</div>`;
  const ramCls = s.ram_pct > 85 ? "crit" : s.ram_pct > 65 ? "warn" : "";
  return `
    <h2>${IC.monitor} ${t("monitor")}</h2>
    <div class="sec">
      <div class="lbl">${t("recent_cpu")}</div>
      <canvas id="cpu-spark" width="272" height="44"></canvas>
    </div>
    <div class="sec">
      <div class="lbl">${t("memory") ?? "메모리"} ${Math.round(s.ram_pct)}%</div>
      <div class="meter"><div class="${ramCls}" style="width:${Math.min(s.ram_pct, 100)}%"></div></div>
    </div>
    <div class="stat-grid" style="margin-bottom:10px;">
      <div class="sec"><div class="stat-val">${Math.round(s.cpu)}<span style="font-size:12px">%</span></div><div class="stat-sub">${t("cpu")}</div></div>
      <div class="sec"><div class="stat-val">${s.ram_used_gb}<span style="font-size:12px">G</span></div><div class="stat-sub">RAM / ${s.ram_total_gb}G</div></div>
      <div class="sec"><div class="stat-val">${s.battery_pct != null ? s.battery_pct + (s.battery_charging ? "⚡" : "%") : "-"}</div><div class="stat-sub">${t("battery")}</div></div>
    </div>
    <div class="sec">
      <div class="lbl">${t("network")} ↓${s.net_rx_kbs} · ↑${s.net_tx_kbs} KB/s</div>
      <div class="meter"><div style="width:${Math.min((s.net_rx_kbs + s.net_tx_kbs) / 10, 100)}%"></div></div>
      <div class="lbl" style="margin-top:10px;">${t("uptime")} ${esc(s.uptime)} · ${t("disk_free")} ${s.disk_free_gb}G</div>
    </div>`;
}

function drawSpark() {
  const c = $("#cpu-spark");
  if (!c) return;
  const ctx = c.getContext("2d");
  ctx.clearRect(0, 0, c.width, c.height);
  const hist = S.hist || [];
  if (hist.length < 2) return;
  const step = c.width / (hist.length - 1);
  ctx.beginPath();
  hist.forEach((v, i) => {
    const x = i * step;
    const y = c.height - 4 - (Math.min(v, 100) / 100) * (c.height - 8);
    i ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
  });
  ctx.strokeStyle = "#e8eaf0";
  ctx.lineWidth = 1.6;
  ctx.stroke();
  ctx.lineTo((hist.length - 1) * step, c.height);
  ctx.lineTo(0, c.height);
  ctx.closePath();
  ctx.fillStyle = "rgba(255, 255, 255, 0.10)";
  ctx.fill();
}

/* --- 토글 --- */
function popToggles() {
  const tg = S.toggles;
  if (!tg) return `<h2>${IC.toggles} ${t("toggles")}</h2><div class="empty">…</div>`;
  const sw = (key, label, sub, avail = true) => `
    <div class="row">
      <div><div class="rl">${label}</div><div class="rs">${sub}</div></div>
      <div class="switch ${tg[key] ? "on" : ""} ${avail ? "" : "unavail"}" data-act="tg_${key}" ${avail ? "" : "data-off"}></div>
    </div>`;
  return `
    <h2>${IC.toggles} ${t("toggles")}</h2>
    <div class="sec">
      ${sw("dark", t("dark"), t("appearance"))}
      ${sw("wifi", "Wi-Fi", tg.wifi_available ? t("network") : t("device_none"), tg.wifi_available)}
      ${sw("bt", "Bluetooth", tg.bt_available ? "blueutil" : "brew install blueutil", tg.bt_available)}
      ${sw("mute", t("mute"), t("system_volume"))}
      ${sw("caffeine", t("stay_awake"), t("keep_display"))}
    </div>
    <div class="stat-sub">${t("perm_note")}</div>`;
}

/* --- 에이전트 --- */
function popAgents() {
  const list = S.agents.length
    ? S.agents.map((a) => `
      <div class="list-item">
        <div class="li-icon">${AGENT_EMOJI[a.kind] || "🤖"}</div>
        <div class="li-main">
          <div class="li-title">${esc(a.name)}</div>
          <div class="li-sub">pid ${a.pid} · ${a.elapsed} · ${a.mem_mb}MB</div>
        </div>
        <div class="rs" style="font-variant-numeric:tabular-nums; color:${a.cpu > 30 ? "var(--amber)" : "var(--text-dim)"};">${a.cpu.toFixed(0)}%</div>
        <button class="small" data-act="ag_copy" data-pid="${a.pid}" title="kill 명령 복사">⧉</button>
      </div>`).join("")
    : `<div class="empty">${t("no_agents")}<br><span style="font-size:10.5px">${t("agents_hint")}</span></div>`;
  return `
    <h2>${IC.agents} ${t("agents_title")} <span class="rs" style="font-weight:400">· ${S.agents.length}${t("running_suffix")}</span></h2>
    ${list}
    <div class="stat-sub" style="margin-top:4px;">${t("agents_refresh")}</div>`;
}

/* --- 선반 --- */
function popShelf() {
  const totalMb = S.shelf.reduce((a, it) => a + it.size_mb, 0);
  const list = S.shelf.length
    ? S.shelf.map((it, i) => `
      <div class="list-item">
        <div class="li-icon">${it.is_dir ? "📁" : "📄"}</div>
        <div class="li-main">
          <div class="li-title">${esc(it.name)}</div>
          <div class="li-sub">${it.is_dir ? t("folder") : it.size_mb + "MB"} · ${esc(it.path.split("/").slice(0, -1).pop() || "")}</div>
        </div>
        <div class="acts">
          <button class="small" data-act="sh_open" data-i="${i}">${t("open")}</button>
          <button class="small" data-act="sh_copy" data-i="${i}">${t("copy")}</button>
          <button class="small" data-act="sh_move1" data-i="${i}">${t("move")}</button>
          <button class="small" data-act="sh_reveal" data-i="${i}">🔍</button>
          <button class="small danger" data-act="sh_rm" data-i="${i}">✕</button>
        </div>
      </div>`).join("")
    : `<div class="empty">${t("shelf_empty")}<br><span style="font-size:10.5px">${t("shelf_empty_sub")}</span></div>`;
  return `
    <h2>${IC.shelf} ${t("shelf_title")} <span class="rs" style="font-weight:400">· ${S.shelf.length}${t("items_suffix")} · ${t("total")} ${totalMb.toFixed(1)}MB</span></h2>
    ${list}
    ${S.shelf.length ? `
    <div class="btns">
      <button class="primary" data-act="sh_move_all">${t("move_all")}</button>
      <button data-act="sh_copy_all">${t("copy_all")}</button>
      <button class="danger" data-act="sh_clear">${t("clear")}</button>
    </div>` : ""}`;
}

/* --- 포매터 --- */
function popFormat() {
  return `
    <h2>${IC.format} ${t("formatter")}</h2>
    <div class="sec">
      <div class="row" style="padding:2px 0 6px;">
        <span class="rl">${t("input")}</span>
        <div class="seg" id="fmt-in">
          <div data-f="auto" class="on">${t("auto")}</div><div data-f="json">JSON</div><div data-f="yaml">YAML</div><div data-f="toml">TOML</div>
        </div>
      </div>
      <textarea id="fmt-src" rows="7" placeholder="${t("fmt_placeholder")}" spellcheck="false"></textarea>
      <div class="btns">
        <button class="primary" data-act="fmt_pretty">${t("pretty")}</button>
        <button data-act="fmt_min">${t("minify")}</button>
        <button data-act="fmt_paste">${t("paste")}</button>
        <button data-act="fmt_copy">${t("copy_out")}</button>
      </div>
    </div>
    <div class="sec">
      <div class="row" style="padding:2px 0 6px;">
        <span class="rl">${t("convert")}</span>
        <div class="seg" id="fmt-out">
          <div data-f="json">JSON</div><div data-f="yaml">YAML</div><div data-f="toml">TOML</div>
        </div>
      </div>
      <textarea id="fmt-out-ta" rows="7" placeholder="${t("result")}" spellcheck="false" readonly></textarea>
      <div id="fmt-msg" class="stat-sub" style="margin-top:6px;"></div>
    </div>`;
}

/* --- 피드 --- */
function popFeed() {
  const list = S.feed.length
    ? S.feed.map((f) => `
      <div class="list-item">
        <div class="li-icon">${esc(f.icon)}</div>
        <div class="li-main">
          <div class="li-title">${esc(f.title)}</div>
          <div class="li-sub">${esc(f.body)}</div>
        </div>
        <div class="rs">${new Date(f.ts).toLocaleTimeString((S.config?.lang === "en") ? "en-US" : "ko-KR", { hour: "2-digit", minute: "2-digit" })}</div>
      </div>`).join("")
    : `<div class="empty">${t("feed_empty")}</div>`;
  return `
    <h2>${IC.feed} ${t("notifications")}</h2>${list}
    ${S.feed.length ? `<div class="btns"><button class="danger" data-act="feed_clear">${t("feed_clear")}</button></div>` : ""}`;
}

/* --- 설정 --- */
function popSettings() {
  const c = S.config || { form: "orb", lang: "ko" };
  const seg = (act, val, txt, on) => `
    <div style="flex:1; text-align:center; font-size:11.5px; font-weight:600; padding:7px 4px; border-radius:7px;
      ${on ? "background:rgba(255,255,255,.14); color:#fff;" : "color:var(--text-dim)"}"
      data-act="${act}_${val}">${txt}</div>`;
  return `
    <h2>${IC.settings} ${t("settings")}</h2>
    <div class="sec">
      <div class="lbl">${t("language")}</div>
      <div class="seg" style="display:flex;">
        ${seg("lang", "ko", t("korean"), c.lang === "ko")}${seg("lang", "en", t("english"), c.lang === "en")}
      </div>
    </div>
    <div class="sec">
      <div class="lbl">${t("widgets_visible")}</div>
      ${ORB_ORDER.map((w) => {
        const hidden = new Set(S.config?.hidden_widgets || []).has(w);
        const locked = w === "settings";
        return `<div class="row" style="padding:5px 0;">
          <span class="rl">${t(w)}</span>
          <div class="switch ${!hidden ? "on" : ""} ${locked ? "unavail" : ""}" data-act="wv_${w}" ${locked ? 'title="' + t("always_on") + '"' : ""}></div>
        </div>`;
      }).join("")}
    </div>
    <div class="stat-sub">${t("auto_note")}</div>
    <div class="btns" style="margin-top:14px;">
      <button class="danger" data-act="quit">${t("quit")}</button>
    </div>
    <div class="stat-sub" style="margin-top:10px; text-align:center;">dock-util v${S.version || "?"}</div>`;
}

/* ---------- 팝오버 액션 ---------- */
function bindPopActions() {
  document.querySelectorAll("[data-act]").forEach((el) => {
    el.addEventListener("click", async () => {
      const act = el.dataset.act;
      const i = el.dataset.i != null ? Number(el.dataset.i) : undefined;
      try {
        switch (true) {
          case act === "pom_start": invoke("pom_start", {}); break;
          case act === "pom_preset": invoke("pom_start", { focus_min: Number(el.dataset.f), break_min: Number(el.dataset.b) }); break;
          case act === "pop_close": invoke("close_popover"); break;
          case act === "ag_copy": {
            try { await navigator.clipboard.writeText(`kill ${el.dataset.pid}`); toast(`kill ${el.dataset.pid} ${t("copied")}`); }
            catch { toast(t("copy_fail")); }
            break;
          }
          case act === "pom_pause": invoke("pom_pause"); break;
          case act === "pom_resume": invoke("pom_resume"); break;
          case act === "pom_reset": invoke("pom_reset"); break;
          case act.startsWith("tg_"): {
            const key = act.slice(3);
            if (el.dataset.off) return;
            const r = { dark: "toggle_dark", wifi: "toggle_wifi", bt: "toggle_bt", mute: "toggle_mute", caffeine: "toggle_caffeine" }[key];
            const on = await invoke(r);
            if (S.toggles) S.toggles[key] = on;
            renderPop();
            break;
          }
          case act === "sh_copy": toast(await invoke("shelf_copy", { idx: i }) + "개 복사됨"); break;
          case act === "sh_copy_all": toast(await invoke("shelf_copy", {}) + "개 복사됨"); break;
          case act === "sh_reveal": invoke("shelf_reveal", { idx: i }); break;
          case act === "sh_open": invoke("shelf_open", { idx: i }); break;
          case act === "feed_clear": invoke("clear_feed"); break;
          case act === "ai_cli": {
            await invoke("term_launch_cli", { program: el.dataset.p });
            await invoke("open_popover", { widget: "terminal", tileX: ORB_GEO.cx });
            break;
          }
          case act === "ai_app": {
            try {
              await invoke("open_gui_app", { name: el.dataset.n });
              toast(el.dataset.n === "claude" ? "Claude" : "ChatGPT");
            } catch (e) { toast(String(e)); }
            break;
          }
          case act === "term_new": invoke("term_reset").then(() => invoke("term_init").catch(() => {})); if (TERM) { TERM.reset(); } toast(t("new_shell_started")); break;
          case act === "term_font_up": if (TERM) setTermFont(TERM, Math.min(20, TERM.options.fontSize + 1)); break;
          case act === "term_font_down": if (TERM) setTermFont(TERM, Math.max(9, TERM.options.fontSize - 1)); break;
          case act === "sh_rm": invoke("shelf_remove", { idx: i }); break;
          case act === "sh_clear": invoke("shelf_clear"); break;
          case act === "sh_move1": { const [ok, fail] = await invoke("shelf_move_to", { idx: i }); toast(`${t("moved")} ${ok} · ${t("failed")} ${fail}`); break; }
          case act === "sh_move_all": { const [ok, fail] = await invoke("shelf_move_to", {}); toast(`${t("moved")} ${ok} · ${t("failed")} ${fail}`); break; }
          case act === "fmt_pretty": runFormat(true, null); break;
          case act === "fmt_min": runFormat(false, null); break;
          case act === "fmt_paste": {
            try { $("#fmt-src").value = await navigator.clipboard.readText(); }
            catch { toast(t("paste_fail")); }
            break;
          }
          case act === "fmt_copy": {
            try { await navigator.clipboard.writeText($("#fmt-out-ta").value); toast(t("copied")); }
            catch { toast(t("copy_fail")); }
            break;
          }
          case act.startsWith("form_"): await invoke("set_form", { form: act.slice(5) }); S.config = await invoke("get_config"); renderPop(); break;
          case act.startsWith("lang_"): await invoke("set_lang", { lang: act.slice(5) }); S.config = await invoke("get_config"); renderPop(); break;
          case act.startsWith("wv_"): {
            const w = act.slice(3);
            if (w === "settings") break;
            const hidden = new Set(S.config?.hidden_widgets || []);
            await invoke("set_widget_visible", { widget: w, visible: hidden.has(w) });
            S.config = await invoke("get_config");
            renderPop();
            break;
          }
          case act === "quit": invoke("quit_app"); break;
        }
      } catch (e) { toast(String(e)); }
    });
  });
  // 포맷 세그먼트
  const segIn = $("#fmt-in"), segOut = $("#fmt-out");
  if (segIn) {
    const pick = (seg, target) => seg.querySelectorAll("[data-f]").forEach((d) =>
      d.addEventListener("click", () => {
        seg.querySelectorAll("[data-f]").forEach((x) => x.classList.remove("on"));
        d.classList.add("on");
        if (target) target();
      }));
    pick(segIn, null);
    pick(segOut, () => {
      const from = segIn.querySelector(".on")?.dataset.f || "json";
      const to = segOut.querySelector(".on")?.dataset.f || "json";
      if (from !== to && $("#fmt-src").value.trim()) runFormat(true, to);
    });
  }
  // 선반 팝오버에도 드롭 허용
  if (popWidget === "shelf") {
    listen("tauri://drag-drop", (e) => {
      const paths = e.payload?.paths || [];
      if (paths.length) invoke("shelf_add", { paths });
    });
  }
}

function segF(sel) { return document.querySelector(`${sel} .on`)?.dataset.f || "json"; }

async function runFormat(pretty, convert) {
  const src = $("#fmt-src"), out = $("#fmt-out-ta"), msg = $("#fmt-msg");
  try {
    const res = await invoke("format_text", {
      text: src.value, format: segF("#fmt-in"), pretty, convert,
    });
    out.value = res;
    msg.textContent = convert ? `${segF("#fmt-in")} → ${convert} ${t("fmt_convert_done")}` : `${segF("#fmt-in")}${t("fmt_done")}`;
    msg.style.color = "var(--green)";
  } catch (e) {
    out.value = "";
    msg.textContent = String(e);
    msg.style.color = "var(--red)";
  }
}

function toast(text) {
  const d = document.createElement("div");
  d.textContent = text;
  d.style.cssText = "position:fixed;bottom:14px;left:50%;transform:translateX(-50%);background:rgba(30,32,40,.92);border:0.5px solid rgba(255,255,255,.18);color:#fff;font-size:12px;padding:8px 14px;border-radius:10px;z-index:99;box-shadow:0 6px 20px rgba(0,0,0,.4);";
  document.body.appendChild(d);
  setTimeout(() => d.remove(), 1800);
}

/* ---------- 부팅 ---------- */
async function boot() {
  await bindEvents();
  if (isOrb) {
    document.body.classList.add("orb-mode");
    $("#orb").classList.remove("hidden");
    buildOrb();
    S.config = await invoke("get_config");
    invoke("orb_ready");
  } else if (popWidget) {
    document.body.classList.add("pop-mode");
    $("#pop").classList.remove("hidden");
    // 팝오버 초기 데이터 로드
    if (popWidget === "pomodoro" || popWidget === "settings") S.config = await invoke("get_config");
    if (popWidget === "settings") { try { S.version = await T.app.getVersion(); } catch { S.version = "?"; } }
    if (popWidget === "ai_term" || popWidget === "ai_apps") {
      try { S.aiStatus = await invoke("ai_status"); } catch { S.aiStatus = {}; }
    }
    if (popWidget === "shelf") S.shelf = await invoke("shelf_list");
    if (popWidget === "feed") { S.feed = await invoke("get_feed"); S.unread = 0; }
    if (popWidget === "toggles") S.toggles = await invoke("get_toggles");
    renderPop();
    invoke("popover_ready");
    // Esc / ⌘W 로 닫기 (고정 팝오버 포함)
    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" || (e.metaKey && e.key.toLowerCase() === "w")) {
        e.preventDefault();
        invoke("close_popover");
      }
    });
  }
}
boot();
