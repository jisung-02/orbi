//! 실행중인 AI 에이전트 감지 (프로세스 스캔, 10초 주기)

use serde::Serialize;
use std::time::Duration;
use sysinfo::System;
use tauri::{AppHandle, Emitter};

const AGENT_KEYWORDS: &[&str] = &[
    "claude", "codex", "gemini", "copilot", "aider", "cline", "cursor-agent",
    "opencode", "goose", "amp", "droid", "crush", "windsurf", "qwen",
];

#[derive(Serialize, Clone)]
pub struct AgentInfo {
    pub pid: u32,
    pub name: String,
    pub kind: String,
    pub cpu: f32,
    pub mem_mb: u64,
    pub elapsed: String,
}

fn elapsed_str(start_unix: u64) -> String {
    let now = crate::platform::now_ms() as u64;
    let secs = now.saturating_sub(start_unix);
    let h = secs / 3600;
    let m = (secs % 3600) / 60;
    if h > 0 {
        format!("{h}시간 {m}분")
    } else {
        format!("{m}분")
    }
}

/// 프로세스 이름/커맨드라인에서 에이전트 종류를 식별
fn classify(name: &str, cmd: &str) -> Option<&'static str> {
    for kw in AGENT_KEYWORDS {
        if name == *kw || name.starts_with(&format!("{kw}-")) || name.starts_with(&format!("{kw}.")) {
            return Some(kw);
        }
        // npm/venv 래퍼: .../bin/claude, .../claude ... 식의 경로
        if cmd.contains(&format!("/bin/{kw}"))
            || cmd.contains(&format!("/{kw} "))
            || cmd.ends_with(&format!("/{kw}"))
        {
            return Some(kw);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detects_direct_process_names() {
        assert_eq!(classify("claude", ""), Some("claude"));
        assert_eq!(classify("codex", ""), Some("codex"));
        assert_eq!(classify("gemini-3.2", ""), Some("gemini"));
    }

    #[test]
    fn detects_node_wrappers_via_cmd_path() {
        assert_eq!(
            classify("node", "/usr/local/bin/codex exec --full-auto"),
            Some("codex")
        );
        assert_eq!(
            classify("node", "/opt/homebrew/bin/claude"),
            Some("claude")
        );
    }

    #[test]
    fn ignores_unrelated_processes() {
        assert_eq!(classify("Safari", "/Applications/Safari.app"), None);
        assert_eq!(classify("dock-util", "target/debug/dock-util"), None);
    }
}

fn scan_from(sys: &mut System) -> Vec<AgentInfo> {
    let mut found: Vec<AgentInfo> = Vec::new();
    for (pid, p) in sys.processes() {
        let name_raw = p.name().to_string_lossy();
        let name = name_raw.to_lowercase();
        // sysinfo 0.33: cmd()는 &[OsString]
        let cmd = p
            .cmd()
            .iter()
            .map(|s| s.to_string_lossy().to_lowercase())
            .collect::<Vec<_>>()
            .join(" ");
        if let Some(kind) = classify(&name, &cmd) {
            // 자기 자신/스캐너 제외
            if name.contains("dock-util") {
                continue;
            }
            found.push(AgentInfo {
                pid: pid.as_u32(),
                name: name_raw
                    .split(['-', '.', ' '])
                    .next()
                    .unwrap_or(name_raw.as_ref())
                    .to_string(),
                kind: kind.to_string(),
                cpu: p.cpu_usage(),
                mem_mb: p.memory() / 1024 / 1024,
                elapsed: elapsed_str(p.start_time()),
            });
        }
    }
    found.sort_by(|a, b| b.cpu.partial_cmp(&a.cpu).unwrap_or(std::cmp::Ordering::Equal));
    found.truncate(10);
    found
}

pub fn agents_loop(app: AppHandle) {
    // 모니터 위젯과 sysinfo 인스턴스를 공유하지 않는다 (락 경합 제거)
    let mut sys = System::new();
    let mut seen: std::collections::HashSet<u32> = std::collections::HashSet::new();
    loop {
        std::thread::sleep(Duration::from_secs(10));
        sys.refresh_processes(sysinfo::ProcessesToUpdate::All, true);
        let list = scan_from(&mut sys);
        let pids: std::collections::HashSet<u32> = list.iter().map(|a| a.pid).collect();
        let new_pids: Vec<u32> = pids.difference(&seen).copied().collect();
        for a in list.iter().filter(|a| new_pids.contains(&a.pid)) {
            crate::widgets::feed::push(
                &app,
                "🤖",
                "에이전트 시작",
                &format!("{} (pid {})", a.name, a.pid),
            );
        }
        let exited_count = seen.difference(&pids).count();
        seen = pids;
        let _ = app.emit("agents", &serde_json::json!({ "list": list, "exited": exited_count }));
    }
}
