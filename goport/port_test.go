package main

// 단위테스트: Rust 버전의 테스트를 Go로 포트

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- placement ----------

func TestOrbPinsBottomRightOfTargetScreen(t *testing.T) {
	r := orbCompute(Rect{X: 0, Y: 0, W: 1470, H: 956})
	want := Rect{X: 1470 - orbW - orbEdgeMargin, Y: orbBottomMargin, W: orbW, H: orbH}
	if r != want {
		t.Fatalf("got %+v want %+v", r, want)
	}
}

func TestOrbFollowsSecondScreen(t *testing.T) {
	r := orbCompute(Rect{X: 1470, Y: 0, W: 1920, H: 1080})
	want := Rect{X: 1470 + 1920 - orbW - orbEdgeMargin, Y: orbBottomMargin, W: orbW, H: orbH}
	if r != want {
		t.Fatalf("got %+v want %+v", r, want)
	}
}

func TestOrbHotZoneAndWindowBounds(t *testing.T) {
	rect := Rect{X: 1082, Y: 6, W: orbW, H: orbH}
	cx := rect.X + orbCX
	cy := rect.Y + orbCY
	if !orbInHot(rect, cx, cy) {
		t.Fatal("center should be hot")
	}
	if !orbInHot(rect, cx-50, cy+40) {
		t.Fatal("(50,40) should be hot (r≈64)")
	}
	if orbInHot(rect, cx-100, cy+100) {
		t.Fatal("(100,100) should not be hot (r≈141)")
	}
	if !orbInWindow(rect, cx-100, cy+100) {
		t.Fatal("should be in window")
	}
	if orbInWindow(rect, rect.X-1, cy) {
		t.Fatal("left of window")
	}
	if orbInWindow(rect, cx, rect.Y+rect.H+1) {
		t.Fatal("above window")
	}
}

// ---------- pomodoro ----------

func TestIdleSnapshotShowsConfiguredFocusTime(t *testing.T) {
	p := PomodoroState{phase: PhaseIdle, focusSecs: 25 * 60, breakSecs: 5 * 60}
	s := p.snapshot()
	if s.Phase != PhaseIdle || s.RemainingSecs != 1500 || s.Running {
		t.Fatalf("got %+v", s)
	}
}

func TestPausedSnapshotFreezesRemaining(t *testing.T) {
	p := PomodoroState{phase: PhaseFocus, running: true, paused: true, remaining: 602 * time.Second, focusSecs: 1500, breakSecs: 300}
	s := p.snapshot()
	if !s.Paused || s.RemainingSecs != 602 {
		t.Fatalf("got %+v", s)
	}
}

func TestRunningSnapshotCountsDown(t *testing.T) {
	p := PomodoroState{phase: PhaseFocus, running: true, endsAt: time.Now().Add(120 * time.Second), hasEndsAt: true, focusSecs: 1500, breakSecs: 300}
	s := p.snapshot()
	if s.RemainingSecs < 100 || s.RemainingSecs > 120 {
		t.Fatalf("got %d", s.RemainingSecs)
	}
}

func TestPersistRoundtripRestoresRunningTimer(t *testing.T) {
	p := PomodoroState{phase: PhaseFocus, running: true, endsAt: time.Now().Add(120 * time.Second), hasEndsAt: true, focusSecs: 1500, breakSecs: 300, rounds: 2}
	restored := PomodoroState{}
	pp := p.persisted()
	restored.restoreFrom(pp)
	if !restored.running || restored.paused {
		t.Fatal("should be running")
	}
	if restored.rounds != 2 {
		t.Fatalf("rounds=%d", restored.rounds)
	}
	s := restored.snapshot()
	if s.RemainingSecs < 100 || s.RemainingSecs > 120 {
		t.Fatalf("remaining=%d", s.RemainingSecs)
	}
}

func TestRestoreDiscardsExpiredTimer(t *testing.T) {
	p := PomodoroState{phase: PhaseFocus, running: true, endsAt: time.Now().Add(120 * time.Second), hasEndsAt: true, focusSecs: 1500, breakSecs: 300}
	pp := p.persisted()
	past := nowMs() - 10_000
	pp.EndsAtMs = &past
	restored := PomodoroState{}
	restored.restoreFrom(pp)
	// 앱이 꺼진 동안 만료된 포모는 무음으로 정지 (좀비 알림 루프 방지)
	if restored.running || restored.phase != PhaseIdle {
		t.Fatalf("만료 포모는 Idle로 정지해야 함: phase=%s running=%v", restored.phase, restored.running)
	}
}

// ---------- agents ----------

func TestDetectsDirectProcessNames(t *testing.T) {
	if classify("claude", "") != "claude" || classify("codex", "") != "codex" || classify("gemini-3.2", "") != "gemini" {
		t.Fatal("direct names not detected")
	}
}

func TestDetectsNodeWrappersViaCmdPath(t *testing.T) {
	if classify("node", "/usr/local/bin/codex exec --full-auto") != "codex" {
		t.Fatal("codex wrapper")
	}
	if classify("node", "/opt/homebrew/bin/claude") != "claude" {
		t.Fatal("claude wrapper")
	}
}

