//! 파일 선반/드롭존: 파일을 임시로 올려두고 옮기기 편하게

use serde::Serialize;

#[derive(Clone, Serialize)]
pub struct ShelfItem {
    pub path: String,
    pub name: String,
    pub size_mb: f64,
    pub is_dir: bool,
    pub added: i64,
}

const MAX_ITEMS: usize = 30;

pub fn add(st: &crate::app::AppState, paths: &[String]) -> Vec<ShelfItem> {
    let mut shelf = st.shelf.lock();
    for p in paths {
        let path = std::path::PathBuf::from(p);
        if !path.exists() {
            continue;
        }
        let canon = path.to_string_lossy().to_string();
        if shelf.iter().any(|i| i.path == canon) {
            continue;
        }
        let meta = std::fs::metadata(&path).ok();
        let is_dir = meta.as_ref().map(|m| m.is_dir()).unwrap_or(false);
        let size = meta.as_ref().map(|m| m.len()).unwrap_or(0);
        shelf.push(ShelfItem {
            path: canon,
            name: path
                .file_name()
                .map(|n| n.to_string_lossy().to_string())
                .unwrap_or_default(),
            size_mb: (size as f64 / 1048576.0 * 10.0).round() / 10.0,
            is_dir,
            added: crate::platform::now_ms(),
        });
    }
    while shelf.len() > MAX_ITEMS {
        shelf.remove(0);
    }
    shelf.clone()
}

pub fn list(st: &crate::app::AppState) -> Vec<ShelfItem> {
    st.shelf.lock().clone()
}

pub fn remove(st: &crate::app::AppState, idx: usize) -> Vec<ShelfItem> {
    let mut shelf = st.shelf.lock();
    if idx < shelf.len() {
        shelf.remove(idx);
    }
    shelf.clone()
}

pub fn clear(st: &crate::app::AppState) -> Vec<ShelfItem> {
    st.shelf.lock().clear();
    Vec::new()
}

/// 파일 참조로 클립보드에 복사 (Finder에 붙여넣기 가능)
pub fn copy_refs(st: &crate::app::AppState, idx: Option<usize>) -> Result<usize, String> {
    let shelf = st.shelf.lock();
    let items: Vec<&ShelfItem> = match idx {
        Some(i) => shelf
            .get(i)
            .map(|it| vec![it])
            .ok_or_else(|| "항목이 없습니다".to_string())?,
        None => shelf.iter().collect(),
    };
    let paths: Vec<std::path::PathBuf> = items.iter().map(|it| it.path.clone().into()).collect();
    let n = paths.len();
    if crate::platform::copy_file_refs(&paths) {
        Ok(n)
    } else {
        Err("클립보드 복사 실패".into())
    }
}

pub fn reveal(st: &crate::app::AppState, idx: usize) -> Result<(), String> {
    let path = st
        .shelf
        .lock()
        .get(idx)
        .map(|it| it.path.clone())
        .ok_or_else(|| "항목이 없습니다".to_string())?;
    crate::platform::reveal_in_finder(std::path::Path::new(&path))
}

/// 항목을 기본 앱으로 열기
pub fn open(st: &crate::app::AppState, idx: usize) -> Result<(), String> {
    let path = st
        .shelf
        .lock()
        .get(idx)
        .map(|it| it.path.clone())
        .ok_or_else(|| "항목이 없습니다".to_string())?;
    std::process::Command::new("open")
        .arg(&path)
        .spawn()
        .map(|_| ())
        .map_err(|e| e.to_string())
}

fn pick_folder_on_main(app: &tauri::AppHandle) -> Result<std::path::PathBuf, String> {
    use std::sync::mpsc;
    let (tx, rx) = mpsc::channel::<Option<std::path::PathBuf>>();
    app.run_on_main_thread(move || {
        let picked = rfd::FileDialog::new()
            .set_title("이동할 폴더 선택")
            .pick_folder();
        let _ = tx.send(picked);
    })
    .map_err(|e| e.to_string())?;
    rx.recv()
        .map_err(|_| "폴더 선택 취소".to_string())?
        .ok_or_else(|| "폴더가 선택되지 않았습니다".to_string())
}

/// 선반의 모든 파일(또는 한 개)을 선택한 폴더로 이동. 반환: (성공, 실패)
pub fn move_to(
    app: &tauri::AppHandle,
    st: &crate::app::AppState,
    idx: Option<usize>,
) -> Result<(usize, usize), String> {
    let dir = pick_folder_on_main(app)?;
    let items: Vec<(String, String)> = {
        let shelf = st.shelf.lock();
        match idx {
            Some(i) => shelf
                .get(i)
                .map(|it| vec![(it.path.clone(), it.name.clone())])
                .ok_or_else(|| "항목이 없습니다".to_string())?,
            None => shelf.iter().map(|it| (it.path.clone(), it.name.clone())).collect(),
        }
    };
    let mut ok = 0;
    let mut fail = 0;
    for (src, name) in &items {
        let dst = dir.join(name);
        if move_file(std::path::Path::new(src), &dst).is_ok() {
            ok += 1;
        } else {
            fail += 1;
        }
    }
    {
        let mut shelf = st.shelf.lock();
        // 실제로 이동이 성공한 항목(dst에 존재)만 선반에서 제거
        shelf.retain(|it| {
            let dst = dir.join(std::path::Path::new(&it.path).file_name().unwrap_or_default());
            !(dst.exists() && items.iter().any(|(s, _)| s == &it.path))
        });
    }
    Ok((ok, fail))
}

fn move_file(src: &std::path::Path, dst: &std::path::Path) -> std::io::Result<()> {
    match std::fs::rename(src, dst) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::CrossesDevices => {
            std::fs::copy(src, dst)?;
            std::fs::remove_file(src)
        }
        Err(e) => Err(e),
    }
}
