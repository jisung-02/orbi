use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Serialize, Deserialize, Clone, Debug)]
pub struct Config {
    pub focus_min: u64,
    pub break_min: u64,
    /// 포모도로 실행 상태 (앱 재시작 시 복원용)
    #[serde(default)]
    pub pomodoro: Option<crate::widgets::pomodoro::PersistedPom>,
    /// UI 언어 ("ko" | "en")
    #[serde(default)]
    pub lang: String,
    /// 오브에서 숨길 위젯 목록
    #[serde(default)]
    pub hidden_widgets: Vec<String>,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            focus_min: 25,
            break_min: 5,
            pomodoro: None,
            lang: "ko".into(),
            hidden_widgets: Vec::new(),
        }
    }
}

pub fn config_path() -> PathBuf {
    dirs::data_dir()
        .unwrap_or_else(std::env::temp_dir)
        .join("dock-util")
}

impl Config {
    pub fn load() -> Self {
        let p = config_path().join("config.json");
        std::fs::read_to_string(p)
            .ok()
            .and_then(|s| serde_json::from_str(&s).ok())
            .unwrap_or_default()
    }

    pub fn save(&self) {
        let dir = config_path();
        let _ = std::fs::create_dir_all(&dir);
        if let Ok(json) = serde_json::to_string_pretty(self) {
            let _ = std::fs::write(dir.join("config.json"), json);
        }
    }
}
