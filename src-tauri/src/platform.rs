//! macOS 네이티브 레이어: 접근성(API)으로 Dock 프레임을 읽고,
//! NSWindow를 독 옆 공간에 붙이며, 파일 참조를 클립보드에 넣는다.

use core_foundation::array::{CFArrayGetCount, CFArrayGetValueAtIndex, CFArrayRef};
use core_foundation::base::{CFRelease, CFTypeRef, TCFType};
use core_foundation::dictionary::CFDictionaryRef;
use core_foundation::string::{CFString, CFStringRef};
use core_graphics::display::CGDisplay;
use core_graphics::geometry::CGPoint;
use objc2::rc::autoreleasepool;
use objc2::runtime::AnyObject;
use objc2::{class, msg_send};
use serde::Serialize;
use std::os::raw::{c_int, c_void};
use std::path::Path;

pub type AXUIElementRef = *mut c_void;
pub type AXError = i32;

#[link(name = "ApplicationServices", kind = "framework")]
extern "C" {
    fn AXUIElementCreateApplication(pid: c_int) -> AXUIElementRef;
    fn AXUIElementCopyAttributeValue(
        element: AXUIElementRef,
        attribute: CFStringRef,
        value: *mut CFTypeRef,
    ) -> AXError;
    fn AXValueGetValue(value: CFTypeRef, the_type: u32, out: *mut c_void) -> bool;
    fn AXIsProcessTrustedWithOptions(options: CFDictionaryRef) -> bool;
    fn AXUIElementCopyAttributeNames(element: AXUIElementRef, names: *mut CFTypeRef) -> AXError;
}

const AX_VALUE_CGPOINT: u32 = 1;
const AX_VALUE_CGSIZE: u32 = 2;

/// 논리 포인트 단위 사각형.
/// Dock 프레임은 AX 좌표(좌상단 원점, y 아래로 증가),
/// NSWindow 배치는 AppKit 좌표(좌하단 원점, y 위로 증가)로 쓴다.
#[derive(Clone, Copy, Debug, Serialize, PartialEq)]
pub struct RectF {
    pub x: f64,
    pub y: f64,
    pub w: f64,
    pub h: f64,
}

pub fn dock_pid() -> Option<i32> {
    let mut sys = sysinfo::System::new();
    sys.refresh_processes(sysinfo::ProcessesToUpdate::All, false);
    sys.processes()
        .iter()
        .find(|(_, p)| p.name().to_string_lossy().eq_ignore_ascii_case("Dock"))
        .map(|(pid, _)| pid.as_u32() as i32)
}

/// 독 PID 캐시 (전체 프로세스 스캔은 최초 1회/실패 시에만)
static DOCK_PID: parking_lot::Mutex<Option<(i32, u32)>> = parking_lot::Mutex::new(None);

fn dock_pid_cached() -> Option<i32> {
    let mut cached = DOCK_PID.lock();
    match *cached {
        Some((pid, _)) => Some(pid),
        None => {
            let pid = dock_pid();
            *cached = pid.map(|p| (p, 0));
            pid
        }
    }
}



unsafe fn copy_attr_point(el: AXUIElementRef, name: &str) -> Option<(f64, f64)> {
    let cf_name = CFString::new(name);
    let mut val: CFTypeRef = std::ptr::null();
    let err = AXUIElementCopyAttributeValue(el, cf_name.as_concrete_TypeRef(), &mut val);
    if err != 0 || val.is_null() {
        if !val.is_null() {
            CFRelease(val);
        }
        return None;
    }
    let mut pt = CGPoint { x: 0.0, y: 0.0 };
    let ok = AXValueGetValue(val, AX_VALUE_CGPOINT, &mut pt as *mut _ as *mut c_void);
    CFRelease(val);
    ok.then_some((pt.x, pt.y))
}

unsafe fn copy_attr_string(el: AXUIElementRef, name: &str) -> Option<String> {
    let cf_name = CFString::new(name);
    let mut val: CFTypeRef = std::ptr::null();
    let err = AXUIElementCopyAttributeValue(el, cf_name.as_concrete_TypeRef(), &mut val);
    if err != 0 || val.is_null() { return None; }
    let s = core_foundation::string::CFString::wrap_under_create_rule(val as CFStringRef).to_string();
    Some(s)
}

