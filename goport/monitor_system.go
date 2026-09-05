package main

import (
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	gnet "github.com/shirou/gopsutil/v3/net"
)

// systemMetrics adapts macOS/gopsutil to the monitor's consumer-owned port.
// Each monitor owns an instance; gopsutil's CPU baseline must have one sampler.
type systemMetrics struct{ bootCached int64 }

func (s *systemMetrics) Sample() metricSample {
	var sample metricSample
	if values, err := cpu.Percent(0, false); err == nil && len(values) > 0 {
		sample.CPU = values[0]
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		sample.MemoryTotal, sample.MemoryAvailable = vm.Total, vm.Available
	}
	if io, err := gnet.IOCounters(false); err == nil && len(io) > 0 {
		sample.Rx, sample.Tx, sample.NetworkValid = io[0].BytesRecv, io[0].BytesSent, true
	}
	return sample
}
func (*systemMetrics) Battery() (*uint32, bool)      { return battery() }
func (*systemMetrics) DiskFreeGB() float64           { return diskFreeGb() }
func (s *systemMetrics) Uptime(now time.Time) string { return uptimeString(&s.bootCached, now) }

// 부팅 시각(초) 캐시 — 시스템이 바뀌지 않으므로 1회만 조회
func bootSeconds(cached *int64) int64 {
	if *cached >= 0 {
		return *cached
	}
	out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		*cached = 0
		return 0
	}
	s := string(out)
	// sec = 1234567890, usec = ...
	if i := strings.Index(s, "sec = "); i >= 0 {
		rest := s[i+6:]
		if j := strings.IndexByte(rest, ','); j >= 0 {
			rest = rest[:j]
		}
		v := 0
		for _, c := range strings.TrimSpace(rest) {
			if c < '0' || c > '9' {
				break
			}
			v = v*10 + int(c-'0')
		}
		*cached = int64(v)
		return *cached
	}
	*cached = 0
	return 0
}

func uptimeString(cached *int64, now time.Time) string {
	boot := bootSeconds(cached)
	if boot == 0 {
		return "-"
	}
	up := now.Unix() - boot
	d := up / 86400
	h := (up % 86400) / 3600
	m := (up % 3600) / 60
	switch {
	case d > 0:
		return itoa(d) + "일 " + itoa(h) + "시간"
	case h > 0:
		return itoa(h) + "시간 " + itoa(m) + "분"
	default:
		return itoa(m) + "분"
	}
}

// 배터리 (pmset 파싱): (퍼센트, 충전중)
func battery() (*uint32, bool) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return nil, false
	}
	s := string(out)
	idx := strings.IndexByte(s, '%')
	if idx < 0 {
		return nil, false
	}
	// % 앞의 연속 숫자 파싱
	i := idx
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == idx {
		return nil, false
	}
	v := uint32(0)
	for _, c := range s[i:idx] {
		v = v*10 + uint32(c-'0')
	}
	charging := strings.Contains(s, "AC Power")
	return &v, charging
}

// 루트 볼륨 여유 공간(GB)
func diskFreeGb() float64 {
	var info syscall.Statfs_t
	if err := syscall.Statfs("/", &info); err != nil {
		return 0
	}
	return float64(info.Bavail) * float64(info.Bsize) / 1073741824.0
}
