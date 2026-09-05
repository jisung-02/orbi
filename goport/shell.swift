// dock-util 네이티브 셸 (Swift) — Go 백엔드 + Swift 네이티브 하이브리드
//
// NSPanel(nonactivating) + WKWebView로 오브/팝오버를 띄우고, AX로 독 프레임을
// 읽고, Carbon 핫키와 CGEvent QA를 담당한다. Go 측(shell.go)과의 경계는
// du_* C ABI(@_cdecl)이며, Go 쪽 콜백은 du_callbacks.h 선언을
// dynamic_lookup으로 바인딩해 호출한다.
//
// 모든 NSWindow 조작은 메인 큐에서 실행한다 (macOS 백그라운드 조작 트랩 회피).
//
// 주의: du_panel_apply는 다른 앱을 활성화하지 않는다. 틱마다
// activateIgnoringOtherApps를 호출하면 다른 앱이 계속 비활성화되어
// 커서 호버 UI(툴팁 등)가 사라지는 버그가 생긴다. 활성화는 시작 시
// du_app_activate()로 1회만 한다.

import Cocoa
import WebKit
import Carbon
import ApplicationServices

private var gUIDir = ""
private var gAppDelegate: DUAppDelegate?
var panels: [Int64: DUWin] = [:]

// borderless 패널은 기본 canBecomeKey=false → 터미널 타이핑이 안 된다.
// nonactivating 특성은 유지한 채 키 포커스를 받을 수 있게 허용한다.
final class DUPanel: NSPanel, NSDraggingDestination {
    var acceptsShelfFiles = false
    func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        NSLog("[shelf-drop] window entered shelf=%d", acceptsShelfFiles ? 1 : 0)
        return acceptsShelfFiles ? shelfDragOperation(sender) : []
    }
    func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation { acceptsShelfFiles ? shelfDragOperation(sender) : [] }
    func prepareForDragOperation(_ sender: NSDraggingInfo) -> Bool { acceptsShelfFiles && !shelfDragOperation(sender).isEmpty }
    func performDragOperation(_ sender: NSDraggingInfo) -> Bool { acceptsShelfFiles && receiveShelfDrop(sender) }

    override var canBecomeKey: Bool { true }

    // 비활성(키 아님) 상태에서 AppKit이 첫 클릭을 '창 활성화'로 낚아채는 것을 막고
    // 히트된 뷰(WKWebView)로 클릭/이동을 강제 전달한다. 오브는 호버 즉시 클릭이 필수.
    override func sendEvent(_ event: NSEvent) {
        if ProcessInfo.processInfo.environment["DU_DEBUG"] != "",
           event.type == .leftMouseDown || event.type == .leftMouseUp {
            let cv = contentView
            let p = cv.map { $0.convert(event.locationInWindow, from: nil) }
            let hit = p.flatMap { cv?.hitTest($0) }
            NSLog("[sendEvent] type=%d key=%d loc=%@ hit=%@",
                  event.type.rawValue, isKeyWindow, NSStringFromPoint(event.locationInWindow),
                  hit == nil ? "nil" : String(describing: type(of: hit!)))
        }
        if !isKeyWindow {
            switch event.type {
            case .leftMouseDown, .rightMouseDown, .otherMouseDown,
                 .leftMouseUp, .rightMouseUp, .otherMouseUp,
                 .mouseMoved, .leftMouseDragged, .rightMouseDragged:
                if let cv = contentView {
                    let p = cv.convert(event.locationInWindow, from: nil)
                    if let hit = cv.hitTest(p) {
                        switch event.type {
                        case .leftMouseDown, .rightMouseDown, .otherMouseDown:
                            hit.mouseDown(with: event)
                        case .leftMouseUp, .rightMouseUp, .otherMouseUp:
                            hit.mouseUp(with: event)
                        case .leftMouseDragged, .rightMouseDragged:
                            hit.mouseDragged(with: event)
                        default:
                            hit.mouseMoved(with: event)
                        }
                        return
                    }
                }
            default:
                break
            }
        }
        super.sendEvent(event)
    }
}

