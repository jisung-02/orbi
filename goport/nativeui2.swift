// nativeui2.swift — 네이티브 팝오버 위젯 컨트롤러들
// 각 위젯은 싱글턴 뷰를 만들고, 패널이 열릴 때 재사용된다.

import Cocoa

// ---------- 공용 유틸 ----------

func duLabel(_ text: String, _ size: CGFloat = 12, _ alpha: CGFloat = 0.9, bold: Bool = false) -> NSTextField {
    let l = NSTextField(labelWithString: text)
    l.font = bold ? NSFont.boldSystemFont(ofSize: size) : NSFont.systemFont(ofSize: size)
    l.textColor = NSColor.white.withAlphaComponent(alpha)
    l.isBezeled = false
    l.isEditable = false
    l.drawsBackground = false
    return l
}

final class DUActionBox: NSObject {
    let handler: () -> Void
    init(_ handler: @escaping () -> Void) { self.handler = handler }
    @objc func fire() { handler() }
    @objc func fireSeg() { handler() }
}

private var duActionKey: UInt8 = 0 // 컨트롤이 해제되면 액션도 해제

func duButton(_ title: String, action: (() -> Void)? = nil, small: Bool = true, danger: Bool = false) -> NSButton {
    let b = NSButton(title: title, target: nil, action: nil)
    b.bezelStyle = .rounded
    b.isBordered = true
    b.controlSize = small ? .small : .regular
    b.font = NSFont.systemFont(ofSize: small ? 11 : 13)
    if danger {
        b.contentTintColor = NSColor.systemRed
    }
    if let action {
        let box = DUActionBox(action)
        objc_setAssociatedObject(b, &duActionKey, box, .OBJC_ASSOCIATION_RETAIN_NONATOMIC)
        b.target = box
        b.action = #selector(DUActionBox.fire)
    }
    return b
}

func duSegment(_ labels: [String], selected: Int = 0, action: ((Int) -> Void)? = nil) -> NSSegmentedControl {
    let seg = NSSegmentedControl(labels: labels, trackingMode: .selectOne, target: nil, action: nil)
    seg.selectedSegment = selected
    seg.controlSize = .small
    seg.font = NSFont.systemFont(ofSize: 11)
    if let action {
        let box = DUActionBox { [weak seg] in
            guard let seg else { return }
            action(seg.selectedSegment)
        }
        objc_setAssociatedObject(seg, &duActionKey, box, .OBJC_ASSOCIATION_RETAIN_NONATOMIC)
        seg.target = box
        seg.action = #selector(DUActionBox.fireSeg)
    }
    return seg
}

// ---------- 타이머 (포모/타이머/스톱워치) ----------