#[allow(dead_code)]
unsafe fn copy_attr_values(el: AXUIElementRef, name: &str) -> Result<Vec<AXUIElementRef>, i32> {
    let cf_name = CFString::new(name);
    let mut vals: CFTypeRef = std::ptr::null();
    let err = AXUIElementCopyAttributeValue(el, cf_name.as_concrete_TypeRef(), &mut vals);
    if err != 0 || vals.is_null() { return Err(err); }
    let arr = vals as CFArrayRef;
    let n = CFArrayGetCount(arr);
    let mut out = Vec::with_capacity(n as usize);
    for i in 0..n {
        out.push(CFArrayGetValueAtIndex(arr, i) as AXUIElementRef);
    }
    Ok(out)
}

unsafe fn copy_attr_size(el: AXUIElementRef, name: &str) -> Option<(f64, f64)> {
    let cf_name = CFString::new(name);
    let mut val: CFTypeRef = std::ptr::null();
    let err = AXUIElementCopyAttributeValue(el, cf_name.as_concrete_TypeRef(), &mut val);
    if err != 0 || val.is_null() {
        if !val.is_null() {
            CFRelease(val);
        }
        return None;
    }
    // CGSize는 {width, height} 순서로 CGPoint와 동일한 메모리 레이아웃
    let mut sz = CGPoint { x: 0.0, y: 0.0 };
    let ok = AXValueGetValue(val, AX_VALUE_CGSIZE, &mut sz as *mut _ as *mut c_void);
    CFRelease(val);
    ok.then_some((sz.x, sz.y))
}

/// 진단용: AX 에러 코드와 Dock 요소 속성 이름 반환
pub fn ax_debug() -> String {
    let screens = all_screens();
    let (mx, my) = mouse_location();
    let chosen = screen_containing_mouse();
    let mut out = format!("screens={screens:?} mouse=({mx},{my}) chosen={chosen:?}");
    let _ = &mut out;
    let Some(pid) = dock_pid_cached() else { return "no dock pid".into() };
    autoreleasepool(|_| unsafe {
        let app = AXUIElementCreateApplication(pid);
        if app.is_null() { return "null app".into(); }
        let mut names: CFTypeRef = std::ptr::null();
        let e1 = AXUIElementCopyAttributeNames(app, &mut names);
        let app_names = if e1 == 0 && !names.is_null() {
            let cf = core_foundation::array::CFArray::<CFString>::wrap_under_create_rule(names as core_foundation::array::CFArrayRef);
            format!(" app_attrs={:?}", cf.iter().map(|n| n.to_string()).collect::<Vec<_>>())
        } else { String::new() };
        let mut docks: CFTypeRef = std::ptr::null();
        let e2 = AXUIElementCopyAttributeValue(app, CFString::new("AXDocks").as_concrete_TypeRef(), &mut docks);
        out += &format!(" pid={pid} names_err={e1} docks_err={e2}{app_names}");
        // AXChildren 탐색 (macOS 26: AXDocks 제거, 자식 요소로 이동)
        if let Ok(children) = copy_attr_values(app, "AXChildren") {
            out += &format!(" children={}", children.len());
            for (i, c) in children.into_iter().enumerate().take(4) {
                let role = copy_attr_string(c, "AXRole").unwrap_or_default();
                let sub = copy_attr_string(c, "AXSubrole").unwrap_or_default();
                let pos = copy_attr_point(c, "AXPosition").map(|(x,y)| format!("({x},{y})")).unwrap_or_default();
                let size = copy_attr_size(c, "AXSize").map(|(w,h)| format!("({w}x{h})")).unwrap_or_default();
                out += &format!(" [#{i} role={role}/{sub} pos={pos} size={size}]");
            }
        }
        if e2 == 0 && !docks.is_null() {
            let arr = docks as CFArrayRef;
            out += &format!(" docks_count={}", CFArrayGetCount(arr));
            if CFArrayGetCount(arr) > 0 {
                let dock = CFArrayGetValueAtIndex(arr, 0) as AXUIElementRef;
                let mut dnames: CFTypeRef = std::ptr::null();
                let e3 = AXUIElementCopyAttributeNames(dock, &mut dnames);
                out += &format!(" dock_attrs_err={e3}");
                if e3 == 0 && !dnames.is_null() {
                    let cf_names = core_foundation::array::CFArray::<CFString>::wrap_under_create_rule(dnames as core_foundation::array::CFArrayRef);
                    let list: Vec<String> = cf_names.iter().map(|n| n.to_string()).collect();
                    out += &format!(" dock_attrs={:?}", list);
                }
            }
        }
        CFRelease(app);
        out
    })
}

