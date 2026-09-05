package main

// dock-util Go 포트 진입점
//
// 구조: 메인 고루틴은 [NSApp run]에 진입하고, 모든 앱 로직은 고루틴에서
// 돌아간다. NSWindow 조작은 cgo 셸(shell_darwin.m)이 GCD 메인 큐로 보낸다.

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"time"
)

func init() {
	// AppKit의 이벤트 루프는 최초 OS 메인 스레드에서 실행해야 한다.
	runtime.LockOSThread()
	// 메모리 최적화: 이 앱은 힙이 작으므로 GC를 공격적으로 돌려 상주분을 낮춘다
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(48 << 20) // 48MB 소프트 리밋
}

func main() {
	args := os.Args[1:]
	for _, a := range args {
		if a == "--check" {
			runCheck()
			return
		}
		if a == "--qa" {
			runQA()
			return
		}
		if a == "--fskey" {
			runFsKey()
			return
		}
		if a == "--ai-status" {
			fmt.Printf(`{"codex":%t,"claude":%t,"claude_app":%t,"chatgpt_app":%t}`+"\n",
				commandExists("codex"), commandExists("claude"),
				appInstalled("Claude.app"), appInstalled("ChatGPT.app"))
			return
		}
		if a == "--clickpid" && len(args) >= 3 {
			// 검증용: 실행 중인 앱에 클릭 주입 (--clickpid PID X,Y)
			var pid int
			var x, y float64
			fmt.Sscanf(args[1], "%d", &pid)
			fmt.Sscanf(args[2], "%f,%f", &x, &y)
			PostMouseClickPid(pid, float64(x), float64(y))
			fmt.Printf("click injected to pid %d at (%.0f, %.0f)\n", pid, x, y)
			return
		}
		if a == "--hotkey-o" {
			// 검증용: ⌘⌥O 키 입력을 시스템에 보낸다 (오브 숨김 토글)
			PostKey(0x00080000|0x00100000, 31) // option+command+O
			return
		}
		if a == "--mouse" && len(args) >= 2 {
			// 검증용: 지정 CG 좌표로 커서 이동 (예: --mouse 745,-540)
			var x, y float64
			fmt.Sscanf(args[1], "%f,%f", &x, &y)
			PostMouseMove(x, y)
			fmt.Printf("mouse moved to (%.0f, %.0f)\n", x, y)
			return
		}
	}

	// GUI 앱 모드: 시작 직후 간헐적 WebKit 초기화 트랩(외부 원인)이 있어
	// 조기 비정상 종료 시 자동 재시작하는 슈퍼바이저를 거친다.
	if os.Getenv("DU_SUPERVISE") != "1" {
		supervise()
		return
	}
	runApp()
}

// supervise: 자식으로 자기 자신을 실행하고 6초 내 비정상 종료하면 최대 3회 재시작
func supervise() {
	exe, err := os.Executable()
	if err != nil {
		runApp()
		return
	}
	for attempt := 0; attempt < 3; attempt++ {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Env = append(os.Environ(), "DU_SUPERVISE=1")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		start := time.Now()
		debug.FreeOSMemory() // 대기 프로세스는 힙이 거의 없으므로 상주분을 OS에 반환
		runErr := cmd.Run()
		if runErr == nil || time.Since(start) > 6*time.Second {
			if ee, ok := runErr.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			os.Exit(0)
		}
		fmt.Println("[supervisor] 조기 비정상 종료 — 재시도", attempt+1)
		time.Sleep(1500 * time.Millisecond)
	}
	os.Exit(1)
}

func runApp() {
	// 장기 실행 시 조각난 힙을 주기적으로 OS에 반환 (RSS 상승 억제)
	go func() {
		for range time.Tick(5 * time.Minute) {
			debug.FreeOSMemory()
		}
	}()
	st := newState(fileConfigStore{dir: configDir()}, shellEvents{}, detectWifiDevice())
	startEventLoop(st)
	AppRun(resolveUIDir())
}

