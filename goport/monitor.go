package main

import (
	"context"
	"math"
	"time"
)

type Stats struct {
	Cpu             float64 `json:"cpu"`
	RamUsedGb       float64 `json:"ram_used_gb"`
	RamTotalGb      float64 `json:"ram_total_gb"`
	RamPct          float64 `json:"ram_pct"`
	BatteryPct      *uint32 `json:"battery_pct"`
	BatteryCharging bool    `json:"battery_charging"`
	Uptime          string  `json:"uptime"`
	NetRxKbs        float64 `json:"net_rx_kbs"`
	NetTxKbs        float64 `json:"net_tx_kbs"`
	DiskFreeGb      float64 `json:"disk_free_gb"`
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// The monitor policy depends only on these ports, not on AppKit or gopsutil.
type MetricsSource interface {
	Sample() metricSample
	Battery() (*uint32, bool)
	DiskFreeGB() float64
	Uptime(time.Time) string
}

type metricSample struct {
	CPU                          float64
	MemoryTotal, MemoryAvailable uint64
	Rx, Tx                       uint64
	NetworkValid                 bool
}

type MonitorOutput interface {
	Active() bool
	PublishStats(Stats)
}

type monitorService struct {
	source               MetricsSource
	output               MonitorOutput
	first                bool
	prevRx, prevTx       uint64
	sampledAt, batteryAt time.Time
	batteryPct           *uint32
	batteryCharging      bool
}

func newMonitor(source MetricsSource, output MonitorOutput) *monitorService {
	return &monitorService{source: source, output: output, first: true}
}

// Run borrows the tick channel; its owner stops the ticker. Closing it or
// cancelling the context terminates the worker without an unbounded sleep.
func (m *monitorService) Run(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case now, ok := <-ticks:
			if !ok || ctx.Err() != nil {
				return
			}
			m.Step(now)
		}
	}
}

func (m *monitorService) Step(now time.Time) {
	if !m.output.Active() {
		m.first = true
		m.sampledAt = time.Time{}
		return
	}
	sample := m.source.Sample()
	if m.first {
		sample.CPU = 0
	}
	m.first = false
	var rx, tx float64
	if sample.NetworkValid {
		if !m.sampledAt.IsZero() {
			rx = networkRate(sample.Rx, m.prevRx, now.Sub(m.sampledAt))
			tx = networkRate(sample.Tx, m.prevTx, now.Sub(m.sampledAt))
		}
		m.sampledAt, m.prevRx, m.prevTx = now, sample.Rx, sample.Tx
	}
	if m.batteryAt.IsZero() || now.Sub(m.batteryAt) >= 15*time.Second {
		pct, charging := m.source.Battery()
		m.batteryPct = nil
		if pct != nil {
			value := *pct
			m.batteryPct = &value
		}
		m.batteryCharging, m.batteryAt = charging, now
	}
	total := float64(sample.MemoryTotal) / 1073741824.0
	used := float64(sample.MemoryTotal-min(sample.MemoryAvailable, sample.MemoryTotal)) / 1073741824.0
	snap := Stats{
		Cpu: round1(sample.CPU), RamUsedGb: round1(used), RamTotalGb: round1(total),
		BatteryCharging: m.batteryCharging, Uptime: m.source.Uptime(now),
		NetRxKbs: round1(rx), NetTxKbs: round1(tx), DiskFreeGb: round1(m.source.DiskFreeGB()),
	}
	if total > 0 {
		snap.RamPct = used / total * 100
	}
	if m.batteryPct != nil {
		value := *m.batteryPct
		snap.BatteryPct = &value
	}
	if m.output.Active() {
		m.output.PublishStats(snap)
	}
}

func networkRate(current, previous uint64, elapsed time.Duration) float64 {
	if current < previous || elapsed <= 0 {
		return 0
	}
	return float64(current-previous) / elapsed.Seconds() / 1024
}
