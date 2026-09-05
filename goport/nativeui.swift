// nativeui.swift — 네이티브 UI (JS/웹뷰 제거 버전)
//
// DU_UI=web이 아니면 팝오버/오브를 AppKit 뷰로 렌더링한다.
//  - Swift→Go: dockutil_on_message(-1, {seq,cmd,args}) 로 기존 커맨드 디스패처 재사용
//  - Go→Swift: du_native_event(name, json) / 응답은 du_native_reply(seq, ok, json)
//  - 터미널은 별도 xterm.js 웹 패널을 사용한다.

import Cocoa

let duDebug = ProcessInfo.processInfo.environment["DU_DEBUG"] == "1"

final class NativeUI: NSObject {
    static let shared = NativeUI()

    private var seq: Int64 = 0
    private var pending: [Int64: (_ ok: Bool, _ value: Any?) -> Void] = [:]
    private let pendingLock = NSLock()

    private(set) var orbPanel: Int64 = 0
    private(set) var popPanel: Int64 = 0
    private(set) var popWidget: String = ""

    // 배지 카운트 (JS S.* 대체)
    private(set) var agentsList: [[String: Any]] = []
    private(set) var shelfList: [[String: Any]] = []
    private(set) var feedUnread = 0
    private(set) var lastStats: [String: Any]?
    private(set) var lastAgents: [[String: Any]] = []

    var orbView: OrbFanView? { panels[orbPanel]?.nativeOrb }

    // ---------- invoke: Go 커맨드 호출 ----------
    func invoke(_ cmd: String, _ args: [String: Any] = [:], reply: ((Any?, String?) -> Void)? = nil) {
        seq += 1
        let s = seq
        if let r = reply {
            pendingLock.lock()
            pending[s] = { ok, value in
                DispatchQueue.main.async {
                    let err = ok ? nil : (value as? String ?? "error")
                    r(ok ? value : nil, err)
                }
            }
            pendingLock.unlock()
        }
        let body: [String: Any] = ["id": s, "cmd": cmd, "args": args]
        guard let data = try? JSONSerialization.data(withJSONObject: body),
              let json = String(data: data, encoding: .utf8) else { return }
        dockutil_on_message(-1, (json as NSString).utf8String!)
    }

    // Go → du_native_reply(seq, ok, json)
    func handleReply(seq: Int64, ok: Bool, json: String) {
        pendingLock.lock()
        let cb = pending.removeValue(forKey: seq)
        pendingLock.unlock()
        guard let cb else { return }
        var value: Any?
        if let data = json.data(using: .utf8), json != "null" {
            value = try? JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
        }
        cb(ok, ok ? value : (value as? String ?? json))
    }

    // Go → du_native_event(name, json)
    func handleEvent(name: String, json: String) {
        if duDebug { NSLog("[event] %@", name) }
        DispatchQueue.main.async {
            let value = json.data(using: .utf8).flatMap {
                try? JSONSerialization.jsonObject(with: $0, options: [.fragmentsAllowed])
            }
            self.route(name: name, value: value)
        }
    }

