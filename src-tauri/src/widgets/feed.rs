//! 인앱 알림 피드 (이벤트/결과 통합)

use serde::Serialize;
use tauri::{AppHandle, Emitter, Manager};

#[derive(Clone, Serialize)]
pub struct FeedItem {
    pub ts: i64,
    pub icon: String,
    pub title: String,
    pub body: String,
}

const MAX_ITEMS: usize = 60;

pub fn list(st: &crate::app::AppState) -> Vec<FeedItem> {
    st.feed.lock().clone()
}

pub fn push(app: &AppHandle, icon: &str, title: &str, body: &str) {
    let item = FeedItem {
        ts: crate::platform::now_ms(),
        icon: icon.into(),
        title: title.into(),
        body: body.into(),
    };
    {
        let st = app.state::<crate::app::AppState>();
        let mut feed = st.feed.lock();
        feed.insert(0, item.clone());
        feed.truncate(MAX_ITEMS);
    }
    let _ = app.emit("feed-new", &item);
}