// ---------- 레지스트리 객체 ----------

final class DUWin {
    let panel: NSPanel
    let web: DUWebView?      // 웹 UI 모드 (DU_UI=web)
    let nativeOrb: OrbFanView? // 네이티브 모드 오브 팬
    let del: DUPanelDelegate // NSWindow.delegate는 weak이므로 직접 보유
    let mh: DUMessageHandler?
    let nav: DUNavDelegate?

    init(panel: NSPanel, web: DUWebView?, nativeOrb: OrbFanView?, del: DUPanelDelegate,
         mh: DUMessageHandler?, nav: DUNavDelegate?) {
        self.panel = panel
        self.web = web
        self.nativeOrb = nativeOrb
        self.del = del
        self.mh = mh
        self.nav = nav
    }
}

final class DUPanelDelegate: NSObject, NSWindowDelegate {
    let panelId: Int64
    init(panelId: Int64) { self.panelId = panelId }

    func windowDidResignKey(_ notification: Notification) {
        dockutil_on_blur(panelId)
    }

    func windowWillClose(_ notification: Notification) {
        panels[panelId] = nil
        dockutil_on_closed(panelId)
    }
}

final class DUMessageHandler: NSObject, WKScriptMessageHandler {
    let panelId: Int64
    init(panelId: Int64) { self.panelId = panelId }

    func userContentController(_ userContentController: WKUserContentController,
                               didReceive message: WKScriptMessage) {
        guard let dict = message.body as? [String: Any],
              let data = try? JSONSerialization.data(withJSONObject: dict),
              let json = String(data: data, encoding: .utf8) else { return }
        dockutil_on_message(panelId, json)
    }
}

// 페이지 로드 진단용
final class DUNavDelegate: NSObject, WKNavigationDelegate {
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        NSLog("[nav] didFinish \(webView.url?.absoluteString ?? "?")")
    }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        NSLog("[nav] FAIL \(error.localizedDescription)")
    }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        NSLog("[nav] PROVISIONAL FAIL \(error.localizedDescription)")
    }
}

// 비활성 상태에서도 첫 클릭이 웹뷰로 전달되어야 한다 (acceptsFirstMouse)
final class DUWebView: WKWebView {
    override func acceptsFirstMouse(for event: NSEvent?) -> Bool { true }

    // WebKit이 페이지 로드 후 자체 드래그 타입으로 덮어쓰므로 fileURL을 항상 유지한다 —
    // 아니면 다른 앱에서 끌어온 파일 드롭이 패널에 도달하지 않는다.
    override func registerForDraggedTypes(_ newTypes: [NSPasteboard.PasteboardType]) {
        var types = newTypes
        if !types.contains(.fileURL) { types.append(.fileURL) }
        super.registerForDraggedTypes(types)
    }

    override init(frame frameRect: NSRect, configuration: WKWebViewConfiguration) {
        super.init(frame: frameRect, configuration: configuration)
        registerForDraggedTypes([.fileURL])
    }
    required init?(coder: NSCoder) { fatalError("unsupported") }

    private func fileURLs(_ sender: NSDraggingInfo) -> [URL] {
        sender.draggingPasteboard.readObjects(forClasses: [NSURL.self],
                                              options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        fileURLs(sender).isEmpty ? super.draggingEntered(sender) : .copy
    }

    override func draggingUpdated(_ sender: NSDraggingInfo) -> NSDragOperation {
        fileURLs(sender).isEmpty ? super.draggingUpdated(sender) : .copy
    }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        let urls = fileURLs(sender)
        if !urls.isEmpty {
            let paths = urls.compactMap { $0.path }
            if let data = try? JSONSerialization.data(withJSONObject: paths),
               let json = String(data: data, encoding: .utf8) {
                let pid = panels.first { $0.value.web === self }?.key ?? 0
                if pid > 0 { dockutil_on_drop(pid, json) }
            }
            return true
        }
        return super.performDragOperation(sender)
    }
}

// ---------- 앱 생명주기 ----------

