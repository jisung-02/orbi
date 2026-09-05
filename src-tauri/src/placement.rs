//! 배치 엔진: 오브(동그라미)를 마우스가 있는 화면의 우하단에 붙인다.
//! AppKit 좌표(좌하단 원점) 기준.

use crate::platform::{self, RectF};
use tauri::Manager;

pub const BOTTOM_MARGIN: f64 = 6.0;
pub const EDGE_MARGIN: f64 = 8.0; // 화면 가장자리 여백

/// 오브(동그라미) 형태 전용: 확장 팬이 그려지는 투명 윈도우 영역
pub const ORB_W: f64 = 380.0;
pub const ORB_H: f64 = 300.0;
/// 윈도우 로컬 오브 중심 (AppKit 좌하단 원점): 우하단에 배치
pub const ORB_CX: f64 = 336.0;
pub const ORB_CY: f64 = 36.0;
/// 마우스가 이 반경 안으로 들어오면 펼침
pub const ORB_HOT_R: f64 = 85.0;

/// 오브 호버 판정: 마우스가 오브 중심 핫존 안인지
pub fn orb_in_hot(rect: RectF, mx: f64, my: f64) -> bool {
    let cx = rect.x + ORB_CX;
    let cy = rect.y + ORB_CY;
    (mx - cx).powi(2) + (my - cy).powi(2) <= ORB_HOT_R * ORB_HOT_R
}

/// 마우스가 오브 윈도우(확장 팬 영역) 안에 있는지
pub fn orb_in_window(rect: RectF, mx: f64, my: f64) -> bool {
    mx >= rect.x && mx <= rect.x + rect.w && my >= rect.y && my <= rect.y + rect.h
}

/// 오브 배치: 대상 화면(마우스가 있는 화면) 우하단 고정
pub fn compute(screen: &RectF) -> RectF {
    RectF {
        x: screen.x + screen.w - ORB_W - EDGE_MARGIN,
        y: screen.y + BOTTOM_MARGIN,
        w: ORB_W,
        h: ORB_H,
    }
}

pub fn apply_now(st: &tauri::State<crate::app::AppState>) {
    let screen = platform::screen_containing_mouse().unwrap_or_else(|| {
        let (w, h) = platform::main_screen();
        RectF { x: 0.0, y: 0.0, w, h }
    });
    let rect = compute(&screen);

    // 호버 판정/팝오버 앵커가 읽는 현재 오브 프레임
    *st.orb_rect.lock() = Some(rect);

    // 전체화면/스페이스 전환 후에도 유지되도록 매 틱 패널에 적용 (메인 큐)
    let panel = crate::panel::get(&st.orb_panel);
    if panel == 0 {
        return;
    }
    crate::panel::on_main_async(move || {
        crate::panel::apply_overlay(panel, rect, 1000);
    });
}

/// 배치 폴링: 커서 화면 이동/디스플레이 구성 변경 시 오브 위치 갱신
pub fn placement_loop(app: tauri::AppHandle) {
        loop {
        let st = app.state::<crate::app::AppState>();
        apply_now(&st);
        // 화면이 여러 개면 커서 추적을 빠르게 (0.7s), 하나면 1.5s
        let multi = platform::all_screens().len() > 1;
        std::thread::sleep(std::time::Duration::from_millis(if multi { 700 } else { 1500 }));
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::platform::RectF;

    const SW: f64 = 1470.0;
    const SH: f64 = 956.0;
    const MAIN: RectF = RectF { x: 0.0, y: 0.0, w: SW, h: SH };
    const SECOND: RectF = RectF { x: SW, y: 0.0, w: 1920.0, h: 1080.0 };

    #[test]
    fn orb_pins_bottom_right_of_target_screen() {
        let r = compute(&MAIN);
        assert_eq!(
            (r.x, r.y, r.w, r.h),
            (SW - ORB_W - EDGE_MARGIN, BOTTOM_MARGIN, ORB_W, ORB_H)
        );
    }

    #[test]
    fn orb_follows_second_screen() {
        let r = compute(&SECOND);
        assert_eq!(
            (r.x, r.y, r.w, r.h),
            (SECOND.x + SECOND.w - ORB_W - EDGE_MARGIN, SECOND.y + BOTTOM_MARGIN, ORB_W, ORB_H)
        );
    }

    #[test]
    fn orb_hot_zone_and_window_bounds() {
        let rect = RectF { x: 1082.0, y: 6.0, w: ORB_W, h: ORB_H };
        let cx = rect.x + ORB_CX; // 1418
        let cy = rect.y + ORB_CY; // 42
        assert!(orb_in_hot(rect, cx, cy));
        assert!(orb_in_hot(rect, cx - 50.0, cy + 40.0));
        assert!(!orb_in_hot(rect, cx - 100.0, cy + 100.0));
        assert!(orb_in_window(rect, cx - 100.0, cy + 100.0));
        assert!(!orb_in_window(rect, rect.x - 1.0, cy));
        assert!(!orb_in_window(rect, cx, rect.y + rect.h + 1.0));
    }
}

/// 오브 호버 폴링(25ms): 마우스가 오브에 접근하면 입력을 켜고 펼침,
/// 팬 영역을 250ms 이상 벗어나면 접는다
pub fn orb_loop(app: tauri::AppHandle) {
    use tauri::Manager;
    let mut left_at: Option<std::time::Instant> = None;
    loop {
        std::thread::sleep(std::time::Duration::from_millis(25));
        let st = app.state::<crate::app::AppState>();
        let Some(rect) = *st.orb_rect.lock() else { continue };
        let (mx, my) = platform::mouse_location();
        let expanded = *st.orb_expanded.lock();
        let want_expand = !expanded && orb_in_hot(rect, mx, my);
        let want_collapse = if !expanded || orb_in_window(rect, mx, my) {
            left_at = None;
            false
        } else {
            left_at.get_or_insert_with(std::time::Instant::now).elapsed()
                >= std::time::Duration::from_millis(250)
        };
        if !want_expand && !want_collapse {
            continue;
        }
        let expand_now = want_expand;
        if !expand_now {
            left_at = Some(std::time::Instant::now());
        } else {
            left_at = None;
        }
        let panel = crate::panel::get(&st.orb_panel);
        if panel != 0 {
            crate::panel::set_ignores_on_main(panel, !expand_now);
        }
        tauri::Emitter::emit_to(&app, "orb", "orb-toggle", expand_now).ok();
        *st.orb_expanded.lock() = expand_now;
    }
}
