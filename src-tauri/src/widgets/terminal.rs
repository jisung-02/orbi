//! 터미널 위젯: portable-pty로 사용자 셸을 띄우고 xterm.js와 연결한다.
//! 세션은 팝오버를 닫아도 유지되며, 닫힌 동안의 출력은 스크롤백에 보관되어
//! 다시 열 때 한 번에 내려준다.

use parking_lot::Mutex;
use portable_pty::{native_pty_system, Child, ChildKiller, CommandBuilder, MasterPty, PtySize};
use std::io::{Read, Write};
use std::sync::Arc;
use tauri::{AppHandle, Emitter};

const SCROLLBACK_CAP: usize = 128 * 1024;

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

    // 출력 → 스크롤백 보관 + 웹뷰 스트리밍
    let app2 = app.clone();
    let sb = scrollback.clone();
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
                    let chunk = String::from_utf8_lossy(&buf[..n]).to_string();
                    let _ = app2.emit_to("popover-terminal", "term-out", chunk);
                }
                Err(_) => break,
            }
        }
        let _ = app2.emit_to("popover-terminal", "term-exit", true);
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