func TestIgnoresUnrelatedProcesses(t *testing.T) {
	if classify("safari", "/Applications/Safari.app") != "" {
		t.Fatal("safari should not match")
	}
	if classify("dock-util", "target/debug/dock-util") != "" {
		t.Fatal("self should not match")
	}
}

// ---------- formatter ----------

func TestJSONPrettyAndMinify(t *testing.T) {
	out, err := formatText(`{"b":2,"a":[1,2]}`, "json", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\"a\": [\n") && !strings.Contains(out, "\"a\": [") {
		t.Fatalf("pretty output: %q", out)
	}
	min, err := formatText(out, "json", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if min != `{"a":[1,2],"b":2}` && min != `{"b":2,"a":[1,2]}` {
		t.Fatalf("minified: %q", min)
	}
}

func TestJSONToYAMLAndBack(t *testing.T) {
	yamlOut, err := formatText(`{"a":1,"b":[1,2]}`, "json", true, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(yamlOut, "a: 1") {
		t.Fatalf("yaml: %q", yamlOut)
	}
	jsonOut, err := formatText(yamlOut, "yaml", true, "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOut, `"b"`) {
		t.Fatalf("json: %q", jsonOut)
	}
}

func TestTOMLToJSON(t *testing.T) {
	out, err := formatText("a = 1\n[b]\nc = \"x\"\n", "toml", true, "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\"x\"") {
		t.Fatalf("json: %q", out)
	}
}

func TestInvalidInputReportsParseError(t *testing.T) {
	_, err := formatText("{nope", "json", true, "")
	if err == nil || !strings.Contains(err.Error(), "JSON 파싱 실패") {
		t.Fatalf("err=%v", err)
	}
}

// ---------- i18n ----------

func TestFmtTrReplacesInOrder(t *testing.T) {
	got := fmtTr("ko", "{}개 성공 {}개 실패", "{} ok {} failed", "3", "1")
	if got != "3개 성공 1개 실패" {
		t.Fatalf("got %q", got)
	}
}

func TestAutoDetectTOML(t *testing.T) {
	out, err := formatText("answer = 42", "auto", false, "json")
	if err != nil || out != `{"answer":42}` {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestJSONPreservesLargeInteger(t *testing.T) {
	const input = `{"id":9007199254740993}`
	for _, format := range []string{"json", "auto"} {
		out, err := formatText(input, format, false, "json")
		if err != nil || out != input {
			t.Fatalf("%s: got %q, %v", format, out, err)
		}
	}
}

func TestOrbCircleUsesAppKitCoordinates(t *testing.T) {
	rect := Rect{X: 100, Y: 200, W: orbW, H: orbH}
	x, y := orbCircleCenter(rect, 0, 11)
	if math.Abs(x-421.985) < 0.01 && math.Abs(y-350.143) < 0.01 {
		return
	}
	t.Fatalf("first visible circle = (%f,%f), want (421.985,350.143)", x, y)
}

func TestOrbSingleCircleUsesRingMidpoint(t *testing.T) {
	x, y := orbCircleCenter(Rect{}, 0, 1)
	if math.Abs(x-255.395) > 0.01 || math.Abs(y-118.024) > 0.01 {
		t.Fatalf("single circle = (%f,%f), want (255.395,118.024)", x, y)
	}
}

func TestConfigSnapshotDoesNotShareWidgetStorage(t *testing.T) {
	st := &AppState{cfg: Config{HiddenWidgets: []string{"terminal", "feed"}}}
	snap := st.configSnapshot()
	snap.HiddenWidgets[0] = "monitor"
	if st.configSnapshot().HiddenWidgets[0] != "terminal" {
		t.Fatal("snapshot mutated live config")
	}
}

func TestMoveFileDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.WriteFile(src, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err == nil {
		t.Error("overwriting an existing destination must fail")
	}
	for path, want := range map[string]string{src: "source", dst: "keep"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Errorf("%s = %q, %v", path, data, err)
		}
	}
}

func TestMoveDirectoryFailureDoesNotCreateFile(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "source", "child")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err == nil {
		t.Fatal("move into self must fail")
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("failed move left destination: %v", err)
	}
}

func TestOrbCollapseWaitsOutsideWindow(t *testing.T) {
	now := time.Now()
	var leftAt time.Time
	if orbShouldCollapse(true, false, &leftAt, now) {
		t.Fatal("collapsed immediately")
	}
	if orbShouldCollapse(true, false, &leftAt, now.Add(249*time.Millisecond)) {
		t.Fatal("collapsed too early")
	}
	if !orbShouldCollapse(true, false, &leftAt, now.Add(250*time.Millisecond)) {
		t.Fatal("did not collapse")
	}
	if orbShouldCollapse(true, true, &leftAt, now.Add(time.Second)) || !leftAt.IsZero() {
		t.Fatal("reentry did not cancel collapse")
	}
}