final class DUAppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        DispatchQueue.main.async { dockutil_on_ready() }
    }
}

private func installHotkey() {
    var spec = EventTypeSpec(eventClass: OSType(kEventClassKeyboard), eventKind: UInt32(kEventHotKeyPressed))
    InstallEventHandler(GetApplicationEventTarget(), { _, eventRef, _ in
        var hkID = EventHotKeyID()
        let st = GetEventParameter(eventRef, EventParamName(kEventParamDirectObject),
                                   EventParamName(typeEventHotKeyID), nil,
                                   MemoryLayout<EventHotKeyID>.size, nil, &hkID)
        NSLog("[hotkey] fired id=%d status=%d", hkID.id, st)
        if st == noErr {
            dockutil_on_hotkey(Int32(hkID.id))
        } else {
            dockutil_on_hotkey(1)
        }
        return noErr
    }, 1, &spec, nil, nil)
    var hotRef: EventHotKeyRef?
    let sig = OSType(0x6475_776B) /* 'duwk' */
    let r1 = RegisterEventHotKey(UInt32(kVK_ANSI_W), UInt32(cmdKey | optionKey),
                        EventHotKeyID(signature: sig, id: 1), GetApplicationEventTarget(), 0, &hotRef)
    var hotRef2: EventHotKeyRef?
    let r2 = RegisterEventHotKey(UInt32(kVK_ANSI_O), UInt32(cmdKey | optionKey),
                        EventHotKeyID(signature: sig, id: 2), GetApplicationEventTarget(), 0, &hotRef2)
    NSLog("[hotkey] register W=%d O=%d", r1, r2)
}

@_cdecl("du_app_run")
public func du_app_run(_ uiDir: UnsafePointer<CChar>!) {
    gUIDir = uiDir != nil ? String(cString: uiDir) : ""
    panels = [:]
    NSLog("[shell] uiDir=\(gUIDir)")
    _ = NSApplication.shared
    NSApp.setActivationPolicy(.accessory)
    installHotkey()
    gAppDelegate = DUAppDelegate()
    NSApp.delegate = gAppDelegate
    // 크래시 진단: ObjC 예외 + SIGTRAP 시 백트레이스를 stderr로 남긴다
    NSSetUncaughtExceptionHandler { ex in
        NSLog("[crash] EXCEPTION %@: %@\n%@",
              ex.name.rawValue, ex.reason ?? "-",
              ex.callStackSymbols.joined(separator: "\n"))
    }
    signal(SIGTRAP) { _ in
        NSLog("[crash] SIGTRAP\n" + Thread.callStackSymbols.joined(separator: "\n"))
        signal(SIGTRAP, SIG_DFL)
        raise(SIGTRAP)
    }
    // 진단: DU_DEBUG=1일 때 마우스 이벤트 흐름 추적
    if ProcessInfo.processInfo.environment["DU_DEBUG"] != "" {
        NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .leftMouseUp]) { e in
            NSLog("[events] GLOBAL \(e.type) @\(NSEvent.mouseLocation)")
        }
        NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .leftMouseUp]) { e in
            NSLog("[events] LOCAL \(e.type) @\(e.locationInWindow)")
            return e
        }
    }
    // 스페이스/전체화면 전환 → 오브를 새 스페이스에 재합류 (활성화 불필요)
    NotificationCenter.default.addObserver(forName: NSWorkspace.activeSpaceDidChangeNotification,
                                           object: nil, queue: .main) { _ in
        dockutil_on_space_change()
    }
    NSApp.run()
}

@_cdecl("du_quit")
public func du_quit() {
    DispatchQueue.main.async { NSApp.terminate(nil) }
}

// 시작 시 1회 호출 — 틱마다 활성화하면 다른 앱의 호버 UI가 사라진다
@_cdecl("du_app_activate")
public func du_app_activate() {
    DispatchQueue.main.async { NSApp.activate(ignoringOtherApps: true) }
}