final class TimerPopoverController: NSObject, NativePopover {
    static let shared = TimerPopoverController()
    private(set) var view: NSView = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 330))

    private var mode = 0 // 0=포모 1=타이머 2=스톱워치
    private var snap: [String: Any] = [:]
    // 로컬 타이머/스톱워치 (JS localTimer 대체)
    private var running = false
    private var paused = false
    private var startAt: Date?
    private var pausedElapsed: TimeInterval = 0
    private var durationSecs: Double = 300
    private var laps: [TimeInterval] = []
    private var tick: Timer?

    private var displayLabel: NSTextField!
    private var statusLabel: NSTextField!
    private var actionRow: NSView!
    private var actionButtons: [NSButton] = []
    private var lapsLabel: NSTextField!

    private override init() {
        super.init()
        build()
    }

    private func build() {
        view.wantsLayer = true
        let h: CGFloat = view.frame.height

        let tabs = NSSegmentedControl(labels: ["포모", "타이머", "스톱워치"], trackingMode: .selectOne, target: self, action: #selector(modeChanged(_:)))
        tabs.selectedSegment = 0
        tabs.frame = NSRect(x: 16, y: h - 66, width: 268, height: 24)
        tabs.font = NSFont.systemFont(ofSize: 11)
        view.addSubview(tabs)

        displayLabel = duLabel("25:00", 40, 0.95, bold: true)
        displayLabel.frame = NSRect(x: 16, y: h - 130, width: 268, height: 48)
        displayLabel.alignment = .center
        view.addSubview(displayLabel)

        statusLabel = duLabel("", 11, 0.6)
        statusLabel.frame = NSRect(x: 16, y: h - 152, width: 268, height: 16)
        statusLabel.alignment = .center
        view.addSubview(statusLabel)

        let presetLbl = duLabel("프리셋", 10, 0.55)
        presetLbl.frame = NSRect(x: 16, y: h - 178, width: 120, height: 14)
        view.addSubview(presetLbl)
        let presets = duSegment(["25/5", "50/10", "15/5"], action: { [weak self] i in
            let v: [(Double, Double)] = [(25, 5), (50, 10), (15, 5)]
            guard let self else { return }
            self.snap["focus_min"] = Int(v[i].0)
            self.snap["break_min"] = Int(v[i].1)
            self.snap["phase"] = "idle"
            self.snap["running"] = false
            self.invokePom("pom_start")
        })
        presets.frame = NSRect(x: 140, y: h - 180, width: 144, height: 22)
        view.addSubview(presets)

        actionButtons = []
        actionRow = NSView(frame: NSRect(x: 16, y: h - 226, width: 268, height: 34))
        view.addSubview(actionRow)

        let timerPresets = duSegment(["1분", "5분", "10분", "30분"], action: { [weak self] i in
            guard let self else { return }
            self.durationSecs = [60, 300, 600, 1800][i]
            self.resetLocal()
            self.rebuildActions()
        })
        timerPresets.frame = NSRect(x: 16, y: h - 180, width: 268, height: 22)
        timerPresets.isHidden = true
        timerPresets.identifier = NSUserInterfaceItemIdentifier("timerPresets")
        view.addSubview(timerPresets)

        lapsLabel = duLabel("", 11, 0.7)
        lapsLabel.frame = NSRect(x: 16, y: h - 318, width: 268, height: 88)
        view.addSubview(lapsLabel)
    }

    @objc private func modeChanged(_ seg: NSSegmentedControl) {
        mode = seg.selectedSegment
        view.subviews.first {  $0.identifier?.rawValue == "timerPresets" }?.isHidden = mode != 1
        rebuildActions()
        refreshDisplay()
    }

    private func elapsed() -> TimeInterval {
        if let s = startAt { return Date().timeIntervalSince(s) + pausedElapsed }
        return pausedElapsed
    }

    private func resetLocal() {
        running = false; paused = false; startAt = nil; pausedElapsed = 0
        tick?.invalidate(); tick = nil
    }

    private func onTick() {
        guard running && !paused else { return }
        let el = elapsed()
        if mode == 2 {
            displayLabel.stringValue = swFormat(el)
        } else if mode == 1 {
            let remain = max(0, durationSecs - el)
            displayLabel.stringValue = fmtClock(ceil(remain))
            if remain <= 0 {
                resetLocal()
                pausedElapsed = durationSecs
                displayLabel.stringValue = "00:00"
                NativeUI.shared.invoke("timer_done", ["secs": durationSecs])
                rebuildActions()
            }
        }
    }

    private func fmtClock(_ s: TimeInterval) -> String {
        let v = Int(s)
        return String(format: "%02d:%02d", v / 60, v % 60)
    }

    private func swFormat(_ ms: TimeInterval) -> String {
        let total = Int((ms * 100).rounded())
        let cs = total % 100, s = (total / 100) % 60, m = (total / 6000) % 60, hh = total / 360000
        return hh > 0 ? String(format: "%d:%02d:%02d.%02d", hh, m, s, cs) : String(format: "%02d:%02d.%02d", m, s, cs)
    }

    private func invokePom(_ cmd: String) {
        var args: [String: Any] = [:]
        if let f = snap["focus_min"], cmd == "pom_start" { args["focus_min"] = f }
        if let b = snap["break_min"], cmd == "pom_start" { args["break_min"] = b }
        NativeUI.shared.invoke(cmd, args) { [weak self] _, _ in
            DispatchQueue.main.async { self?.refreshDisplay() }
        }
    }

    private func clearActions() {
        actionButtons.forEach { $0.removeFromSuperview() }
        actionButtons = []
    }

    private func addButton(_ title: String, x: CGFloat, w: CGFloat, _ h: (() -> Void)?) {
        let b = duButton(title, action: h, small: false)
        b.frame = NSRect(x: x, y: 2, width: w, height: 30)
        actionRow.addSubview(b)
        actionButtons.append(b)
    }

    private func rebuildActions() {
        if running && !paused {
            let interval = mode == 2 ? 0.1 : 1.0
            if tick == nil || tick?.timeInterval != interval {
                tick?.invalidate()
                tick = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in self?.onTick() }
            }
        } else {
            tick?.invalidate(); tick = nil
        }
        clearActions()
        guard mode == 0 else {
            // 타이머/스톱워치 버튼
            let runningNow = running && !paused
            if mode == 2 && runningNow {
                addButton("Lap", x: 16, w: 80) { [weak self] in
                    guard let self else { return }
                    self.laps.insert(self.elapsed(), at: 0)
                    self.lapsLabel.stringValue = self.laps.prefix(6).enumerated()
                        .map { "#\($0.offset + 1)  " + self.swFormat($0.element) }.joined(separator: "\n")
                }
            }
            let x: CGFloat = 100
            if running && !paused {
                addButton("일시정지", x: x, w: 84) { [weak self] in
                    guard let self else { return }
                    self.pausedElapsed = self.elapsed(); self.paused = true; self.startAt = nil
                    self.rebuildActions()
                }
            } else if paused {
                addButton("재개", x: x, w: 84) { [weak self] in
                    guard let self else { return }
                    self.startAt = Date(); self.paused = false
                    self.rebuildActions()
                }
            } else {
                addButton("시작", x: x, w: 84) { [weak self] in
                    guard let self else { return }
                    self.running = true; self.paused = false; self.startAt = Date(); self.pausedElapsed = 0
                    self.rebuildActions()
                }
            }
            addButton("리셋", x: 192, w: 84) { [weak self] in
                guard let self else { return }
                self.resetLocal(); self.laps = []
                self.lapsLabel.stringValue = ""
                self.rebuildActions(); self.refreshDisplay()
            }
            refreshDisplay()
            return
        }
        let p = snap
        let runningNow = p["running"] as? Bool ?? false
        let pausedNow = p["paused"] as? Bool ?? false
        if runningNow && pausedNow {
            addButton("재개", x: 16, w: 128) { [weak self] in self?.invokePom("pom_resume") }
        } else if runningNow {
            addButton("일시정지", x: 16, w: 128) { [weak self] in self?.invokePom("pom_pause") }
        } else {
            addButton("시작", x: 16, w: 128) { [weak self] in self?.invokePom("pom_start") }
        }
        addButton("리셋", x: 152, w: 128) { [weak self] in self?.invokePom("pom_reset") }
    }

    private func refreshDisplay() {
        let p = snap
        let runningNow = p["running"] as? Bool ?? false
        let pausedNow = p["paused"] as? Bool ?? false
        let phase = p["phase"] as? String ?? "idle"
        let focusMin = p["focus_min"] as? Int ?? 25
        let rounds = p["rounds_done"] as? Int ?? 0
        let remain = p["remaining_secs"] as? Int ?? focusMin * 60
        let lang = OrbBadgeState.shared.lang

        if mode == 0 {
            displayLabel.stringValue = runningNow ? fmtClock(Double(remain)) : fmtClock(Double(focusMin * 60))
            let phaseTxt = (phase == "break" ? (lang == "en" ? "Break" : "휴식") : (lang == "en" ? "Focus" : "집중"))
            statusLabel.stringValue = runningNow
                ? (pausedNow ? (lang == "en" ? "Paused · " : "일시정지 · ") + phaseTxt + " · " : phaseTxt + " · ") + (lang == "en" ? "rounds \(rounds)" : "라운드 \(rounds)")
                : (lang == "en" ? "Idle · rounds \(rounds)" : "대기 중 · 라운드 \(rounds)")
        } else if mode == 1 {
            let remainS = max(0, durationSecs - elapsed())
            displayLabel.stringValue = fmtClock(ceil(remainS))
            statusLabel.stringValue = running ? (paused ? (lang == "en" ? "Paused" : "일시정지") : "") : (lang == "en" ? "Idle" : "대기 중")
        } else {
            displayLabel.stringValue = swFormat(elapsed())
            statusLabel.stringValue = running ? (paused ? (lang == "en" ? "Paused" : "") : "") : (lang == "en" ? "Idle" : "대기 중")
        }
    }

    func update(snapshot: [String: Any]) {
        let controlsChanged = (snap["running"] as? Bool) != (snapshot["running"] as? Bool)
            || (snap["paused"] as? Bool) != (snapshot["paused"] as? Bool)
        snap = snapshot
        guard mode == 0 else { return }
        refreshDisplay()
        if controlsChanged { rebuildActions() }
    }

    func onOpen() {
        NativeUI.shared.invoke("pom_snapshot") { [weak self] value, _ in
            DispatchQueue.main.async {
                guard let self, let snap = value as? [String: Any] else { return }
                self.snap = snap
                self.refreshDisplay()
                self.rebuildActions()
            }
        }
        refreshDisplay()
        rebuildActions()
    }
}

