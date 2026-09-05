package main

// 플랫폼 유틸: osascript, 셸 명령, Dock PID 캐시, 알림 (Rust platform.rs 유틸부 포트)

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

func nowMs() int64 {
	return time.Now().UnixMilli()
}

// 접근성 권한이 필요한 모든 경로에서 공용으로 쓰는 osascript 래퍼
func osascript(script string) (string, error) {
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func notify(title, body string) {
	esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	_, _ = osascript(`display notification "` + esc(body) + `" with title "` + esc(title) + `"`)
}

func shellOutput(cmd string, args ...string) (string, bool) {
	c := exec.Command(cmd, args...)
	c.Env = envWithPath()
	out, err := c.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// GUI 실행 시 PATH가 /usr/bin:/bin 으로만 나와 brew/npm 설치 CLI를 못 찾는다.
// 흔한 설치 경로를 한 번 보강해서 모든 자식 프로세스에 적용한다.
var (
	pathOnce sync.Once
	augPath  string
)

func commandPath() string {
	pathOnce.Do(func() {
		home, _ := os.UserHomeDir()
		path := os.Getenv("PATH")
		for _, d := range []string{
			"/opt/homebrew/bin", "/opt/homebrew/sbin",
			"/usr/local/bin", "/usr/local/sbin",
			home + "/.local/bin", home + "/bin",
		} {
			if !strings.Contains(path, d) {
				path += ":" + d
			}
		}
		augPath = path
	})
	return augPath
}

func envWithPath() []string {
	return append(os.Environ(), "PATH="+commandPath())
}

// 외부 명령 존재 여부 (보강된 PATH로 quiet exit 체크)
func commandExists(cmd string) bool {
	c := exec.Command("sh", "-c", "command -v "+cmd)
	c.Env = envWithPath()
	return c.Run() == nil
}

func execOpen(args ...string) error {
	return exec.Command("open", args...).Start()
}

// 앱 설치 여부: /Applications, ~/Applications, /System/Applications 순서 확인
func appInstalled(name string) bool {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", home + "/Applications", "/System/Applications"} {
		if _, err := os.Stat(dir + "/" + name); err == nil {
			return true
		}
	}
	return false
}

// Dock PID 캐시 (전체 스캔은 pgrep 1회/실패 시 재시도)
var (
	dockPidMu    sync.Mutex
	dockPidCache = -1
	dockPidFails int
)

func dockPid() int {
	dockPidMu.Lock()
	defer dockPidMu.Unlock()
	if dockPidCache > 0 {
		return dockPidCache
	}
	out, err := exec.Command("pgrep", "-x", "Dock").Output()
	if err != nil {
		return -1
	}
	pid := 0
	for _, c := range strings.TrimSpace(string(out)) {
		if c < '0' || c > '9' {
			break
		}
		pid = pid*10 + int(c-'0')
	}
	if pid > 0 {
		dockPidCache = pid
		dockPidFails = 0
	}
	return pid
}

// 독 프레임: 성공 시 실패 카운터 리셋, 연속 실패 5회면 PID 캐시 폐기
func dockFrameCached() (Rect, bool) {
	pid := dockPid()
	if pid <= 0 {
		return Rect{}, false
	}
	r, ok := DockFrame(pid)
	dockPidMu.Lock()
	if ok {
		dockPidFails = 0
	} else {
		dockPidFails++
		if dockPidFails >= 5 {
			dockPidCache = -1
		}
	}
	dockPidMu.Unlock()
	return r, ok
}
