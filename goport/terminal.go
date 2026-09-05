package main

// 터미널 위젯: creack/pty로 사용자 셸을 띄우고 xterm.js와 연결한다.
// 세션은 팝오버를 닫아도 유지되며, 닫힌 동안의 출력은 스크롤백에 보관되어
// 다시 열 때 한 번에 내려준다. (Rust widgets/terminal.rs 포트)
//
// PTY reader → scrollback(128KB cap) + pending(64KB cap)
// → 플러셔(50ms) → 팝오버 열려있으면 emit (최대 32KB/전송)

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
)

const (
	scrollbackCap   = 128 * 1024
	pendingCap      = 64 * 1024
	drainBatch      = 16 * 1024
	flushIntervalMs = 50
	maxChunk        = 32 * 1024
)

type TermSession struct {
	outputReady chan struct{}
	done        chan struct{}
	stopOnce    sync.Once
	master      *os.File
	cmd         *exec.Cmd
	sbMu        sync.Mutex
	scrollback  []byte
	pendMu      sync.Mutex
	pending     []byte
}

type termGuard struct {
	mu      sync.Mutex
	session *TermSession
}

var term = &termGuard{}

// 세션이 없으면 새로 띄운다. 셸은 $SHELL(기본 zsh) 로그인 셸, 홈 디렉터리에서 시작.
func termEnsure() error { return termEnsureForPanel(0) }

func termEnsureForPanel(panelID int64) error {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.session != nil {
		if panelID != 0 {
			sess := term.session
			sess.sbMu.Lock()
			sess.pendMu.Lock()
			history := string(sess.scrollback)
			sess.pending = nil
			sess.pendMu.Unlock()
			sess.sbMu.Unlock()
			// Queue history before the flusher can publish newer output.
			emitTo(panelID, "term-history", history)
		}
		return nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.Command(shell, "-l")
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	// GUI 실행 시 TERM/COLORTERM이 없어 색상이 꺼진다 — 256색+트루컬러 강제.
	// 부모 env에 TERM이 이미 있어도 우리 값이 이기도록 기존 키는 제거 후 추가.
	env := os.Environ()
	kept := env[:0]
	hasLang := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "TERM=") || strings.HasPrefix(kv, "COLORTERM=") {
			continue
		}
		if strings.HasPrefix(kv, "LANG=") {
			hasLang = true
		}
		kept = append(kept, kv)
	}
	if !hasLang {
		kept = append(kept, "LANG=ko_KR.UTF-8") // 로케일 없으면 프로그램이 ASCII만 출력
	}
	cmd.Env = append(kept, "TERM=xterm-256color", "COLORTERM=truecolor")
	master, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("셸 실행 실패: %v", err)
	}
	// 초기 크기 (Rust PtySize rows:28 cols:96)
	_ = pty.Setsize(master, &pty.Winsize{Rows: 28, Cols: 96})

	sess := &TermSession{master: master, cmd: cmd, done: make(chan struct{}), outputReady: make(chan struct{}, 1)}
	term.session = sess

	// 리더: PTY 출력 → 스크롤백 + 대기 버퍼 (둘 다 cap 적용)
	go func() {
		defer sess.stop()
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				sess.sbMu.Lock()
				sess.scrollback = append(sess.scrollback, chunk...)
				if len(sess.scrollback) > scrollbackCap+drainBatch {
					over := len(sess.scrollback) - scrollbackCap
					sess.scrollback = append(sess.scrollback[:0], sess.scrollback[over:]...)
				}
				sess.pendMu.Lock()
				sess.pending = append(sess.pending, chunk...)
				if len(sess.pending) > pendingCap+drainBatch {
					over := len(sess.pending) - pendingCap
					sess.pending = append(sess.pending[:0], sess.pending[over:]...)
				}
				sess.pendMu.Unlock()
				sess.sbMu.Unlock()
				select {
				case sess.outputReady <- struct{}{}:
				default:
				}
			}
			if err != nil {
				break
			}
		}

	}()

	flush := func() {
		id, ok := idByLabel("popover-terminal")
		if !ok {
			sess.pendMu.Lock()
			sess.pending = sess.pending[:0]
			sess.pendMu.Unlock()
			return
		}
		sess.pendMu.Lock()
		chunk := sess.pending
		sess.pending = nil
		sess.pendMu.Unlock()
		if len(chunk) == 0 {
			return
		}
		if len(chunk) > maxChunk {
			chunk = chunk[len(chunk)-maxChunk:]
			// 앞쪽 절단이 룬 중간에서 시작하지 않게 정렬
			for i := 0; i < 3 && len(chunk) > 0; i++ {
				if chunk[0]&0xC0 != 0x80 {
					break
				}
				chunk = chunk[1:]
			}
		}
		// 한글(3바이트 UTF-8)이 청크 경계에서 잘리면 다음 전송으로 이월한다 —
		// 잘린 채 보내면 JSON 직렬화에서 U+FFFD로 깨진다
		emit, carry := splitUTF8Tail(chunk)
		if len(carry) > 0 {
			sess.pendMu.Lock()
			sess.pending = append(append([]byte(nil), carry...), sess.pending...)
			sess.pendMu.Unlock()
		}
		if len(emit) == 0 {
			return
		}
		if !utf8.Valid(emit) {
			println("[term] 경고: 불완전한 UTF-8 청크 전송됨")
		}
		emitTo(id, "term-out", string(emit))
	}
	go func() {
		timer := time.NewTimer(time.Duration(flushIntervalMs) * time.Millisecond)
		timer.Stop()
		defer timer.Stop()
		for {
			exited := false
			select {
			case <-sess.done:
				exited = true
			case <-sess.outputReady:
				timer.Reset(time.Duration(flushIntervalMs) * time.Millisecond)
				select {
				case <-sess.done:
					exited = true
				case <-timer.C:
				}
			}
			term.mu.Lock()
			if term.session == sess {
				flush()
				if exited {
					emitToLabel("popover-terminal", "term-exit", true)
				}
			}
			term.mu.Unlock()
			if exited {
				return
			}
		}
	}()

	// 좀비 방지 wait
	go func() { _ = cmd.Wait() }()

	return nil
}

