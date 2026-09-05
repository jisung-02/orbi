package main

// ObjC 셸 → Go 콜백 (GCD 메인 스레드에서 호출됨).
// 메인 스레드를 막지 않도록 채널에 넣고 별도 고루틴에서 처리한다.

import "C"

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

type shellEvent struct {
	kind string // "ready" | "message" | "blur" | "closed" | "hotkey" | "folder" | "drop"
	id   int64
	json string
}

var (
	evtCh     chan shellEvent
	folderMu  sync.Mutex
	folderRes = map[int64]chan string{}
	folderSeq int64
)

func startEventLoop(st *AppState) {
	evtCh = make(chan shellEvent, 512)
	go func() {
		for ev := range evtCh {
			switch ev.kind {
			case "ready":
				go onAppReady(st)
			case "message":
				go handleMessage(st, ev.id, ev.json)
			case "blur":
				go onPanelBlur(st, ev.id)
			case "closed":
				go onPanelClosed(st, ev.id)
			case "hotkey":
				go func() {
					if ev.id == 2 {
						// ⌘⌥O: 오브 숨김/표시 토글
						if _, err := cmdToggleOrbHidden(st, Args{}, 0); err == nil {
							println("[orb] hidden 토글됨")
						}
						return
					}
					closeCurrentPopover(st)
				}()
			case "folder":
				folderMu.Lock()
				ch := folderRes[ev.id]
				delete(folderRes, ev.id)
				folderMu.Unlock()
				if ch != nil {
					ch <- ev.json
					close(ch)
				}
			case "drop":
				go onPanelDrop(st, ev.id, ev.json)
			case "space":
				go onSpaceChange(st)
			}
		}
	}()
}

func pushEvent(kind string, id int64, payload string) {
	select {
	case evtCh <- shellEvent{kind: kind, id: id, json: payload}:
	default:
		log.Println("[events] 채널 가득 — 이벤트 드롭:", kind)
	}
}

// ---------- //export: ObjC에서 호출 ----------

//export dockutil_on_ready
func dockutil_on_ready() {
	pushEvent("ready", 0, "")
}

//export dockutil_on_message
func dockutil_on_message(id C.longlong, jsonStr *C.char) {
	payload := ""
	if jsonStr != nil {
		payload = C.GoString(jsonStr)
	}
	pushEvent("message", int64(id), payload)
}

//export dockutil_on_blur
func dockutil_on_blur(id C.longlong) {
	pushEvent("blur", int64(id), "")
}

//export dockutil_on_closed
func dockutil_on_closed(id C.longlong) {
	pushEvent("closed", int64(id), "")
}

//export dockutil_on_hotkey
func dockutil_on_hotkey(kind C.int) {
	pushEvent("hotkey", int64(kind), "")
}

//export dockutil_on_folder
func dockutil_on_folder(reqID C.longlong, path *C.char) {
	p := ""
	if path != nil {
		p = C.GoString(path)
	}
	pushEvent("folder", int64(reqID), p)
}

//export dockutil_on_drop
func dockutil_on_drop(id C.longlong, jsonPaths *C.char) {
	payload := "[]"
	if jsonPaths != nil {
		payload = C.GoString(jsonPaths)
	}
	pushEvent("drop", int64(id), payload)
}

//export dockutil_on_space_change
func dockutil_on_space_change() {
	pushEvent("space", 0, "")
}

// ---------- 메시지 처리 ----------

type jsMessage struct {
	ID   int64           `json:"id"`
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args"`
}

