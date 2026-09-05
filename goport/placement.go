package main

// 배치 엔진: 오브(동그라미)를 마우스가 있는 화면의 우하단에 붙인다.
// AppKit 좌표(좌하단 원점) 기준. (Rust placement.rs 포트)

import (
	"math"
	"time"
)

const (
	// 창 레벨: 전체화면 창은 내부적으로 높은 레벨에서 합성되므로 그 위로 띄운다
	orbLevel     = 2000
	popoverLevel = 2001

	orbBottomMargin = 6.0
	orbEdgeMargin   = 8.0 // 화면 가장자리 여백
	orbW            = 380.0
	orbH            = 300.0
	// 윈도우 로컬 오브 중심 (AppKit 좌하단 원점): 우하단에 배치
	orbCX = 336.0
	orbCY = 36.0
	// 마우스가 이 반경 안으로 들어오면 펼침
	orbHotR = 85.0
)

// 오브 호버 판정: 마우스가 오브 중심 핫존 안인지
func orbInHot(rect Rect, mx, my float64) bool {
	cx := rect.X + orbCX
	cy := rect.Y + orbCY
	return (mx-cx)*(mx-cx)+(my-cy)*(my-cy) <= orbHotR*orbHotR
}

// 마우스가 오브 윈도우(확장 팬 영역) 안에 있는지
func orbInWindow(rect Rect, mx, my float64) bool {
	return mx >= rect.X && mx <= rect.X+rect.W && my >= rect.Y && my <= rect.Y+rect.H
}

// 오브 배치: 대상 화면(마우스가 있는 화면) 우하단 고정
func orbCompute(screen Rect) Rect {
	return Rect{
		X: screen.X + screen.W - orbW - orbEdgeMargin,
		Y: screen.Y + orbBottomMargin,
		W: orbW,
		H: orbH,
	}
}

func screenContainingMouse() (Rect, bool) {
	mx, my := MouseLocation()
	return screenFrameAt(mx, my)
}

func screenFrameAt(x, y float64) (Rect, bool) {
	n := ScreenCount()
	for i := 0; i < n; i++ {
		if r, ok := ScreenAt(i); ok {
			if x >= r.X && x <= r.X+r.W && y >= r.Y && y <= r.Y+r.H {
				return r, true
			}
		}
	}
	return Rect{}, false
}

func applyNow(st *AppState) {
	screen, ok := screenContainingMouse()
	if !ok {
		w, h := MainScreen()
		screen = Rect{X: 0, Y: 0, W: w, H: h}
	}
	_, screenChanged := st.updateTargetScreen(screen)
	rect := orbCompute(screen)
	st.setOrbRect(rect)
	// 숨김 중이면 창을 다시 보이게 하지 않는다
	if !st.orbHidden.Load() {
		if id := st.orbPanel.Load(); id != 0 {
			// 전체화면/스페이스 전환 후에도 유지되도록 매 틱 패널에 적용 (메인 큐)
			PanelApply(int64(id), rect, orbLevel)
		}
	}
	// 커서가 다른 화면으로 넘어가면 열려 있는 팝오버도 오브를 따라간다
	if screenChanged {
		st.followPopoverToScreen(screen)
	}
}

// 오브의 네이티브 팬 배치(JS orbLayout 포트): 위젯 순서 + 링 배치
func orbVisibleWidgets(hidden map[string]bool) []string {
	order := []string{"terminal", "ai_term", "timer", "shelf", "monitor", "toggles", "agents", "ai_apps", "format", "feed"}
	var out []string
	for _, w := range order {
		if !hidden[w] {
			out = append(out, w)
		}
	}
	return append(out, "settings") // 설정은 항상 표시
}

// 인덱스 i번째 서클의 중심 (화면 AppKit 좌표)
func orbCircleCenter(rect Rect, i, count int) (float64, float64) {
	rings := []struct {
		r, from, to float64
		cap         int
	}{{115, 97, 172, 4}, {185, 172, 97, 4}, {240, 97, 172, 3}}
	geoX, geoY := orbCX, orbCY
	idx := 0
	for _, ring := range rings {
		if idx >= count {
			break
		}
		n := ring.cap
		if remain := count - idx; remain < n {
			n = remain
		}
		for j := 0; j < n; j++ {
			if idx == i {
				deg := (ring.from + ring.to) / 2
				if n > 1 {
					deg = ring.from + (ring.to-ring.from)*float64(j)/float64(n-1)
				}
				rad := deg * math.Pi / 180
				return rect.X + geoX + ring.r*math.Cos(rad),
					rect.Y + geoY + ring.r*math.Sin(rad)
			}
			idx++
		}
	}
	return 0, 0
}

// 호버 루프가 소유하며 설정 변경 시에만 기하를 다시 계산한다.
type orbHitGeometry struct {
	revision uint64
	valid    bool
	centers  [][2]float64
}

func (g *orbHitGeometry) refresh(st *AppState) {
	revision := st.cfgRevision.Load()
	if g.valid && g.revision == revision {
		return
	}
	cfg := st.configSnapshot()
	hidden := make(map[string]bool, len(cfg.HiddenWidgets))
	for _, widget := range cfg.HiddenWidgets {
		hidden[widget] = true
	}
	widgets := orbVisibleWidgets(hidden)
	g.centers = g.centers[:0]
	for i := range widgets {
		x, y := orbCircleCenter(Rect{}, i, len(widgets))
		g.centers = append(g.centers, [2]float64{x, y})
	}
	g.revision, g.valid = revision, true
}