// 보관된 스크롤백 스냅샷 (세션은 유지)
func termScrollback() (string, bool) {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.session == nil {
		return "", false
	}
	term.session.sbMu.Lock()
	defer term.session.sbMu.Unlock()
	return string(term.session.scrollback), true
}

func termWrite(data string) error {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.session == nil {
		return fmt.Errorf("터미널 세션이 없습니다")
	}
	_, err := term.session.master.Write([]byte(data))
	return err
}

func termResize(cols, rows uint16) {
	term.mu.Lock()
	defer term.mu.Unlock()
	if term.session == nil {
		return
	}
	if rows < 4 {
		rows = 4
	}
	if cols < 10 {
		cols = 10
	}
	_ = pty.Setsize(term.session.master, &pty.Winsize{Rows: rows, Cols: cols})
}

// 셸을 종료하고 세션을 정리
func termReset() {
	term.mu.Lock()
	sess := term.session
	term.session = nil
	term.mu.Unlock()
	if sess != nil {
		sess.stop()
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Kill()
		}
		_ = sess.master.Close()
	}
}

// splitUTF8Tail: 청크 끝이 불완전한 UTF-8 시퀀스로 끝나면 (내보낼 부분, 이월할 꼬리)로 나눈다
func splitUTF8Tail(chunk []byte) (emit, carry []byte) {
	cut := len(chunk)
	for i := 1; i <= 3 && i <= cut; i++ {
		b := chunk[cut-i]
		if b&0xC0 == 0x80 {
			continue // 연속 바이트 — 리드 바이트까지 거슬러 올라감
		}
		var seq int
		switch {
		case b&0xE0 == 0xC0:
			seq = 2
		case b&0xF0 == 0xE0:
			seq = 3
		case b&0xF8 == 0xF0:
			seq = 4
		default:
			seq = 1
		}
		if seq > i {
			return chunk[:cut-i], chunk[cut-i:]
		}
		return chunk, nil
	}
	return chunk, nil
}

func (sess *TermSession) stop() { sess.stopOnce.Do(func() { close(sess.done) }) }
