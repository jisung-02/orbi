//! 터미널 위젯: portable-pty로 사용자 셸을 띄우고 xterm.js와 연결한다.
//! 세션은 팝오버를 닫아도 유지되며, 닫힌 동안의 출력은 스크롤백에 보관되어
//! 다시 열 때 한 번에 내려준다.
//!
//! 출력 처리 파이프라인:
//!   PTY reader → scrollback(128KB cap) + pending(64KB cap)
//!   → 플러셔(50ms) → 팝오버 열려있으면 emit_to (최대 32KB/전송)
//!   → JS: 수신 버퍼링 후 rAF에서 xterm.write

use parking_lot::Mutex;
use portable_pty::{native_pty_system, Child, ChildKiller, CommandBuilder, MasterPty, PtySize};
use std::io::{Read, Write};
use std::sync::Arc;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager};

const SCROLLBACK_CAP: usize = 128 * 1024;
const PENDING_CAP: usize = 64 * 1024;
const FLUSH_INTERVAL_MS: u64 = 50;
const MAX_CHUNK: usize = 32 * 1024;

pub struct TermSession {
    pub master: Box<dyn MasterPty + Send>,
    pub writer: Box<dyn Write + Send>,
    pub child: Box<dyn Child + Send + Sync>,
    pub killer: Box<dyn ChildKiller + Send + Sync>,
    pub scrollback: Arc<Mutex<Vec<u8>>>,
}

pub type SharedSession = Mutex<Option<TermSession>>;

/// 세션이 없으면 새로 띄운다. 셸은 $SHELL(기본 zsh) 로그인 셸, 홈 디렉터리에서 시작.
pub fn ensure(app: &AppHandle, st: &crate::app::AppState) -> Result<(), String> {
    let mut guard = st.term.lock();
    if guard.is_some() {
        return Ok(());
    }
    let pty_system = native_pty_system();
    let pair = pty_system
        .openpty(PtySize { rows: 28, cols: 96, pixel_width: 0, pixel_height: 0 })
        .map_err(|e| format!("PTY 생성 실패: {e}"))?;

    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".into());
    let mut cmd = CommandBuilder::new(shell);
    cmd.arg("-l");
    if let Some(home) = dirs::home_dir() {
        cmd.cwd(home);
    }
    let child: Box<dyn Child + Send + Sync> = pair
        .slave
        .spawn_command(cmd)
        .map_err(|e| format!("셸 실행 실패: {e}"))?;
    let killer = child.clone_killer();
    let writer = pair.master.take_writer().map_err(|e| e.to_string())?;
    let reader = pair.master.try_clone_reader().map_err(|e| e.to_string())?;
    let master = pair.master;
    let scrollback = Arc::new(Mutex::new(Vec::new()));

    let pending: Arc<Mutex<Vec<u8>>> = Arc::new(Mutex::new(Vec::new()));
    let sb = scrollback.clone();
    let pend = pending.clone();

    // 리더: PTY 출력 → 스크롤백 + 대기 버퍼 (둘 다 cap 적용)
    let app2 = app.clone();
    std::thread::spawn(move || {
        let mut reader = reader;
        let mut buf = [0u8; 4096];
        loop {
            match reader.read(&mut buf) {
                Ok(0) => break,
                Ok(n) => {
                    {
                        let mut sb = sb.lock();
                        sb.extend_from_slice(&buf[..n]);
                        let overflow = sb.len().saturating_sub(SCROLLBACK_CAP);
                        if overflow > 0 {
                            sb.drain(..overflow);
                        }
                    }
                    {
                        let mut pd = pend.lock();
                        pd.extend_from_slice(&buf[..n]);
                        let overflow = pd.len().saturating_sub(PENDING_CAP);
                        if overflow > 0 {
                            pd.drain(..overflow);
                        }
                    }
                }
                Err(_) => break,
            }
        }
        let _ = app2.emit_to("popover-terminal", "term-exit", true);
    });

    // 플러셔: 50ms마다 대기 버퍼를 웹뷰로 전송 (팝오버 열려 있을 때만)
    let app3 = app.clone();
    let pend2 = pending.clone();
    std::thread::spawn(move || loop {
        std::thread::sleep(Duration::from_millis(FLUSH_INTERVAL_MS));
        if app3.get_webview_window("popover-terminal").is_none() {
            // 팝오버 닫힘: pending 비워서 무한 증가 방지
            pend2.lock().clear();
            continue;
        }
        let mut chunk = std::mem::take(&mut *pend2.lock());
        if chunk.is_empty() {
            continue;
        }
        // 전송 크기 제한 (너무 크면 잘라서 뒤쪽 우선)
        if chunk.len() > MAX_CHUNK {
            let skip = chunk.len() - MAX_CHUNK;
            chunk.drain(..skip);
        }
        let text = String::from_utf8_lossy(&chunk).to_string();
        let _ = app3.emit_to("popover-terminal", "term-out", text);
    });

    *guard = Some(TermSession { master, writer, child, killer, scrollback });
    Ok(())
}

/// 보관된 스크롤백 스냅샷 (세션은 유지)
pub fn scrollback(st: &crate::app::AppState) -> Option<String> {
    st.term
        .lock()
        .as_ref()
        .map(|s| String::from_utf8_lossy(&s.scrollback.lock()).to_string())
}

pub fn write(st: &crate::app::AppState, data: &str) -> Result<(), String> {
    let mut guard = st.term.lock();
    let Some(session) = guard.as_mut() else {
        return Err("터미널 세션이 없습니다".into());
    };
    session
        .writer
        .write_all(data.as_bytes())
        .map_err(|e| e.to_string())
}

pub fn resize(st: &crate::app::AppState, cols: u16, rows: u16) {
    if let Some(session) = st.term.lock().as_ref() {
        let _ = session.master.resize(PtySize {
            rows: rows.max(4),
            cols: cols.max(10),
            pixel_width: 0,
            pixel_height: 0,
        });
    }
}

/// 셸을 종료하고 세션을 정리 (wait 스레드로 좀비 방지)
pub fn reset(st: &crate::app::AppState) {
    if let Some(mut session) = st.term.lock().take() {
        let _ = session.killer.kill();
        std::thread::spawn(move || {
            let _ = session.child.wait();
        });
    }
}