// 열려 있는 팝오버를 오브가 있는 화면으로 재배치 (열 때의 앵커 유지)
func (st *AppState) followPopoverToScreen(screen Rect) {
	st.popoverMu.Lock()
	label, widget, tileX := st.popover, st.popWidget, st.popTileX
	st.popoverMu.Unlock()
	if label == "" || widget == "" {
		return
	}
	id, ok := idByLabel(label)
	if !ok {
		return
	}
	orbRect, hasOrb := st.getOrbRect()
	if !hasOrb {
		return
	}
	w, h := popoverSize(widget)
	var x, y float64
	if hasOrb {
		center := orbRect.X + tileX
		x = center - w/2
		if x < screen.X+8 {
			x = screen.X + 8
		}
		if x > screen.X+screen.W-w-8 {
			x = screen.X + screen.W - w - 8
		}
		y = orbRect.Y + 96
	} else {
		x = screen.X + (screen.W-w)/2
		y = screen.Y + 90
	}
	PanelApply(id, Rect{X: x, Y: y, W: w, H: h}, popoverLevel)
}

// 배치 폴링: 커서 화면 이동/디스플레이 구성 변경 시 오브 위치 갱신
func placementLoop(st *AppState) {
	for {
		applyNow(st)
		interval := time.Duration(1500 * time.Millisecond)
		if ScreenCount() > 1 {
			interval = 700 * time.Millisecond
		}
		time.Sleep(interval)
	}
}

// 오브 호버 폴링(25ms): 마우스가 오브에 접근하면 입력을 켜고 펼침,
// 팬 영역을 250ms 이상 벗어나면 접는다.
// 커서가 팬에서 45초 이상 떨어져 있으면 GPU 펄스 애니메이션을 정지시킨다.
func orbLoop(st *AppState) {
	var leftAt time.Time
	var geometry orbHitGeometry
	var idleSince time.Time
	lastOver := false
	idleActive := false
	setIdle := func(v bool) {
		if idleActive == v {
			return
		}
		idleActive = v
		if orbID := st.orbPanel.Load(); orbID != 0 {
			emitTo(int64(orbID), "orb-idle", v)
		}
	}
	for {
		if st.orbHidden.Load() {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if st.orbExpanded.Load() {
			time.Sleep(25 * time.Millisecond)
		} else {
			time.Sleep(60 * time.Millisecond)
		}
		rect, ok := st.getOrbRect()
		if !ok {
			continue
		}
		mx, my := MouseLocation()
		expanded := st.orbExpanded.Load()

		// 정밀 클릭 패스스루: 그려진 동그라미 위에서만 클릭을 받고,
		// 빈 영역은 아래 앱으로 클릭이 통과된다
		over := false
		{
			const clickR = 26.0
			if !expanded {
				ccx := rect.X + orbCX
				ccy := rect.Y + orbCY
				dx, dy := mx-ccx, my-ccy
				over = dx*dx+dy*dy <= clickR*clickR
			} else {
				geometry.refresh(st)
				for _, center := range geometry.centers {
					ccx, ccy := rect.X+center[0], rect.Y+center[1]
					dx, dy := mx-ccx, my-ccy
					if dx*dx+dy*dy <= clickR*clickR {
						over = true
						break
					}
				}
			}
		}
		if over != lastOver {
			lastOver = over
			if id := st.orbPanel.Load(); id != 0 {
				PanelSetIgnores(id, !over)
			}
		}

		// 유휴 감지: 접힌 상태에서 커서가 팬 영역 밖이면 펄스를 멈춘다
		if expanded || orbInWindow(rect, mx, my) {
			idleSince = time.Time{}
			setIdle(false)
		} else if idleActive {
			// 유지
		} else {
			if idleSince.IsZero() {
				idleSince = time.Now()
			} else if time.Since(idleSince) >= 45*time.Second {
				setIdle(true)
			}
		}

		wantExpand := !expanded && orbInHot(rect, mx, my)
		wantCollapse := orbShouldCollapse(expanded, orbInWindow(rect, mx, my), &leftAt, time.Now())
		if !wantExpand && !wantCollapse {
			continue
		}
		expandNow := wantExpand
		if id := st.orbPanel.Load(); id != 0 {
			PanelSetIgnores(int64(id), !expandNow)
		}
		if expandNow {
			// 팝오버가 열려 있는 상태로 오브를 다시 호버하면 열려 있던 것을 닫는다
			st.popoverMu.Lock()
			hadPop := st.popover != ""
			st.popoverMu.Unlock()
			if hadPop {
				closeCurrentPopover(st)
			}
		}
		if orbID := st.orbPanel.Load(); orbID != 0 {
			emitTo(int64(orbID), "orb-toggle", expandNow)
		}
		st.orbExpanded.Store(expandNow)
	}
}

// 활성화 복귀 (예비): activHold는 현재 미사용 — 팝오버가 열려 있거나 오브가
// 펼쳐져 있으면 원래 앱 복귀를 보류한다
func releaseActivationHold(st *AppState) {
	st.popoverMu.Lock()
	hold := st.activHold
	st.activHold = ""
	busy := st.popover != "" || st.orbExpanded.Load()
	st.popoverMu.Unlock()
	if hold != "" && !busy {
		ActivateBundle(hold)
	}
}

func orbShouldCollapse(expanded, inside bool, leftAt *time.Time, now time.Time) bool {
	if !expanded || inside {
		*leftAt = time.Time{}
		return false
	}
	if leftAt.IsZero() {
		*leftAt = now
		return false
	}
	return now.Sub(*leftAt) >= 250*time.Millisecond
}