// 앱 준비 완료(ObjC applicationDidFinishLaunching) → 오브 패널 생성 + 루프 시작
func onAppReady(st *AppState) {
	id := st.nextPanelID()
	registerPanel(id, "orb")
	st.orbPanel.Store(id)
	if useNativeUI() {
		registerPanelKind(id, "orb", true) // 네이티브 팬: orb-toggle 등 이름 이벤트로 수신
		PanelCreateNative(id, "orb", 1082, 600, 380, 300, orbLevel, true, false)
	} else {
		registerPanel(id, "orb")
		PanelCreate(id, "orb", shimFor("orb"), 1082, 600, 380, 300, true, orbLevel, false)
	}
	// 전체화면 스페이스 합류용 앱 활성화 — 시작 시 1회만 (호버 유실 버그 방지)
	AppActivate()
	lang := st.lang()
	notify(
		tr(lang, "dock-util 시작", "dock-util started"),
		tr(lang, "독 옆 글래스 바가 실행되었습니다", "Glass bar is running"),
	)

	go placementLoop(st)
	go orbLoop(st)
	go pomodoroTickLoop(st)
	go runSystemMonitor(st.events)
	go agentsLoop(st)

	// Claude/ChatGPT 앱을 백그라운드로 미리 기동 (-g: 포커스 가져가지 않음).
	// AI 앱 팝오버의 ▶는 이후 `open -a`로 전면 활성화만 하면 즉시 뜬다.
	go func() {
		time.Sleep(3 * time.Second)
		for _, app := range []string{"Claude", "ChatGPT"} {
			if appInstalled(app + ".app") {
				_ = exec.Command("open", "-g", "-a", app).Start()
			}
		}
	}()

	// 개발용: DU_DEBUG=1 → 오브 웹뷰 상태를 3초마다 덤프, 6틱째 첫 서클 DOM 클릭
	if os.Getenv("DU_DEBUG") != "" {
		orb := id
		go func() {
			clicked := false
			for i := 0; i < 16; i++ {
				time.Sleep(3 * time.Second)
				if i == 7 {
					println("[debug] 오브 숨김 토글 (hide)")
					_, _ = cmdToggleOrbHidden(st, Args{}, 0)
				}
				if i == 10 {
					println("[debug] 오브 표시 토글 (show)")
					_, _ = cmdToggleOrbHidden(st, Args{}, 0)
				}
				if !clicked && i == 5 {
					clicked = true
					println("[debug] 첫 서클 DOM 클릭 발화")
					EvalJS(orb, `(()=>{const el=document.querySelectorAll('.orb-item')[0]; if(el) el.click();})()`)
				}
				// 오브 상태는 항상 덤프 (idle 진입 확인 포함)
				DebugEval(orb, `JSON.stringify({clicks: window.__duClicks||0, invokes: window.__invokes||0, idle: document.body.classList.contains('idle'), items: document.querySelectorAll('.orb-item').length})`)
				// 열려 있는 팝오버의 브랜드 이미지 로딩 상태 + 타이머 배선 검증
				regMu.Lock()
				pids := make([]int64, 0, len(reg))
				for pid := range reg {
					if pid != int64(st.orbPanel.Load()) {
						pids = append(pids, pid)
					}
				}
				regMu.Unlock()
				isTimerPop := os.Getenv("DU_DEBUG_POPOVER") == "timer"
				for _, pid := range pids {
					if isTimerPop && i == 2 {
						EvalJS(pid, `(()=>{const el=document.querySelector('[data-act="mode_timer"]'); if(el) el.click();})()`)
					}
					if isTimerPop && i == 3 {
						EvalJS(pid, `(()=>{const el=document.querySelector('[data-act="tmr_start"]'); if(el) el.click();})()`)
					}
					DebugEval(pid, `JSON.stringify({brandImgs: [...document.querySelectorAll('img.brand-img')].map(i => i.naturalWidth), mode: (typeof localTimer !== "undefined") ? localTimer.mode : null, running: (typeof localTimer !== "undefined") ? localTimer.running : null})`)
				}
			}
		}()
	}

	// 개발용: DU_DEBUG_POPOVER=<위젯> 지정 시 2초 후 팝오버를 열어 검증
	if w := os.Getenv("DU_DEBUG_POPOVER"); w != "" {
		go func() {
			time.Sleep(2 * time.Second)
			st.popoverMu.Lock()
			st.popover = ""
			st.popoverMu.Unlock()
			if _, err := cmdOpenPopover(st, Args{"widget": w, "tileX": float64(orbCX)}, 0); err != nil {
				println("[debug-popover]", err.Error())
			} else {
				println("[debug-popover] opened:", w)
			}
		}()
	}
}

