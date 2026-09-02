//! Tauri IPC 커맨드

use crate::app::AppState;

use tauri::{AppHandle, Emitter, Manager, WebviewUrl, WebviewWindowBuilder};
use tauri::utils::config::WindowEffectsConfig;
use crate::tr;
use tauri::window::{Effect, EffectState};

#[tauri::command]
pub fn quit_app(app: AppHandle) {
    app.exit(0);
}

/// 오브 윈도우 준비: 입력 차단 상태로 시작하고, 오브 형태일 때만 표시
#[tauri::command]
pub fn orb_ready(_app: AppHandle, _st: tauri::State<AppState>) {
    eprintln!("[orb_ready] orb JS 로드됨");
}

/// 오브를 접고 입력을 다시 차단 (동그라미 클릭 시 팝오버가 팬에 가려지지 않도록)
#[tauri::command]
pub fn orb_collapse(app: AppHandle, st: tauri::State<AppState>) {
    *st.orb_expanded.lock() = false;
    let _ = app.emit_to("orb", "orb-toggle", false);
    let panel = crate::panel::get(&st.orb_panel);
    if panel != 0 {
        crate::panel::set_ignores_on_main(panel, true);
    }
}

/// 형태 전환 (오브 ↔ 바)
#[tauri::command]
pub fn get_config(st: tauri::State<AppState>) -> crate::config::Config {
    st.cfg.lock().clone()
}

#[tauri::command]
pub fn set_widget_visible(
    app: AppHandle,
    st: tauri::State<AppState>,
    widget: String,
    visible: bool,
) -> Result<Vec<String>, String> {
    use tauri::Emitter;
    {
        let mut cfg = st.cfg.lock();
        cfg.hidden_widgets.retain(|w| w != &widget);
        if !visible && widget != "settings" {
            cfg.hidden_widgets.push(widget.clone());
        }
        cfg.save();
    }
    let hidden = st.cfg.lock().hidden_widgets.clone();
    let _ = app.emit_to("orb", "visibility-changed", ());
    Ok(hidden)
}

#[tauri::command]
pub fn set_lang(app: AppHandle, st: tauri::State<AppState>, lang: String) -> Result<String, String> {
    if lang != "ko" && lang != "en" {
        return Err("지원하지 않는 언어".into());
    }
    {
        let mut cfg = st.cfg.lock();
        cfg.lang = lang.clone();
        cfg.save();
    }
    use tauri::Emitter;
    let _ = app.emit("lang-changed", &lang);
    Ok(lang)
}

// ---- 포모도로 ----

#[tauri::command]
pub fn pom_snapshot(st: tauri::State<AppState>) -> crate::widgets::pomodoro::PomodoroSnapshot {
    st.pomodoro.lock().snapshot()
}

#[tauri::command]
pub fn pom_start(app: AppHandle, st: tauri::State<AppState>, focus_min: Option<u64>, break_min: Option<u64>) {
    {
        let mut p = st.pomodoro.lock();
        let f = focus_min.unwrap_or(p.focus_secs / 60).max(1);
        let b = break_min.unwrap_or(p.break_secs / 60).max(1);
        p.focus_secs = f * 60;
        p.break_secs = b * 60;
        p.phase = crate::widgets::pomodoro::Phase::Focus;
        p.running = true;
        p.paused = false;
        p.remaining = None;
        use std::time::{Duration, Instant};
        p.ends_at = Some(Instant::now() + Duration::from_secs(p.focus_secs));
        let mut cfg = st.cfg.lock();
        cfg.focus_min = f;
        cfg.break_min = b;
        cfg.save();
    }
    crate::widgets::pomodoro::persist(&app);
    let snap = st.pomodoro.lock().snapshot();
    let _ = app.emit("pomodoro", &snap);
}

#[tauri::command]
pub fn pom_pause(app: AppHandle, st: tauri::State<AppState>) {
    {
        let mut p = st.pomodoro.lock();
        if p.running && !p.paused {
            if let Some(e) = p.ends_at {
                p.remaining =
                    Some(e.saturating_duration_since(std::time::Instant::now()));
            }
            p.paused = true;
        }
    }
    crate::widgets::pomodoro::persist(&app);
    let snap = st.pomodoro.lock().snapshot();
    let _ = app.emit("pomodoro", &snap);
}

