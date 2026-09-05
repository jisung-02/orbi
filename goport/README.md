# dock-util — Swift + Go (네이티브 UI)

맥 독 옆 글래스 위젯 오버레이. **Swift(네이티브 셸 + 네이티브 UI) + Go(앱 로직)**.

- **네이티브 UI 모드(기본)**: 오브 팬 + 위젯 팝오버를 AppKit 뷰로 렌더링 — 웹뷰/JS 없음
  (WebContent 프로세스 0개). 터미널만 예외적으로 xterm.js 웹 패널 유지
  (SwiftTerm의 forkpty 스폰이 불안정하여, libghostty C API 안정화 시 스왑 예정).
  `DU_UI=web`으로 전 모드 롤백 가능.
- **Swift 셸** ([shell.swift](shell.swift) + [nativeui.swift](nativeui.swift) +
  [nativeui2.swift](nativeui2.swift) → `libdu_shell.dylib`): NSPanel(nonactivating)
  생성/조작, 네이티브 오브 팬(CALayer, SF Symbols), 위젯 팝오버 뷰(타이머/토글/
  에이전트/선반/모니터/포매터/피드/설정/AI 런처), AX 독 프레임, 파일 드래그&드롭,
  ⌘⌥W 핫키. Go와의 경계는 `du_*` C ABI이며 Go 콜백은 dynamic_lookup으로 연결.
- **Go 백엔드**: 설정/i18n/포모도로/PTY 터미널/선반/모니터/에이전트/토글/포매터/피드/
  배치·호버 루프/IPC. 웹 UI(`../ui`)는 `DU_UI=web` 폴백 모드에서만 사용.

## 빌드 / 실행

```bash
cd goport
./build.sh     # swiftc → libdu_shell.dylib, 그 다음 go build
./dock-util
```

- `--check`: 접근성 신뢰/독 프레임/화면 구성 진단
- `--qa`: 마우스 이벤트 시뮬레이션으로 호버/클릭 자동 테스트
- `DU_DEBUG_POPOVER=<위젯>`: 실행 2초 뒤 해당 팝오버를 열어 검증
- `DU_UI_DIR=<경로>`: ui 디렉터리 명시 지정
- 검증: `go test -race ./...`, `go vet ./...`, `./test-native.sh`
- 웹 UI/배포 회귀 테스트(저장소 루트): `node --test tests/ui.test.cjs`, `python3 tests/release_test.py`
- SwiftTerm 외부 소스 없이 Xcode Command Line Tools와 Go로 빌드합니다.

## 리소스 비교 (2026-09-04, 1470×956 + 1920×1080, 유휴)

| 항목 | Rust/Tauri | Go+ObjC (이전) | **Go+Swift (현재)** |
|---|---|---|---|
| 바이너리 | 6.12 MB | 3.96 MB | **3.85 MB + dylib 0.17 MB** |
| RSS (본체+XPC) | ~106 MB | ~81 MB | **~86 MB** |
| 스레드 | 31 | 20 | 23 |
| CPU (유휴) | <1% | <1% | <1% |

## 성능 최적화 (2026-09-05, 실측: 유휴 안정 상태)

| 지표 | 이전 | 최적화 후 |
|---|---|---|
| 유휴 CPU | 3.10% | **1.20% (−61%)** |
| 본체 RSS | 87.8 MB | **77.3 MB** |
| 실제 물리 비용 (footprint) | — | **24.1 MB** (RSS의 ~55MB는 dyld 공유 캐시 프레임워크 페이지 — 시스템 전체 공유분) |
| JS→Go invoke | 초당 ~25회 (40ms 폴링) | **유휴 시 0회** |
| 렌더러 프로세스 | 패널별 개수 미확인 | **웹뷰 3개가 WebContent 1개 공유** |

적용: `orb_state` 40ms 폴링 제거(orb-toggle 이벤트 전환) · WebKit 프로세스 풀 공유 ·
Go GC 튜닝(GCPercent 50 + 48MB 소프트 리밋) · 불변 프레임 setFrame 스킵 ·
cgo 마우스 폴링 2콜→1콜 · 포모 스냅샷 변화 시에만 emit · stats는 모니터 팝오버
열렸을 때만 emit · 45초 유휴 시 오브 펄스 GPU 애니메이션 정지(접근 시 재개).
SIMD는 수치 연산 핫루프가 없어 적용 대상 없음 — 남은 1.2%는 주기 기능 작업
(모니터 수집/포모 틱/에이전트 ps 스캔)이며, 더 줄이려면 웹뷰 없는 네이티브 UI 전환 필요.

## 참고

- **호버 유실 버그 (수정 + 실측 완료)**: 배치 틱마다 `activateIgnoringOtherApps`를 호출해
  다른 앱이 계속 비활성화 → 커서 호버 UI(툴팁 등)가 사라지던 문제.
  활성화는 시작 시 1회만, `du_panel_apply`는 활성화 없이 프레임/레벨/컬렉션/전면 재단언만.
  실측: 앱 실행 중 20초 관측에서 dock-util으로의 frontmost 플립 0회.
