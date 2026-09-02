//! 포모도로/타이머: 1초 틱 루프 + 페이즈 자동 전환 + 상태 영속화

use serde::{Deserialize, Serialize};
use std::time::{Duration, Instant};
use tauri::{AppHandle, Emitter};

#[derive(Clone, Copy, Serialize, Deserialize, PartialEq, Debug)]
#[serde(rename_all = "snake_case")]
pub enum Phase {
    Idle,
    Focus,
    Break,
}

#[derive(Serialize, Clone)]
pub struct PomodoroSnapshot {
    pub phase: Phase,
    pub running: bool,
    pub paused: bool,
    pub remaining_secs: u64,
    pub focus_min: u64,
    pub break_min: u64,
    pub rounds_done: u32,
}

/// 설정 파일에 저장되는 실행 상태 (재시작 복원용)
#[derive(Serialize, Deserialize, Clone, Debug, Default)]
pub struct PersistedPom {
    pub phase: Phase,
    pub running: bool,
    pub paused: bool,
    pub ends_at_ms: Option<i64>,
    pub remaining_ms: Option<u64>,
    pub focus_secs: u64,
    pub break_secs: u64,
    pub rounds_done: u32,
}

#[derive(Default)]
pub struct PomodoroState {
    pub phase: Phase,
    pub running: bool,
    pub paused: bool,
    pub ends_at: Option<Instant>,
    /// 일시정지 시 남은 시간
    pub remaining: Option<Duration>,
    pub focus_secs: u64,
    pub break_secs: u64,
    pub rounds_done: u32,
}

impl Default for Phase {
    fn default() -> Self {
        Phase::Idle
    }
}

impl PomodoroState {
    pub fn snapshot(&self) -> PomodoroSnapshot {
        let remaining_secs = if self.running && !self.paused {
            self.ends_at
                .map(|e| e.saturating_duration_since(Instant::now()).as_secs())
                .unwrap_or(0)
        } else {
            self.remaining.map(|r| r.as_secs()).unwrap_or(
                match self.phase {
                    Phase::Focus => self.focus_secs,
                    Phase::Break => self.break_secs,
                    Phase::Idle => self.focus_secs,
                }
                .min(86399),
            )
        };
        PomodoroSnapshot {
            phase: self.phase,
            running: self.running,
            paused: self.paused,
            remaining_secs,
            focus_min: self.focus_secs / 60,
            break_min: self.break_secs / 60,
            rounds_done: self.rounds_done,
        }
    }

    pub fn persisted(&self) -> PersistedPom {
        PersistedPom {
            phase: self.phase,
            running: self.running,
            paused: self.paused,
            ends_at_ms: self.ends_at.map(|e| {
                let now = Instant::now();
                crate::platform::now_ms()
                    + e.saturating_duration_since(now).as_millis() as i64
            }),
            remaining_ms: self.remaining.map(|r| r.as_millis() as u64),
            focus_secs: self.focus_secs,
            break_secs: self.break_secs,
            rounds_done: self.rounds_done,
        }
    }

    /// 설정 파일의 실행 상태를 복원 (기한이 지난 타이머는 폐기)
    pub fn restore_from(&mut self, p: &PersistedPom) {
        if !p.running {
            return;
        }
        self.focus_secs = p.focus_secs;
        self.break_secs = p.break_secs;
        self.rounds_done = p.rounds_done;
        self.phase = p.phase;
        self.running = true;
        self.paused = p.paused;
        if p.paused {
            self.remaining = p.remaining_ms.map(Duration::from_millis);
            self.ends_at = None;
        } else if let Some(ms) = p.ends_at_ms {
            let remain_ms = ms - crate::platform::now_ms();
            if remain_ms > 0 {
                self.ends_at = Some(Instant::now() + Duration::from_millis(remain_ms as u64));
            } else {
                // 앱이 꺼져 있을 때 이미 기한이 지났으면 다음 페이즈부터
                let next = if p.phase == Phase::Focus {
                    self.rounds_done += 1;
                    Phase::Break
                } else {
                    Phase::Focus
                };
                self.phase = next;
                let secs = match next {
                    Phase::Focus => self.focus_secs,
                    Phase::Break => self.break_secs,
                    Phase::Idle => self.focus_secs,
                };
                self.ends_at = Some(Instant::now() + Duration::from_secs(secs));
            }
        }
    }
}

/// 현재 상태를 설정 파일에 저장
pub fn persist(app: &AppHandle) {
    use tauri::Manager;
    if let Some(st) = app.try_state::<crate::app::AppState>() {
        let mut cfg = st.cfg.lock();
        cfg.pomodoro = Some(st.pomodoro.lock().persisted());
        cfg.save();
    }
}

