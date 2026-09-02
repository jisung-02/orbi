//! 시스템 모니터: CPU / RAM / 배터리 / uptime 2초 주기 브로드캐스트

use serde::Serialize;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager};

#[derive(Serialize, Clone)]
pub struct Stats {
    pub cpu: f32,
    pub ram_used_gb: f64,
    pub ram_total_gb: f64,
    pub ram_pct: f64,
    pub battery_pct: Option<u32>,
    pub battery_charging: bool,
    pub uptime: String,
    pub net_rx_kbs: f64,
    pub net_tx_kbs: f64,
    pub disk_free_gb: f64,
}

pub fn monitor_loop(app: AppHandle) {
    // pmset/df 호출이 무거우므로 배터리·디스크는 15초 캐시
    type BattCache = Option<(std::time::Instant, Option<(u32, bool)>, f64)>;
    let mut slow_cache: BattCache = None;
    loop {
        std::thread::sleep(Duration::from_secs(2));
        let (battery, disk_free) = match &slow_cache {
            Some((t, b, d)) if t.elapsed() < Duration::from_secs(15) => (*b, *d),
            _ => {
                let b = crate::platform::battery();
                let d = crate::platform::disk_free_gb();
                slow_cache = Some((std::time::Instant::now(), b, d));
                (b, d)
            }
        };
        let snapshot = {
            let st = app.state::<crate::app::AppState>();
            let mut sys = st.sys.lock();
            sys.refresh_cpu_usage();
            sys.refresh_memory();
            let cpu = sys.global_cpu_usage();
            let total = sys.total_memory() as f64 / 1073741824.0;
            let used = sys.used_memory() as f64 / 1073741824.0;
            // 네트워크: 2초 창의 총 송수신 → KB/s
            let (net_rx_kbs, net_tx_kbs) = {
                let mut net = st.net.lock();
                net.refresh(true);
                let (mut rx, mut tx) = (0u64, 0u64);
                for data in net.values() {
                    rx += data.received();
                    tx += data.transmitted();
                }
                (rx as f64 / 2.0 / 1024.0, tx as f64 / 2.0 / 1024.0)
            };
            Stats {
                cpu,
                ram_used_gb: (used * 10.0).round() / 10.0,
                ram_total_gb: (total * 10.0).round() / 10.0,
                ram_pct: if total > 0.0 { used / total * 100.0 } else { 0.0 },
                battery_pct: battery.map(|(p, _)| p),
                battery_charging: battery.map(|(_, c)| c).unwrap_or(false),
                uptime: crate::platform::uptime_string(),
                net_rx_kbs: (net_rx_kbs * 10.0).round() / 10.0,
                net_tx_kbs: (net_tx_kbs * 10.0).round() / 10.0,
                disk_free_gb: (disk_free * 10.0).round() / 10.0,
            }
        };
        let _ = app.emit("stats", &snapshot);
    }
}