// ---------- 토글 ----------

final class TogglesPopoverController: NSObject, NativePopover {
    static let shared: TogglesPopoverController = {
        let c = TogglesPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 270, height: 360))
    private var switches: [String: NSSwitch] = [:]
    private let keys = ["dark", "wifi", "bt", "mute", "caffeine"]
    private let titles = ["다크모드", "Wi-Fi", "Bluetooth", "음소거", "잠금 방지"]

    private func build() {
        for (i, key) in keys.enumerated() {
            let y = view.frame.height - 70 - CGFloat(i) * 48
            let l = duLabel(titles[i], 12.5)
            l.frame = NSRect(x: 20, y: y + 4, width: 150, height: 18)
            view.addSubview(l)
            let sw = NSSwitch(frame: NSRect(x: 205, y: y, width: 44, height: 26))
            sw.controlSize = .small
            sw.target = self
            sw.action = #selector(toggled(_:))
            sw.identifier = NSUserInterfaceItemIdentifier(key)
            view.addSubview(sw)
            switches[key] = sw
        }
        let note = duLabel("일부 토글은 첫 사용 시 자동화 권한을 요청합니다.", 10, 0.5)
        note.frame = NSRect(x: 20, y: 24, width: 230, height: 26)
        view.addSubview(note)
        refresh()
    }

    @objc private func toggled(_ sw: NSSwitch) {
        let key = sw.identifier?.rawValue ?? ""
        let on = sw.state == .on
        NativeUI.shared.invoke("toggle_" + key, [:]) { [weak self] value, _ in
            DispatchQueue.main.async {
                let nowOn = (value as? Bool) ?? on
                self?.switches[key]?.state = nowOn ? .on : .off
            }
        }
        _ = on
    }

    private func refresh() {
        NativeUI.shared.invoke("get_toggles") { [weak self] value, _ in
            DispatchQueue.main.async {
                guard let self, let d = value as? [String: Any] else { return }
                for key in self.keys {
                    self.switches[key]?.state = (d[key] as? Bool ?? false) ? .on : .off
                    if key == "wifi" { self.switches[key]?.isEnabled = d["wifi_available"] as? Bool ?? true }
                    if key == "bt" { self.switches[key]?.isEnabled = d["bt_available"] as? Bool ?? false }
                }
            }
        }
    }

    func onOpen() { refresh() }
}

// ---------- 에이전트 ----------