fn switch_to(state: &mut PomodoroState, phase: Phase, lang: &str) -> (String, String) {
    state.phase = phase;
    let secs = match phase {
        Phase::Focus => state.focus_secs,
        Phase::Break => state.break_secs,
        Phase::Idle => unreachable!(),
    };
    state.ends_at = Some(Instant::now() + Duration::from_secs(secs));
    state.remaining = None;
    state.paused = false;
    match phase {
        Phase::Focus => (
            crate::i18n::tr(&lang, "집중 시작", "Focus started").to_string(),
            crate::i18n::fmt(lang, "{}분 집중!", "Focus for {} min!", &[&(secs / 60)]),
        ),
        Phase::Break => (
            crate::i18n::tr(&lang, "휴식 시작", "Break started").to_string(),
            crate::i18n::fmt(lang, "{}분 휴식!", "Break for {} min!", &[&(secs / 60)]),
        ),
        Phase::Idle => unreachable!(),
    }
}

pub fn tick_loop(app: AppHandle) {
    use tauri::Manager;
    loop {
        std::thread::sleep(Duration::from_secs(1));
        let mut phase_change: Option<(String, String)> = None;
        let snapshot;
        {
            let st = app.state::<crate::app::AppState>();
            let lang = st.cfg.lock().lang.clone();
            let mut p = st.pomodoro.lock();
            if p.running && !p.paused {
                if let Some(end) = p.ends_at {
                    if end <= Instant::now() {
                        let next = match p.phase {
                            Phase::Focus => {
                                p.rounds_done += 1;
                                Phase::Break
                            }
                            _ => Phase::Focus,
                        };
                        phase_change = Some(switch_to(&mut p, next, &lang));
                        persist(&app);
                    }
                }
            }
            snapshot = p.snapshot();
        }
        let _ = app.emit("pomodoro", &snapshot);
        if let Some((title, body)) = phase_change {
            crate::widgets::feed::push(&app, "⏱", &title, &body);
            crate::platform::notify(&title, &body);
            // 전환 사운드 (실패해도 무시)
            let _ = std::process::Command::new("afplay")
                .arg("/System/Library/Sounds/Glass.aiff")
                .spawn();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn idle_snapshot_shows_configured_focus_time() {
        let mut p = PomodoroState { focus_secs: 25 * 60, break_secs: 5 * 60, ..Default::default() };
        p.phase = Phase::Idle;
        let s = p.snapshot();
        assert_eq!((s.phase, s.remaining_secs, s.running), (Phase::Idle, 1500, false));
    }

    #[test]
    fn paused_snapshot_freezes_remaining() {
        let mut p = PomodoroState {
            phase: Phase::Focus,
            running: true,
            paused: true,
            remaining: Some(Duration::from_secs(602)),
            focus_secs: 1500,
            break_secs: 300,
            ..Default::default()
        };
        p.phase = Phase::Focus;
        let s = p.snapshot();
        assert_eq!((s.paused, s.remaining_secs), (true, 602));
    }

    #[test]
    fn running_snapshot_counts_down() {
        let mut p = PomodoroState {
            phase: Phase::Focus,
            running: true,
            paused: false,
            ends_at: Some(Instant::now() + Duration::from_secs(120)),
            focus_secs: 1500,
            break_secs: 300,
            ..Default::default()
        };
        p.phase = Phase::Focus;
        let s = p.snapshot();
        assert!((100..=120).contains(&s.remaining_secs));
    }

    #[test]
    fn persist_roundtrip_restores_running_timer() {
        let mut p = PomodoroState {
            phase: Phase::Focus,
            running: true,
            paused: false,
            ends_at: Some(Instant::now() + Duration::from_secs(120)),
            focus_secs: 1500,
            break_secs: 300,
            rounds_done: 2,
            ..Default::default()
        };
        p.phase = Phase::Focus;
        // JSON 직렬화 왕복 (설정 파일과 동일한 경로)
        let persisted = p.persisted();
        let json = serde_json::to_string(&persisted).unwrap();
        let restored_input: PersistedPom = serde_json::from_str(&json).unwrap();
        let mut restored = PomodoroState::default();
        restored.restore_from(&restored_input);
        assert!(restored.running && !restored.paused);
        assert_eq!(restored.rounds_done, 2);
        let s = restored.snapshot();
        assert!((100..=120).contains(&s.remaining_secs));
    }

    #[test]
    fn restore_discards_expired_timer() {
        let mut p = PomodoroState {
            phase: Phase::Focus,
            running: true,
            paused: false,
            ends_at: Some(Instant::now() + Duration::from_secs(120)),
            focus_secs: 1500,
            break_secs: 300,
            ..Default::default()
        };
        p.phase = Phase::Focus;
        let mut persisted = p.persisted();
        // 기한을 과거로 밀어버림 → 복원 시 다음 페이즈(휴식)로 롤오버
        persisted.ends_at_ms = Some(crate::platform::now_ms() - 10_000);
        let mut restored = PomodoroState::default();
        restored.restore_from(&persisted);
        assert_eq!(restored.phase, Phase::Break);
        assert!(restored.running);
        assert!(restored.snapshot().remaining_secs > 0);
    }
}