#[tauri::command]
pub fn pom_resume(app: AppHandle, st: tauri::State<AppState>) {
    {
        let mut p = st.pomodoro.lock();
        if p.running && p.paused {
            use std::time::{Duration, Instant};
            p.ends_at = Some(Instant::now() + p.remaining.unwrap_or(Duration::from_secs(60)));
            p.paused = false;
        }
    }
    crate::widgets::pomodoro::persist(&app);
    let snap = st.pomodoro.lock().snapshot();
    let _ = app.emit("pomodoro", &snap);
}

#[tauri::command]
pub fn pom_reset(app: AppHandle, st: tauri::State<AppState>) {
    {
        let mut p = st.pomodoro.lock();
        p.phase = crate::widgets::pomodoro::Phase::Idle;
        p.running = false;
        p.paused = false;
        p.ends_at = None;
        p.remaining = None;
        p.rounds_done = 0;
    }
    crate::widgets::pomodoro::persist(&app);
    let snap = st.pomodoro.lock().snapshot();
    let _ = app.emit("pomodoro", &snap);
}

// ---- 토글 ----

#[tauri::command]
pub fn get_toggles(st: tauri::State<AppState>) -> crate::widgets::toggles::TogglesState {
    crate::widgets::toggles::get_state(&st.wifi_dev.lock().clone(), st.caffeinate.lock().is_some())
}

#[tauri::command]
pub fn toggle_dark(app: AppHandle, st: tauri::State<AppState>) -> Result<bool, String> {
    let state = crate::widgets::toggles::get_state(&st.wifi_dev.lock().clone(), false);
    let next = !state.dark;
    let lang = st.cfg.lock().lang.clone();
    crate::widgets::toggles::dark_mode_set(next)?;
    let state_txt = if next { crate::i18n::tr(&lang, "켬", "On") } else { crate::i18n::tr(&lang, "끔", "Off") };
    crate::widgets::feed::push(&app, "🌗", &crate::i18n::tr(&lang, "다크모드", "Dark mode"), &state_txt);
    Ok(next)
}

#[tauri::command]
pub fn toggle_wifi(app: AppHandle, st: tauri::State<AppState>) -> Result<bool, String> {
    let dev = st.wifi_dev.lock().clone();
    let next = !crate::widgets::toggles::wifi_state(&dev).unwrap_or(false);
    let lang = st.cfg.lock().lang.clone();
    crate::widgets::toggles::wifi_set(&dev, next)?;
    let state_txt = if next { crate::i18n::tr(&lang, "켬", "On") } else { crate::i18n::tr(&lang, "끔", "Off") };
    crate::widgets::feed::push(&app, "📶", "Wi-Fi", &state_txt);
    Ok(next)
}

#[tauri::command]
pub fn toggle_bt(app: AppHandle, st: tauri::State<AppState>) -> Result<bool, String> {
    let cur = crate::platform::shell_output("blueutil", &["-p"]).map(|v| v == "1").unwrap_or(false);
    let next = !cur;
    let lang = st.cfg.lock().lang.clone();
    crate::widgets::toggles::bt_set(next)?;
    let state_txt = if next { crate::i18n::tr(&lang, "켬", "On") } else { crate::i18n::tr(&lang, "끔", "Off") };
    crate::widgets::feed::push(&app, "🅑", "Bluetooth", &state_txt);
    Ok(next)
}

#[tauri::command]
pub fn toggle_mute(app: AppHandle, st: tauri::State<AppState>) -> Result<bool, String> {
    let cur = crate::platform::osascript("output muted of (get volume settings)")
        .map(|v| v == "true")
        .unwrap_or(false);
    let next = !cur;
    let lang = st.cfg.lock().lang.clone();
    crate::widgets::toggles::mute_set(next)?;
    let state_txt = if next { crate::i18n::tr(&lang, "켬", "On") } else { crate::i18n::tr(&lang, "끔", "Off") };
    crate::widgets::feed::push(&app, "🔇", &crate::i18n::tr(&lang, "음소거", "Mute"), &state_txt);
    Ok(next)
}

#[tauri::command]
pub fn toggle_caffeine(app: AppHandle, st: tauri::State<AppState>) -> Result<bool, String> {
    let on;
    {
        let mut guard = st.caffeinate.lock();
        if let Some(c) = guard.as_mut() {
            let _ = c.kill();
            let _ = c.wait();
            *guard = None;
            on = false;
        } else {
            let child = std::process::Command::new("caffeinate")
                .arg("-d")
                .spawn()
                .map_err(|e| format!("caffeinate 실행 실패: {e}"))?;
            *guard = Some(child);
            on = true;
        }
    }
    let lang = st.cfg.lock().lang.clone();
    let state_txt = if on { crate::i18n::tr(&lang, "켬 (디스플레이 유지)", "On (keep display awake)") } else { crate::i18n::tr(&lang, "끔", "Off") };
    crate::widgets::feed::push(&app, "☕", &crate::i18n::tr(&lang, "잠금방지", "Stay awake"), &state_txt);
    Ok(on)
}