    private func route(name: String, value: Any?) {
        if duDebug { NSLog("[route] %@", name) }
        switch name {
        case "orb-toggle":
            var foundOrb: OrbFanView?
            for (_, win) in panels {
                if let o = win.nativeOrb { foundOrb = o }
            }
            if let o = foundOrb {
                if duDebug { NSLog("[route] FOUND orb, expanding") }
                o.setExpanded((value as? Bool) == true)
            } else {
                if duDebug { NSLog("[route] NO ORB FOUND in %d panels", panels.count) }
            }
        case "orb-idle":
            orbView?.setIdle((value as? Bool) == true)
        case "pomodoro":
            if let snap = value as? [String: Any] {
                TimerPopoverController.shared.update(snapshot: snap)
                OrbBadgeState.shared.updatePomodoro(snap)
            }
        case "stats":
            if let s = value as? [String: Any] {
                lastStats = s
                MonitorPopoverController.shared.update(stats: s)
            }
        case "agents":
            if let d = value as? [String: Any] {
                agentsList = d["list"] as? [[String: Any]] ?? []
                lastAgents = agentsList
                OrbBadgeState.shared.setBadge("agents", agentsList.isEmpty ? "" : String(agentsList.count))
                AgentsPopoverController.shared.update(list: agentsList)
            }
        case "shelf":
            if let list = value as? [[String: Any]] {
                shelfList = list
                OrbBadgeState.shared.setBadge("shelf", list.isEmpty ? "" : String(list.count))
                ShelfPopoverController.shared.update(list: list)
            }
        case "feed":
            feedUnread = 0
            OrbBadgeState.shared.setBadge("feed", "")
            if let list = value as? [[String: Any]] {
                FeedPopoverController.shared.update(list: list)
            }
        case "feed-new":
            if popWidget == "feed" { feedUnread = 0 } else { feedUnread = min(feedUnread + 1, 99) }
            OrbBadgeState.shared.setBadge("feed", feedUnread == 0 ? "" : String(feedUnread))
            if let item = value as? [String: Any] {
                FeedPopoverController.shared.prepend(item: item)
            }
        case "visibility-changed":
            orbView?.refetchConfig()
        case "lang-changed":
            OrbBadgeState.shared.lang = (value as? String) ?? "ko"
            orbView?.refreshLabels()
            // 열려 있는 팝오버는 닫았다 다시 열 때 새 언어로 그려진다
        default:
            break
        }
        orbView?.refreshBadges()
    }

    // ---------- 패널 열고 닫기 ----------
    func attach(id: Int64, kind: String, orb: OrbFanView?) {
        if let orb {
            orbPanel = id
            OrbBadgeState.shared.orb = orb
            orb.refreshBadges()
        } else {
            popPanel = id
            popWidget = kind
            feedUnread = 0
        }
    }

    func panelClosed(id: Int64) {
        if id == popPanel {
            popPanel = 0
            popWidget = ""
        }
        if id == orbPanel {
            orbPanel = 0
        }
    }
}

// 배지/언어 상태
final class OrbBadgeState {
    static let shared = OrbBadgeState()
    var lang = "ko"
    private var badges: [String: String] = [:]
    weak var orb: OrbFanView?

    func setBadge(_ kind: String, _ text: String) {
        badges[kind] = text
        orb?.applyBadges(badges)
    }
    func updatePomodoro(_ snap: [String: Any]) {
        let running = snap["running"] as? Bool ?? false
        let paused = snap["paused"] as? Bool ?? false
        if running && !paused {
            let secs = snap["remaining_secs"] as? Int ?? 0
            setBadge("pomodoro", String(format: "%02d:%02d", secs / 60, secs % 60))
        } else {
            setBadge("pomodoro", "")
        }
    }
}

// ---------- Swift↔Go 경계 (Go 프리앰블에 선언된 함수들 호출) ----------

// Go → Swift
@_cdecl("du_native_event")
public func du_native_event(_ name: UnsafePointer<CChar>!, _ json: UnsafePointer<CChar>!) {
    let n = name != nil ? String(cString: name) : ""
    let j = json != nil ? String(cString: json) : "null"
    NativeUI.shared.handleEvent(name: n, json: j)
}

@_cdecl("du_native_reply")
public func du_native_reply(_ seq: Int64, _ ok: Int32, _ json: UnsafePointer<CChar>!) {
    let j = json != nil ? String(cString: json) : "null"
    NativeUI.shared.handleReply(seq: seq, ok: ok != 0, json: j)
}

// ---------- 오브 팬 뷰 ----------

// JS orbLayout 포트: 안쪽→바깥 링 스네이크 배치
private func orbFanLayout(count: Int) -> [CGPoint] {
    let geo = CGPoint(x: 336, y: 264) // CSS 좌표(위=0)
    let rings: [(r: Double, from: Double, to: Double, cap: Int)] = [
        (115, 97, 172, 4), (185, 172, 97, 4), (240, 97, 172, 3),
    ]
    var out: [CGPoint] = []
    var idx = 0
    for ring in rings {
        if idx >= count { break }
        let n = min(ring.cap, count - idx)
        for i in 0..<n {
            let deg = ring.r == 240 ? 172 - Double(n - 1 - i) * 25 : (n == 1 ? (ring.from + ring.to) / 2 : ring.from + ((ring.to - ring.from) * Double(i)) / Double(n - 1))
            let rad = deg * .pi / 180
            let x = Double(geo.x) + ring.r * cos(rad)
            let y = Double(geo.y) - ring.r * sin(rad)
            out.append(CGPoint(x: x, y: y))
            idx += 1
        }
    }
    return out
}

