package main

// 포모도로: 1초 틱 루프 + 페이즈 자동 전환 + 상태 영속화 (Rust widgets/pomodoro.rs 포트)

import (
	"math"
	"os/exec"
	"time"
)

const (
	PhaseIdle  = "idle"
	PhaseFocus = "focus"
	PhaseBreak = "break"
)

type PomodoroSnapshot struct {
	Phase         string `json:"phase"`
	Running       bool   `json:"running"`
	Paused        bool   `json:"paused"`
	RemainingSecs uint64 `json:"remaining_secs"`
	FocusMin      uint64 `json:"focus_min"`
	BreakMin      uint64 `json:"break_min"`
	RoundsDone    uint32 `json:"rounds_done"`
}

type PomodoroState struct {
	phase     string
	running   bool
	paused    bool
	endsAt    time.Time
	hasEndsAt bool
	remaining time.Duration // 일시정지 시 남은 시간
	focusSecs uint64
	breakSecs uint64
	rounds    uint32
}

func newPomodoro(cfg *Config) PomodoroState {
	p := PomodoroState{
		focusSecs: max64(cfg.FocusMin, 1) * 60,
		breakSecs: max64(cfg.BreakMin, 1) * 60,
		phase:     PhaseIdle,
	}
	if cfg.Pomodoro != nil {
		p.restoreFrom(cfg.Pomodoro)
	}
	return p
}

func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func phaseSecs(p *PomodoroState, phase string) uint64 {
	if phase == PhaseBreak {
		return p.breakSecs
	}
	return p.focusSecs
}

func (p *PomodoroState) snapshot() PomodoroSnapshot {
	var remaining uint64
	if p.running && !p.paused {
		if p.hasEndsAt {
			d := time.Until(p.endsAt)
			if d < 0 {
				d = 0
			}
			remaining = uint64(math.Floor(d.Seconds()))
		}
	} else if p.remaining > 0 && p.running {
		remaining = uint64(math.Floor(p.remaining.Seconds()))
	} else {
		remaining = phaseSecs(p, p.phase)
	}
	if remaining > 86399 {
		remaining = 86399
	}
	return PomodoroSnapshot{
		Phase:         p.phase,
		Running:       p.running,
		Paused:        p.paused,
		RemainingSecs: remaining,
		FocusMin:      p.focusSecs / 60,
		BreakMin:      p.breakSecs / 60,
		RoundsDone:    p.rounds,
	}
}

func (p *PomodoroState) persisted() *PersistedPom {
	pp := &PersistedPom{
		Phase:      p.phase,
		Running:    p.running,
		Paused:     p.paused,
		FocusSecs:  p.focusSecs,
		BreakSecs:  p.breakSecs,
		RoundsDone: p.rounds,
	}
	if p.hasEndsAt {
		ms := nowMs() + int64(time.Until(p.endsAt).Milliseconds())
		pp.EndsAtMs = &ms
	}
	if p.remaining > 0 {
		ms := uint64(p.remaining.Milliseconds())
		pp.RemainingMs = &ms
	}
	return pp
}

// 설정 파일의 실행 상태를 복원 (기한이 지난 타이머는 다음 페이즈로 롤오버)
func (p *PomodoroState) restoreFrom(pp *PersistedPom) {
	if !pp.Running {
		return
	}
	p.focusSecs = pp.FocusSecs
	p.breakSecs = pp.BreakSecs
	p.rounds = pp.RoundsDone
	p.phase = pp.Phase
	p.running = true
	p.paused = pp.Paused
	switch {
	case pp.Paused:
		if pp.RemainingMs != nil {
			p.remaining = time.Duration(*pp.RemainingMs) * time.Millisecond
		}
		p.hasEndsAt = false
	case pp.EndsAtMs != nil:
		remainMs := *pp.EndsAtMs - nowMs()
		if remainMs > 0 {
			p.endsAt = time.Now().Add(time.Duration(remainMs) * time.Millisecond)
			p.hasEndsAt = true
		} else {
			// 앱이 꺼져 있을 때 만료됐으면 이어서 돌리지 않고 정지한다 —
			// 무음으로 다음 페이즈를 굴리면 사용자가 모르는 사이 알림이 계속 온다
			p.phase = PhaseIdle
			p.running = false
			p.paused = false
			p.hasEndsAt = false
			p.remaining = 0
		}
	}
}

// 락 없이 데이터를 받아 저장 (락 홀드 중 저장 호출 방지)
func pomPersistData(st *AppState, pp *PersistedPom) {
	st.withConfig(func(cfg *Config) { cfg.Pomodoro = pp })
}

func pomPersist(st *AppState) {
	st.pomMu.Lock()
	pp := st.pom.persisted()
	st.pomMu.Unlock()
	pomPersistData(st, pp)
}

// switchTo: 페이즈 전환과 (피드 제목, 본문) 반환
func pomSwitchTo(p *PomodoroState, phase, lang string) (string, string) {
	p.phase = phase
	secs := phaseSecs(p, phase)
	p.endsAt = time.Now().Add(time.Duration(secs) * time.Second)
	p.hasEndsAt = true
	p.remaining = 0
	p.paused = false
	if phase == PhaseFocus {
		return tr(lang, "집중 시작", "Focus started"), fmtTr(lang, "{}분 집중!", "Focus for {} min!", itoa(int64(secs/60)))
	}
	return tr(lang, "휴식 시작", "Break started"), fmtTr(lang, "{}분 휴식!", "Break for {} min!", itoa(int64(secs/60)))
}

func pomodoroTickLoop(st *AppState) {
	var lastEmitted PomodoroSnapshot
	hasLast := false
	for {
		time.Sleep(time.Second)
		var phaseChange [2]string
		hasChange := false
		st.pomMu.Lock()
		p := &st.pom
		if p.running && !p.paused && p.hasEndsAt && !time.Now().Before(p.endsAt) {
			next := PhaseFocus
			if p.phase == PhaseFocus {
				p.rounds++
				next = PhaseBreak
			}
			lang := st.lang()
			t, b := pomSwitchTo(p, next, lang)
			phaseChange = [2]string{t, b}
			hasChange = true
		}
		snap := p.snapshot()
		var pp *PersistedPom
		if hasChange {
			pp = p.persisted()
		}
		st.pomMu.Unlock()

		// 변화 없는 스냅샷(유휴 상태)은 매초 emit하지 않는다
		if !hasLast || lastEmitted != snap {
			lastEmitted = snap
			hasLast = true
			st.events.Publish("pomodoro", snap)
		}
		if hasChange {
			pomPersistData(st, pp)
			feedPush(st, "⏱", phaseChange[0], phaseChange[1])
			notify(phaseChange[0], phaseChange[1])
			_ = exec.Command("afplay", "/System/Library/Sounds/Glass.aiff").Start()
		}
	}
}
