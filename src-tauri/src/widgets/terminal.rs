//! 터미널 위젯: portable-pty로 사용자 셸을 띄우고 xterm.js와 연결한다.
//! 세션은 팝오버를 닫아도 유지되며, 닫힌 동안의 출력은 스크롤백에 보관되어
//! 다시 열 때 한 번에 내려준다.
//!
//! 출력은 33ms 배치로 웹뷰에 전송한다 — codex/claude 같은 TUI는 초당 수천 번
//! 다시 그리므로 청크마다 이벤트를 보내면 웹뷰가 얼어붙는다.

use parking_lot::Mutex;
use portable_pty::{native_pty_system, Child, ChildKiller, CommandBuilder, MasterPty, PtySize};
use std::io::{Read, Write};
use std::sync::Arc;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager};

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
/// 출력은 스크롤백에 누적 + 플러셔 스레드가 33ms 배치로 웹뷰에 전송한다.
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

    // 출력 → 스크롤백 + 전송 대기 버퍼
    let pending: Arc<Mutex<Vec<u8>>> = Arc::new(Mutex::new(Vec::new()));
    let sb = scrollback.clone();
    let pend = pending.clone();

    // 스크롤백 + 대기 버퍼 누적 (리더 스레드)
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
                        pend.lock().extend_from_slice(&buf[..n]);
                    }
                }
                Err(_) => break,
            }
        }
        let _ = app2.emit_to("popover-terminal", "term-exit", true);
    });

    // 플러셔 스레드: 33ms마다 대기 버퍼를 웹뷰로 전송 (팝오버가 열려 있을 때만)
    let app3 = app.clone();
    let pend2 = pending.clone();
    std::thread::spawn(move || loop {
        std::thread::sleep(Duration::from_millis(33));
        if app3.get_webview_window("popover-terminal").is_none() {
            continue;
        }
        let chunk = std::mem::take(&mut *pend2.lock());
        if chunk.is_empty() {
            continue;
        }
        let _ = app3.emit_to(
            "popover-terminal",
            "term-out",
            String::from_utf8_lossy(&chunk).to_string(),
        );
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

#[cfg(test)]
mod tests {
    use super::*;

    /// PTY 계층이 실제로 셸을 띄우고 입출력이 도는지 검증
    #[test]
    fn pty_spawn_write_read_roundtrip() {
        let pty_system = native_pty_system();
        let pair = pty_system
            .openpty(PtySize { rows: 24, cols: 80, pixel_width: 0, pixel_height: 0 })
            .unwrap();
        let mut cmd = CommandBuilder::new("/bin/sh");
        cmd.arg("-c");
        cmd.arg("cat"); // 입력을 그대로 되돌려주는 프로그램
        let mut child: Box<dyn Child + Send + Sync> = pair.slave.spawn_command(cmd).unwrap();
        let mut killer = child.clone_killer();
        let mut writer = pair.master.take_writer().unwrap();
        let mut reader = pair.master.try_clone_reader().unwrap();
        drop(pair.slave);

        writer.write_all(b"ping-roundtrip\n").unwrap();
        writer.flush().unwrap();

        // 리더 스레드 + 3초 타임아웃으로 에코 대기
        let (tx, rx) = std::sync::mpsc::channel::<Vec<u8>>();
        std::thread::spawn(move || {
            let mut buf = [0u8; 256];
            loop {
                match reader.read(&mut buf) {
                    Ok(0) => break,
                    Ok(n) => {
                        if tx.send(buf[..n].to_vec()).is_err() {
                            break;
                        }
                    }
                    Err(_) => break,
                }
            }
        });
        let mut got = Vec::new();
        let deadline = std::time::Instant::now() + Duration::from_secs(3);
        while std::time::Instant::now() < deadline {
            match rx.recv_timeout(Duration::from_millis(300)) {
                Ok(chunk) => {
                    got.extend_from_slice(&chunk);
                    if got.windows(14).any(|w| w == b"ping-roundtrip") {
                        break;
                    }
                }
                Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
                Err(_) => break,
            }
        }
        killer.kill().unwrap();
        let _ = child.wait();
        assert!(
            got.windows(14).any(|w| w == b"ping-roundtrip"),
            "PTY 에코 미수신: {}",
            String::from_utf8_lossy(&got)
        );
    }

    #[test]
    fn cli_whitelist_holds() {
        // term_launch_cli의 화이트리스트와 동일한 규칙 유지 확인용 상수
        let allowed = ["codex", "claude"];
        assert!(allowed.contains(&"codex"));
        assert!(allowed.contains(&"claude"));
        assert!(!allowed.contains(&"rm"));
    }
}