final class AgentsPopoverController: NSObject, NativePopover {
    static let shared: AgentsPopoverController = {
        let c = AgentsPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 350, height: 400))
    private var listLabel: NSTextField!

    private func build() {
        let title = duLabel("실행중 에이전트", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 42, width: 300, height: 20)
        view.addSubview(title)
        let hint = duLabel("10초마다 자동 갱신 · claude/codex/gemini/copilot 등 감지", 10, 0.5)
        hint.frame = NSRect(x: 20, y: 20, width: 310, height: 16)
        view.addSubview(hint)
        listLabel = duLabel("동작중인 에이전트가 없습니다", 11.5, 0.75)
        listLabel.frame = NSRect(x: 20, y: 50, width: 310, height: view.frame.height - 110)
        listLabel.isSelectable = true
        view.addSubview(listLabel)
    }

    func update(list: [[String: Any]]) {
        guard !list.isEmpty else {
            listLabel.stringValue = "동작중인 에이전트가 없습니다"
            return
        }
        listLabel.stringValue = list.prefix(10).map { a in
            let name = a["name"] as? String ?? "?"
            let pid = a["pid"] as? Int ?? 0
            let cpu = a["cpu"] as? Double ?? 0
            let mem = a["mem_mb"] as? Int ?? 0
            let el = a["elapsed"] as? String ?? ""
            return "\(name)  pid \(pid) · \(el) · \(mem)MB · \(Int(cpu))%"
        }.joined(separator: "\n\n")
    }

    func onOpen() {
        if !NativeUI.shared.lastAgents.isEmpty {
            update(list: NativeUI.shared.lastAgents)
        }
    }
}

// ---------- 선반 ----------

final class ShelfPopoverController: NSObject, NativePopover {
    static let shared: ShelfPopoverController = {
        let c = ShelfPopoverController()
        c.build()
        return c
    }()
    private(set) var view: NSView = ShelfDropView(frame: NSRect(x: 0, y: 0, width: 400, height: 440))
    private var rowsStack: NSView!
    private var emptyLabel: NSTextField!
    private var totalLabel: NSTextField!
    private var list: [[String: Any]] = []

    private func build() {
        NSLog("[shelf] build 1")
        let title = duLabel("파일 선반", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 40, width: 160, height: 20)
        view.addSubview(title)
        NSLog("[shelf] build 2")
        totalLabel = duLabel("", 11, 0.6)
        totalLabel.frame = NSRect(x: 180, y: view.frame.height - 38, width: 200, height: 16)
        totalLabel.alignment = .right
        view.addSubview(totalLabel)
        NSLog("[shelf] build 3")

        rowsStack = NSView(frame: NSRect(x: 12, y: 64, width: view.frame.width - 24, height: view.frame.height - 104))
        view.addSubview(rowsStack)
        NSLog("[shelf] build 4")

        emptyLabel = duLabel("파일을 끌어다 놓으세요\n(임시 보관 후 한 번에 이동)", 12, 0.6)
        emptyLabel.frame = NSRect(x: 20, y: view.frame.height / 2 - 20, width: 360, height: 44)
        emptyLabel.alignment = .center
        view.addSubview(emptyLabel)

        let b1 = duButton("모두 이동") { NativeUI.shared.invoke("shelf_move_to") }
        b1.frame = NSRect(x: 14, y: 26, width: 100, height: 28)
        view.addSubview(b1)
        let b2 = duButton("모두 복사") { NativeUI.shared.invoke("shelf_copy") }
        b2.frame = NSRect(x: 120, y: 26, width: 100, height: 28)
        view.addSubview(b2)
        let b3 = duButton("비우기") { NativeUI.shared.invoke("shelf_clear") }
        b3.frame = NSRect(x: 226, y: 26, width: 90, height: 28)
        view.addSubview(b3)
        NSLog("[shelf] build 5 done")
    }

    private func clearRows() {
        rowsStack.subviews.forEach { $0.removeFromSuperview() }
    }

    private func makeRow(_ it: [String: Any], idx: Int) -> NSView {
        let row = NSView(frame: NSRect(x: 0, y: 0, width: view.frame.width - 40, height: 40))
        row.wantsLayer = true
        row.layer?.backgroundColor = NSColor.white.withAlphaComponent(0.06).cgColor
        row.layer?.cornerRadius = 8

        let isDir = it["is_dir"] as? Bool ?? false
        let name = it["name"] as? String ?? "?"
        let mb = it["size_mb"] as? Double ?? 0
        let icon = duLabel(isDir ? "📁" : "📄", 14)
        icon.frame = NSRect(x: 8, y: 10, width: 20, height: 20)
        row.addSubview(icon)
        let nameL = duLabel(name + (isDir ? "" : "  \(mb)MB"), 11.5, 0.9)
        nameL.frame = NSRect(x: 32, y: 12, width: 118, height: 16)
        nameL.cell?.truncatesLastVisibleLine = true
        nameL.cell?.wraps = false
        row.addSubview(nameL)

        var x = row.frame.width - 8
        let defs: [(String, String)] = [("✕", "shelf_remove"), ("🔍", "shelf_reveal"), ("이동", "shelf_move_to"), ("복사", "shelf_copy"), ("열기", "shelf_open")]
        for (title, cmd) in defs.reversed() {
            let w: CGFloat = title.count > 1 ? 40 : 26
            x -= w
            let b = duButton(title, action: {
                var args: [String: Any] = [:]
                if cmd != "shelf_clear" { args["idx"] = idx }
                NativeUI.shared.invoke(cmd, args)
            }, small: true)
            b.frame = NSRect(x: x, y: 7, width: w, height: 24)
            row.addSubview(b)
            x -= 2
        }
        return row
    }