private let orbWidgets = ["terminal", "ai_term", "timer", "shelf", "monitor", "toggles", "agents", "ai_apps", "format", "feed", "settings"]

final class OrbFanView: NSView {
    private let centerLayer = CALayer()
    private var itemLayers: [CALayer] = []
    private var iconLayers: [CALayer] = []
    private var labelLayers: [CATextLayer] = []
    private var badgeLayers: [CATextLayer] = []
    private var widgets: [String] = []
    private var positions: [CGPoint] = [] // CSS 좌표
    private var expanded = false
    private var hiddenWidgets: Set<String> = []
    private var config: [String: Any]?

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        layer?.masksToBounds = false
        registerForDraggedTypes([.fileURL]) // 파일 드롭 → 선반 추가

        let pulse = CALayer()
        pulse.name = "pulse"
        pulse.frame = CGRect(x: 336 - 28, y: frame.height - 264 - 28, width: 56, height: 56)
        pulse.cornerRadius = 28
        pulse.backgroundColor = NSColor.white.withAlphaComponent(0.22).cgColor
        pulse.borderColor = NSColor.white.withAlphaComponent(0.55).cgColor
        pulse.borderWidth = 1
        pulse.contentsScale = NSScreen.main?.backingScaleFactor ?? 2
        let anim = CABasicAnimation(keyPath: "transform.scale")
        anim.fromValue = 0.7
        anim.toValue = 1.25
        anim.duration = 2.6
        anim.repeatCount = .infinity
        anim.timingFunction = CAMediaTimingFunction(name: .easeOut)
        pulse.add(anim, forKey: "pulse")
        layer?.addSublayer(pulse)

        centerLayer.frame = CGRect(x: 336 - 28, y: frame.height - 264 - 28, width: 56, height: 56)
        centerLayer.cornerRadius = 28
        centerLayer.backgroundColor = NSColor(calibratedWhite: 0.38, alpha: 0.78).cgColor
        centerLayer.borderColor = NSColor.white.withAlphaComponent(0.55).cgColor
        centerLayer.borderWidth = 1
        if let img = NSImage(systemSymbolName: "circle.dotted", accessibilityDescription: nil) {
            let conf = NSImage.SymbolConfiguration(pointSize: 20, weight: .thin)
            centerLayer.contents = img.withSymbolConfiguration(conf) ?? img
            centerLayer.contentsGravity = .center
        }
        layer?.addSublayer(centerLayer)