// ---- 선반 ----

#[tauri::command]
pub fn shelf_add(app: AppHandle, st: tauri::State<AppState>, paths: Vec<String>) -> Vec<crate::widgets::shelf::ShelfItem> {
    let before = st.shelf.lock().len();
    let list = crate::widgets::shelf::add(&st, &paths);
    let lang = st.cfg.lock().lang.clone();
    let added = list.len().saturating_sub(before);
    if added > 0 {
        crate::widgets::feed::push(
            &app,
            "📥",
            &crate::i18n::tr(&lang, "선반 추가", "Shelf added"),
            &crate::i18n::fmt(&lang, "새 항목 {}개 · 총 {}개", "Added {} · total {}", &[&added, &list.len()]),
        );
    }
    let _ = app.emit("shelf", &list);
    list
}

#[tauri::command]
pub fn shelf_list(st: tauri::State<AppState>) -> Vec<crate::widgets::shelf::ShelfItem> {
    crate::widgets::shelf::list(&st)
}

#[tauri::command]
pub fn shelf_remove(app: AppHandle, st: tauri::State<AppState>, idx: usize) -> Vec<crate::widgets::shelf::ShelfItem> {
    let list = crate::widgets::shelf::remove(&st, idx);
    let _ = app.emit("shelf", &list);
    list
}

#[tauri::command]
pub fn shelf_clear(app: AppHandle, st: tauri::State<AppState>) -> Vec<crate::widgets::shelf::ShelfItem> {
    let list = crate::widgets::shelf::clear(&st);
    let _ = app.emit("shelf", &list);
    list
}

#[tauri::command]
pub fn shelf_copy(st: tauri::State<AppState>, idx: Option<usize>) -> Result<usize, String> {
    crate::widgets::shelf::copy_refs(&st, idx)
}

#[tauri::command]
pub fn shelf_reveal(st: tauri::State<AppState>, idx: usize) -> Result<(), String> {
    crate::widgets::shelf::reveal(&st, idx)
}

#[tauri::command]
pub fn shelf_open(st: tauri::State<AppState>, idx: usize) -> Result<(), String> {
    crate::widgets::shelf::open(&st, idx)
}

#[tauri::command]
pub fn shelf_open_path(path: String) -> Result<(), String> {
    let expanded = if path.starts_with('~') {
        if let Some(home) = dirs::home_dir() {
            home.join(&path[2..]).to_string_lossy().to_string()
        } else {
            path.clone()
        }
    } else {
        path.clone()
    };
    let dir = std::path::Path::new(&expanded);
    if dir.exists() {
        std::process::Command::new("open").arg(dir).spawn().map(|_| ()).map_err(|e| e.to_string())
    } else {
        Err(format!("경로가 존재하지 않습니다: {}", expanded))
    }
}

#[tauri::command]
pub fn shelf_move_to(app: AppHandle, st: tauri::State<AppState>, idx: Option<usize>) -> Result<(usize, usize), String> {
    let lang = st.cfg.lock().lang.clone();
    let (ok, fail) = crate::widgets::shelf::move_to(&app, &st, idx)?;
    crate::widgets::feed::push(&app, "📦", &crate::i18n::tr(&lang, "파일 이동", "Files moved"), &crate::i18n::fmt(&lang, "성공 {} · 실패 {}", "ok {} · failed {}", &[&ok, &fail]));
    let _ = app.emit("shelf", crate::widgets::shelf::list(&st));
    Ok((ok, fail))
}

// ---- 포매터 ----

#[tauri::command]
pub fn format_text(text: String, format: String, pretty: bool, convert: Option<String>) -> Result<String, String> {
    crate::widgets::formatter::format_text(&text, &format, pretty, convert.as_deref())
}

// ---- 피드 ----

#[tauri::command]
pub fn get_feed(st: tauri::State<AppState>) -> Vec<crate::widgets::feed::FeedItem> {
    crate::widgets::feed::list(&st)
}

#[tauri::command]
pub fn clear_feed(app: AppHandle, st: tauri::State<AppState>) -> Vec<crate::widgets::feed::FeedItem> {
    st.feed.lock().clear();
    let _ = app.emit("feed", Vec::<crate::widgets::feed::FeedItem>::new());
    Vec::new()
}

// ---- 터미널 ----