func handleMessage(st *AppState, panelID int64, raw string) {
	var msg jsMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return
	}
	args := Args{}
	if len(msg.Args) > 0 {
		_ = json.Unmarshal(msg.Args, &args)
	}
	reply := func(ok bool, value any) {
		vj, _ := json.Marshal(value)
		if panelID == -1 {
			// 네이티브 UI 호출 — du_native_reply로 응답
			NativeReply(msg.ID, ok, string(vj))
			return
		}
		EvalJS(panelID, "window.__duInvokeResponse && __duInvokeResponse("+itoa(msg.ID)+","+boolJS(ok)+","+string(vj)+")")
	}
	switch msg.Cmd {
	case "__js_error":
		println("[js-error]", args.Str("msg"))
		reply(true, nil)
	case "__clip_get":
		reply(true, ClipText())
	case "__clip_set":
		ClipSetText(args.Str("text"))
		reply(true, nil)
	case "__emit":
		st.events.Publish(args.Str("name"), args["payload"])
		reply(true, nil)
	default:
		h, ok := commandHandlers[msg.Cmd]
		if !ok {
			println("[ipc] unknown command:", msg.Cmd)
			reply(false, "unknown command: "+msg.Cmd)
			return
		}
		if msg.Cmd == "open_popover" {
			println("[ipc] open_popover args:", string(msg.Args))
		}
		go func() {
			value, err := h(st, args, panelID)
			if err != nil {
				reply(false, err.Error())
				return
			}
			reply(true, value)
		}()
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func boolJS(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---------- 패널 블러/종료/드롭 ----------

func onPanelBlur(st *AppState, id int64) {
	label, ok := labelOf(id)
	if !ok {
		return
	}
	st.popoverMu.Lock()
	cur := st.popover
	st.popoverMu.Unlock()
	if cur != label || isPinnedWidget(widgetOfLabel(label)) {
		return
	}
	PanelClose(id)
}

func onPanelClosed(st *AppState, id int64) {
	label, ok := labelOf(id)
	if !ok {
		return
	}
	unregisterPanel(id)
	st.popoverMu.Lock()
	if st.popover == label {
		st.popover = ""
		st.popWidget = ""
	}
	st.popoverMu.Unlock()
	releaseActivationHold(st)
}

func onPanelDrop(st *AppState, id int64, jsonPaths string) {
	var paths []string
	if err := json.Unmarshal([]byte(jsonPaths), &paths); err != nil {
		return
	}
	if len(paths) == 0 {
		return
	}
	list := shelfAdd(st, paths)
	st.events.Publish("shelf", list)
}

// 스페이스/전체화면 전환 직후: 오브와 열려 있는 팝오버를 새 스페이스에 재합류
func onSpaceChange(st *AppState) {
	time.Sleep(350 * time.Millisecond)
	// 숨김 중인 오브는 재합류하지 않는다 (다시 나타나는 것을 방지)
	if !st.orbHidden.Load() {
		if orbID := st.orbPanel.Load(); orbID != 0 {
			PanelRejoin(orbID)
		}
	}
	regMu.Lock()
	ids := make([]int64, 0, len(reg))
	for pid := range reg {
		ids = append(ids, pid)
	}
	regMu.Unlock()
	for _, pid := range ids {
		if int64(pid) != int64(st.orbPanel.Load()) {
			PanelRejoin(pid)
		}
	}
}

func labelOf(id int64) (string, bool) {
	regMu.Lock()
	defer regMu.Unlock()
	l, ok := reg[id]
	return l, ok
}

// 팝오버 라벨 "popover-<widget>"에서 위젯명 추출
func widgetOfLabel(label string) string {
	const p = "popover-"
	if len(label) > len(p) && label[:len(p)] == p {
		return label[len(p):]
	}
	return label
}

// Args: invoke 인자 래퍼 (JS는 camelCase/snake_case를 섞어 보낸다)
type Args map[string]any

func (a Args) Has(key string) bool {
	_, ok := a[key]
	return ok
}

func (a Args) Str(key string) string {
	if v, ok := a[key].(string); ok {
		return v
	}
	return ""
}

func (a Args) OptStr(key string) (string, bool) {
	v, ok := a[key].(string)
	return v, ok
}

// Num: 숫자 인자 (JSON은 float64로 디코딩됨). alt 키 폴백 (tileX/tile_x 등)
func (a Args) Num(keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := a[k]; ok {
			switch n := v.(type) {
			case float64:
				return n, true
			case int:
				return float64(n), true
			case int64:
				return float64(n), true
			case json.Number:
				f, _ := n.Float64()
				return f, true
			}
		}
	}
	return 0, false
}

func (a Args) Bool(key string) bool {
	v, _ := a[key].(bool)
	return v
}

// StrList: 문자열 배열 인자
func (a Args) StrList(key string) []string {
	arr, ok := a[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
