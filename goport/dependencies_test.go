package main

import (
	"context"
	"testing"
	"time"
)

type memoryConfigStore struct {
	cfg   Config
	saves int
}

func (s *memoryConfigStore) Load() Config    { return s.cfg }
func (s *memoryConfigStore) Save(cfg Config) { s.cfg = cfg; s.saves++ }

type recordedEvent struct {
	name    string
	payload any
}
type recordingEvents struct{ events []recordedEvent }

func (e *recordingEvents) Publish(name string, payload any) {
	e.events = append(e.events, recordedEvent{name, payload})
}

func TestInjectedStateStorageAndEvents(t *testing.T) {
	end := int64(10)
	store := &memoryConfigStore{cfg: defaultConfig()}
	store.cfg.HiddenWidgets = []string{"terminal"}
	store.cfg.Pomodoro = &PersistedPom{EndsAtMs: &end}
	events := &recordingEvents{}
	st := newState(store, events, "test-device")
	store.cfg.HiddenWidgets[0] = "feed"
	*store.cfg.Pomodoro.EndsAtMs = 20
	if st.configSnapshot().HiddenWidgets[0] != "terminal" || *st.configSnapshot().Pomodoro.EndsAtMs != 10 {
		t.Fatal("state aliases repository data")
	}
	st.withConfig(func(cfg *Config) { cfg.Lang = "en" })
	if store.saves != 1 || store.cfg.Lang != "en" || st.cfgRevision.Load() != 1 || st.wifiDev != "test-device" {
		t.Fatal("injected state wiring failed")
	}
	snapshot := st.configSnapshot()
	snapshot.HiddenWidgets[0] = "monitor"
	*snapshot.Pomodoro.EndsAtMs = 30
	if store.cfg.HiddenWidgets[0] != "terminal" || *st.configSnapshot().Pomodoro.EndsAtMs != 10 {
		t.Fatal("snapshot aliases persisted or live state")
	}
	feedPush(st, "!", "title", "body")
	feedClear(st)
	if len(events.events) != 2 || events.events[0].name != "feed-new" || events.events[1].name != "feed" {
		t.Fatal("feed bypassed injected publisher")
	}
}

func TestFileConfigStoreUsesInjectedDirectory(t *testing.T) {
	store := fileConfigStore{dir: t.TempDir()}
	cfg := store.Load()
	if cfg.Lang != "ko" {
		t.Fatal("missing file must use defaults")
	}
	cfg.Lang = "en"
	cfg.HiddenWidgets = []string{"terminal"}
	store.Save(cfg)
	loaded := store.Load()
	if loaded.Lang != "en" || len(loaded.HiddenWidgets) != 1 {
		t.Fatal("config roundtrip failed")
	}
}

type fakeMetrics struct {
	sample                             metricSample
	samples, batteries, disks, uptimes int
	pct                                uint32
}

func (f *fakeMetrics) Sample() metricSample     { f.samples++; return f.sample }
func (f *fakeMetrics) Battery() (*uint32, bool) { f.batteries++; return &f.pct, true }
func (f *fakeMetrics) DiskFreeGB() float64      { f.disks++; return 42 }
func (f *fakeMetrics) Uptime(time.Time) string  { f.uptimes++; return "test uptime" }

type recordingMonitor struct {
	active    bool
	snapshots []Stats
}

func (o *recordingMonitor) Active() bool         { return o.active }
func (o *recordingMonitor) PublishStats(s Stats) { o.snapshots = append(o.snapshots, s) }

func TestMonitorInjectedSamplingAndResume(t *testing.T) {
	source := &fakeMetrics{sample: metricSample{CPU: 37, MemoryTotal: 8 << 30, MemoryAvailable: 2 << 30, NetworkValid: true}, pct: 80}
	output := &recordingMonitor{}
	service := newMonitor(source, output)
	now := time.Unix(1000, 0)
	service.Step(now)
	if source.samples+source.batteries+source.disks+source.uptimes != 0 {
		t.Fatal("hidden monitor sampled OS")
	}
	output.active = true
	service.Step(now)
	first := output.snapshots[0]
	if first.Cpu != 0 || first.NetRxKbs != 0 || first.RamPct != 75 || first.RamUsedGb != 6 {
		t.Fatalf("bad baseline: %+v", first)
	}
	source.pct = 10 // Provider-owned pointers must not mutate the cached/published result.
	source.sample.Rx, source.sample.Tx = 10240, 5120
	service.Step(now.Add(5 * time.Second))
	second := output.snapshots[1]
	if second.Cpu != 37 || second.NetRxKbs != 2 || second.NetTxKbs != 1 || *second.BatteryPct != 80 || *first.BatteryPct != 80 || source.batteries != 1 {
		t.Fatalf("bad rates/cache: %+v", second)
	}
	service.Step(now.Add(15 * time.Second))
	if source.batteries != 2 || *output.snapshots[2].BatteryPct != 10 {
		t.Fatal("battery cache did not expire")
	}
	output.active = false
	service.Step(now.Add(20 * time.Second))
	output.active = true
	source.sample.Rx = 1 << 30
	service.Step(now.Add(25 * time.Second))
	resumed := output.snapshots[3]
	if resumed.Cpu != 0 || resumed.NetRxKbs != 0 {
		t.Fatalf("stale baseline after resume: %+v", resumed)
	}
}

func TestMonitorNetworkFailureDoesNotResetCPU(t *testing.T) {
	source := &fakeMetrics{sample: metricSample{CPU: 50}}
	output := &recordingMonitor{active: true}
	service := newMonitor(source, output)
	now := time.Unix(1000, 0)
	service.Step(now)
	service.Step(now.Add(2 * time.Second))
	if output.snapshots[1].Cpu != 50 {
		t.Fatal("network failure reset CPU baseline")
	}
	source.sample.NetworkValid, source.sample.Rx = true, 10240
	service.Step(now.Add(4 * time.Second))
	if output.snapshots[2].NetRxKbs != 0 {
		t.Fatal("first successful network sample must establish baseline")
	}
	source.sample.Rx += 4096
	service.Step(now.Add(6 * time.Second))
	if output.snapshots[3].NetRxKbs != 2 {
		t.Fatal("network did not recover")
	}
}

func TestMonitorRunStopsWithoutWallClockTicks(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		source := &fakeMetrics{}
		service := newMonitor(source, &recordingMonitor{active: true})
		ticks := make(chan time.Time)
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		} else {
			close(ticks)
		}
		done := make(chan struct{})
		go func() { service.Run(ctx, ticks); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("worker did not stop")
		}
		cancel()
		if source.samples != 0 {
			t.Fatal("stopped worker sampled")
		}
	}
}