/// 하단 Dock의 프레임(AX 좌표). 접근성 미신뢰 시 즉시 None(호출 비용 0),
/// 실패가 5회 누적되면 PID 캐시를 폐기해 재스캔.
pub fn dock_frame() -> Option<RectF> {
    if !ax_trusted() {
        return None;
    }
    let pid = dock_pid_cached()?;
    autoreleasepool(|_| unsafe {
        let app = AXUIElementCreateApplication(pid);
        if app.is_null() {
            return None;
        }
        let mut result = None;
        let docks_attr = CFString::new("AXDocks");
        let mut docks: CFTypeRef = std::ptr::null();
        let err = AXUIElementCopyAttributeValue(app, docks_attr.as_concrete_TypeRef(), &mut docks);
        if err == 0 && !docks.is_null() {
            let arr = docks as CFArrayRef;
            if CFArrayGetCount(arr) > 0 {
                let dock = CFArrayGetValueAtIndex(arr, 0) as AXUIElementRef;
                if let Some((x, y)) = copy_attr_point(dock, "AXPosition") {
                    if let Some((w, h)) = copy_attr_size(dock, "AXSize") {
                        result = Some(RectF { x, y, w, h });
                    }
                }
            }
            CFRelease(docks);
        }

        // macOS 26+: AXDocks 속성 제거됨 → AXChildren의 AXList(독 바)로 폴백
        if result.is_none() {
            if let Ok(children) = copy_attr_values(app, "AXChildren") {
                for c in children {
                    if let (Some((x, y)), Some((w, h))) =
                        (copy_attr_point(c, "AXPosition"), copy_attr_size(c, "AXSize"))
                    {
                        if w > 100.0 && h > 10.0 {
                            result = Some(RectF { x, y, w, h });
                            break;
                        }
                    }
                }
            }
        }
        CFRelease(app);

        // 성공/실패 기록: 연속 실패 5회면 PID가 낡았다고 보고 캐시 폐기
        {
            let mut cached = DOCK_PID.lock();
            match (result, &mut *cached) {
                (Some(_), Some((_, fails))) => *fails = 0,
                (None, Some((_, fails))) => {
                    *fails += 1;
                    if *fails >= 5 {
                        *cached = None;
                    }
                }
                _ => {}
            }
        }
        result
    })
}

pub fn ax_trusted() -> bool {
    unsafe { AXIsProcessTrustedWithOptions(std::ptr::null()) }
}

/// 메인 디스플레이 크기(논리 포인트).
pub fn main_screen() -> (f64, f64) {
    let d = CGDisplay::main();
    let b = d.bounds();
    (b.size.width, b.size.height)
}

/// 현재 마우스 위치(AppKit 좌표, 좌하단 원점). 권한 불필요.
pub fn mouse_location() -> (f64, f64) {
    unsafe {
        let pt: objc2_foundation::NSPoint = msg_send![class!(NSEvent), mouseLocation];
        (pt.x, pt.y)
    }
}

/// 모든 활성 화면의 프레임 목록 (AppKit 좌하단 원점으로 변환, 스레드 세이프)
pub fn all_screens() -> Vec<RectF> {
    use core_graphics::display::CGDisplay;
    let Ok(displays) = CGDisplay::active_displays() else { return Vec::new() };
    // CG는 좌상단 원점 → AppKit(좌하단) 변환에 필요한 프라이머리 높이 찾기 (원점 0,0인 디스플레이)
    let primary_h = displays
        .iter()
        .map(|&id| CGDisplay::new(id).bounds())
        .find(|b| b.origin.x == 0.0 && b.origin.y == 0.0)
        .map(|b| b.size.height);
    let Some(primary_h) = primary_h else { return Vec::new() };
    displays
        .iter()
        .map(|&id| {
            let b = CGDisplay::new(id).bounds();
            let y = primary_h - b.origin.y - b.size.height;
            RectF { x: b.origin.x, y, w: b.size.width, h: b.size.height }
        })
        .collect()
}