- **전체화면 오버레이 (실측 완료)**: 오브 레벨 2000 / 팝오버 2001 +
  `activeSpaceDidChangeNotification` 시 `orderOut → orderFrontRegardless` 재합류.
  Finder 전체화면 유지 중 오브 서클 클릭이 웹뷰에 도달해 팝오버가 열리는 것을
  CGWindowList로 실측 확인. 스페이스 경계에서 CGWindowList z-index는 시각 순서와
  다르게 보일 수 있어 클릭 도달로 판정했다.
- **클릭/타이핑 패널 수정**: borderless nonactivating 패널은 ① 비활성 상태 첫 클릭이
  AppKit의 '창 활성화'로 낚아채므로 `DUPanel.sendEvent`에서 히트 뷰(WKWebView)로
  강제 전달, ② `canBecomeKey=false`라 터미널 타이핑이 안 되므로 `canBecomeKey` 허용.
- **화면 전환 팝오버 팔로우**: 커서가 다른 디스플레이로 넘어가면 열려 있던 팝오버도
  같은 서클 앵커를 유지한 채 오브를 따라 새 화면으로 재배치된다 (배치 루프에서
  대상 화면 변경을 감지).
- **시작 크래시 자동 복구**: 간헐적(~10%) WebKit 초기화 트랩에 대비해 슈퍼바이저가
  6초 내 비정상 종료를 감지하면 최대 3회 재시작한다 (`DU_SUPERVISE` 내부 플래그).
- **정밀 클릭 패스스루**: 오브 패널 영역 중 그려진 동그라미 위에서만 클릭을 받고,
  팬의 빈 공간은 아래 앱으로 클릭이 통과된다 (25ms 폴링으로 커서 위치 판정).
- **오브 숨기기**: ⌘⌥O 또는 설정 팝오버의 "오브 숨기기" 버튼으로 오브를 숨긴다
  (숨길 때 canJoinAllSpaces를 해제하고 orderOut — 스페이스 합류 부활 방지).
  다시 ⌘⌥O를 누르면 현재 커서 화면에 오브가 돌아온다.
- **오브 재호버 = 팝오버 닫기**: 팝오버가 열려 있을 때 오브에 마우스를 다시 올리면
  팬이 펼쳐지며 열려 있던 팝오버(고정 위젯 포함)를 닫는다.
- **터미널 색상**: PTY 자식에 `TERM=xterm-256color` + `COLORTERM=truecolor`를 강제
  (GUI 실행 시 부모에 TERM이 없어 색이 꺼지던 문제). 부모에 TERM이 있어도 치환.
- **AI 앱 선기동**: 시작 3초 후 Claude/ChatGPT 앱을 `open -g -a`로 백그라운드 기동
  (포커스 미인출). 팝오버 ▶는 `open -a`로 전면 활성화만 수행 → 즉시 실행 상태로 전환.
- **터미널 엔진 결정**: libghostty의 C API는 1.3 기준 WIP(미안정화)라 현재
  xterm.js + creack/pty 유지. libghostty 안정화 시 스왑 검토.
- **AI 런처 감지**: GUI 실행 시 기본 PATH(/usr/bin:/bin)로는 brew/npm CLI를 못 찾으므로
  `/opt/homebrew/bin`, `~/.local/bin` 등을 보강한 PATH로 감지.
  앱은 /Applications, ~/Applications, /System/Applications 순으로 확인.
  `--ai-status` 플래그로 감지 결과 단독 확인 가능.
- **파일 드래그&드롭**: WebKit이 페이지 로드 후 드래그 타입을 덮어써서 fileURL 등록이
  사라지는 것을 `registerForDraggedTypes` 오버라이드로 방지.
- 폴더 선택은 `NSOpenPanel.begin` 비동기 (메인 루프 차단 없음 — Rust rfd hang 해결).
- 에이전트 스캔은 `ps` 일괄 파싱 (gopsutil 개별 시스템콜 대비 10초 주기 CPU 스파이크 제거).
- 설정은 Rust 버전과 같은 `~/Library/Application Support/dock-util/config.json` 공유.
- 접근성 권한: 독 프레임 AX 판독에 필요 (미승인 시 주 화면 우하단 폴백).
- 알려진 문제: 이전 인스턴스 종료 직후(~2초 내) 재실행 시 간헐적(~10%) macOS 측
  트랩으로 죽음 (lldb 부착 시 미재현 — WebKit 초기화 경합 추정). 2초 이상 간격 유지.
  `--fskey`(전체화면 토글 키 입력), `--qa2`(오브 호버+클릭)는 접근성 신뢰 바이너리에서만 동작.
- 레거시: `legacy/shell_darwin.m` (ObjC 셸 — 참고용)
