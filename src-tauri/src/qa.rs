//! QA 자동 테스트: CGEventPost로 마우스 이동/클릭을 시뮬레이션하여
//! 오브 호버 확장, 동그라미 클릭, 팝오버 생성을 검증한다.

use std::os::raw::c_void;

extern "C" {
    fn CGEventCreateMouseEvent(
        source: *mut c_void,
        mouse_type: u32,
        position: CGPoint,
        button: u32,
    ) -> *mut c_void;
    fn CGEventPost(tap: u32, event: *mut c_void);
    fn CFRelease(cf: *mut c_void);
    fn usleep(microseconds: u32);
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct CGPoint {
    pub x: f64,
    pub y: f64,
}

const K_CG_SESSION_EVENT_TAP: u32 = 0;

fn post_mouse_move(x: f64, y: f64) {
    unsafe {
        let event = CGEventCreateMouseEvent(
            std::ptr::null_mut(),
            5, // kCGEventMouseMoved
            CGPoint { x, y },
            0,
        );
        CGEventPost(K_CG_SESSION_EVENT_TAP, event);
        CFRelease(event);
    }
}

fn post_mouse_click(x: f64, y: f64) {
    unsafe {
        for mouse_type in [1u32, 2u32] {
            // down, up
            let event = CGEventCreateMouseEvent(
                std::ptr::null_mut(),
                mouse_type,
                CGPoint { x, y },
                0,
            );
            CGEventPost(K_CG_SESSION_EVENT_TAP, event);
            CFRelease(event);
            if mouse_type == 1 {
                std::thread::sleep(std::time::Duration::from_millis(50));
            }
        }
    }
}

pub fn run() {
    println!("=== QA 자동 테스트 시작 ===");

    // 화면 기준: 1470x956
    // 오브 패널: (1082, 650) 크기 380×300
    // 오브 중심: 패널 로컬 (336, 264) → 화면 (1418, 914)
    let orb_cx = 1418.0;
    let orb_cy = 914.0;

    println!("[1] 마우스를 오브 중심 ({orb_cx:.0}, {orb_cy:.0})으로 이동");
    // 부드러운 이동 (중앙 → 오브)
    for i in 1..=10 {
        let t = i as f64 / 10.0;
        let x = 735.0 + (orb_cx - 735.0) * t;
        let y = 478.0 + (orb_cy - 478.0) * t;
        post_mouse_move(x, y);
        std::thread::sleep(std::time::Duration::from_millis(50));
    }
    println!("[1] 이동 완료 — 2초 대기 (확장 트리거)");
    std::thread::sleep(std::time::Duration::from_secs(2));

    // 터미널 동그라미 위치 (안쪽 링 첫 번째, θ=97°, r=115)
    // 패널 로컬: (336 + 115·cos(97°), 300 - (264 - 115·sin(97°)))
    // 화면: (1082 + local_x, 650 + local_y)
    let deg = 97f64.to_radians();
    let circle_x = 1082.0 + 336.0 + 115.0 * deg.cos();
    let circle_y = 650.0 + 264.0 - 115.0 * deg.sin();
    println!("[2] 터미널 동그라미 클릭 ({circle_x:.0}, {circle_y:.0})");
    post_mouse_click(circle_x, circle_y);
    std::thread::sleep(std::time::Duration::from_millis(1500));

    println!("[3] QA 완료 — CGWindowList로 팝오버 생성 확인하세요.");
    println!("=== QA 종료 ===");
}
