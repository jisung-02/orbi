# Dependency boundaries

The Go runtime uses constructor injection without a DI container. Interfaces are
owned by the code consuming them; macOS, file storage, and UI adapters implement
those interfaces. Production wiring is explicit in `main.go` and `adapters.go`.

| Consumer | Required port | Production adapter |
| --- | --- | --- |
| `AppState` | `ConfigStore` | `fileConfigStore`, with an explicit directory |
| State-based commands, feed, timer and agent events | `EventPublisher` | `shellEvents`, forwarding to native/web IPC |
| `monitorService` | `MetricsSource` | `systemMetrics`, wrapping gopsutil and macOS queries |
| `monitorService` | `MonitorOutput` | `shellMonitorOutput`, checking panel visibility and publishing stats |

`newState(store, events, wifiDevice)` does not discover devices or read global
configuration paths. The startup boundary supplies these values. Configuration
slices and persisted timer pointers are copied at repository and snapshot
boundaries so injected stores cannot accidentally mutate live state. Persistence
retains the existing best-effort contract; this refactor does not add durable
write acknowledgements.

`newMonitor(source, output)` owns sampling policy and caches. `Step(now)` makes
elapsed time explicit. `Run(ctx, ticks)` accepts a borrowed tick channel and stops
on cancellation or channel closure; the caller owns ticker cleanup. Production
runs one monitor worker for the process lifetime. The service and its adapters
are owned by that worker and are not intended for concurrent `Step` calls.

Hidden monitors perform no system collection. Reopening establishes fresh CPU
and network baselines. Battery values remain cached for 15 seconds. No service
locator, reflection, dependency framework, or additional polling was introduced;
the orb geometry cache and event-driven terminal flushing remain unchanged.

These boundaries cover the Go runtime's configuration, broadcast events and
monitoring. AppKit panel operations, terminal PTY handling and notification/sound
commands still use their existing platform implementations. Rust and Swift
internals have not been converted to dependency injection in this change.

## Verification

`dependencies_test.go` uses in-memory stores, recording outputs and fake metrics
to test configuration ownership, event delivery, hidden/resumed sampling,
network error recovery, cache expiration and worker shutdown. The file adapter
round-trip test writes only to `t.TempDir()`.

```sh
cd goport
go test -race ./...
go vet ./...
go test -run '^$' -bench 'Benchmark(DiskFree|OrbHitGeometry)$' -benchmem
```