    func update(list: [[String: Any]]) {
        NSLog("[shelf] update n=%d", list.count)
        self.list = list
        NSLog("[shelf] update clearRows")
        clearRows()
        NSLog("[shelf] update empty=%d", emptyLabel.isHidden ? 0 : 1)
        emptyLabel.isHidden = !list.isEmpty
        let total = list.reduce(0.0) { $0 + ($1["size_mb"] as? Double ?? 0) }
        totalLabel.stringValue = list.isEmpty ? "" : "\(list.count)개 · \(String(format: "%.1f", total))MB"
        NSLog("[shelf] update total ok")
        for (i, it) in list.prefix(8).enumerated() {
            NSLog("[shelf] update row %d", i)
            let row = makeRow(it, idx: i)
            row.frame.origin = NSPoint(x: 0, y: rowsStack.frame.height - 44 - CGFloat(i) * 44)
            rowsStack.addSubview(row)
        }
        NSLog("[shelf] update done")
    }

    func onOpen() {
        NativeUI.shared.invoke("shelf_list") { [weak self] value, _ in
            DispatchQueue.main.async {
                if let list = value as? [[String: Any]] { self?.update(list: list) }
            }
        }
    }
}

// 드래그&드롭 가능한 선반 뷰
final class ShelfDropView: NSView {
    override init(frame: NSRect) {
        super.init(frame: frame)
        registerForDraggedTypes([.fileURL])
    }
    required init?(coder: NSCoder) { fatalError("unsupported") }

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
}

// ---------- 피드 ----------

final class FeedPopoverController: NSObject, NativePopover {
    static let shared: FeedPopoverController = {
        let c = FeedPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 360, height: 440))
    private var listLabel: NSTextField!

    private func build() {
        let title = duLabel("알림", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 42, width: 200, height: 20)
        view.addSubview(title)
        let clear = duButton("전체 지우기") { NativeUI.shared.invoke("clear_feed") }
        clear.frame = NSRect(x: view.frame.width - 110, y: view.frame.height - 46, width: 90, height: 26)
        view.addSubview(clear)
        listLabel = duLabel("알림이 없습니다", 11.5, 0.75)
        listLabel.frame = NSRect(x: 20, y: 30, width: 320, height: view.frame.height - 90)
        listLabel.isSelectable = true
        view.addSubview(listLabel)
    }

    func update(list: [[String: Any]]) {
        guard !list.isEmpty else {
            listLabel.stringValue = "알림이 없습니다"
            return
        }
        listLabel.stringValue = list.prefix(20).map { f in
            let title = f["title"] as? String ?? ""
            let body = f["body"] as? String ?? ""
            let ts = f["ts"] as? Double ?? 0
            let time = Date(timeIntervalSince1970: ts / 1000)
            let fmt = DateFormatter()
            fmt.dateFormat = "HH:mm"
            return "[\(fmt.string(from: time))] \(title) — \(body)"
        }.joined(separator: "\n\n")
    }

    func prepend(item: [String: Any]) {
        let title = item["title"] as? String ?? ""
        let body = item["body"] as? String ?? ""
        listLabel.stringValue = "[방금] \(title) — \(body)\n\n" + listLabel.stringValue
    }

    func onOpen() {
        NativeUI.shared.invoke("get_feed") { [weak self] value, _ in
            DispatchQueue.main.async {
                if let list = value as? [[String: Any]] { self?.update(list: list) }
            }
        }
    }
}

// ---------- 모니터 ----------

final class MonitorPopoverController: NSObject, NativePopover {
    static let shared: MonitorPopoverController = {
        let c = MonitorPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 320, height: 360))
    private var bodyLabel: NSTextField!
    private var history: [Double] = []
    private var spark: CAShapeLayer!

    private func build() {
        let title = duLabel("시스템", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 42, width: 200, height: 20)
        view.addSubview(title)

        let sparkView = NSView(frame: NSRect(x: 20, y: view.frame.height - 110, width: 280, height: 56))
        sparkView.wantsLayer = true
        sparkView.layer?.backgroundColor = NSColor.white.withAlphaComponent(0.06).cgColor
        sparkView.layer?.cornerRadius = 6
        view.addSubview(sparkView)
        spark = CAShapeLayer()
        spark.strokeColor = NSColor.white.withAlphaComponent(0.85).cgColor
        spark.fillColor = NSColor.clear.cgColor
        spark.lineWidth = 1.6
        sparkView.layer?.addSublayer(spark)

        bodyLabel = duLabel("수집 중…", 12, 0.85)
        bodyLabel.frame = NSRect(x: 20, y: 24, width: 280, height: view.frame.height - 130)
        bodyLabel.isSelectable = true
        view.addSubview(bodyLabel)
    }

    func update(stats: [String: Any]) {
        history.append(stats["cpu"] as? Double ?? 0)
        if history.count > 48 { history.removeFirst(history.count - 48) }
        // 스파크라인
        if history.count >= 2 {
            let path = CGMutablePath()
            let w = 280.0, h = 56.0
            for (i, v) in history.enumerated() {
                let x = Double(i) * w / Double(max(history.count - 1, 1))
                let y = h - 4 - (min(v, 100) / 100) * (h - 8)
                if i == 0 { path.move(to: CGPoint(x: x, y: y)) } else { path.addLine(to: CGPoint(x: x, y: y)) }
            }
            spark.path = path
        }
        let cpu = stats["cpu"] as? Double ?? 0
        let ramPct = stats["ram_pct"] as? Double ?? 0
        let used = stats["ram_used_gb"] as? Double ?? 0
        let total = stats["ram_total_gb"] as? Double ?? 0
        let batt = stats["battery_pct"] as? Int ?? -1
        let charging = stats["battery_charging"] as? Bool ?? false
        let rx = stats["net_rx_kbs"] as? Double ?? 0
        let tx = stats["net_tx_kbs"] as? Double ?? 0
        let up = stats["uptime"] as? String ?? "-"
        let disk = stats["disk_free_gb"] as? Double ?? 0
        let battTxt = batt >= 0 ? "\(batt)%\(charging ? " ⚡" : "")" : "-"
        bodyLabel.stringValue = """
        CPU  \(String(format: "%.0f", cpu))%
        RAM  \(String(format: "%.0f", ramPct))%  (\(used)G / \(total)G)
        네트워크  ↓\(rx) ↑\(tx) KB/s
        배터리  \(battTxt)
        가동시간  \(up)  ·  디스크 여유 \(disk)G
        """
    }

    func onOpen() {
        if let s = NativeUI.shared.lastStats { update(stats: s) }
    }
}

