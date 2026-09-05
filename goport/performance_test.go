package main

import (
	"math"
	"testing"
	"time"
)

var benchmarkDisk float64

func BenchmarkDiskFree(b *testing.B) {
	for i := 0; i < b.N; i++ {
		benchmarkDisk = diskFreeGb()
	}
}

func BenchmarkOrbHitGeometry(b *testing.B) {
	st := &AppState{cfg: defaultConfig()}
	var geometry orbHitGeometry
	geometry.refresh(st)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		geometry.refresh(st)
	}
}

func TestOrbGeometryRefreshesAfterConfigChange(t *testing.T) {
	st := &AppState{cfg: defaultConfig()}
	var geometry orbHitGeometry
	geometry.refresh(st)
	if len(geometry.centers) != 11 {
		t.Fatal("missing initial circles")
	}
	st.cfgMu.Lock()
	st.cfg.HiddenWidgets = []string{"terminal", "shelf"}
	st.cfgRevision.Add(1)
	st.cfgMu.Unlock()
	geometry.refresh(st)
	if len(geometry.centers) != 9 {
		t.Fatal("stale geometry after changing visibility")
	}
	for i, center := range geometry.centers {
		x, y := orbCircleCenter(Rect{}, i, 9)
		if math.Abs(center[0]-x) > 0.001 || math.Abs(center[1]-y) > 0.001 {
			t.Fatal("cached hit target differs from rendered geometry")
		}
	}
	if allocations := testing.AllocsPerRun(100, func() { geometry.refresh(st) }); allocations != 0 {
		t.Fatalf("steady hover allocates %v objects", allocations)
	}
}

func TestNetworkRateHandlesResetsAndSampleTiming(t *testing.T) {
	if rate := networkRate(10240, 0, 5*time.Second); rate != 2 {
		t.Fatalf("rate=%v", rate)
	}
	if networkRate(100, 200, time.Second) != 0 {
		t.Fatal("counter reset underflow")
	}
	if networkRate(200, 100, 0) != 0 {
		t.Fatal("zero interval")
	}
}

func TestDiskFreeIsAvailable(t *testing.T) {
	if gb := diskFreeGb(); gb <= 0 || math.IsNaN(gb) || math.IsInf(gb, 0) {
		t.Fatalf("disk free=%v", gb)
	}
}

func TestOuterRingFillsFromLowerEnd(t *testing.T) {
	for count := 9; count <= 11; count++ {
		for offset := 0; offset < count-8; offset++ {
			x, y := orbCircleCenter(Rect{}, count-1-offset, count)
			angle := math.Atan2(y-orbCY, x-orbCX) * 180 / math.Pi
			if math.Abs(angle-(172-float64(offset)*25)) > 0.001 {
				t.Fatalf("count %d offset %d: angle %v", count, offset, angle)
			}
		}
	}
}
