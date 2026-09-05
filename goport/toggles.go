package main

// 시스템 토글: 다크모드 / Wi-Fi / Bluetooth / 음소거 / 잠금방지(caffeinate)
// (Rust widgets/toggles.rs 포트)

import (
	"fmt"
	"os/exec"
	"strings"
)

type TogglesState struct {
	Dark          bool `json:"dark"`
	Wifi          bool `json:"wifi"`
	Bt            bool `json:"bt"`
	Mute          bool `json:"mute"`
	Caffeine      bool `json:"caffeine"`
	WifiAvailable bool `json:"wifi_available"`
	BtAvailable   bool `json:"bt_available"`
}

func detectWifiDevice() string {
	out, ok := shellOutput("networksetup", "-listallhardwareports")
	if !ok {
		return ""
	}
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "Hardware Port: Wi-Fi" {
			for _, devLine := range lines[i+1:] {
				if strings.HasPrefix(devLine, "Device: ") {
					return strings.TrimSpace(strings.TrimPrefix(devLine, "Device: "))
				}
			}
		}
	}
	return ""
}

func wifiState(dev string) (bool, bool) {
	if dev == "" {
		return false, false
	}
	out, ok := shellOutput("networksetup", "-getairportpower", dev)
	if !ok {
		return false, false
	}
	return strings.Contains(strings.ToLower(out), "on"), true
}

func getTogglesState(st *AppState) TogglesState {
	dark := false
	if out, ok := shellOutput("defaults", "read", "-g", "AppleInterfaceStyle"); ok {
		dark = strings.EqualFold(out, "Dark")
	}
	wifi, _ := wifiState(st.wifiDev)
	btAvailable := commandExists("blueutil")
	bt := false
	if btAvailable {
		if out, ok := shellOutput("blueutil", "-p"); ok {
			bt = out == "1"
		}
	}
	mute := false
	if out, err := osascript("output muted of (get volume settings)"); err == nil {
		mute = out == "true"
	}
	caffeinate := false
	st.caffeinateMu.Lock()
	caffeinate = st.caffeinate != nil
	st.caffeinateMu.Unlock()
	return TogglesState{
		Dark:          dark,
		Wifi:          wifi,
		Bt:            bt,
		Mute:          mute,
		Caffeine:      caffeinate,
		WifiAvailable: st.wifiDev != "",
		BtAvailable:   btAvailable,
	}
}

func wifiSet(dev string, on bool) error {
	if dev == "" {
		return fmt.Errorf("Wi-Fi 장치를 찾을 수 없습니다")
	}
	state := "off"
	if on {
		state = "on"
	}
	if _, ok := shellOutput("networksetup", "-setairportpower", dev, state); !ok {
		return fmt.Errorf("networksetup 실패 (권한 필요)")
	}
	return nil
}

func darkModeSet(on bool) error {
	val := "false"
	if on {
		val = "true"
	}
	_, err := osascript(`tell application "System Events" to tell appearance preferences to set dark mode to ` + val)
	return err
}

func muteSet(on bool) error {
	val := "false"
	if on {
		val = "true"
	}
	_, err := osascript("set volume output muted " + val)
	return err
}

func btSet(on bool) error {
	if !commandExists("blueutil") {
		return fmt.Errorf("blueutil이 필요합니다: brew install blueutil")
	}
	val := "0"
	if on {
		val = "1"
	}
	if _, ok := shellOutput("blueutil", "-p", val); !ok {
		return fmt.Errorf("blueutil 실행 실패")
	}
	return nil
}

func caffeineToggle(st *AppState) (bool, error) {
	st.caffeinateMu.Lock()
	defer st.caffeinateMu.Unlock()
	if st.caffeinate != nil {
		if st.caffeinate.Process != nil {
			_ = st.caffeinate.Process.Kill()
		}
		st.caffeinate = nil
		return false, nil
	}
	cmd := exec.Command("caffeinate", "-d")
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("caffeinate 실행 실패: %v", err)
	}
	st.caffeinate = cmd
	go func() {
		_ = cmd.Wait()
		st.caffeinateMu.Lock()
		if st.caffeinate == cmd {
			st.caffeinate = nil
		}
		st.caffeinateMu.Unlock()
	}()
	return true, nil
}