        NativeUI.shared.invoke("get_config") { value, _ in
            DispatchQueue.main.async {
                self.config = value as? [String: Any]
                self.rebuild()
            }
        }
        rebuild()
    }

    required init?(coder: NSCoder) { fatalError("unsupported") }

    private func symbol(_ widget: String) -> String {
        switch widget {
        case "terminal": return "terminal"
        case "ai_term": return "sparkles"
        case "timer": return "timer"
        case "shelf": return "shippingbox"
        case "monitor": return "waveform.path.ecg"
        case "toggles": return "switch.2"
        case "agents": return "cpu"
        case "ai_apps": return "square.grid.2x2"
        case "format": return "curlybraces.square"
        case "feed": return "bell"
        case "settings": return "gearshape"
        default: return "circle"
        }
    }

    private func rebuild() {
        itemLayers.forEach { $0.removeFromSuperlayer() }
        iconLayers.forEach { $0.removeFromSuperlayer() }
        labelLayers.forEach { $0.removeFromSuperlayer() }
        badgeLayers.forEach { $0.removeFromSuperlayer() }
        itemLayers = []; iconLayers = []; labelLayers = []; badgeLayers = []

        let hidden = Set(((config?["hidden_widgets"] as? [String]) ?? []))
        widgets = orbWidgets.filter { !hidden.contains($0) }
        if !widgets.contains("settings") { widgets.append("settings") }
        positions = orbFanLayout(count: widgets.count)
        expanded = false

        for (i, w) in widgets.enumerated() {
            let pos = positions[i]
            // CSS(위=0) → 뷰 좌표(아래=0)
            let cx = pos.x, cy = frame.height - pos.y
            let item = CALayer()
            item.frame = CGRect(x: cx - 23, y: cy - 23, width: 46, height: 46)
            item.cornerRadius = 23
            item.borderColor = NSColor.white.withAlphaComponent(0.18).cgColor
            item.borderWidth = 0.5
            item.backgroundColor = NSColor(calibratedRed: 0.34, green: 0.35, blue: 0.38, alpha: 0.78).cgColor
            item.shadowColor = NSColor.black.cgColor
            item.shadowOpacity = 0.18
            item.shadowRadius = 8
            item.shadowOffset = CGSize(width: 0, height: -3)
            item.shadowPath = CGPath(ellipseIn: item.bounds, transform: nil)
            item.opacity = 0
            item.isHidden = true
            item.zPosition = 5
            layer?.addSublayer(item)
            itemLayers.append(item)

            let icon = CALayer()
            icon.frame = CGRect(x: cx - 11, y: cy - 11, width: 22, height: 22)
            icon.contentsScale = NSScreen.main?.backingScaleFactor ?? 2
            icon.contentsGravity = .center
            if let sym = NSImage(systemSymbolName: symbol(w), accessibilityDescription: nil) {
                let conf = NSImage.SymbolConfiguration(pointSize: 13, weight: .light)
                icon.contents = sym.withSymbolConfiguration(conf) ?? sym
            }
            icon.opacity = 0
            icon.isHidden = true
            icon.zPosition = 6
            layer?.addSublayer(icon)
            iconLayers.append(icon)

            let label = CATextLayer()
            label.string = labelFor(w)
            label.fontSize = 10
            label.alignmentMode = .center
            label.foregroundColor = NSColor.white.withAlphaComponent(0.75).cgColor
            label.contentsScale = NSScreen.main?.backingScaleFactor ?? 2
            label.frame = CGRect(x: cx - 45, y: cy - 41, width: 90, height: 13)
            label.opacity = 0
            label.isHidden = true
            label.zPosition = 6
            layer?.addSublayer(label)
            labelLayers.append(label)

            let badge = CATextLayer()
            badge.fontSize = 9
            badge.alignmentMode = .center
            badge.foregroundColor = NSColor.black.cgColor
            badge.backgroundColor = NSColor.white.withAlphaComponent(0.9).cgColor
            badge.cornerRadius = 6
            badge.masksToBounds = true
            badge.contentsScale = NSScreen.main?.backingScaleFactor ?? 2
            badge.frame = CGRect(x: 33, y: 33, width: 22, height: 12)
            badge.string = ""
            badge.isHidden = true
            badge.zPosition = 7
            item.addSublayer(badge)
            badgeLayers.append(badge)
        }
        refreshBadges()
    }

    private func labelFor(_ widget: String) -> String {
        let lang = OrbBadgeState.shared.lang
        let ko = ["terminal": "터미널", "ai_term": "AI 터미널", "timer": "타이머", "shelf": "선반",
                  "monitor": "시스템", "toggles": "토글", "agents": "에이전트", "ai_apps": "AI 앱",
                  "format": "포매터", "feed": "알림", "settings": "설정"]
        let en = ["terminal": "Terminal", "ai_term": "AI Term", "timer": "Timer", "shelf": "Shelf",
                  "monitor": "System", "toggles": "Toggles", "agents": "Agents", "ai_apps": "AI Apps",
                  "format": "Format", "feed": "Alerts", "settings": "Settings"]
        return lang == "en" ? (en[widget] ?? widget) : (ko[widget] ?? widget)
    }

    // 설정에서 위젯 표시가 바뀌면 팬을 재구성한다
    func refetchConfig() {
        NativeUI.shared.invoke("get_config") { value, _ in
            DispatchQueue.main.async {
                if let cfg = value as? [String: Any] {
                    self.config = cfg
                    self.rebuild()
                }
            }
        }
    }

    func refreshLabels() {
        for (i, w) in widgets.enumerated() where i < labelLayers.count {
            labelLayers[i].string = labelFor(w)
        }
    }

    func setExpanded(_ v: Bool) {
        if duDebug { NSLog("[orb] setExpanded %d (was %d)", v ? 1 : 0, expanded ? 1 : 0) }
        guard expanded != v else { return }
        expanded = v
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        let now = CACurrentMediaTime()
        var start = 0
        for capacity in [4, 4, 3] {
            let end = min(start + capacity, positions.count)
            for i in start..<end {
                let rank = start + end - 1 - i
                for (layer, opacity) in [(itemLayers[i], Float(1)), (iconLayers[i], Float(1)), (labelLayers[i], Float(0.9))] {
                    let currentOpacity = layer.presentation()?.opacity ?? layer.opacity
                    let currentScale = layer.presentation()?.value(forKeyPath: "transform.scale") as? CGFloat ?? (layer.isHidden ? 0.8 : 1)
                    layer.removeAnimation(forKey: "orb-reveal")
                    layer.isHidden = false
                    layer.opacity = v ? opacity : 0
                    let fade = CABasicAnimation(keyPath: "opacity")
                    fade.fromValue = currentOpacity
                    fade.toValue = v ? opacity : 0
                    let scale = CABasicAnimation(keyPath: "transform.scale")
                    scale.fromValue = currentScale
                    scale.toValue = v ? 1 : 0.8
                    let reveal = CAAnimationGroup()
                    reveal.animations = [fade, scale]
                    reveal.duration = v ? 0.28 : 0.2
                    let sequence = v ? rank : positions.count - 1 - rank
                    reveal.beginTime = layer.convertTime(now, from: nil) + Double(sequence) * 0.045
                    reveal.fillMode = .backwards
                    reveal.timingFunction = CAMediaTimingFunction(name: v ? .easeOut : .easeIn)
                    layer.add(reveal, forKey: "orb-reveal")
                }
            }
            start = end
        }
        CATransaction.commit()
    }

    func setIdle(_ v: Bool) {
        if let pulse = layer?.sublayers?.first(where: { $0.name == "pulse" }) {
            pulse.speed = v ? 0 : 1
            pulse.opacity = v ? 0.35 : 1
        }
    }

    func applyBadges(_ badges: [String: String]) {
        for (i, w) in widgets.enumerated() where i < badgeLayers.count {
            let text = badges[w] ?? ""
            badgeLayers[i].isHidden = text.isEmpty
            badgeLayers[i].string = text
        }
    }

    func refreshBadges() {
        applyBadges([
            "agents": NativeUI.shared.agentsList.isEmpty ? "" : String(NativeUI.shared.agentsList.count),
            "shelf": NativeUI.shared.shelfList.isEmpty ? "" : String(NativeUI.shared.shelfList.count),
            "feed": NativeUI.shared.feedUnread == 0 ? "" : String(NativeUI.shared.feedUnread),
        ])
    }

    // 파일 드롭 → 선반 추가
    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation { .copy }
    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        let urls = sender.draggingPasteboard.readObjects(forClasses: [NSURL.self],
                                                        options: [.urlReadingFileURLsOnly: true]) as? [URL] ?? []
        let paths = urls.compactMap { $0.path }
        guard !paths.isEmpty else { return false }
        if let data = try? JSONSerialization.data(withJSONObject: paths),
           let json = String(data: data, encoding: .utf8) {
            dockutil_on_drop(-1, (json as NSString).utf8String!)
        }
        return true
    }

    // 클릭 → 위젯 팝오버
    override func mouseDown(with event: NSEvent) {
        guard expanded else { return }
        let p = convert(event.locationInWindow, from: nil)
        if duDebug { NSLog("[orb] mouseDown at %@ items=%d", NSStringFromPoint(p), widgets.count) }
        let cx = p.x, cy = frame.height - p.y // CSS 좌표
        for (i, pos) in positions.enumerated() {
            let dx = cx - pos.x, dy = cy - pos.y
            if dx * dx + dy * dy <= 23 * 23 {
                let w = widgets[i]
                NativeUI.shared.invoke("orb_collapse")
                NativeUI.shared.invoke("open_popover", ["widget": w, "tileX": pos.x])
                return
            }
        }
    }
}