#[tauri::command]
pub fn term_init(
    app: AppHandle,
    st: tauri::State<AppState>,
    window: tauri::WebviewWindow,
) -> Result<(), String> {
    crate::widgets::terminal::ensure(&app, &st)?;
    // 팝오버가 닫혀 있던 동안의 출력을 한 번에 내려준다
    if let Some(buf) = crate::widgets::terminal::scrollback(&st) {
        if !buf.is_empty() {
            let _ = app.emit_to(window.label(), "term-out", buf);
        }
    }
    Ok(())
}

#[tauri::command]
pub fn term_write(st: tauri::State<AppState>, data: String) -> Result<(), String> {
    crate::widgets::terminal::write(&st, &data)
}

#[tauri::command]
pub fn term_resize(st: tauri::State<AppState>, cols: u16, rows: u16) {
    crate::widgets::terminal::resize(&st, cols, rows);
}

#[tauri::command]
pub fn term_reset(st: tauri::State<AppState>) {
    crate::widgets::terminal::reset(&st);
}

// ---- AI 런처 ----

/// AI CLI/앱 설치 상태
#[tauri::command]
pub fn ai_status() -> serde_json::Value {
    let cli = |n: &str| crate::platform::command_exists(n);
    let app = |n: &str| std::path::Path::new("/Applications").join(n).exists();
    serde_json::json!({
        "codex": cli("codex"),
        "claude": cli("claude"),
        "claude_app": app("Claude.app"),
        "chatgpt_app": app("ChatGPT.app"),
    })
}

/// 내장 터미널 세션에서 AI CLI 실행 (세션 유지되어 닫아도 계속 돌아감)
#[tauri::command]
pub fn term_launch_cli(app: AppHandle, st: tauri::State<AppState>, program: String) -> Result<(), String> {
    if program != "codex" && program != "claude" {
        return Err("허용되지 않은 프로그램".into());
    }
    crate::widgets::terminal::ensure(&app, &st)?;
    crate::widgets::terminal::write(&st, &format!("{program}\r"))
}

/// GUI 앱 실행 (화이트리스트)
#[tauri::command]
pub fn open_gui_app(app: AppHandle, st: tauri::State<AppState>, name: String) -> Result<(), String> {
    let app_name = match name.as_str() {
        "claude" => "Claude",
        "codex" | "chatgpt" => "ChatGPT",
        _ => return Err("허용되지 않은 앱".into()),
    };
    std::process::Command::new("open")
        .arg("-a")
        .arg(app_name)
        .spawn()
        .map(|_| ())
        .map_err(|e| e.to_string())?;
    let lang = st.cfg.lock().lang.clone();
    let body = match app_name {
        "Claude" => tr!(lang, "Claude 앱을 실행합니다", "Launching Claude app"),
        _ => tr!(lang, "ChatGPT 앱을 실행합니다", "Launching ChatGPT app"),
    };
    crate::widgets::feed::push(&app, "🚀", &tr!(lang, "앱 실행", "App launched"), &body);
    Ok(())
}

// ---- 팝오버 ----

fn popover_size(widget: &str) -> (f64, f64) {
    match widget {
        "pomodoro" => (300.0, 330.0),
        "monitor" => (320.0, 360.0),
        "toggles" => (270.0, 360.0),
        "agents" => (350.0, 400.0),
        "shelf" => (400.0, 440.0),
        "format" => (440.0, 480.0),
        "feed" => (360.0, 440.0),
        "terminal" => (680.0, 440.0),
        "settings" => (300.0, 300.0),
        _ => (320.0, 360.0),
    }
}

/// 포커스를 잃어도 닫히지 않는 위젯 (파일 드래그 등 다른 앱 조작 중에 열어둠)
fn is_pinned(widget: &str) -> bool {
    matches!(widget, "shelf" | "terminal" | "format")
}

/// 열려 있는 팝오버 닫기 (커맨드와 전역 단축키가 공용)
pub fn close_current_popover(app: &AppHandle) {
    use tauri::Manager;
    if let Some(st) = app.try_state::<AppState>() {
        let label = st.popover.lock().clone();
        if let Some(label) = label {
            if let Some(w) = app.get_webview_window(&label) {
                let _ = w.close();
            }
            *st.popover.lock() = None;
        }
    }
}

#[tauri::command]
pub fn close_popover(app: AppHandle) {
    close_current_popover(&app);
}

/// 오브 확장 상태 + 숨김 목록 (JS 폴링용 — 이벤트 전달 불안정 문제 우회)
#[tauri::command]
pub fn orb_state(st: tauri::State<AppState>) -> serde_json::Value {
    serde_json::json!({
        "expanded": *st.orb_expanded.lock(),
        "hidden": *st.cfg.lock().hidden_widgets,
    })
}

