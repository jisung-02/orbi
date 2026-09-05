package main

// 실행중인 AI 에이전트 감지 (10초 주기) — Rust widgets/agents.rs 포트.
// gopsutil은 프로세스마다 개별 시스템콜을 해 수백 프로세스에서 스파이크가 커서,
// `ps` 한 번의 일괄 출력을 파싱한다 (sysinfo의 벌크 리프레시에 해당하는 방식).

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

var agentKeywords = []string{
	"claude", "codex", "gemini", "copilot", "aider", "cline", "cursor-agent",
	"opencode", "goose", "amp", "droid", "crush", "windsurf", "qwen",
}

type AgentInfo struct {
	Pid     int32   `json:"pid"`
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Cpu     float64 `json:"cpu"`
	MemMb   uint64  `json:"mem_mb"`
	Elapsed string  `json:"elapsed"`
}

func elapsedStr(startMs int64) string {
	secs := nowMs()/1000 - startMs/1000
	if secs < 0 {
		secs = 0
	}
	h := secs / 3600
	m := (secs % 3600) / 60
	if h > 0 {
		return itoa(h) + "시간 " + itoa(m) + "분"
	}
	return itoa(m) + "분"
}

// 프로세스 이름/커맨드라인에서 에이전트 종류를 식별
func classify(name, cmd string) string {
	for _, kw := range agentKeywords {
		if name == kw || strings.HasPrefix(name, kw+"-") || strings.HasPrefix(name, kw+".") {
			return kw
		}
		// npm/venv 래퍼: .../bin/claude, .../claude ... 식의 경로
		if strings.Contains(cmd, "/bin/"+kw) ||
			strings.Contains(cmd, "/"+kw+" ") ||
			strings.HasSuffix(cmd, "/"+kw) {
			return kw
		}
	}
	return ""
}

// etime "MM:SS" / "HH:MM:SS" / "DD-HH:MM:SS" → 시작 시각(ms unix) 역산
func etimeToStartMs(etime string) int64 {
	var days, hours, mins, secs int64
	parts := strings.Split(etime, ":")
	switch len(parts) {
	case 3:
		dp := strings.Split(parts[0], "-")
		if len(dp) == 2 {
			days, _ = strconv.ParseInt(dp[0], 10, 64)
			hours, _ = strconv.ParseInt(dp[1], 10, 64)
		} else {
			hours, _ = strconv.ParseInt(parts[0], 10, 64)
		}
		mins, _ = strconv.ParseInt(parts[1], 10, 64)
		secs, _ = strconv.ParseInt(parts[2], 10, 64)
	case 2:
		mins, _ = strconv.ParseInt(parts[0], 10, 64)
		secs, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	elapsed := ((days*24+hours)*60+mins)*60 + secs
	return nowMs() - elapsed*1000
}

// ps -axo 일괄 출력에서 에이전트 후보를 스캔
func agentsScan() []AgentInfo {
	out, err := exec.Command("ps", "-axo", "pid=,pcpu=,rss=,etime=,comm=").Output()
	if err != nil {
		return nil
	}
	var found []AgentInfo
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 5 {
			continue
		}
		pid, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		cpuPct, _ := strconv.ParseFloat(f[1], 64)
		rssKb, _ := strconv.ParseInt(f[2], 10, 64)
		etime := f[3]
		// comm: 필드 4 이후 전체 (공백 포함 경로)
		comm := strings.Join(f[4:], " ")
		lower := strings.ToLower(comm)
		base := lower
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		if base == "" || strings.Contains(base, "dock-util") || strings.Contains(base, "dock_util") {
			continue
		}
		kind := classify(base, lower)
		if kind == "" {
			continue
		}
		display := base
		if i := strings.IndexAny(base, "-. "); i > 0 {
			display = base[:i]
		}
		found = append(found, AgentInfo{
			Pid:     int32(pid),
			Name:    display,
			Kind:    kind,
			Cpu:     cpuPct,
			MemMb:   uint64(rssKb) / 1024,
			Elapsed: elapsedStr(etimeToStartMs(etime)),
		})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Cpu > found[j].Cpu })
	if len(found) > 10 {
		found = found[:10]
	}
	return found
}

func agentsLoop(st *AppState) {
	seen := map[int32]bool{}
	for {
		time.Sleep(10 * time.Second)
		list := agentsScan()
		pids := map[int32]bool{}
		for _, a := range list {
			pids[a.Pid] = true
		}
		for _, a := range list {
			if !seen[a.Pid] {
				feedPush(st, "🤖", "에이전트 시작", a.Name+" (pid "+itoa(int64(a.Pid))+")")
			}
		}
		exited := 0
		for pid := range seen {
			if !pids[pid] {
				exited++
			}
		}
		seen = pids
		st.events.Publish("agents", map[string]any{"list": list, "exited": exited})
	}
}
