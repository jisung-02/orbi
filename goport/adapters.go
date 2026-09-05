package main

import (
	"context"
	"time"
)

// Concrete adapters and wiring stay at the application boundary.
type shellEvents struct{}

func (shellEvents) Publish(name string, payload any) { emitAll(name, payload) }

type shellMonitorOutput struct{ events EventPublisher }

func (shellMonitorOutput) Active() bool               { _, open := idByLabel("popover-monitor"); return open }
func (o shellMonitorOutput) PublishStats(stats Stats) { o.events.Publish("stats", stats) }

func runSystemMonitor(events EventPublisher) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	service := newMonitor(&systemMetrics{bootCached: -1}, shellMonitorOutput{events: events})
	service.Run(context.Background(), ticker.C)
}

var _ ConfigStore = fileConfigStore{}
var _ EventPublisher = shellEvents{}
var _ MetricsSource = (*systemMetrics)(nil)
var _ MonitorOutput = shellMonitorOutput{}
