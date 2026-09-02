# dock-util

macOS Dock 옆 빈 공간을 활용하는 **글래스 위젯 오브/바**. Rust + Tauri 2 기반.

## 오브 (단일 형태)

화면 우하단에 동그란 글래스 오브 하나가 떠 있고, 마우스를 올리면 위젯 9개가 **2중 링으로 블룸**.
클릭하면 글래스 팝오버가 열림. 접힌 상태에서는 마우스 이벤트가 뒤로 통과.
**마우스 커서가 있는 화면과 현재 스페이스·전체화면을 따라다닙니다.**

## 위젯 (9종)

- **AI 터미널 / AI 앱 런처** — Codex·Claude Code를 내장 터미널에서 바로 실행(세션 유지, 닫아도 계속 돌아감), Claude·ChatGPT 데스크톱 앱 실행. 미설치 항목은 표시
- **터미널** — 실제 zsh 로그인 셸(portable-pty PTY) + xterm.js. 반투명 글래스 배경, **가장자리 드래그로 크기 조절**. ⌘C 복사 · ⌘V 붙여넣기 · ⌘+/−/0 글자크기(설정 저장) · 새 셸 버튼. 팝오버를 닫아도 **셸 세션·스크롤백 유지**(최대 128KB), 다시 열면 이어서 사용
- **포모도로** — 집중/휴식 자동 전환, 프리셋(25/5 · 50/10 · 15/5), 완료 시 알림+사운드, **앱 재시작해도 타이머 이어짐**(설정 파일 저장), 실행 중엔 오브 배지에 남은 시간 표시
- **파일 선반 (드롭존)** — 바/오브/선반 팝오버에 파일을 끌어다 놓으면 임시 보관 → 열기/이동/복사/Finder에서 보기, 폴더·파일 아이콘 구분, 합계 크기 표시. **포커스를 잃어도 안 닫히는 고정 팝오버**
- **시스템 모니터** — CPU 스파크라인 기록, RAM, 배터리(15초 캐시), **네트워크 ↓/↑ KB/s**, **디스크 여유**, 업타임
- **시스템 토글** — 다크모드 · Wi-Fi · Bluetooth · 음소거 · 잠금방지(caffeinate)
- **실행중 에이전트** — claude · codex · gemini · copilot · aider 등 프로세스 감지, `kill <pid>` 복사 버튼, 시작 시 피드 기록
- **포매터** — JSON/YAML/TOML **자동 감지** + 정리·압축·상호 변환 (고정 팝오버)
- **알림 피드** — 이벤트 통합 피드 + 읽지 않음 배지 + 전체 지우기
- **설정** — 언어(한국어/English), 종료

팝오버 닫기: 우측 상단 **✕** · **`Esc`** · **`⌘W`** · 또는 어디서든 **`⌘⌥W`**(전역 단축키 — 다른 앱에 포커스가 있어도 동작). 고정 팝오버(선반·터미널·포매터)는 포커스를 잃어도 유지됩니다.

## 오브 UX 디테일

- 호버 판정은 60ms 폴링(`NSEvent mouseLocation`, 권한 불필요)으로 하고, 커서가 오브 70pt 반경 안에 들어오면 `setIgnoresMouseEvents(false)` + 블룸
- 펼쳐진 동그라미에 라벨은 **호버 시에만** 표시(항상 켜두면 이웃 원과 겹침)
- 에이전트 수·선반 개수·안 읽은 알림이 동그라미 위에 **배지**로 실시간 표시
- 동그라미 클릭 시 팬을 먼저 접은 뒤 팝오버를 열어 팝오버가 가려지지 않게 함

## 디자인

- 네이티브 vibrancy(`NSVisualEffectView` hudWindow) + 저알파 CSS 레이어로 투명한 글래스
- 창은 투명 + 라운딩, 모든 스페이스/풀스크린 위에 표시(window level 25, 팝오버는 101)

## 빌드 & 실행

요구사항: Rust, Xcode Command Line Tools. 프런트엔드는 `ui/` 정적 파일이라 **Node 불필요**.

```bash
cd src-tauri
cargo run          # 개발 실행
cargo build --release
cargo test         # 배치/포매터/포모도로/에이전트 분류/오브 기하 단위 테스트
./target/debug/dock-util --check   # 접근성 신뢰 + 독 프레임 진단
```

## 권한

| 권한 | 필요 시점 | 용도 |
|---|---|---|
| 접근성 | 불필요 (참고용 진단만) | 과거 바 모드의 독 실측용 — 현재 오브 전용이라 미사용 |
| 자동화 (System Events) | 다크모드 토글 첫 클릭 | 모양 전환 |
| 알림 | 첫 실행 | 포모도로 완료 등 |

