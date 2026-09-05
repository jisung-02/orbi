import Cocoa

@_silgen_name("du_test_last_message")
func lastNativeMessage() -> UnsafePointer<CChar>

@main
struct NativeTests {
    static var failures = 0
    static func check(_ condition: Bool, _ message: String) {
        if !condition { failures += 1; print("FAIL: \(message)") }
    }
    static func descendants(_ view: NSView) -> [NSView] {
        view.subviews + view.subviews.flatMap(descendants)
    }
    static func click(_ button: NSButton?) {
        guard let button else { return }
        _ = button.target?.perform(button.action, with: button)
    }
    static func main() {
        _ = NSApplication.shared
        let timer = TimerPopoverController.shared
        let tabs = descendants(timer.view).compactMap { $0 as? NSSegmentedControl }.filter { $0.segmentCount == 3 }
        check(tabs.count == 2, "timer builds its mode and focus controls once")
        let mode = tabs.first!
        mode.selectedSegment = 2
        _ = mode.target?.perform(mode.action, with: mode)
        func button(_ title: String) -> NSButton? {
            descendants(timer.view).compactMap { $0 as? NSButton }.first { $0.title == title }
        }
        click(button("시작"))
        RunLoop.main.run(until: Date().addingTimeInterval(0.3))
        click(button("일시정지"))
        check(button("재개") != nil, "paused stopwatch has a resume action")
        click(button("재개"))
        RunLoop.main.run(until: Date().addingTimeInterval(0.1))
        let readings = descendants(timer.view).compactMap { ($0 as? NSTextField)?.stringValue }.filter { $0.hasPrefix("00:00.") }
        check(readings.contains { (Double($0.suffix(2)) ?? 100) < 60 }, "resume does not count paused elapsed twice")
        click(button("리셋"))

        var reply: Any?
        NativeUI.shared.invoke("format_text") { value, _ in reply = value }
        NativeUI.shared.handleReply(seq: 1, ok: true, json: "\"formatted\"")
        RunLoop.main.run(until: Date().addingTimeInterval(0.02))
        check(reply as? String == "formatted", "native IPC decodes scalar string responses")

        mode.selectedSegment = 0
        _ = mode.target?.perform(mode.action, with: mode)
        var snapshot: [String: Any] = ["running": true, "paused": false, "phase": "focus", "remaining_secs": 1500, "focus_min": 25, "rounds_done": 0]
        timer.update(snapshot: snapshot)
        let pauseButton = button("일시정지")
        let updateStart = CFAbsoluteTimeGetCurrent()
        for remaining in stride(from: 1499, through: 500, by: -1) {
            snapshot["remaining_secs"] = remaining
            timer.update(snapshot: snapshot)
        }
        print("1000 timer updates: \(CFAbsoluteTimeGetCurrent() - updateStart) seconds")
        check(button("일시정지") === pauseButton, "countdown ticks reuse existing action buttons")

        let settings = SettingsPopoverController.shared
        NativeUI.shared.handleReply(seq: 2, ok: true, json: "{\"hidden_widgets\":[]}")
        RunLoop.main.run(until: Date().addingTimeInterval(0.03))
        let switches = descendants(settings.view).compactMap { $0 as? NSSwitch }
        check(switches.count == 10 && switches.allSatisfy { $0.frame.minY >= 90 && settings.view.bounds.contains($0.frame) },
              "all widget visibility switches fit above settings footer")

        weak var retained: NSObject?
        autoreleasepool {
            let token = NSObject()
            retained = token
            let button = duButton("temporary") { _ = token.description }
            click(button)
        }
        check(retained == nil, "removed buttons release action closures")
        let fan = OrbFanView(frame: NSRect(x: 0, y: 0, width: 380, height: 300))
        fan.setExpanded(true)
        let circles = (fan.layer?.sublayers ?? []).filter { $0.bounds.width == 46 }
        let ordered = circles.sorted {
            ($0.animation(forKey: "orb-reveal")?.beginTime ?? 0) < ($1.animation(forKey: "orb-reveal")?.beginTime ?? 0)
        }
        check(circles.count == 11 && circles.allSatisfy { $0.animation(forKey: "orb-reveal") != nil }, "orb schedules each circle reveal")
        for (start, end, ascending) in [(0, 4, true), (4, 8, false), (8, 11, true)] {
            if ordered.count == 11 {
                for i in (start + 1)..<end {
                    check(ascending ? ordered[i].frame.midY > ordered[i-1].frame.midY : ordered[i].frame.midY < ordered[i-1].frame.midY,
                          "native ring reveals in the requested vertical direction")
                }
            }
        }
        check(circles.allSatisfy { ($0.backgroundColor?.alpha ?? 0) >= 0.7 }, "orb buttons retain a readable translucent background")
        if circles.count == 11 {
            check(circles[8...].allSatisfy { $0.frame.midX < 220 }, "third ring is packed toward the lower left")
        }
        fan.setExpanded(false)
        let hiddenOrder = circles.sorted {
            ($0.animation(forKey: "orb-reveal")?.beginTime ?? 0) < ($1.animation(forKey: "orb-reveal")?.beginTime ?? 0)
        }
        check(zip(hiddenOrder, ordered.reversed()).allSatisfy { $0 === $1 }, "collapse reverses the complete reveal sequence")
        check(circles.allSatisfy { $0.opacity == 0 && $0.animation(forKey: "orb-reveal") != nil }, "collapse fades instead of hiding immediately")
        fan.setExpanded(true)
        check(circles.allSatisfy { $0.opacity == 1 && $0.animationKeys()?.count == 1 }, "rapid reentry replaces collapse animations")
        let (card, _) = nativePopoverController("settings", size: NSSize(width: 300, height: 500))
        check((card.layer?.backgroundColor?.alpha ?? 0) >= 0.8, "popover retains its own translucent surface")
        let shelf = ShelfPopoverController.shared
        let fixtures: [[String: Any]] = (0..<30).map { ["name": "file-\($0).txt", "path": "/tmp/file-\($0).txt", "size_mb": 1.0] }
        shelf.update(list: fixtures)
        let shelfRows = descendants(shelf.view).compactMap { $0 as? ShelfFileRow }
        check(shelfRows.count == 30, "shelf keeps all rows accessible instead of truncating at eight")
        let viewport = descendants(shelf.view).compactMap { $0 as? ShelfScrollView }.first
        check(viewport?.hasVerticalScroller == true && (viewport?.documentView?.frame.height ?? 0) > (viewport?.contentSize.height ?? 0), "shelf overflow scrolls")
        check(viewport?.registeredDraggedTypes.contains(.fileURL) == true, "viewport accepts file drops")
        for row in shelfRows {
            check(row.registeredDraggedTypes.contains(.fileURL) && row.fileURL != nil, "each row receives files and supplies an outgoing file URL")
            let buttons = row.subviews.compactMap { $0 as? NSButton }.sorted { $0.frame.minX < $1.frame.minX }
            check(buttons.count == 5 && row.subviews.allSatisfy { row.bounds.contains($0.frame) }, "row labels and actions fit within the card")
            for i in 1..<buttons.count { check(buttons[i-1].frame.maxX <= buttons[i].frame.minX, "row actions never overlap") }
        }
        shelf.update(list: [])
        check(descendants(shelf.view).compactMap { $0 as? ShelfFileRow }.isEmpty, "clearing shelf removes all rows")
        let fileDrag = ShelfDragFixture()
        fileDrag.draggingPasteboard.writeObjects([URL(fileURLWithPath: "/tmp/선반 테스트.txt") as NSURL])
        let dropRoot = ShelfDropView(frame: .zero)
        check(dropRoot.draggingEntered(fileDrag) == .copy, "file drag enters shelf")
        check(dropRoot.prepareForDragOperation(fileDrag), "shelf approves the drop before importing files")
        check(viewport!.prepareForDragOperation(fileDrag), "scroll viewport approves the drop before importing files")
        check(dropRoot.performDragOperation(fileDrag), "shelf imports a released file drag")
        let request = String(cString: lastNativeMessage()).data(using: .utf8)!
        let requestObject = try! JSONSerialization.jsonObject(with: request) as! [String: Any]
        check(requestObject["cmd"] as? String == "shelf_add", "file drop dispatches shelf_add to Go")
        check((requestObject["args"] as? [String: Any])?["paths"] as? [String] == ["/tmp/선반 테스트.txt"], "drop preserves Unicode and spaces in file paths")
        fileDrag.draggingPasteboard.clearContents()
        fileDrag.draggingPasteboard.setPropertyList(["/tmp/legacy-file.txt"], forType: NSPasteboard.PasteboardType("NSFilenamesPboardType"))
        check(dropRoot.draggingEntered(fileDrag) == .copy && shelfURLs(fileDrag.draggingPasteboard).count == 1, "legacy Finder file lists are accepted")
        let window = DUPanel(contentRect: NSRect(x: 0, y: 0, width: 400, height: 440), styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: false)
        window.acceptsShelfFiles = true
        check(window.prepareForDragOperation(fileDrag) && window.performDragOperation(fileDrag), "panel provides fallback file drop destination")
        fileDrag.draggingPasteboard.clearContents()
        fileDrag.draggingPasteboard.setString("not a file", forType: .string)
        check(dropRoot.draggingEntered(fileDrag).isEmpty && !window.prepareForDragOperation(fileDrag), "non-file drags are rejected")
        print("Native tests: \(failures) failure(s)")
        exit(failures == 0 ? 0 : 1)
    }
}
