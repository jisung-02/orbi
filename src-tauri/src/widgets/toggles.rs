//! 시스템 토글: 다크모드 / Wi-Fi / Bluetooth / 음소거 / 잠금방지(caffeinate)
//! 모든 상태조회/변경은 wifi 장치명과 caffeinate 상태를 파라미터로 받는다 (ipc.rs에서 주입)

use serde::Serialize;

#[derive(Serialize)]
pub struct TogglesState {
    pub dark: bool,
    pub wifi: bool,
    pub bt: bool,
    pub mute: bool,
    pub caffeine: bool,
    pub wifi_available: bool,
    pub bt_available: bool,
}

pub fn detect_wifi_device() -> Option<String> {
    let out = crate::platform::shell_output("networksetup", &["-listallhardwareports"])?;
    let mut lines = out.lines();
    while let Some(line) = lines.next() {
        if line.trim() == "Hardware Port: Wi-Fi" {
            if let Some(dev_line) = lines.next() {
                if let Some(dev) = dev_line.strip_prefix("Device: ") {
                    return Some(dev.trim().to_string());
                }
            }
        }
    }
    None
}

pub fn get_state(wifi_dev: &Option<String>, caffeine_on: bool) -> TogglesState {
    let dark = crate::platform::shell_output("defaults", &["read", "-g", "AppleInterfaceStyle"])
        .map(|v| v.eq_ignore_ascii_case("Dark"))
        .unwrap_or(false);

    let wifi = wifi_state(wifi_dev).unwrap_or(false);

    let bt_available = crate::platform::command_exists("blueutil");
    let bt = bt_available
        .then(|| crate::platform::shell_output("blueutil", &["-p"]))
        .flatten()
        .map(|v| v == "1")
        .unwrap_or(false);

    let mute = crate::platform::osascript("output muted of (get volume settings)")
        .map(|v| v == "true")
        .unwrap_or(false);

    TogglesState {
        dark,
        wifi,
        bt,
        mute,
        caffeine: caffeine_on,
        wifi_available: wifi_dev.is_some(),
        bt_available,
    }
}

pub fn wifi_state(dev: &Option<String>) -> Option<bool> {
    let dev = dev.as_ref()?;
    let out = crate::platform::shell_output("networksetup", &["-getairportpower", dev])?;
    Some(out.to_lowercase().contains("on"))
}

pub fn wifi_set(dev: &Option<String>, on: bool) -> Result<(), String> {
    let Some(dev) = dev else {
        return Err("Wi-Fi 장치를 찾을 수 없습니다".into());
    };
    let state = if on { "on" } else { "off" };
    crate::platform::shell_output("networksetup", &["-setairportpower", dev, state])
        .ok_or_else(|| "networksetup 실패 (권한 필요)".to_string())?;
    Ok(())
}

pub fn dark_mode_set(on: bool) -> Result<(), String> {
    let val = if on { "true" } else { "false" };
    crate::platform::osascript(&format!(
        "tell application \"System Events\" to tell appearance preferences to set dark mode to {val}"
    ))
    .map(|_| ())
}

pub fn mute_set(on: bool) -> Result<(), String> {
    let val = if on { "true" } else { "false" };
    crate::platform::osascript(&format!("set volume output muted {val}")).map(|_| ())
}

pub fn bt_set(on: bool) -> Result<(), String> {
    if !crate::platform::command_exists("blueutil") {
        return Err("blueutil이 필요합니다: brew install blueutil".into());
    }
    let val = if on { "1" } else { "0" };
    crate::platform::shell_output("blueutil", &["-p", val])
        .ok_or_else(|| "blueutil 실행 실패".to_string())?;
    Ok(())
}
