//! 오브 전용 NSPanel: tao NSWindow는 전체화면 스페이스에 합류하지 못하는
//! 케이스가 있어, 웹뷰 뷰를 우리가 만든 NSPanel(nonactivating, borderless)로
//! 옮겨 띄운다. Ghostty Quick Terminal / iTerm2 핫키 윈도우와 동일한 구조.
//!
//! 모든 NSWindow 조작은 반드시 메인 큐(on_main_async)에서 실행한다.

use crate::platform::RectF;
use objc2::{class, msg_send};
use objc2::runtime::AnyObject;
use objc2_foundation::{NSPoint, NSRect, NSSize};
use parking_lot::Mutex;

/// 현재 오브 패널 포인터 (usize, 0 = 없음)
pub type SharedPanel = Mutex<usize>;

pub fn store() -> SharedPanel {
    Mutex::new(0)
}

pub fn get(store: &SharedPanel) -> usize {
    *store.lock()
}

pub fn set(store: &SharedPanel, addr: usize) {
    *store.lock() = addr;
}

extern "C" {
    static _dispatch_main_q: u8;
    fn dispatch_async_f(queue: *mut std::os::raw::c_void, context: *mut std::os::raw::c_void, work: extern "C" fn(*mut std::os::raw::c_void));
}

/// 클로저를 GCD 메인 큐에서 실행
pub fn on_main_async(f: impl FnOnce() + Send + 'static) {
    extern "C" fn trampoline(ctx: *mut std::os::raw::c_void) {
        let f: Box<Box<dyn FnOnce()>> = unsafe { Box::from_raw(ctx as *mut Box<dyn FnOnce()>) };
        f();
    }
    let boxed: Box<dyn FnOnce()> = Box::new(f);
    let ctx = Box::into_raw(Box::new(boxed));
    unsafe {
        let queue = std::ptr::addr_of!(_dispatch_main_q) as *mut std::os::raw::c_void;
        dispatch_async_f(queue, ctx as *mut std::os::raw::c_void, trampoline);
    }
}

/// 메인 큐에서 실행하고 결과 대기 (패널 생성 등 초기화용)
/// 오브 패널 생성: 웹뷰 뷰(ns_view)를 컨텐츠로 갖는
/// nonactivating + borderless NSPanel. 반드시 메인 스레드에서 호출.
unsafe fn create_raw(ns_view: *mut std::os::raw::c_void, rect: RectF) -> usize {
    eprintln!("[panel] create_raw 진입 rect=({},{},{},{})", rect.x, rect.y, rect.w, rect.h);
    let panel: *mut AnyObject = msg_send![class!(NSPanel), alloc];
    if panel.is_null() {
        return 0;
    }
    let frame = NSRect::new(NSPoint::new(rect.x, rect.y), NSSize::new(rect.w, rect.h));
    let mask: u64 = 1 << 7; // NSWindowStyleMaskNonactivatingPanel (borderless = 0)
    let backing: i64 = 2; // NSBackingStoreBuffered
    let panel: *mut AnyObject = msg_send![panel,
        initWithContentRect: frame,
        styleMask: mask,
        backing: backing,
        defer: false
    ];
    if panel.is_null() {
        eprintln!("[panel] init 실패");
        return 0;
    }
    eprintln!("[panel] init 성공, 웹뷰 이동");
    // 웹뷰 뷰를 패널 컨텐츠로 이동
    let view: *mut AnyObject = ns_view as *mut AnyObject;
    let _: () = msg_send![panel, setContentView: view];
    // 투명 배경
    let _: () = msg_send![panel, setOpaque: false];
    let clear: *mut AnyObject = msg_send![class!(NSColor), clearColor];
    let _: () = msg_send![panel, setBackgroundColor: clear];
    let _: () = msg_send![panel, setHasShadow: false];
    // 접힘 상태 기본: 입력 통과, 전체화면 위 레벨, 모든 스페이스
    let _: () = msg_send![panel, setIgnoresMouseEvents: true];
    let _: () = msg_send![panel, setLevel: 21i64]; // Dock(20) + 1 — Starboard 레시피
    let behavior: u64 = (1u64 << 0)   // canJoinAllSpaces
        | (1u64 << 4)                 // stationary (스페이스 전환에도 제자리)
        | (1u64 << 8)                 // fullScreenAuxiliary (전체화면 위)
        | (1u64 << 10);               // ignoresCycle
    let _: () = msg_send![panel, setCollectionBehavior: behavior];
    let _: () = msg_send![panel, orderFrontRegardless];
    eprintln!("[panel] 생성 완료 addr={:p}", panel);
    panel as usize
}

/// 패널 생성 — 반드시 메인 스레드(setup 등)에서 호출
pub fn create_on_main(ns_view: *mut std::os::raw::c_void, rect: RectF) -> usize {
    unsafe { create_raw(ns_view, rect) }
}

/// 프레임 지정 + 레벨/컬렉션 재단언 + 전면 표시 (매 틱 호출, 메인 큐)
pub fn apply_overlay(panel: usize, rect: RectF, level: i64) {
    if panel == 0 {
        return;
    }
    on_main_async(move || unsafe {
        let p = panel as *mut AnyObject;
        if p.is_null() {
            return;
        }
        let frame = NSRect::new(NSPoint::new(rect.x, rect.y), NSSize::new(rect.w, rect.h));
        let _: () = msg_send![p, setFrame: frame, display: true];
        let _: () = msg_send![p, setLevel: level];
        let behavior: u64 = (1u64 << 0) | (1u64 << 4) | (1u64 << 8) | (1u64 << 10);
        let _: () = msg_send![p, setCollectionBehavior: behavior];
        let _: () = msg_send![p, orderFrontRegardless];
    });
}

/// 마우스 이벤트 통과 토글 (메인 큐)
pub fn set_ignores_on_main(panel: usize, ignore: bool) {
    on_main_async(move || unsafe {
        let p = panel as *mut AnyObject;
        if p.is_null() {
            return;
        }
        let _: () = msg_send![p, setIgnoresMouseEvents: ignore];
    });
}

/// 패널 숨기기/종료 시 사용 (현재는 미사용)
#[allow(dead_code)]
pub fn order_out_on_main(panel: usize) {
    on_main_async(move || unsafe {
        let p = panel as *mut AnyObject;
        if p.is_null() {
            return;
        }
        let nil: *mut AnyObject = std::ptr::null_mut();
        let _: () = msg_send![p, orderOut: nil];
    });
}