접근성은 실행 중인 **바이너리 경로 기준**으로 기록됩니다. 개발 중에는
`src-tauri/target/debug/dock-util` 을 목록에서 켜야 하며 `--check`로 확인할 수 있습니다.

## 선택 의존성

- `brew install blueutil` — Bluetooth 토글

## 구조

```
src-tauri/src/
├── platform.rs   # AX 독 프레임, NSWindow 제어(GCD 메인 큐), NSPasteboard, 마우스 위치
├── placement.rs  # 배치 엔진 + 오브 호버 폴링
├── config.rs     # 설정 영속화 (~/Library/Application Support/dock-util/)
├── ipc.rs        # Tauri 커맨드
├── app.rs        # 전역 상태
└── widgets/      # terminal · pomodoro · toggles · monitor · shelf · agents · formatter · feed
ui/               # 정적 프런트엔드 (vanilla JS + xterm.js 벤더링)
```

### 구현 노트 (이 환경에서 겪은 것)

- `visible:false`로 생성한 윈도우는 나중에 `show()`해도 스페이스 배정이 유령 상태가 됨 →
  **생성 시 표시(오프스크린) + `set_position`으로 유인**하는 경로 사용
- tao/tauri의 메인 스레드 디스패치(`run_on_main`, `exec_sync`)가 동작하지 않아 GCD main queue를
  직접 쓰는 `on_main_async` FFI 구현
- NSWindow `setFrame`은 백그라운드 스레드에서 크래시 → 위치 변경은 tauri `set_position` 사용

## 리소스 최적화

안정 상태 기준 **CPU ~2%(5초 창, 전체 시스템의 ~0.2%) / 실메모리 ~36MB**.

| 항목 | 최적화 전 | 후 |
|---|---|---|
| 독 PID 탐색 | 0.9초마다 전체 프로세스 스캔 | 캐시 (최초 1회, AX 실패 5회 누적 시 재스캔) |
| uptime(sysctl) | 2초마다 자식 프로세스 | 부팅 시각 OnceLock 캐시, 프로세스 생성 0 |
| 배터리(pmset)·디스크(df) | 2초마다 자식 프로세스 | 15초 캐시 |
| 에이전트 스캔 | 4초, 모니터와 sysinfo 공유(락 경합) | 10초, 독립 인스턴스 |
| 포모도로 틱 | 대기 중에도 1초+전 창 emit | 대기 중 4초·emit 없음 |
| 모니터 수집 | 고정 2초 | 팝오버 열림 2초 / 닫힘 5초 |
| 배치 폴링 | 0.9초 (오브 형태도 AX 호출) | 1.5초 + 오브 형태는 AX 호출 생략 |
| 빌드 | debug | release (LTO + opt-size + strip) |

## 멀티 디스플레이

오브/바는 **마우스 커서가 있는 화면을 따라갑니다.** 디스플레이 연결·해제, 해상도 변경, 화면 배열 변경도
배치 폴링(1.5초) 안에 자동 반영됩니다. 독은 그 화면 위에 있을 때만 실측에 사용하며, 팝오버 클램프도
해당 화면 경계 안에서 이뤄집니다.

## 배포 (GitHub Actions → Homebrew 탭)

시맨틱 태그를 푸시하면 자동 배포됩니다:

```bash
git tag v0.1.0 && git push origin v0.1.0          # 안정본
git tag v0.2.0-beta && git push origin v0.2.0-beta # 프리릴리스
```

| 태그 | GitHub Release | Homebrew |
|---|---|---|
| `v1.2.3` | 정식 릴리스 | `Formula/dock-util.rb` 갱신 → `brew upgrade`로 배포 |
| `v1.2.3-beta` | 사전릴리스 표시 | `Formula/dock-util@1.2.3-beta.rb` 추가 (latest 불변) |

설치:
```bash
brew tap <owner>/homebrew-tap
brew install dock-util                    # 안정본 (upgrade로 갱신)
brew install dock-util@0.2.0-beta         # 특정 프리릴리스 버전 고정 설치
```

**저장소 설정 필요**: `Settings → Secrets → Actions`에 `TAP_TOKEN`(탭 저장소에 push 가능한 PAT),
원한다면 `Variables → TAP_REPOSITORY`(기본값 `<owner>/homebrew-tap`). 탭 저장소에는 `Formula/` 디렉터리만 있으면 시작 가능합니다.

## 알려진 제약

- macOS 알림센터 기록은 공개 API가 없어 알림 위젯은 인앱 이벤트 피드
- 독이 "자동 가림"이어도 독 프레임은 감지되어 정상 배치
- 파일 선반의 드래그 아웃(끌어내기)은 복사/이동 액션으로 대체
- 독이 화면의 94% 이상을 차지해 좌우 여백이 부족하면 바는 우하단 기본 위치에 배치(겹침 최소화)