// 현재 최전면 앱의 bundle id (우린 ad-hoc 바이너라 bundle id가 없어 자기 자신은 ""로 나온다)
@_cdecl("du_frontmost_bundle")
public func du_frontmost_bundle() -> UnsafeMutablePointer<CChar>! {
    strdup(NSWorkspace.shared.frontmostApplication?.bundleIdentifier ?? "")
}

// bundle id로 다른 앱을 다시 활성화 (오브를 접었을 때 활성화를 돌려주는 용도)
@_cdecl("du_activate_bundle")
public func du_activate_bundle(_ bundle: UnsafePointer<CChar>!) {
    let id = bundle != nil ? String(cString: bundle) : ""
    guard !id.isEmpty else { return }
    DispatchQueue.main.async {
        if let app = NSWorkspace.shared.runningApplications.first(where: { $0.bundleIdentifier == id }) {
            _ = app.activate(options: [])
        }
    }
}

// ---------- 패널 생성/조작 ----------

@_cdecl("du_panel_create")
public func du_panel_create(_ id: Int64, _ label: UnsafePointer<CChar>!, _ shim: UnsafePointer<CChar>!,
                            _ x: Double, _ y: Double, _ w: Double, _ h: Double,
                            _ ignores: Int32, _ level: Int64, _ vibrancy: Int32) {
    // C 문자열은 Go가 함수 반환 즉시 해제하므로 메인 큐로 넘기기 전에 복사한다
    let shimSrc = shim != nil ? String(cString: shim) : ""
    let wantVibrancy = vibrancy != 0
    DispatchQueue.main.async {
        let frame = NSRect(x: x, y: y, width: w, height: h)
        let panel = DUPanel(contentRect: frame,
                            styleMask: [.nonactivatingPanel, .borderless],
                            backing: .buffered, defer: false)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.ignoresMouseEvents = ignores != 0
        panel.level = NSWindow.Level(rawValue: Int(level))
        panel.isReleasedWhenClosed = false
        panel.acceptsMouseMovedEvents = true // CSS :hover용 mouseMoved 수신
        // canJoinAllSpaces | stationary | fullScreenAuxiliary | ignoresCycle
        panel.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary, .ignoresCycle]

        let cfg = WKWebViewConfiguration()
        cfg.websiteDataStore = .nonPersistent() // 임시 스토어: 디스크 캐시/아이콘 DB 오버헤드 없음
        cfg.suppressesIncrementalRendering = true // 첫 렌더 준비 전 흰 화면/낭비 방지
        cfg.setValue(false, forKey: "drawsBackground")
        cfg.preferences.setValue(true, forKey: "developerExtrasEnabled")
        let ucc = WKUserContentController()
        ucc.addUserScript(WKUserScript(source: shimSrc, injectionTime: .atDocumentStart, forMainFrameOnly: true))
        let mh = DUMessageHandler(panelId: id)
        ucc.add(mh, name: "duInvoke")
        cfg.userContentController = ucc

        let web = DUWebView(frame: frame, configuration: cfg)
        web.setValue(false, forKey: "drawsBackground")
        web.autoresizingMask = [.width, .height]

        let del = DUPanelDelegate(panelId: id)
        panel.delegate = del
        let nav = DUNavDelegate()
        web.navigationDelegate = nav

        panels[id] = DUWin(panel: panel, web: web, nativeOrb: nil, del: del, mh: mh, nav: nav)

        if wantVibrancy {
            // 팝오버: 네이티브 HUD vibrancy(GPU 경로)를 깔고 웹뷰를 그 위에 얹는다 —
            // CSS backdrop-filter blur(30px)의 매 프레임 GPU 블러를 대체한다
            let container = NSView(frame: NSRect(origin: .zero, size: frame.size))
            container.autoresizingMask = [.width, .height]
            container.wantsLayer = true
            let effect = NSVisualEffectView(frame: container.bounds)
            effect.material = .hudWindow
            effect.blendingMode = .behindWindow
            effect.state = .active
            effect.wantsLayer = true
            effect.layer?.cornerRadius = 18
            effect.layer?.masksToBounds = true
            effect.autoresizingMask = [.width, .height]
            // 웹뷰는 패널 전역 좌표로 생성되므로 컨테이너 로컬 (0,0)으로 반드시 재배치 —
            // 아니면 뷰가 컨테이너 밖으로 밀려나 유리 카드만 보이고 내용이 안 보인다
            web.frame = container.bounds
            container.addSubview(effect)
            container.addSubview(web, positioned: .above, relativeTo: effect)
            panel.contentView = container
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { [weak web] in
                guard let web else { return }
                NSLog("[shell] vibrancy container=%@ web=%@",
                      NSStringFromRect(container.bounds), NSStringFromRect(web.frame))
            }
        } else {
            // 오브: 완전 투명 — 유리 카드 없이 동그라미만 떠 있어야 한다
            panel.contentView = web
        }
        panel.orderFrontRegardless()

        let dir = URL(fileURLWithPath: gUIDir, isDirectory: true)
        NSLog("[shell] panel \(id) created \(Int(w))x\(Int(h)) @(\(Int(x)),\(Int(y)))")
        web.loadFileURL(dir.appendingPathComponent("index.html"), allowingReadAccessTo: dir)
    }
}