/// 마우스 커서가 있는 화면의 프레임(AppKit 좌하단 원점). 못 찾으면 None.
pub fn screen_containing_mouse() -> Option<RectF> {
    let (mx, my) = mouse_location();
    screen_frame_at(mx, my)
}

/// 전역 좌표 (x, y)가 속한 화면의 프레임
pub fn screen_frame_at(x: f64, y: f64) -> Option<RectF> {
    all_screens()
        .into_iter()
        .find(|r| x >= r.x && x <= r.x + r.w && y >= r.y && y <= r.y + r.h)
}

extern "C" {
    static _dispatch_main_q: u8;
    fn dispatch_async_f(queue: *mut c_void, context: *mut c_void, work: extern "C" fn(*mut c_void));
}

/// 클로저를 GCD 메인 큐에서 실행 (NSWindow 조작은 메인 스레드 필요 —
/// macOS 26에선 백그라운드 setLevel이 트랩함)
pub fn on_main_async(f: impl FnOnce() + Send + 'static) {
    extern "C" fn trampoline(ctx: *mut c_void) {
        let f: Box<Box<dyn FnOnce()>> = unsafe { Box::from_raw(ctx as *mut Box<dyn FnOnce()>) };
        f();
    }
    let boxed: Box<dyn FnOnce()> = Box::new(f);
    let ctx = Box::into_raw(Box::new(boxed));
    unsafe {
        let queue = std::ptr::addr_of!(_dispatch_main_q) as *mut c_void;
        dispatch_async_f(queue, ctx as *mut c_void, trampoline);
    }
}

pub mod win {
    use super::*;

    /// 스레드 간 전달용 raw NSWindow 포인터 래퍼 (메인 스레드에서만 역참조)
    pub struct NsWinPtr(pub *mut c_void);
    unsafe impl Send for NsWinPtr {}

    pub unsafe fn set_level(ptr: *mut c_void, level: i64) {
        if ptr.is_null() {
            return;
        }
        let _: () = msg_send![ptr as *mut AnyObject, setLevel: level];
    }

    /// 모든 스페이스 + 풀스크린 위에도 표시
    pub unsafe fn pin_all_spaces(ptr: *mut c_void) {
        if ptr.is_null() {
            return;
        }
        let behavior: u64 = (1u64 << 0) | (1u64 << 8); // canJoinAllSpaces | fullScreenAuxiliary
        let _: () = msg_send![ptr as *mut AnyObject, setCollectionBehavior: behavior];
    }

    pub unsafe fn set_frame(ptr: *mut c_void, r: RectF) {
        if ptr.is_null() {
            return;
        }
        let frame = objc2_foundation::NSRect::new(
            objc2_foundation::NSPoint::new(r.x, r.y),
            objc2_foundation::NSSize::new(r.w, r.h),
        );
        let _: () = msg_send![ptr as *mut AnyObject, setFrame: frame, display: true];
    }

}

/// 파일 경로들을 클립보드에 "파일 참조"로 복사 (Finder 붙여넣기 가능)
pub fn copy_file_refs(paths: &[std::path::PathBuf]) -> bool {
    if paths.is_empty() {
        return false;
    }
    unsafe {
        autoreleasepool(|_| {
            let pb: *mut AnyObject = msg_send![class!(NSPasteboard), generalPasteboard];
            if pb.is_null() {
                return false;
            }
            let _: () = msg_send![pb, clearContents];
            let arr: *mut AnyObject = msg_send![class!(NSMutableArray), arrayWithCapacity: paths.len()];
            for p in paths {
                let cf = CFString::new(&p.to_string_lossy());
                let url: *mut AnyObject = msg_send![class!(NSURL),
                    fileURLWithPath: cf.as_concrete_TypeRef() as *const AnyObject
                ];
                let _: () = msg_send![arr, addObject: url];
            }
            let ok: bool = msg_send![pb, writeObjects: arr];
            ok
        })
    }
}

