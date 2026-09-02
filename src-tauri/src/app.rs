//! 앱 전역 상태

use crate::config::Config;
use crate::platform::RectF;
use crate::widgets::feed::FeedItem;
use crate::widgets::pomodoro::PomodoroState;
use crate::widgets::shelf::ShelfItem;
use parking_lot::Mutex;
use std::process::Child;
use sysinfo::System;
pub struct AppState {
    pub cfg: Mutex<Config>,
    /// 현재 오브 프레임 (AppKit 좌표)
    pub orb_rect: Mutex<Option<RectF>>,
    pub pomodoro: Mutex<PomodoroState>,
    pub feed: Mutex<Vec<FeedItem>>,
    pub shelf: Mutex<Vec<ShelfItem>>,
    pub caffeinate: Mutex<Option<Child>>,
    pub wifi_dev: Mutex<Option<String>>,
    /// 에이전트 감지용 공유 sysinfo
    pub sys: Mutex<System>,
    /// 네트워크 트래픽 (모니터 위젯)
    pub net: Mutex<sysinfo::Networks>,
    /// 현재 열린 팝오버 라벨
    pub popover: Mutex<Option<String>>,
    /// 오브 펼침 상태 (orb_loop와 orb_collapse 커맨드가 공유)
    pub orb_expanded: Mutex<bool>,
    /// 터미널 PTY 세션 (팝오버를 닫아도 유지)
    pub term: crate::widgets::terminal::SharedSession,
}

impl AppState {
    pub fn new() -> Self {
        let cfg = Config::load();
        // 포모도로 초기값: 설정값 + 저장된 실행 상태 복원
        let mut pom = crate::widgets::pomodoro::PomodoroState {
            focus_secs: cfg.focus_min.max(1) * 60,
            break_secs: cfg.break_min.max(1) * 60,
            ..Default::default()
        };
        if let Some(pp) = &cfg.pomodoro {
            pom.restore_from(pp);
        }
        Self {
            cfg: Mutex::new(cfg),
            orb_rect: Mutex::new(None),
            pomodoro: Mutex::new(pom),
            feed: Mutex::new(Vec::new()),
            shelf: Mutex::new(Vec::new()),
            caffeinate: Mutex::new(None),
            wifi_dev: Mutex::new(crate::widgets::toggles::detect_wifi_device()),
            sys: Mutex::new(System::new()),
            net: Mutex::new(sysinfo::Networks::new()),
            popover: Mutex::new(None),
            orb_expanded: Mutex::new(false),
            term: Mutex::new(None),
        }
    }
}