@_cdecl("du_panel_close")
public func du_panel_close(_ id: Int64) {
    DispatchQueue.main.async {
        guard let win = panels[id] else { return }
        win.panel.orderOut(nil)
        win.panel.close()
    }
}

// 프레임 + 레벨/컬렉션 재단언 + 전면 표시 (Rust apply_overlay 동일 — 활성화만 제외).
// canJoinAllSpaces 재단언이 스페이스가 바뀐 뒤 오브를 그 스페이스 위로 재합류시킨다.
// orderFrontRegardless/재단언은 활성화를 가져오지 않으므로 호버 UI 무해.
@_cdecl("du_panel_apply")
public func du_panel_apply(_ id: Int64, _ x: Double, _ y: Double, _ w: Double, _ h: Double, _ level: Int64) {
    DispatchQueue.main.async {
        guard let win = panels[id] else { return }
        let p = win.panel
        // 불변 프레임/레벨 재설정은 Core Animation 커밋을 유발하므로 변경 시에만
        let frame = NSRect(x: x, y: y, width: w, height: h)
        if !NSEqualRects(p.frame, frame) { p.setFrame(frame, display: true) }
        let wantLevel = NSWindow.Level(rawValue: Int(level))
        if p.level != wantLevel { p.level = wantLevel }
        // 전면 재단언/컬렉션은 활성화를 가져오지 않는 저비용 호출 — 매 틱 유지
        p.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary, .ignoresCycle]
        p.orderFrontRegardless()
    }
}

@_cdecl("du_panel_set_ignores")
public func du_panel_set_ignores(_ id: Int64, _ ignore: Int32) {
    DispatchQueue.main.async {
        guard let win = panels[id] else { return }
        win.panel.ignoresMouseEvents = ignore != 0
        if duDebug { NSLog("[shell] panel %lld ignoresMouseEvents=%d", id, ignore) }
    }
}

@_cdecl("du_panel_pin_spaces")
public func du_panel_pin_spaces(_ id: Int64) {
    DispatchQueue.main.async {
        panels[id]?.panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
    }
}

// 오브 숨김/표시 (숨김 중에는 어떤 클릭도 받지 않는다)
@_cdecl("du_panel_order_out")
public func du_panel_order_out(_ id: Int64) {
    DispatchQueue.main.async {
        guard let win = panels[id] else {
            NSLog("[order_out] panel %lld 없음", id)
            return
        }
        // canJoinAllSpaces 패널은 orderOut해도 스페이스 합류가 되살아나므로
        // 합류 동작을 먼저 해제하고 내린다 (핫키 윈도우 앱 표준 레시피)
        win.panel.collectionBehavior = [.ignoresCycle]
        win.panel.orderOut(nil)
        NSLog("[order_out] panel %lld done isVisible=%d", id, win.panel.isVisible ? 1 : 0)
    }
}