pub fn reveal_in_finder(path: &Path) -> Result<(), String> {
    std::process::Command::new("open")
        .arg("-R")
        .arg(path)
        .spawn()
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// 접근성 권한이 필요한 모든 경로에서 공용으로 쓰는 osascript 래퍼
pub fn osascript(script: &str) -> Result<String, String> {
    let out = std::process::Command::new("osascript")
        .arg("-e")
        .arg(script)
        .output()
        .map_err(|e| e.to_string())?;
    if out.status.success() {
        Ok(String::from_utf8_lossy(&out.stdout).trim().to_string())
    } else {
        Err(String::from_utf8_lossy(&out.stderr).trim().to_string())
    }
}

pub fn notify(title: &str, body: &str) {
    let esc_t = title.replace('"', "\\\"");
    let esc_b = body.replace('"', "\\\"");
    let _ = osascript(&format!(
        "display notification \"{esc_b}\" with title \"{esc_t}\""
    ));
}

/// 현재 유닉스 밀리초 (chrono 대체)
pub fn now_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

/// 부팅 시각(초) — 시스템이 바뀌지 않으므로 1회만 조회
static BOOT_SECS: std::sync::OnceLock<i64> = std::sync::OnceLock::new();

fn boot_secs() -> i64 {
    *BOOT_SECS.get_or_init(|| {
        std::process::Command::new("sysctl")
            .args(["-n", "kern.boottime"])
            .output()
            .ok()
            .and_then(|o| String::from_utf8(o.stdout).ok())
            .and_then(|s| {
                s.split("sec = ")
                    .nth(1)
                    .and_then(|r| r.split(',').next())
                    .and_then(|v| v.trim().parse().ok())
            })
            .unwrap_or(0)
    })
}

/// 시스템 uptime 문자열 (부팅 시각 캐시 기반, 프로세스 생성 없음)
pub fn uptime_string() -> String {
    let boot = boot_secs();
    if boot == 0 {
        return "-".into();
    }
    let up = now_ms() / 1000 - boot;
    let d = up / 86400;
    let h = (up % 86400) / 3600;
    let m = (up % 3600) / 60;
    if d > 0 {
        format!("{d}일 {h}시간")
    } else if h > 0 {
        format!("{h}시간 {m}분")
    } else {
        format!("{m}분")
    }
}

/// 배터리 (pmset 파싱): (퍼센트, 충전중)
pub fn battery() -> Option<(u32, bool)> {
    let out = std::process::Command::new("pmset").arg("-g").arg("batt").output().ok()?;
    let s = String::from_utf8_lossy(&out.stdout);
    let pct: u32 = s.split('%').next()?.rsplit(|c: char| !c.is_ascii_digit()).next()?.parse().ok()?;
    let charging = s.contains("AC Power");
    Some((pct, charging))
}

/// 루트 볼륨 여유 공간(GB)
pub fn disk_free_gb() -> f64 {
    let out = std::process::Command::new("df").arg("-k").arg("/").output();
    let Ok(out) = out else { return 0.0 };
    let s = String::from_utf8_lossy(&out.stdout);
    // "Filesystem 1024-blocks Used Available Capacity ..." 에서 두 번째 줄 4번째 칸
    s.lines()
        .nth(1)
        .and_then(|l| l.split_whitespace().nth(3))
        .and_then(|v| v.parse::<f64>().ok())
        .map(|kb| kb / 1048576.0)
        .unwrap_or(0.0)
}

pub fn shell_output(cmd: &str, args: &[&str]) -> Option<String> {
    let out = std::process::Command::new(cmd).args(args).output().ok()?;
    if out.status.success() {
        Some(String::from_utf8_lossy(&out.stdout).trim().to_string())
    } else {
        None
    }
}

/// `sysctl -n machdep.cpu.brand_string` 용도가 아닌, quiet exit 체크
pub fn command_exists(cmd: &str) -> bool {
    std::process::Command::new("sh")
        .args(["-c", &format!("command -v {cmd}")])
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false)
}
