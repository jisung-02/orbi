package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unicode/utf8"
)

// 한글이 청크 경계에서 잘려도 emit이 항상 완전한 룬들로 끝나는지 검증
func TestSplitUTF8TailKorean(t *testing.T) {
	full := []byte("안녕하세요 exit\r\n") // 모든 한글 = 3바이트 UTF-8
	for split := 1; split < len(full); split++ {
		emit, carry := splitUTF8Tail(full[:split])
		merged := append(append([]byte(nil), emit...), carry...)
		if !bytes.Equal(merged, full[:split]) {
			t.Fatalf("split %d: merged != original", split)
		}
		if len(emit) > 0 && !utf8.Valid(emit) {
			t.Fatalf("split %d: emit에 불완전한 룬 포함", split)
		}
		if len(carry) > 3 {
			t.Fatalf("split %d: carry가 3바이트 초과", split)
		}
	}
}

func TestSplitUTF8TailCarryContent(t *testing.T) {
	// "안" = EC 95 88 — 1~2바이트만 있으면 전부 이월되어야 한다
	for split := 1; split <= 2; split++ {
		emit, carry := splitUTF8Tail([]byte("안녕")[:split])
		if len(emit) != 0 {
			t.Fatalf("split %d: emit이 비어야 함 (%d)", split, len(emit))
		}
		if string(carry) != "안녕"[:split] {
			t.Fatalf("split %d: carry %q", split, carry)
		}
	}
	// 3바이트 모두 도착하면 이월 없음
	emit, carry := splitUTF8Tail([]byte("안"))
	if len(emit) != 3 || len(carry) != 0 {
		t.Fatalf("3바이트 완성: emit=%q carry=%q", emit, carry)
	}
}

func TestTermResetStopsSessionWorkers(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("DU_UI", "web")
	for i := 0; i < 3; i++ {
		if err := termEnsure(); err != nil {
			t.Fatal(err)
		}
		termReset()
	}
	deadline := time.Now().Add(time.Second)
	for {
		stack := make([]byte, 128*1024)
		stack = stack[:runtime.Stack(stack, true)]
		if !bytes.Contains(stack, []byte("dockutil.termEnsure.func")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reset left terminal session workers running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTermExitDrainsFinalOutput(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf final\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("DU_UI", "web")
	registerPanel(999, "popover-terminal")
	defer unregisterPanel(999)
	if err := termEnsure(); err != nil {
		t.Fatal(err)
	}
	defer termReset()
	term.mu.Lock()
	sess := term.session
	term.mu.Unlock()
	select {
	case <-sess.done:
	case <-time.After(time.Second):
		t.Fatal("shell did not exit")
	}
	time.Sleep(100 * time.Millisecond)
	sess.pendMu.Lock()
	remaining := string(sess.pending)
	sess.pendMu.Unlock()
	if remaining != "" {
		t.Fatalf("final output never drained: %q", remaining)
	}
}

func TestTerminalReplayConsumesPendingWithoutLosingHistory(t *testing.T) {
	termReset()
	sess := &TermSession{scrollback: []byte("history\x1b[6n"), pending: []byte("history\x1b[6n")}
	term.mu.Lock()
	term.session = sess
	term.mu.Unlock()
	defer func() { term.mu.Lock(); term.session = nil; term.mu.Unlock() }()
	if err := termEnsureForPanel(999999); err != nil {
		t.Fatal(err)
	}
	sess.pendMu.Lock()
	pending := len(sess.pending)
	sess.pendMu.Unlock()
	if pending != 0 {
		t.Fatal("history would be sent again as live output")
	}
	if history, ok := termScrollback(); !ok || history != "history\x1b[6n" {
		t.Fatal("history was lost during replay")
	}
}
