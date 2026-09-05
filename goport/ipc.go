package main

// IPC 커맨드 (Rust ipc.rs 포트): JS invoke → Go 함수 디스패치

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

type commandFunc func(st *AppState, a Args, panelID int64) (any, error)

var commandHandlers map[string]commandFunc

func init() {
	commandHandlers = map[string]commandFunc{
		"orb_ready":          cmdOrbReady,
		"quit_app":           cmdQuit,
		"get_config":         cmdGetConfig,
		"set_lang":           cmdSetLang,
		"set_widget_visible": cmdSetWidgetVisible,
		"set_form":           cmdSetForm,
		"orb_state":          cmdOrbState,
		"orb_collapse":       cmdOrbCollapse,
		// 포모도로
		"pom_snapshot": cmdPomSnapshot,
		"pom_start":    cmdPomStart,
		"pom_pause":    cmdPomPause,
		"pom_resume":   cmdPomResume,
		"pom_reset":    cmdPomReset,
		// 토글
		"get_toggles":     cmdGetToggles,
		"toggle_dark":     cmdToggleDark,
		"toggle_wifi":     cmdToggleWifi,
		"toggle_bt":       cmdToggleBt,
		"toggle_mute":     cmdToggleMute,
		"toggle_caffeine": cmdToggleCaffeine,
		// 선반
		"shelf_add":       cmdShelfAdd,
		"shelf_list":      cmdShelfList,
		"shelf_remove":    cmdShelfRemove,
		"shelf_clear":     cmdShelfClear,
		"shelf_copy":      cmdShelfCopy,
		"shelf_reveal":    cmdShelfReveal,
		"shelf_open":      cmdShelfOpen,
		"shelf_open_path": cmdShelfOpenPath,
		"shelf_move_to":   cmdShelfMoveTo,
		// 포매터/피드
		"format_text": cmdFormatText,
		"get_feed":    cmdGetFeed,
		"clear_feed":  cmdClearFeed,
		// 로컬 타이머 완료 (JS 타이머 위젯)
		"timer_done": cmdTimerDone,
		// 오브 숨김 토글 (⌘⌥O / 설정 버튼)
		"toggle_orb_hidden": cmdToggleOrbHidden,
		// 터미널
		"term_init":       cmdTermInit,
		"term_write":      cmdTermWrite,
		"term_resize":     cmdTermResize,
		"term_reset":      cmdTermReset,
		"term_launch_cli": cmdTermLaunchCli,
		// AI 런처
		"ai_status":    cmdAiStatus,
		"open_gui_app": cmdOpenGuiApp,
		// 팝오버
		"open_popover":  cmdOpenPopover,
		"close_popover": cmdClosePopover,
		"popover_ready": cmdPopoverReady,
	}
}

func cmdOrbReady(*AppState, Args, int64) (any, error) {
	println("[orb_ready] orb JS 로드됨")
	return nil, nil
}

func cmdQuit(*AppState, Args, int64) (any, error) {
	AppQuit()
	os.Exit(0)
	return nil, nil
}

func cmdGetConfig(st *AppState, _ Args, _ int64) (any, error) {
	return st.configSnapshot(), nil
}

func cmdSetLang(st *AppState, a Args, _ int64) (any, error) {
	lang := a.Str("lang")
	if lang != "ko" && lang != "en" {
		return nil, fmt.Errorf("지원하지 않는 언어")
	}
	st.withConfig(func(cfg *Config) { cfg.Lang = lang })
	st.events.Publish("lang-changed", lang)
	return lang, nil
}

func cmdSetWidgetVisible(st *AppState, a Args, _ int64) (any, error) {
	widget := a.Str("widget")
	visible := a.Bool("visible")
	st.withConfig(func(cfg *Config) {
		kept := cfg.HiddenWidgets[:0]
		for _, w := range cfg.HiddenWidgets {
			if w != widget {
				kept = append(kept, w)
			}
		}
		cfg.HiddenWidgets = kept
		if !visible && widget != "settings" {
			cfg.HiddenWidgets = append(cfg.HiddenWidgets, widget)
		}
	})
	hidden := st.configSnapshot().HiddenWidgets
	if orbID := st.orbPanel.Load(); orbID != 0 {
		emitTo(int64(orbID), "visibility-changed", nil)
	}
	return hidden, nil
}