// ---------- 포매터 ----------

final class FormatPopoverController: NSObject, NativePopover {
    static let shared: FormatPopoverController = {
        let c = FormatPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 440, height: 480))
    private var inFormat = "auto"
    private var inputView: NSScrollView!
    private var outputView: NSScrollView!
    private var inputText: NSTextView!
    private var outputText: NSTextView!

    private func build() {
        let title = duLabel("포매터", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 38, width: 200, height: 20)
        view.addSubview(title)

        let inSeg = duSegment(["자동", "JSON", "YAML", "TOML"], action: { [weak self] i in
            self?.inFormat = ["auto", "json", "yaml", "toml"][i]
        })
        inSeg.frame = NSRect(x: 90, y: view.frame.height - 42, width: 220, height: 22)
        view.addSubview(inSeg)

        inputView = NSScrollView(frame: NSRect(x: 16, y: view.frame.height - 208, width: 408, height: 150))
        inputText = NSTextView(frame: inputView.bounds)
        inputText.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        inputText.isRichText = false
        inputText.autoresizingMask = [.width]
        inputView.documentView = inputText
        inputView.hasVerticalScroller = true
        inputView.borderType = .bezelBorder
        view.addSubview(inputView)

        let row = NSStackView()
        row.orientation = .horizontal
        row.spacing = 6
        row.frame = NSRect(x: 16, y: view.frame.height - 238, width: 408, height: 26)
        row.addArrangedSubview(duButton("정리") { [weak self] in self?.run(pretty: true, convert: nil) })
        row.addArrangedSubview(duButton("압축") { [weak self] in self?.run(pretty: false, convert: nil) })
        row.addArrangedSubview(duButton("붙여넣기") { [weak self] in self?.paste() })
        row.addArrangedSubview(duButton("복사") { [weak self] in self?.copyOut() })
        row.addArrangedSubview(duButton("→YAML") { [weak self] in self?.run(pretty: true, convert: "yaml") })
        row.addArrangedSubview(duButton("→TOML") { [weak self] in self?.run(pretty: true, convert: "toml") })
        view.addSubview(row)

        outputView = NSScrollView(frame: NSRect(x: 16, y: 20, width: 408, height: view.frame.height - 272))
        outputText = NSTextView(frame: outputView.bounds)
        outputText.font = NSFont.monospacedSystemFont(ofSize: 11, weight: .regular)
        outputText.isRichText = false
        outputText.isEditable = false
        outputText.autoresizingMask = [.width]
        outputView.documentView = outputText
        outputView.hasVerticalScroller = true
        outputView.borderType = .bezelBorder
        view.addSubview(outputView)
    }

    private func paste() {
        if let s = NSPasteboard.general.string(forType: .string) {
            inputText.string = s
        }
    }

    private func copyOut() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(outputText.string, forType: .string)
    }

    private func run(pretty: Bool, convert: String?) {
        NativeUI.shared.invoke("format_text", ["text": inputText.string, "format": inFormat, "pretty": pretty, "convert": convert as Any]) { [weak self] value, err in
            DispatchQueue.main.async {
                if let e = err { self?.outputText.string = "오류: \(e)" }
                else { self?.outputText.string = (value as? String) ?? "" }
            }
        }
    }

    func onOpen() {}
}

// ---------- 설정 ----------

