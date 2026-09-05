package main

// 설정: ~/Library/Application Support/dock-util/config.json
// Rust 버전과 같은 파일/같은 키를 쓰므로 두 빌드가 설정을 공유한다.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type PersistedPom struct {
	Phase       string  `json:"phase"`
	Running     bool    `json:"running"`
	Paused      bool    `json:"paused"`
	EndsAtMs    *int64  `json:"ends_at_ms"`
	RemainingMs *uint64 `json:"remaining_ms"`
	FocusSecs   uint64  `json:"focus_secs"`
	BreakSecs   uint64  `json:"break_secs"`
	RoundsDone  uint32  `json:"rounds_done"`
}

type Config struct {
	FocusMin      uint64        `json:"focus_min"`
	BreakMin      uint64        `json:"break_min"`
	Pomodoro      *PersistedPom `json:"pomodoro,omitempty"`
	Lang          string        `json:"lang"`
	HiddenWidgets []string      `json:"hidden_widgets"`
	Form          string        `json:"form,omitempty"`
}

func defaultConfig() Config {
	return Config{FocusMin: 25, BreakMin: 5, Lang: "ko", HiddenWidgets: []string{}}
}

func configDir() string {
	base, err := os.UserConfigDir() // ~/Library/Application Support
	if err != nil {
		return filepath.Join(os.TempDir(), "dock-util")
	}
	return filepath.Join(base, "dock-util")
}

type fileConfigStore struct{ dir string }

func (s fileConfigStore) Load() Config {
	cfg := defaultConfig()
	b, err := os.ReadFile(filepath.Join(s.dir, "config.json"))
	if err != nil {
		return cfg
	}
	// 저장된 배열이 nil이 되지 않도록 로드 후 보정
	if err := json.Unmarshal(b, &cfg); err != nil {
		return defaultConfig()
	}
	if cfg.HiddenWidgets == nil {
		cfg.HiddenWidgets = []string{}
	}
	if cfg.Lang != "ko" && cfg.Lang != "en" {
		cfg.Lang = "ko"
	}
	if cfg.FocusMin == 0 {
		cfg.FocusMin = 25
	}
	if cfg.BreakMin == 0 {
		cfg.BreakMin = 5
	}
	return cfg
}

func (s fileConfigStore) Save(cfg Config) {
	_ = os.MkdirAll(s.dir, 0o755)
	if cfg.HiddenWidgets == nil {
		cfg.HiddenWidgets = []string{}
	}
	if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(s.dir, "config.json"), b, 0o644)
	}
}