func cmdSetForm(st *AppState, a Args, _ int64) (any, error) {
	form := a.Str("form")
	st.withConfig(func(cfg *Config) { cfg.Form = form })
	return st.configSnapshot(), nil
}

func cmdOrbState(st *AppState, _ Args, _ int64) (any, error) {
	return map[string]any{
		"expanded": st.orbExpanded.Load(),
		"hidden":   st.configSnapshot().HiddenWidgets,
	}, nil
}

// 오브를 접고 입력을 다시 차단
func cmdOrbCollapse(st *AppState, _ Args, _ int64) (any, error) {
	st.orbExpanded.Store(false)
	if orbID := st.orbPanel.Load(); orbID != 0 {
		emitTo(int64(orbID), "orb-toggle", false)
		PanelSetIgnores(int64(orbID), true)
	}
	return nil, nil
}

// ---------- 포모도로 ----------

func cmdPomSnapshot(st *AppState, _ Args, _ int64) (any, error) {
	st.pomMu.Lock()
	defer st.pomMu.Unlock()
	return st.pom.snapshot(), nil
}

func pomEmit(st *AppState) {
	st.pomMu.Lock()
	snap := st.pom.snapshot()
	st.pomMu.Unlock()
	st.events.Publish("pomodoro", snap)
}

func cmdPomStart(st *AppState, a Args, _ int64) (any, error) {
	st.pomMu.Lock()
	p := &st.pom
	fMin := p.focusSecs / 60
	bMin := p.breakSecs / 60
	if v, ok := a.Num("focus_min"); ok {
		fMin = uint64(v)
	}
	if v, ok := a.Num("break_min"); ok {
		bMin = uint64(v)
	}
	if fMin < 1 {
		fMin = 1
	}
	if bMin < 1 {
		bMin = 1
	}
	p.focusSecs = fMin * 60
	p.breakSecs = bMin * 60
	p.phase = PhaseFocus
	p.running = true
	p.paused = false
	p.remaining = 0
	p.endsAt = time.Now().Add(time.Duration(p.focusSecs) * time.Second)
	p.hasEndsAt = true
	st.pomMu.Unlock()

	st.withConfig(func(cfg *Config) { cfg.FocusMin, cfg.BreakMin = fMin, bMin })
	pomPersist(st)
	pomEmit(st)
	return nil, nil
}

func cmdPomPause(st *AppState, _ Args, _ int64) (any, error) {
	st.pomMu.Lock()
	p := &st.pom
	if p.running && !p.paused {
		if p.hasEndsAt {
			d := time.Until(p.endsAt)
			if d < 0 {
				d = 0
			}
			p.remaining = d
		}
		p.paused = true
	}
	st.pomMu.Unlock()
	pomPersist(st)
	pomEmit(st)
	return nil, nil
}

func cmdPomResume(st *AppState, _ Args, _ int64) (any, error) {
	st.pomMu.Lock()
	p := &st.pom
	if p.running && p.paused {
		rem := p.remaining
		if rem <= 0 {
			rem = time.Minute
		}
		p.endsAt = time.Now().Add(rem)
		p.hasEndsAt = true
		p.paused = false
	}
	st.pomMu.Unlock()
	pomPersist(st)
	pomEmit(st)
	return nil, nil
}

func cmdPomReset(st *AppState, _ Args, _ int64) (any, error) {
	st.pomMu.Lock()
	p := &st.pom
	p.phase = PhaseIdle
	p.running = false
	p.paused = false
	p.endsAt = time.Time{}
	p.hasEndsAt = false
	p.remaining = 0
	p.rounds = 0
	st.pomMu.Unlock()
	pomPersist(st)
	pomEmit(st)
	return nil, nil
}

// ---------- 토글 ----------

func cmdGetToggles(st *AppState, _ Args, _ int64) (any, error) {
	return getTogglesState(st), nil
}

func cmdToggleDark(st *AppState, _ Args, _ int64) (any, error) {
	next := !getTogglesState(st).Dark
	lang := st.lang()
	if err := darkModeSet(next); err != nil {
		return nil, err
	}
	stateTxt := tr(lang, "끔", "Off")
	if next {
		stateTxt = tr(lang, "켬", "On")
	}
	feedPush(st, "🌗", tr(lang, "다크모드", "Dark mode"), stateTxt)
	return next, nil
}