// 스페이스 재합류: orderOut → orderFrontRegardless로 현재 스페이스에 다시 붙인다.
// 전체화면 스페이스 진입 시 canJoinAllSpaces 패널이 콘텐츠 아래로 깔리는 것을 되살린다.
@_cdecl("du_panel_rejoin")
public func du_panel_rejoin(_ id: Int64) {
    DispatchQueue.main.async {
        guard let win = panels[id] else { return }
        win.panel.orderOut(nil)
        win.panel.orderFrontRegardless()
    }
}

@_cdecl("du_panel_focus")
public func du_panel_focus(_ id: Int64) {
    DispatchQueue.main.async {
        guard let p = panels[id]?.panel else { return }
        p.orderFrontRegardless()
        p.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
}

@_cdecl("du_eval")
public func du_eval(_ id: Int64, _ js: UnsafePointer<CChar>!) {
    let code = js != nil ? String(cString: js) : ""
    DispatchQueue.main.async {
        guard let win = panels[id], let web = win.web else { return } // 네이티브 패널은 JS 없음
        web.evaluateJavaScript(code, completionHandler: nil)
    }
}

// 진단용: JS 실행 결과를 로그로
@_cdecl("du_debug_eval")
public func du_debug_eval(_ id: Int64, _ js: UnsafePointer<CChar>!) {
    let code = js != nil ? String(cString: js) : ""
    DispatchQueue.main.async {
        guard let win = panels[id], let web = win.web else { return }
        web.evaluateJavaScript(code) { result, error in
            if let error { NSLog("[debug-eval] err \(error.localizedDescription)") }
            else { NSLog("[debug-eval] \(result ?? "nil")") }
        }
    }
}

// ---------- 좌표/화면 ----------

@_cdecl("du_mouse")
public func du_mouse(_ x: UnsafeMutablePointer<Double>!, _ y: UnsafeMutablePointer<Double>!) {
    let m = NSEvent.mouseLocation // 25ms 폴링 경로 — cgo 횡단을 1회로 줄인다
    x.pointee = m.x
    y.pointee = m.y
}

@_cdecl("du_main_screen")
public func du_main_screen(_ w: UnsafeMutablePointer<Double>!, _ h: UnsafeMutablePointer<Double>!) {
    let b = CGDisplayBounds(CGMainDisplayID())
    w.pointee = Double(b.width)
    h.pointee = Double(b.height)
}

@_cdecl("du_screen_count")
public func du_screen_count() -> Int32 {
    var n: UInt32 = 0
    guard CGGetActiveDisplayList(0, nil, &n) == .success else { return 0 }
    return Int32(n)
}

// AppKit 좌하단 원점으로 변환된 i번째 화면 프레임. 성공 시 1.
@_cdecl("du_screen_at")
public func du_screen_at(_ i: Int32, _ x: UnsafeMutablePointer<Double>!, _ y: UnsafeMutablePointer<Double>!,
                         _ w: UnsafeMutablePointer<Double>!, _ h: UnsafeMutablePointer<Double>!) -> Int32 {
    var n: UInt32 = 0
    guard CGGetActiveDisplayList(0, nil, &n) == .success, n > 0, i >= 0, Int(i) < Int(n) else { return 0 }
    var ids = [CGDirectDisplayID](repeating: 0, count: Int(n))
    guard CGGetActiveDisplayList(n, &ids, &n) == .success else { return 0 }
    var primaryH: CGFloat = 0
    var hasPrimary = false
    for id in ids {
        let b = CGDisplayBounds(id)
        if b.origin.x == 0 && b.origin.y == 0 { primaryH = b.height; hasPrimary = true; break }
    }
    guard hasPrimary else { return 0 }
    let b = CGDisplayBounds(ids[Int(i)])
    x.pointee = Double(b.origin.x)
    y.pointee = Double(primaryH - b.origin.y - b.height)
    w.pointee = Double(b.width)
    h.pointee = Double(b.height)
    return 1
}

// ---------- 접근성: Dock 프레임 ----------

private func readAXRect(_ el: AXUIElement,
                        _ x: UnsafeMutablePointer<Double>, _ y: UnsafeMutablePointer<Double>,
                        _ w: UnsafeMutablePointer<Double>, _ h: UnsafeMutablePointer<Double>) -> Bool {
    var pv: CFTypeRef?
    var sv: CFTypeRef?
    guard AXUIElementCopyAttributeValue(el, "AXPosition" as CFString, &pv) == .success, pv != nil else { return false }
    guard AXUIElementCopyAttributeValue(el, "AXSize" as CFString, &sv) == .success, sv != nil else { return false }
    var pt = CGPoint.zero
    var sz = CGSize.zero
    _ = AXValueGetValue(pv as! AXValue, .cgPoint, &pt)
    _ = AXValueGetValue(sv as! AXValue, .cgSize, &sz)
    x.pointee = Double(pt.x)
    y.pointee = Double(pt.y)
    w.pointee = Double(sz.width)
    h.pointee = Double(sz.height)
    return true
}

@_cdecl("du_ax_trusted")
public func du_ax_trusted() -> Int32 {
    AXIsProcessTrustedWithOptions(nil) ? 1 : 0
}

// pid는 Go가 pgrep으로 캐싱해서 넘긴다. 성공 시 1.
@_cdecl("du_dock_frame")
public func du_dock_frame(_ pid: Int32, _ x: UnsafeMutablePointer<Double>!, _ y: UnsafeMutablePointer<Double>!,
                          _ w: UnsafeMutablePointer<Double>!, _ h: UnsafeMutablePointer<Double>!) -> Int32 {
    guard AXIsProcessTrustedWithOptions(nil) else { return 0 }
    let app = AXUIElementCreateApplication(pid)
    var found = false
    // 1순위: AXDocks (macOS 26 이전)
    var docks: CFTypeRef?
    if AXUIElementCopyAttributeValue(app, "AXDocks" as CFString, &docks) == .success,
       let arr = docks as? NSArray, arr.count > 0 {
        found = readAXRect(arr[0] as! AXUIElement, x, y, w, h)
    }
    // 2순위: macOS 26+ AXDocks 제거 → AXChildren에서 큰 사각형 폴백
    if !found {
        var children: CFTypeRef?
        if AXUIElementCopyAttributeValue(app, "AXChildren" as CFString, &children) == .success,
           let arr = children as? NSArray {
            for item in arr {
                var cx = 0.0, cy = 0.0, cw = 0.0, ch = 0.0
                if readAXRect(item as! AXUIElement, &cx, &cy, &cw, &ch), cw > 100, ch > 10 {
                    x.pointee = cx; y.pointee = cy; w.pointee = cw; h.pointee = ch
                    found = true
                    break
                }
            }
        }
    }
    return found ? 1 : 0
}

// ---------- 클립보드 ----------

// strdup된 UTF8 문자열 반환 (Go에서 free)
@_cdecl("du_clip_text")
public func du_clip_text() -> UnsafeMutablePointer<CChar>! {
    DispatchQueue.main.sync {
        strdup(NSPasteboard.general.string(forType: .string) ?? "")
    }
}

@_cdecl("du_clip_set_text")
public func du_clip_set_text(_ s: UnsafePointer<CChar>!) {
    let str = s != nil ? String(cString: s) : ""
    DispatchQueue.main.async {
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString(str, forType: .string)
    }
}

// 파일 경로(JSON 배열)를 파일 참조로 클립보드에 복사. 성공 1.
@_cdecl("du_copy_file_refs")
public func du_copy_file_refs(_ jsonPaths: UnsafePointer<CChar>!) -> Int32 {
    let raw = jsonPaths != nil ? String(cString: jsonPaths) : "[]"
    guard let data = raw.data(using: .utf8),
          let arr = try? JSONSerialization.jsonObject(with: data) as? [String],
          !arr.isEmpty else { return 0 }
    let urls = arr.map { URL(fileURLWithPath: $0) as NSURL }
    var ok = false
    DispatchQueue.main.sync {
        let pb = NSPasteboard.general
        pb.clearContents()
        ok = pb.writeObjects(urls)
    }
    return ok ? 1 : 0
}

// ---------- 폴더 선택 (비동기 — 메인 루프 막지 않음) ----------

@_cdecl("du_pick_folder")
public func du_pick_folder(_ reqId: Int64, _ title: UnsafePointer<CChar>!) {
    let t = title != nil ? String(cString: title) : ""
    DispatchQueue.main.async {
        let p = NSOpenPanel()
        p.canChooseDirectories = true
        p.canChooseFiles = false
        p.allowsMultipleSelection = false
        p.title = t
        p.begin { resp in
            let path = (resp == .OK) ? (p.urls.first?.path ?? "") : ""
            dockutil_on_folder(reqId, path)
        }
    }
}

// ---------- QA: 마우스 이벤트 시뮬레이션 ----------

private func postMouseEvent(_ type: CGEventType, _ x: Double, _ y: Double) {
    let ev = CGEvent(mouseEventSource: nil, mouseType: type,
                     mouseCursorPosition: CGPoint(x: x, y: y), mouseButton: .left)
    ev?.setIntegerValueField(.mouseEventClickState, value: 1) // clickState 0이면 AppKit이 무시한다
    ev?.post(tap: .cgSessionEventTap)
}

@_cdecl("du_post_mouse_move")
public func du_post_mouse_move(_ x: Double, _ y: Double) {
    postMouseEvent(.mouseMoved, x, y)
}

@_cdecl("du_post_mouse_click")
public func du_post_mouse_click(_ x: Double, _ y: Double) {
    postMouseEvent(.leftMouseDown, x, y)
    usleep(50_000)
    postMouseEvent(.leftMouseUp, x, y)
}

// 검증용: 자기 프로세스 이벤트 큐에 직접 클릭 주입 (윈도우 서버 히트테스트 생략)
@_cdecl("du_post_mouse_click_pid")
public func du_post_mouse_click_pid(_ pid: Int32, _ x: Double, _ y: Double) {
    let down = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                       mouseCursorPosition: CGPoint(x: x, y: y), mouseButton: .left)
    down?.setIntegerValueField(.mouseEventClickState, value: 1)
    down?.postToPid(pid)
    usleep(50_000)
    let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                     mouseCursorPosition: CGPoint(x: x, y: y), mouseButton: .left)
    up?.setIntegerValueField(.mouseEventClickState, value: 1)
    up?.postToPid(pid)
}