// 진단 모드: 접근성 신뢰 + 독 프레임 실측 (--check)
func runCheck() {
	trusted := AxTrusted()
	dock, _ := dockFrameCached()
	w, h := MainScreen()
	mx, my := MouseLocation()
	fmt.Printf("ax_trusted=%v\n", trusted)
	fmt.Printf("dock_frame=%+v\n", dock)
	fmt.Printf("screen=%.0fx%.0f\n", w, h)
	fmt.Printf("mouse=(%.0f,%.0f)\n", mx, my)
	n := ScreenCount()
	fmt.Printf("screens=%d\n", n)
	for i := 0; i < n; i++ {
		if r, ok := ScreenAt(i); ok {
			fmt.Printf("  [%d] %+v\n", i, r)
		}
	}
}

// --fskey: 전체화면 오버레이 검증용. Finder 활성화 → cmd+N(새 창) → 마우스를 주
// 화면으로 → ctrl+cmd+F(전체화면 토글). AX 신뢰 바이너리에서만 동작.
func runFsKey() {
	if out, err := exec.Command("osascript", "-e", `tell application "Finder" to activate`).CombinedOutput(); err != nil {
		fmt.Println("finder activate 실패:", err, string(out))
		os.Exit(1)
	}
	fmt.Println("[1] Finder 활성화됨")
	time.Sleep(600 * time.Millisecond)

	const (
		flagControl = 0x00040000
		flagCommand = 0x00100000
		keyF        = 3  // kVK_ANSI_F
		keyN        = 45 // kVK_ANSI_N
	)
	PostKey(flagCommand, keyN) // 새 Finder 창
	fmt.Println("[2] cmd+N — 새 Finder 창 생성")
	time.Sleep(900 * time.Millisecond)

	// 마우스를 주 디스플레이 중앙으로 이동 → 오브가 주 화면을 따라간다
	w, h := MainScreen()
	for i := 1; i <= 5; i++ {
		t := float64(i) / 5.0
		PostMouseMove(w/2*t, h/2*t)
		time.Sleep(60 * time.Millisecond)
	}
	time.Sleep(1600 * time.Millisecond) // 배치 틱(0.7s) 적용 대기

	PostKey(flagControl|flagCommand, keyF)
	fmt.Println("[3] ctrl+cmd+F 전송됨 — 3초 후 종료")
	time.Sleep(3 * time.Second)
}

// --qa: 주 화면 오브 좌표 기준 호버+클릭 자동 테스트 (CG 좌표계)
func runQA() {
	// 현재 오브 프레임을 배치 규칙으로 계산 (CG 좌표, 위쪽 원점)
	w, h := MainScreen()
	cx := w - orbW - orbEdgeMargin + orbCX   // 오브 중심 X
	cyTopLeft := h - orbBottomMargin - orbCY // 오브 중심 Y (CG)
	fmt.Printf("[1] 오브 중심 (%.0f, %.0f)으로 이동\n", cx, cyTopLeft)
	for i := 1; i <= 10; i++ {
		t := float64(i) / 10.0
		PostMouseMove(w/2+(cx-w/2)*t, h/2+(cyTopLeft-h/2)*t)
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(1200 * time.Millisecond) // 확장

	// 터미널 동그라미: 패널 로컬 CSS (322, 150) = CG (panel.x+322, panel.y+150)
	px := w - orbW - orbEdgeMargin
	py := h - (orbBottomMargin + orbH)
	coin := 97.0 * math.Pi / 180.0
	circleX := px + orbCX + 115*math.Cos(coin)
	circleY := py + (orbH - orbCY) + 115*(-math.Sin(coin))
	fmt.Printf("[2] 커서를 서클로 이동 (%.0f, %.0f)\n", circleX, circleY)
	for i := 1; i <= 5; i++ {
		t := float64(i) / 5.0
		PostMouseMove(cx+(circleX-cx)*t, cyTopLeft+(circleY-cyTopLeft)*t)
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("[3] 세션 탭 클릭 (%.0f, %.0f)\n", circleX, circleY)
	PostMouseClick(circleX, circleY)
	time.Sleep(1200 * time.Millisecond)
	fmt.Println("[4] HID 탭 클릭")
	PostMouseClickHID(circleX, circleY)
	time.Sleep(1500 * time.Millisecond)
	fmt.Println("[5] QA2 완료")
}