func cmdToggleWifi(st *AppState, _ Args, _ int64) (any, error) {
	cur, _ := wifiState(st.wifiDev)
	next := !cur
	lang := st.lang()
	if err := wifiSet(st.wifiDev, next); err != nil {
		return nil, err
	}
	stateTxt := tr(lang, "끔", "Off")
	if next {
		stateTxt = tr(lang, "켬", "On")
	}
	feedPush(st, "📶", "Wi-Fi", stateTxt)
	return next, nil
}

func cmdToggleBt(st *AppState, _ Args, _ int64) (any, error) {
	cur := false
	if out, ok := shellOutput("blueutil", "-p"); ok {
		cur = out == "1"
	}
	next := !cur
	lang := st.lang()
	if err := btSet(next); err != nil {
		return nil, err
	}
	stateTxt := tr(lang, "끔", "Off")
	if next {
		stateTxt = tr(lang, "켬", "On")
	}
	feedPush(st, "🅑", "Bluetooth", stateTxt)
	return next, nil
}

func cmdToggleMute(st *AppState, _ Args, _ int64) (any, error) {
	cur := false
	if out, err := osascript("output muted of (get volume settings)"); err == nil {
		cur = out == "true"
	}
	next := !cur
	lang := st.lang()
	if err := muteSet(next); err != nil {
		return nil, err
	}
	stateTxt := tr(lang, "끔", "Off")
	if next {
		stateTxt = tr(lang, "켬", "On")
	}
	feedPush(st, "🔇", tr(lang, "음소거", "Mute"), stateTxt)
	return next, nil
}

func cmdToggleCaffeine(st *AppState, _ Args, _ int64) (any, error) {
	on, err := caffeineToggle(st)
	if err != nil {
		return nil, err
	}
	lang := st.lang()
	stateTxt := tr(lang, "끔", "Off")
	if on {
		stateTxt = tr(lang, "켬 (디스플레이 유지)", "On (keep display awake)")
	}
	feedPush(st, "☕", tr(lang, "잠금방지", "Stay awake"), stateTxt)
	return on, nil
}

// ---------- 선반 ----------

func cmdShelfAdd(st *AppState, a Args, _ int64) (any, error) {
	before := len(shelfList(st))
	list := shelfAdd(st, a.StrList("paths"))
	lang := st.lang()
	if added := len(list) - before; added > 0 {
		feedPush(st, "📥", tr(lang, "선반 추가", "Shelf added"),
			fmtTr(lang, "새 항목 {}개 · 총 {}개", "Added {} · total {}", itoa(int64(added)), itoa(int64(len(list)))))
	}
	st.events.Publish("shelf", list)
	return list, nil
}

func cmdShelfList(st *AppState, _ Args, _ int64) (any, error) { return shelfList(st), nil }

func cmdShelfRemove(st *AppState, a Args, _ int64) (any, error) {
	idx, _ := a.Num("idx")
	list := shelfRemove(st, int(idx))
	st.events.Publish("shelf", list)
	return list, nil
}

func cmdShelfClear(st *AppState, _ Args, _ int64) (any, error) {
	list := shelfClear(st)
	st.events.Publish("shelf", list)
	return list, nil
}

func cmdShelfCopy(st *AppState, a Args, _ int64) (any, error) {
	idx, hasIdx := a.Num("idx")
	n, err := shelfCopyRefs(st, int(idx), hasIdx)
	if err != nil {
		return nil, err
	}
	return float64(n), nil
}

func cmdShelfReveal(st *AppState, a Args, _ int64) (any, error) {
	idx, _ := a.Num("idx")
	return nil, shelfReveal(st, int(idx))
}

func cmdShelfOpen(st *AppState, a Args, _ int64) (any, error) {
	idx, _ := a.Num("idx")
	return nil, shelfOpen(st, int(idx))
}

func cmdShelfOpenPath(_ *AppState, a Args, _ int64) (any, error) {
	return nil, shelfOpenPath(a.Str("path"))
}

func cmdShelfMoveTo(st *AppState, a Args, _ int64) (any, error) {
	idx, hasIdx := a.Num("idx")
	ok, fail, err := shelfMoveTo(st, int(idx), hasIdx)
	if err != nil {
		return nil, err
	}
	lang := st.lang()
	feedPush(st, "📦", tr(lang, "파일 이동", "Files moved"),
		fmtTr(lang, "성공 {} · 실패 {}", "ok {} · failed {}", itoa(int64(ok)), itoa(int64(fail))))
	st.events.Publish("shelf", shelfList(st))
	return []int{ok, fail}, nil
}