// QA용 클릭 (HID 탭 — 시스템 최상위 경로)
@_cdecl("du_post_mouse_click_hid")
public func du_post_mouse_click_hid(_ x: Double, _ y: Double) {
    let ev = CGEvent(mouseEventSource: nil, mouseType: .leftMouseDown,
                     mouseCursorPosition: CGPoint(x: x, y: y), mouseButton: .left)
    ev?.setIntegerValueField(.mouseEventClickState, value: 1)
    ev?.post(tap: .cghidEventTap)
    usleep(50_000)
    let up = CGEvent(mouseEventSource: nil, mouseType: .leftMouseUp,
                     mouseCursorPosition: CGPoint(x: x, y: y), mouseButton: .left)
    up?.setIntegerValueField(.mouseEventClickState, value: 1)
    up?.post(tap: .cghidEventTap)
}

// QA용 키 입력 (modifiers는 CGEventFlags raw: control=0x40000, command=0x100000)
@_cdecl("du_post_key")
public func du_post_key(_ flags: UInt64, _ keyCode: UInt64) {
    let down = CGEvent(keyboardEventSource: nil, virtualKey: CGKeyCode(keyCode), keyDown: true)
    down?.flags = CGEventFlags(rawValue: flags)
    down?.post(tap: .cgSessionEventTap)
    usleep(30_000)
    let up = CGEvent(keyboardEventSource: nil, virtualKey: CGKeyCode(keyCode), keyDown: false)
    up?.flags = CGEventFlags(rawValue: flags)
    up?.post(tap: .cgSessionEventTap)
}
