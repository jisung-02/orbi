package main

// 앱 전역 상태 (Rust AppState 포트)

import (
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"
)

// ConfigStore is owned by the state layer. Storage adapters preserve the
// existing best-effort persistence contract.
type ConfigStore interface {
	Load() Config
	Save(Config)
}

type EventPublisher interface{ Publish(string, any) }

type AppState struct {
	configStore ConfigStore
	events      EventPublisher
	cfgRevision atomic.Uint64
	cfgMu       sync.Mutex
	cfg         Config

	orbRectMu sync.Mutex
	orbRect   Rect
	orbRectOK bool

	lastScreenMu sync.Mutex
	lastScreen   Rect
	lastScreenOK bool

	pomMu sync.Mutex
	pom   PomodoroState

	feedMu sync.Mutex
	feed   []FeedItem
	unread int64 // 오브 배지용 (JS가 자체 카운트도 함)

	shelfMu sync.Mutex
	shelf   []ShelfItem

	caffeinateMu sync.Mutex
	caffeinate   *exec.Cmd

	wifiDev string // 빈 문자열 = 감지 실패

	popoverMu sync.Mutex
	popover   string  // 현재 열린 팝오버 라벨
	popWidget string  // 열린 팝오버 위젯 (화면 전환 시 재배치용)
	popTileX  float64 // 열린 팝오버의 오브 로컬 서클 X (재배치 앵커)
	activHold string  // 오브 확장 시 빼앗은 최전면 앱 bundle id (접힘 시 복귀시킴)

	orbExpanded atomic.Bool
	orbHidden   atomic.Bool // 사용자가 오브를 숨긴 상태 (⌘⌥O / 설정)
	panelSeq    atomic.Int64
	orbPanel    atomic.Int64 // 오브 패널 id (0 = 미생성)
}

func newState(store ConfigStore, events EventPublisher, wifiDev string) *AppState {
	cfg := cloneConfig(store.Load())
	st := &AppState{cfg: cfg, configStore: store, events: events}
	st.shelf = []ShelfItem{}
	st.feed = []FeedItem{}
	st.wifiDev = wifiDev
	st.pom = newPomodoro(&cfg)
	return st
}

func (st *AppState) withConfig(fn func(cfg *Config)) {
	st.cfgMu.Lock()
	fn(&st.cfg)
	st.configStore.Save(cloneConfig(st.cfg))
	st.cfgRevision.Add(1)
	st.cfgMu.Unlock()
}

func (st *AppState) configSnapshot() Config {
	st.cfgMu.Lock()
	defer st.cfgMu.Unlock()
	return cloneConfig(st.cfg)
}

func (st *AppState) lang() string {
	st.cfgMu.Lock()
	defer st.cfgMu.Unlock()
	return st.cfg.Lang
}

func (st *AppState) setOrbRect(r Rect) {
	st.orbRectMu.Lock()
	st.orbRect = r
	st.orbRectOK = true
	st.orbRectMu.Unlock()
}

func (st *AppState) getOrbRect() (Rect, bool) {
	st.orbRectMu.Lock()
	defer st.orbRectMu.Unlock()
	return st.orbRect, st.orbRectOK
}

// 대상 화면 기록: 바뀜 감지용. (이전 값, 변경 여부) 반환
func (st *AppState) updateTargetScreen(s Rect) (Rect, bool) {
	st.lastScreenMu.Lock()
	defer st.lastScreenMu.Unlock()
	prev, had := st.lastScreen, st.lastScreenOK
	st.lastScreen = s
	st.lastScreenOK = true
	changed := had && prev != s
	return prev, changed
}

func (st *AppState) nextPanelID() int64 { return st.panelSeq.Add(1) }

// Mutable data never aliases a repository or a returned snapshot.
func cloneConfig(cfg Config) Config {
	cfg.HiddenWidgets = slices.Clone(cfg.HiddenWidgets)
	if cfg.Pomodoro != nil {
		pom := *cfg.Pomodoro
		if pom.EndsAtMs != nil {
			v := *pom.EndsAtMs
			pom.EndsAtMs = &v
		}
		if pom.RemainingMs != nil {
			v := *pom.RemainingMs
			pom.RemainingMs = &v
		}
		cfg.Pomodoro = &pom
	}
	return cfg
}