// ---------- 포매터/피드 ----------

func cmdFormatText(_ *AppState, a Args, _ int64) (any, error) {
	convert, _ := a.OptStr("convert")
	return formatText(a.Str("text"), a.Str("format"), a.Bool("pretty"), convert)
}

func cmdGetFeed(st *AppState, _ Args, _ int64) (any, error) { return feedList(st), nil }

// 로컬 타이머(타이머 모드) 완료 시 JS에서 호출 — 피드 + 알림 + 사운드
func cmdTimerDone(st *AppState, a Args, _ int64) (any, error) {
	secs, _ := a.Num("secs")
	lang := st.lang()
	title := tr(lang, "타이머 완료", "Timer finished")
	body := fmtTr(lang, "{}분 타이머가 끝났습니다", "{} min timer finished", itoa(int64(secs/60)))
	feedPush(st, "⏰", title, body)
	notify(title, body)
	_ = exec.Command("afplay", "/System/Library/Sounds/Glass.aiff").Start()
	return nil, nil
}

func cmdClearFeed(st *AppState, _ Args, _ int64) (any, error) { return feedClear(st), nil }

// ---------- 터미널 ----------

func cmdTermInit(_ *AppState, _ Args, panelID int64) (any, error) {
	if err := termEnsure(); err != nil {
		return nil, err
	}
	// 팝오버가 닫혀 있던 동안의 출력을 한 번에 내려준다
	if buf, ok := termScrollback(); ok && buf != "" {
		emitTo(panelID, "term-out", buf)
	}
	return nil, nil
}

func cmdTermWrite(_ *AppState, a Args, _ int64) (any, error) {
	return nil, termWrite(a.Str("data"))
}

func cmdTermResize(_ *AppState, a Args, _ int64) (any, error) {
	cols, _ := a.Num("cols")
	rows, _ := a.Num("rows")
	termResize(uint16(cols), uint16(rows))
	return nil, nil
}

func cmdTermReset(_ *AppState, _ Args, _ int64) (any, error) {
	termReset()
	return nil, nil
}

// 내장 터미널 세션에서 AI CLI 실행 (세션 유지되어 닫아도 계속 돌아감)
func cmdTermLaunchCli(_ *AppState, a Args, _ int64) (any, error) {
	program := a.Str("program")
	if program != "codex" && program != "claude" {
		return nil, fmt.Errorf("허용되지 않은 프로그램")
	}
	if err := termEnsure(); err != nil {
		return nil, err
	}
	return nil, termWrite(program + "\r")
}

// ---------- AI 런처 ----------

func cmdAiStatus(_ *AppState, _ Args, _ int64) (any, error) {
	return map[string]bool{
		"codex":       commandExists("codex"),
		"claude":      commandExists("claude"),
		"claude_app":  appInstalled("Claude.app"),
		"chatgpt_app": appInstalled("ChatGPT.app"),
	}, nil
}

// GUI 앱 실행 (화이트리스트)
func cmdOpenGuiApp(st *AppState, a Args, _ int64) (any, error) {
	name := a.Str("name")
	appName := ""
	switch name {
	case "claude":
		appName = "Claude"
	case "codex", "chatgpt":
		appName = "ChatGPT"
	default:
		return nil, fmt.Errorf("허용되지 않은 앱")
	}
	if err := exec.Command("open", "-a", appName).Start(); err != nil {
		return nil, err
	}
	lang := st.lang()
	body := tr(lang, "ChatGPT 앱을 실행합니다", "Launching ChatGPT app")
	if appName == "Claude" {
		body = tr(lang, "Claude 앱을 실행합니다", "Launching Claude app")
	}
	feedPush(st, "🚀", tr(lang, "앱 실행", "App launched"), body)
	return nil, nil
}

// ---------- 팝오버 ----------

func popoverSize(widget string) (float64, float64) {
	switch widget {
	case "pomodoro", "timer":
		return 300, 330
	case "monitor":
		return 320, 360
	case "toggles":
		return 270, 360
	case "agents", "ai_term", "ai_apps":
		return 350, 400
	case "shelf":
		return 400, 440
	case "format":
		return 440, 480
	case "feed":
		return 360, 440
	case "terminal":
		return 680, 440
	case "settings":
		return 300, 500
	default:
		return 320, 360
	}
}