final class SettingsPopoverController: NSObject, NativePopover {
    static let shared: SettingsPopoverController = {
        let c = SettingsPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 300, height: 500))

    private func build() {
        let title = duLabel("설정", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 40, width: 150, height: 20)
        view.addSubview(title)

        let langLbl = duLabel("언어", 11, 0.6)
        langLbl.frame = NSRect(x: 20, y: view.frame.height - 66, width: 80, height: 14)
        view.addSubview(langLbl)
        let lang = duSegment(["한국어", "English"], action: { i in
            NativeUI.shared.invoke("set_lang", ["lang": i == 1 ? "en" : "ko"])
        })
        lang.frame = NSRect(x: 130, y: view.frame.height - 70, width: 150, height: 24)
        view.addSubview(lang)

        let widgetsLbl = duLabel("위젯 표시", 11, 0.6)
        widgetsLbl.frame = NSRect(x: 20, y: view.frame.height - 94, width: 120, height: 14)
        view.addSubview(widgetsLbl)

        let names = ["terminal", "ai_term", "timer", "shelf", "monitor", "toggles", "agents", "ai_apps", "format", "feed"]
        let ko = ["터미널", "AI 터미널", "타이머", "선반", "시스템", "토글", "에이전트", "AI 앱", "포매터", "알림"]
        NativeUI.shared.invoke("get_config") { [weak self] value, _ in
            DispatchQueue.main.async {
                guard let self, let cfg = value as? [String: Any],
                      let hidden = Set(cfg["hidden_widgets"] as? [String] ?? []) as Set<String>? else { return }
                for (i, w) in names.enumerated() {
                    let y = self.view.frame.height - 128 - CGFloat(i) * 30
                    let l = duLabel(ko[i], 12)
                    l.frame = NSRect(x: 20, y: y + 3, width: 130, height: 18)
                    self.view.addSubview(l)
                    let sw = NSSwitch(frame: NSRect(x: 230, y: y, width: 44, height: 24))
                    sw.state = hidden.contains(w) ? .off : .on
                    sw.controlSize = .small
                    let widget = w
                    let box = DUActionBox { [weak sw] in
                        guard let sw else { return }
                        let visible = sw.state == .on
                        NativeUI.shared.invoke("set_widget_visible", ["widget": widget, "visible": visible])
                    }
                    objc_setAssociatedObject(sw, &duActionKey, box, .OBJC_ASSOCIATION_RETAIN_NONATOMIC)
                    sw.target = box
                    sw.action = #selector(DUActionBox.fire)
                    self.view.addSubview(sw)
                }
            }
        }

        let hide = duButton("오브 숨기기 (⌘⌥O)", action: {
            NativeUI.shared.invoke("toggle_orb_hidden")
        }, small: false)
        hide.frame = NSRect(x: 70, y: 54, width: 160, height: 26)
        self.view.addSubview(hide)
        let quit = duButton("종료", action: { NativeUI.shared.invoke("quit_app") }, small: false, danger: true)
        quit.frame = NSRect(x: 100, y: 18, width: 100, height: 28)
        self.view.addSubview(quit)
    }

    func onOpen() {}
}

// ---------- AI 런처 ----------

final class AiTermPopoverController: NSObject, NativePopover {
    static let shared: AiTermPopoverController = {
        let c = AiTermPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 350, height: 400))

    private func build() {
        let title = duLabel("AI 터미널", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 44, width: 220, height: 20)
        view.addSubview(title)
        let sub = duLabel("내장 터미널에서 실행되며 닫아도 계속 돌아갑니다.", 10.5, 0.55)
        sub.frame = NSRect(x: 20, y: 30, width: 310, height: 16)
        view.addSubview(sub)

        let apps = [("codex", "Codex", "icons/chatgpt.png"), ("claude", "Claude Code", "icons/claude.png")]
        for (i, app) in apps.enumerated() {
            let y = view.frame.height - 110 - CGFloat(i) * 70
            let icon = NSImageView(frame: NSRect(x: 20, y: y, width: 40, height: 40))
            icon.image = NSImage(contentsOfFile: Bundle.main.bundlePath + "/" + app.2) ??
                NSImage(contentsOfFile: "/Users/chaejisung/Desktop/Project/dock-util/ui/" + app.2)
            icon.wantsLayer = true
            icon.layer?.cornerRadius = 8
            view.addSubview(icon)
            let name = duLabel(app.1, 13, 0.95, bold: true)
            name.frame = NSRect(x: 72, y: y + 18, width: 180, height: 18)
            view.addSubview(name)
            let status = duLabel("확인 중…", 10.5, 0.55)
            status.frame = NSRect(x: 72, y: y, width: 180, height: 14)
            status.identifier = NSUserInterfaceItemIdentifier("st_" + app.0)
            view.addSubview(status)
            let run = duButton("▶") {
                NativeUI.shared.invoke("term_launch_cli", ["program": app.0]) { _, _ in
                    DispatchQueue.main.async {
                        NativeUI.shared.invoke("open_popover", ["widget": "terminal"])
                    }
                }
            }
            run.frame = NSRect(x: 290, y: y + 6, width: 40, height: 26)
            view.addSubview(run)
        }

        NativeUI.shared.invoke("ai_status") { [weak self] value, _ in
            DispatchQueue.main.async {
                guard let self, let d = value as? [String: Any] else { return }
                for app in apps {
                    if let st = (self.view.subviews.first { ($0 as? NSTextField)?.identifier?.rawValue == "st_" + app.0 }) as? NSTextField {
                        let installed = d[app.0] as? Bool ?? false
                        st.stringValue = installed ? "설치됨 · 터미널에서 실행" : "미설치"
                        st.textColor = installed ? NSColor.systemGreen.withAlphaComponent(0.9) : NSColor.white.withAlphaComponent(0.55)
                    }
                }
            }
        }
    }

    func onOpen() {}
}

