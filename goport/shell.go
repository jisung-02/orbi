package main

// cgo 브리지: 네이티브 셸(build.sh로 빌드한 libdu_shell.dylib, 소스는 shell.swift)을
// Go로 감싸고, WKWebView 안의 app.js가 그대로 동작하도록 window.__TAURI__ 셸임을 주입한다.
//
// - dylib의 du_* 심볼에 링크하고, 반대로 dylib이 부르는 dockutil_on_* 콜백은
//   -export_dynamic으로 실행파일에서 익스포트한다 (dylib은 -undefined dynamic_lookup).
// - rpath는 실행파일/테스트 바이너리 위치 기준 goport 디렉터리를 가리킨다.

/*
#cgo LDFLAGS: -L${SRCDIR} -ldu_shell -Wl,-rpath,${SRCDIR}
#include <stdlib.h>

void du_app_run(const char* uiDir);
void du_quit(void);
void du_app_activate(void);
char* du_frontmost_bundle(void);
void du_activate_bundle(const char* bundle);
void du_panel_rejoin(long long id);
void du_panel_order_out(long long id);
void du_panel_create(long long id, const char* label, const char* shim, double x, double y, double w, double h, int ignores, long long level, int vibrancy);
void du_panel_close(long long id);
void du_panel_apply(long long id, double x, double y, double w, double h, long long level);
void du_panel_set_ignores(long long id, int ignore);
void du_panel_pin_spaces(long long id);
void du_panel_focus(long long id);
void du_eval(long long id, const char* js);
void du_debug_eval(long long id, const char* js);
double du_mouse(double* x, double* y);
void du_main_screen(double* w, double* h);
int du_screen_count(void);
int du_screen_at(int i, double* x, double* y, double* w, double* h);
int du_ax_trusted(void);
int du_dock_frame(int pid, double* x, double* y, double* w, double* h);
char* du_clip_text(void);
void du_clip_set_text(const char* s);
int du_copy_file_refs(const char* jsonPaths);
void du_pick_folder(long long reqId, const char* title);
void du_post_mouse_move(double x, double y);
void du_post_mouse_click(double x, double y);
void du_post_mouse_click_hid(double x, double y);
void du_post_key(unsigned long long flags, unsigned long long keyCode);
void du_post_mouse_click_pid(int pid, double x, double y);
void du_native_event(const char* name, const char* json);
void du_native_reply(long long seq, int ok, const char* json);
void du_panel_create_native(long long id, const char* kind, double x, double y, double w, double h, long long level, int ignores, int vibrancy);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

const AppVersion = "0.2.0-go"

// ---------- JS 셸임: app.js가 쓰는 window.__TAURI__ 표면을 구현 ----------
// 라벨/버전은 패널마다 다르므로 생성 시 문자열로 치환해 documentStart에서 주입한다.
const shimTemplate = `(function(){
  window.__duLoaded = 42;
  const label = "__LABEL__";
  const listeners = {};
  let msgSeq = 0;
  const pending = {};
  const post = (obj) => { try { window.webkit.messageHandlers.duInvoke.postMessage(obj); } catch (e) {} };
  window.__TAURI__ = {
    core: {
      invoke: (cmd, args) => new Promise((res, rej) => {
        const id = ++msgSeq;
        pending[id] = { res, rej };
        window.__invokes = (window.__invokes || 0) + 1;
        post({ id: id, cmd: cmd, args: args || {} });
      }),
    },
    event: {
      listen: (name, fn) => {
        (listeners[name] = listeners[name] || []).push(fn);
        return Promise.resolve(() => {
          listeners[name] = listeners[name].filter((f) => f !== fn);
        });
      },
      emit: (name, payload) => { post({ cmd: "__emit", args: { name: name, payload: payload } }); return Promise.resolve(); },
    },
    window: {
      getCurrentWindow: () => ({ get label(){ return label; } }),
      getCurrent: () => ({ get label(){ return label; } }),
    },
    app: { getVersion: () => Promise.resolve("__VERSION__") },
  };
  // Go → JS 이벤트 디스패치
  window.__duDispatch = (name, payload) => {
    const e = { payload: payload, event: name, id: 0 };
    (listeners[name] || []).slice().forEach((fn) => { try { fn(e); } catch (err) {} });
  };
  window.onerror = (m) => { try { post({ cmd: "__js_error", args: { msg: String(m) } }); } catch (e) {} };
  // 진단/최적화 측정용 invoke 카운터
  window.__invokes = 0;
  window.__duClicks = 0;
  window.__duLastClick = null;
  document.addEventListener("click", (e) => {
    window.__duClicks = (window.__duClicks || 0) + 1;
    window.__duLastClick = { x: e.clientX, y: e.clientY, target: (e.target && e.target.className) || "" };
  }, true);
  // invoke 응답
  window.__duInvokeResponse = (id, ok, value) => {
    const p = pending[id];
    if (!p) return;
    delete pending[id];
    try { ok ? p.res(value) : p.rej(value); } catch (err) {}
  };
  // navigator.clipboard 폴백 (file:// 에서 미지원 환경 대비)
  const clip = {
    writeText: (t) => { post({ cmd: "__clip_set", args: { text: String(t ?? "") } }); return Promise.resolve(); },
    readText: () => new Promise((res, rej) => {
      const id = ++msgSeq;
      pending[id] = { res, rej };
      post({ id: id, cmd: "__clip_get", args: {} });
    }),
  };
  if (!navigator.clipboard) navigator.clipboard = clip;
})();`

func shimFor(label string) string {
	s := strings.ReplaceAll(shimTemplate, "__LABEL__", label)
	return strings.ReplaceAll(s, "__VERSION__", AppVersion)
}

// ---------- Go → ObjC 래퍼 ----------

func cstr(s string) *C.char { return C.CString(s) }
func freeStr(p *C.char)     { C.free(unsafe.Pointer(p)) }

func AppRun(uiDir string) {
	cs := cstr(uiDir)
	defer freeStr(cs)
	C.du_app_run(cs)
}

func AppQuit() { C.du_quit() }

// 시작 시 1회만 호출 — 틱마다 활성화하면 다른 앱의 호버 UI가 사라진다
func AppActivate() { C.du_app_activate() }

// 최전면 앱 bundle id (자기 자신은 bundle id가 없어 "" 반환)
func FrontmostBundle() string {
	p := C.du_frontmost_bundle()
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

// 다른 앱 활성화 복귀 (오브 접힘 시 활성화 돌려주기)
func ActivateBundle(bundle string) {
	cs := cstr(bundle)
	defer freeStr(cs)
	C.du_activate_bundle(cs)
}

// 스페이스 재합류 (orderOut → orderFront)
func PanelRejoin(id int64) { C.du_panel_rejoin(C.longlong(id)) }

// 오브 숨기기 (orderOut — 어떤 클릭도 받지 않음)
func PanelOrderOut(id int64) { C.du_panel_order_out(C.longlong(id)) }

// 네이티브 UI 응답 (Swift du_native_reply로 전달)
func NativeReply(seq int64, ok bool, json string) {
	cs := cstr(json)
	defer freeStr(cs)
	C.du_native_reply(C.longlong(seq), boolToInt(ok), cs)
}

// 네이티브 패널 생성 (Swift가 위젯 뷰를 만든다)
func PanelCreateNative(id int64, kind string, x, y, w, h float64, level int64, ignores, vibrancy bool) {
	ks := cstr(kind)
	defer freeStr(ks)
	ig := 0
	if ignores {
		ig = 1
	}
	vb := 0
	if vibrancy {
		vb = 1
	}
	C.du_panel_create_native(C.longlong(id), ks, C.double(x), C.double(y), C.double(w), C.double(h), C.longlong(level), C.int(ig), C.int(vb))
}

func PanelCreate(id int64, label, shim string, x, y, w, h float64, ignores bool, level int64, vibrancy bool) {
	l := cstr(label)
	defer freeStr(l)
	sh := cstr(shim)
	defer freeStr(sh)
	ig := 0
	if ignores {
		ig = 1
	}
	vb := 0
	if vibrancy {
		vb = 1
	}
	C.du_panel_create(C.longlong(id), l, sh, C.double(x), C.double(y), C.double(w), C.double(h), C.int(ig), C.longlong(level), C.int(vb))
}

func PanelClose(id int64)               { C.du_panel_close(C.longlong(id)) }
func PanelFocus(id int64)               { C.du_panel_focus(C.longlong(id)) }
func PanelPinSpaces(id int64)           { C.du_panel_pin_spaces(C.longlong(id)) }
func PanelSetIgnores(id int64, ig bool) { C.du_panel_set_ignores(C.longlong(id), boolToInt(ig)) }
func PanelApply(id int64, r Rect, level int64) {
	C.du_panel_apply(C.longlong(id), C.double(r.X), C.double(r.Y), C.double(r.W), C.double(r.H), C.longlong(level))
}

func EvalJS(id int64, js string) {
	cs := cstr(js)
	defer freeStr(cs)
	C.du_eval(C.longlong(id), cs)
}

// 진단용: JS 실행 결과를 로그로 출력
func DebugEval(id int64, js string) {
	cs := cstr(js)
	defer freeStr(cs)
	C.du_debug_eval(C.longlong(id), cs)
}

// 25ms 호버 폴링 경로 — cgo 횡단 1회로 마우스 좌표를 가져온다
func MouseLocation() (float64, float64) {
	var x, y C.double
	C.du_mouse(&x, &y)
	return float64(x), float64(y)
}

func MainScreen() (float64, float64) {
	var w, h C.double
	C.du_main_screen(&w, &h)
	return float64(w), float64(h)
}

func ScreenCount() int { return int(C.du_screen_count()) }

func ScreenAt(i int) (Rect, bool) {
	var x, y, w, h C.double
	if C.du_screen_at(C.int(i), &x, &y, &w, &h) == 0 {
		return Rect{}, false
	}
	return Rect{X: float64(x), Y: float64(y), W: float64(w), H: float64(h)}, true
}

func AxTrusted() bool { return C.du_ax_trusted() != 0 }

func DockFrame(pid int) (Rect, bool) {
	var x, y, w, h C.double
	if C.du_dock_frame(C.int(pid), &x, &y, &w, &h) == 0 {
		return Rect{}, false
	}
	return Rect{X: float64(x), Y: float64(y), W: float64(w), H: float64(h)}, true
}

func ClipText() string {
	p := C.du_clip_text()
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

func ClipSetText(s string) {
	cs := cstr(s)
	defer freeStr(cs)
	C.du_clip_set_text(cs)
}

func CopyFileRefs(paths []string) bool {
	b, _ := json.Marshal(paths)
	cs := cstr(string(b))
	defer freeStr(cs)
	return C.du_copy_file_refs(cs) != 0
}

func PickFolder(reqID int64, title string) {
	cs := cstr(title)
	defer freeStr(cs)
	C.du_pick_folder(C.longlong(reqID), cs)
}

func PostMouseMove(x, y float64) { C.du_post_mouse_move(C.double(x), C.double(y)) }
func PostMouseClick(x, y float64) {
	C.du_post_mouse_click(C.double(x), C.double(y))
}

// HID 탭 경로 클릭 (시스템 최상위)
func PostMouseClickHID(x, y float64) {
	C.du_post_mouse_click_hid(C.double(x), C.double(y))
}

// 특정 프로세스 이벤트 큐에 직접 클릭 주입 (검증용)
func PostMouseClickPid(pid int, x, y float64) {
	C.du_post_mouse_click_pid(C.int(pid), C.double(x), C.double(y))
}

// QA용 키 입력 (flags는 CGEventFlags raw 값)
func PostKey(flags, keyCode uint64) { C.du_post_key(C.ulonglong(flags), C.ulonglong(keyCode)) }

func boolToInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// ---------- 좌표 타입 ----------

// Rect: 논리 포인트. Dock 프레임은 AX(좌상단 원점), 창 배치는 AppKit(좌하단 원점).
type Rect struct {
	X, Y, W, H float64
}

// ---------- 패널 레지스트리 (id → 라벨) ----------

var (
	regMu        sync.Mutex
	reg          = map[int64]string{}
	nativePanels = map[int64]bool{} // 네이티브 뷰 패널(웹뷰 없음) — 이벤트 라우팅 구분용
)

func registerPanel(id int64, label string) {
	registerPanelKind(id, label, false)
}

// 네이티브 패널: 이름 이벤트로, 웹 패널: JS 디스패치로 이벤트를 받는다
func registerPanelKind(id int64, label string, native bool) {
	regMu.Lock()
	reg[id] = label
	nativePanels[id] = native
	regMu.Unlock()
}

func unregisterPanel(id int64) {
	regMu.Lock()
	delete(reg, id)
	delete(nativePanels, id)
	regMu.Unlock()
}

func isNativePanel(id int64) bool {
	regMu.Lock()
	defer regMu.Unlock()
	return nativePanels[id]
}

func idByLabel(label string) (int64, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	for id, l := range reg {
		if l == label {
			return id, true
		}
	}
	return 0, false
}

func allPanelIDs() []int64 {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]int64, 0, len(reg))
	for id := range reg {
		out = append(out, id)
	}
	return out
}

// ---------- 이벤트 emit ----------

// DU_UI=web이면 웹뷰(JS)로, 아니면 네이티브 AppKit UI로 이벤트를 보낸다
func useNativeUI() bool { return os.Getenv("DU_UI") != "web" }

func emitNativeEvent(name string, payload any) {
	pj, _ := json.Marshal(payload)
	ns := cstr(name) // 이벤트 이름은 JSON 마샬링 없이 순수 문자열로
	defer freeStr(ns)
	ps := cstr(string(pj))
	defer freeStr(ps)
	C.du_native_event((*C.char)(unsafe.Pointer(ns)), (*C.char)(unsafe.Pointer(ps)))
}

func emitTo(id int64, name string, payload any) {
	// 대상 패널 종류로 라우팅: 네이티브 패널은 이름 이벤트, 웹 패널은 JS 디스패치.
	// 터미널(웹) 출력이 네이티브 라우팅으로 유실되지 않게 하기 위함.
	if isNativePanel(id) {
		emitNativeEvent(name, payload)
		return
	}
	pj, err := json.Marshal(payload)
	if err != nil {
		return
	}
	nj, _ := json.Marshal(name)
	EvalJS(id, fmt.Sprintf("__duDispatch(%s,%s)", nj, pj))
}

func emitToLabel(label, name string, payload any) {
	if id, ok := idByLabel(label); ok {
		emitTo(id, name, payload)
		return
	}
	// 아직 패널이 없으면 네이티브 이벤트로 브로드캐스트 (라벨 불일치 대비)
	if useNativeUI() {
		emitNativeEvent(name, payload)
	}
}

func emitAll(name string, payload any) {
	if useNativeUI() {
		// 네이티브 뷰는 du_native_event 브로드캐스트 한 번으로 처리
		emitNativeEvent(name, payload)
	}
	// 웹 패널(터미널 등)에는 개별 JS 디스패치
	for _, id := range allPanelIDs() {
		if !isNativePanel(id) {
			emitTo(id, name, payload)
		}
	}
}

// ---------- UI 디렉터리 탐색 ----------

func resolveUIDir() string {
	if v := os.Getenv("DU_UI_DIR"); v != "" {
		if st, err := os.Stat(filepath.Join(v, "index.html")); err == nil && !st.IsDir() {
			return v
		}
	}
	exe, _ := os.Executable()
	cands := []string{
		filepath.Join(filepath.Dir(exe), "ui"),
		"ui",
		filepath.Join("..", "ui"),
		filepath.Join("goport", "..", "ui"),
	}
	for _, c := range cands {
		if st, err := os.Stat(filepath.Join(c, "index.html")); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
	}
	return "ui"
}