#[tauri::command]
pub fn open_popover(
    app: AppHandle,
    st: tauri::State<AppState>,
    widget: String,
    tile_x: f64,
) -> Result<(), String> {
    // 이미 열려 있으면 닫기(토글). 창이 완전히 닫힌 뒤 새 창을 만든다 (경합 방지)
    if let Some(prev) = st.popover.lock().clone() {
        let was_same = prev == format!("popover-{widget}");
        if let Some(w) = app.get_webview_window(&prev) {
            let _ = w.close();
        }
        for _ in 0..50 {
            if app.get_webview_window(&prev).is_none() {
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        *st.popover.lock() = None;
        if was_same {
            return Ok(());
        }
    }

    let label = format!("popover-{widget}");
    let (w, h) = popover_size(&widget);
    let win = WebviewWindowBuilder::new(
        &app,
        &label,
        WebviewUrl::App("index.html".into()),
    )
    .title(widget.clone())
    .inner_size(w, h)
    .decorations(false)
    .transparent(true)
    .shadow(false)
    .resizable(widget == "terminal")
    .min_inner_size(420.0, 260.0)
    .skip_taskbar(true)
    .focused(true)
    .effects(WindowEffectsConfig {
        effects: vec![Effect::HudWindow],
        state: Some(EffectState::FollowsWindowActiveState),
        radius: Some(18.0),
        color: None,
    })
    .build()
    .map_err(|e| format!("팝오버 생성 실패: {e}"))?;

    if let Ok(ptr) = win.ns_window() {
        let orb = *st.orb_rect.lock();
        // 오브가 있는 화면 기준으로 클램프 (멀티 디스플레이)
        let (scr_x, scr_w) = orb
            .and_then(|b| crate::platform::screen_frame_at(b.x + b.w / 2.0, b.y + b.h / 2.0))
            .map(|scr| (scr.x, scr.w))
            .unwrap_or_else(|| {
                let (sw, _) = crate::platform::main_screen();
                (0.0, sw)
            });
        // 동그라미 중앙(tile_x) 위에 팝오버 중앙이 오도록 배치 + 화면 경계 클램프
        let x = match orb {
            Some(b) => {
                let center = b.x + tile_x;
                (center - w / 2.0).clamp(scr_x + 8.0, scr_x + scr_w - w - 8.0)
            }
            None => (scr_x + scr_w - w) / 2.0,
        };
        // 오브 원 위쪽에 팝오버가 붙는다
        let y = orb.map(|b| b.y + 96.0).unwrap_or(90.0);
        let rect = crate::platform::RectF { x, y, w, h };
        let ptr = crate::platform::win::NsWinPtr(ptr);
        crate::platform::on_main_async(move || unsafe {
            let p = ptr;
            crate::platform::win::set_level(p.0, 1001); // 오브(1000)보다 위, 전체화면 위
            crate::platform::win::pin_all_spaces(p.0);
            crate::platform::win::set_frame(p.0, rect);
        });
    }

    // 포커스 잃으면 자동 닫기 (고정 위젯 제외 — ✕ 버튼으로 닫음)
    let label2 = label.clone();
    let app2 = app.clone();
    let pinned = is_pinned(&widget);
    win.on_window_event(move |e| {
        if let tauri::WindowEvent::Focused(false) = e {
            if pinned {
                return;
            }
            if let Some(w) = app2.get_webview_window(&label2) {
                let _ = w.close();
            }
            if let Some(st) = app2.try_state::<AppState>() {
                // 새 팝오버가 이미 열려 있으면 상태를 지우지 않는다
                let mut cur = st.popover.lock();
                if cur.as_deref() == Some(&label2) {
                    *cur = None;
                }
            }
        }
    });

    *st.popover.lock() = Some(label);
    Ok(())
}

#[tauri::command]
pub fn popover_ready(app: AppHandle) {
    // 팝오버 윈도우도 모든 스페이스에 표시
    if let Some(st) = app.try_state::<AppState>() {
        let label = st.popover.lock().clone();
        if let Some(label) = label {
            if let Some(win) = app.get_webview_window(&label) {
                if let Ok(ptr) = win.ns_window() {
                    let ptr = crate::platform::win::NsWinPtr(ptr);
                    crate::platform::on_main_async(move || unsafe {
                        let p = ptr;
                        crate::platform::win::pin_all_spaces(p.0);
                    });
                }
            }
        }
    }
}