final class AiAppsPopoverController: NSObject, NativePopover {
    static let shared: AiAppsPopoverController = {
        let c = AiAppsPopoverController()
        c.build()
        return c
    }()
    private(set) var view = NSView(frame: NSRect(x: 0, y: 0, width: 350, height: 400))

    private func build() {
        let title = duLabel("AI 앱", 14, 0.95, bold: true)
        title.frame = NSRect(x: 20, y: view.frame.height - 44, width: 220, height: 20)
        view.addSubview(title)

        let apps = [("claude", "Claude", "icons/claude.png"), ("chatgpt", "ChatGPT", "icons/chatgpt.png")]
        for (i, app) in apps.enumerated() {
            let y = view.frame.height - 110 - CGFloat(i) * 70
            let icon = NSImageView(frame: NSRect(x: 20, y: y, width: 40, height: 40))
            icon.image = NSImage(contentsOfFile: "/Users/chaejisung/Desktop/Project/dock-util/ui/" + app.2)
            icon.wantsLayer = true
            icon.layer?.cornerRadius = 8
            view.addSubview(icon)
            let name = duLabel(app.1, 13, 0.95, bold: true)
            name.frame = NSRect(x: 72, y: y + 18, width: 180, height: 18)
            view.addSubview(name)
            let status = duLabel("확인 중…", 10.5, 0.55)
            status.frame = NSRect(x: 72, y: y, width: 180, height: 14)
            status.identifier = NSUserInterfaceItemIdentifier("st_" + app.0)
            view.addSubview(status)
            let run = duButton("▶") {
                NativeUI.shared.invoke("open_gui_app", ["name": app.0])
            }
            run.frame = NSRect(x: 290, y: y + 6, width: 40, height: 26)
            view.addSubview(run)
        }

        NativeUI.shared.invoke("ai_status") { [weak self] value, _ in
            DispatchQueue.main.async {
                guard let self, let d = value as? [String: Any] else { return }
                for app in apps {
                    if let st = (self.view.subviews.first { ($0 as? NSTextField)?.identifier?.rawValue == "st_" + app.0 }) as? NSTextField {
                        let key = app.0 + "_app"
                        let installed = d[key] as? Bool ?? false
                        st.stringValue = installed ? "/Applications" : "미설치"
                        st.textColor = installed ? NSColor.systemGreen.withAlphaComponent(0.9) : NSColor.white.withAlphaComponent(0.55)
                    }
                }
            }
        }
    }

    func onOpen() {}
}

// ---------- 팝오버 뷰 팩토리 ----------

protocol NativePopover: AnyObject {
    var view: NSView { get }
    func onOpen()
}

@discardableResult
func nativePopoverController(_ kind: String, size: NSSize) -> (NSView, (() -> Void)?) {
    let c: NativePopover
    switch kind {
    case "timer", "pomodoro": c = TimerPopoverController.shared
    case "toggles": c = TogglesPopoverController.shared
    case "agents": c = AgentsPopoverController.shared
    case "shelf": c = ShelfPopoverController.shared
    case "feed": c = FeedPopoverController.shared
    case "monitor": c = MonitorPopoverController.shared
    case "format": c = FormatPopoverController.shared
    case "settings": c = SettingsPopoverController.shared
    case "ai_term": c = AiTermPopoverController.shared
    case "ai_apps": c = AiAppsPopoverController.shared
    default: c = SettingsPopoverController.shared
    }
    c.view.frame = NSRect(origin: .zero, size: size)
    return (c.view, { c.onOpen() })
}

// ---------- 네이티브 패널 생성 (du_panel_create_native) ----------

@_cdecl("du_panel_create_native")
public func du_panel_create_native(_ id: Int64, _ kind: UnsafePointer<CChar>!,
                                   _ x: Double, _ y: Double, _ w: Double, _ h: Double,
                                   _ level: Int64, _ ignores: Int32, _ vibrancy: Int32) {
    let k = kind != nil ? String(cString: kind) : "settings"
    DispatchQueue.main.async {
        let frame = NSRect(x: x, y: y, width: w, height: h)
        let panel = DUPanel(contentRect: frame,
                            styleMask: [.nonactivatingPanel, .borderless],
                            backing: .buffered, defer: false)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = k == "orb" ? false : true
        panel.ignoresMouseEvents = ignores != 0
        panel.level = NSWindow.Level(rawValue: Int(level))
        panel.isReleasedWhenClosed = false
        panel.acceptsMouseMovedEvents = true
        panel.collectionBehavior = [.canJoinAllSpaces, .stationary, .fullScreenAuxiliary, .ignoresCycle]

        let size = NSSize(width: w, height: h)
        var content: NSView
        var orb: OrbFanView?
        if k == "orb" {
            let fan = OrbFanView(frame: NSRect(origin: .zero, size: size))
            orb = fan
            content = fan
        } else {
            let (view, onOpen) = nativePopoverController(k, size: size)
            content = view
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { onOpen?() }
        }

        let del = DUPanelDelegate(panelId: id)
        panel.delegate = del

        let win = DUWin(panel: panel, web: nil, nativeOrb: orb, del: del, mh: nil, nav: nil)
        panels[id] = win
        panel.contentView = content
        panel.orderFrontRegardless()
        if ignores == 0 {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.05) {
                panel.makeKeyAndOrderFront(nil)
            }
        }
        NativeUI.shared.attach(id: id, kind: k, orb: orb)
    }
}