// 포커스를 잃어도 닫히지 않는 위젯 (파일 드래그 등 다른 앱 조작 중에 열어둠)
func isPinnedWidget(widget string) bool {
	return widget == "shelf" || widget == "terminal" || widget == "format"
}

func closeCurrentPopover(st *AppState) {
	st.popoverMu.Lock()
	label := st.popover
	st.popoverMu.Unlock()
	if label == "" {
		return
	}
	if id, ok := idByLabel(label); ok {
		PanelClose(id)
	}
	st.popoverMu.Lock()
	if st.popover == label {
		st.popover = ""
		st.popWidget = ""
	}
	st.popoverMu.Unlock()
	releaseActivationHold(st)
}

// 오브 숨김/표시 토글. 숨김 중엔 패널이 화면에서 빠져 어떤 클릭도 받지 않는다.
func cmdToggleOrbHidden(st *AppState, _ Args, _ int64) (any, error) {
	hidden := !st.orbHidden.Load()
	st.orbHidden.Store(hidden)
	orb := int64(st.orbPanel.Load())
	if orb == 0 {
		return hidden, nil
	}
	if hidden {
		PanelOrderOut(orb)
	} else {
		// 현재 커서 화면 위치로 재적용 + 전면 복귀
		if rect, ok := st.getOrbRect(); ok {
			PanelApply(orb, rect, orbLevel)
		} else {
			PanelRejoin(orb)
		}
	}
	return hidden, nil
}

func cmdClosePopover(st *AppState, _ Args, _ int64) (any, error) {
	closeCurrentPopover(st)
	return nil, nil
}

func cmdOpenPopover(st *AppState, a Args, _ int64) (any, error) {
	widget := a.Str("widget")
	tileX, _ := a.Num("tileX", "tile_x")
	label := "popover-" + widget

	// 이미 열려 있으면 닫기(토글). 창이 완전히 닫힌 뒤 새 창을 만든다 (경합 방지)
	st.popoverMu.Lock()
	prev := st.popover
	st.popover = ""
	st.popoverMu.Unlock()
	wasSame := prev == label
	if prev != "" {
		if id, ok := idByLabel(prev); ok {
			PanelClose(id)
		}
		for i := 0; i < 50; i++ {
			if _, ok := idByLabel(prev); !ok {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if wasSame {
			return nil, nil
		}
	}

	w, h := popoverSize(widget)
	orbRect, hasOrb := st.getOrbRect()
	// 오브가 있는 화면 기준으로 클램프 (멀티 디스플레이)
	scrX, scrW := 0.0, 0.0
	if hasOrb {
		if scr, ok := screenFrameAt(orbRect.X+orbRect.W/2, orbRect.Y+orbRect.H/2); ok {
			scrX, scrW = scr.X, scr.W
		}
	}
	if scrW == 0 {
		mw, _ := MainScreen()
		scrW = mw
	}
	// 동그라미 중앙(tileX) 위에 팝오버 중앙이 오도록 배치 + 화면 경계 클램프
	var x, y float64
	if hasOrb {
		center := orbRect.X + tileX
		x = center - w/2
		if x < scrX+8 {
			x = scrX + 8
		}
		if x > scrX+scrW-w-8 {
			x = scrX + scrW - w - 8
		}
		y = orbRect.Y + 96
	} else {
		x = (scrX + scrW - w) / 2
		y = 90
	}

	id := st.nextPanelID()
	native := useNativeUI() && widget != "terminal"
	registerPanelKind(id, label, native)
	// 터미널만 웹(xterm.js + Go PTY) 유지 — SwiftTerm forkpty 스폰이 불안정.
	// 그 외 위젯은 전부 네이티브 AppKit 뷰 (JS 없음).
	if native {
		PanelCreateNative(id, widget, x, y, w, h, popoverLevelFor(widget), false, true)
	} else {
		PanelCreate(id, label, shimFor(label), x, y, w, h, false, popoverLevelFor(widget), true)
	}
	PanelPinSpaces(id)
	PanelFocus(id)

	// 화면 전환 시 팝오버가 오브를 따라갈 수 있도록 앵커를 기억한다
	st.popoverMu.Lock()
	st.popover = label
	st.popWidget = widget
	st.popTileX = tileX
	st.popoverMu.Unlock()
	return nil, nil
}

func cmdPopoverReady(_ *AppState, _ Args, panelID int64) (any, error) {
	// 팝오버 윈도우도 모든 스페이스에 표시
	PanelPinSpaces(panelID)
	return nil, nil
}
