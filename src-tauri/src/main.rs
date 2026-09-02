// Prevents additional console window on Windows in release, DO NOT REMOVE!!
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod app;
mod config;
mod i18n;
mod ipc;
mod placement;
mod platform;
mod widgets;


fn main() {
    // 진단 모드: 접근성 신뢰 + 독 프레임 실측
    if std::env::args().any(|a| a == "--check") {
        let trusted = platform::ax_trusted();
        let dock = platform::dock_frame();
        let screen = platform::main_screen();
        println!("ax_trusted={trusted}");
        println!("dock_frame={dock:?}");
        println!("screen={screen:?}");
        println!("ax_debug={}", platform::ax_debug());
        return;
    }

    tauri::Builder::default()
        .manage(app::AppState::new())
        .invoke_handler(tauri::generate_handler![
            ipc::orb_ready,
            ipc::quit_app,
            ipc::set_lang,
            ipc::set_widget_visible,
            ipc::get_config,
            ipc::pom_start,
            ipc::pom_pause,
            ipc::pom_resume,
            ipc::pom_reset,
            ipc::get_toggles,
            ipc::toggle_dark,
            ipc::toggle_wifi,
            ipc::toggle_bt,
            ipc::toggle_mute,
            ipc::toggle_caffeine,
            ipc::shelf_add,
            ipc::shelf_list,
            ipc::shelf_remove,
            ipc::shelf_clear,
            ipc::shelf_copy,
            ipc::shelf_reveal,
            ipc::shelf_open,
            ipc::shelf_move_to,
            ipc::format_text,
            ipc::get_feed,
            ipc::clear_feed,
            ipc::term_init,
            ipc::term_write,
            ipc::term_resize,
            ipc::term_reset,
            ipc::term_launch_cli,
            ipc::ai_status,
            ipc::open_gui_app,
            ipc::close_popover,
            ipc::open_popover,
            ipc::orb_ready,
            ipc::orb_collapse,
            ipc::popover_ready,
        ])
        .setup(|app| {
                        // 오버레이 유틸리티이므로 Dock 아이콘/앱 전환기에서 숨김
            app.set_activation_policy(tauri::ActivationPolicy::Accessory);
            let handle = app.handle().clone();
            // 전역 단축키: ⌘⌥W = 열려 있는 팝오버 닫기 (다른 앱에 포커스가 있어도 동작)
            use tauri::Manager;
use tauri_plugin_global_shortcut::ShortcutState;
            app.handle().plugin(
                tauri_plugin_global_shortcut::Builder::new()
                    .with_shortcuts(["cmd+alt+w"])?
                    .with_handler(|app, _shortcut, event| {
                        if event.state == ShortcutState::Pressed {
                            ipc::close_current_popover(app);
                        }
                    })
                    .build(),
            )?;
            let lang = config::Config::load().lang;
            crate::platform::notify(
                &crate::i18n::tr(&lang, "dock-util 시작", "dock-util started"),
                &crate::i18n::tr(&lang, "독 옆 글래스 바가 실행되었습니다", "Glass bar is running"),
            );

            // 배치 엔진
            {
                let h = handle.clone();
                std::thread::spawn(move || placement::placement_loop(h));
            }
            // 포모도로 틱
            {
                let h = handle.clone();
                std::thread::spawn(move || widgets::pomodoro::tick_loop(h));
            }
            // 시스템 모니터
            {
                let h = handle.clone();
                std::thread::spawn(move || widgets::monitor::monitor_loop(h));
            }
            // 에이전트 감지
            {
                let h = handle.clone();
                std::thread::spawn(move || widgets::agents::agents_loop(h));
            }
            // 오브 호버 폴링
            {
                let h = handle.clone();
                std::thread::spawn(move || placement::orb_loop(h));
            }
            // 시작 직후 즉시 1회 배치 (메인 스레드)
            {
                let h_call = handle.clone();
                let h_inner = handle.clone();
                let _ = h_call.run_on_main_thread(move || {
                    let st = h_inner.state::<app::AppState>();
                    placement::apply_now(&h_inner, &st);
                });
            }
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running dock-util");
}
