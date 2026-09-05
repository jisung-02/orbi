#!/bin/bash
# dock-util Swift+Go 빌드
#   [1] shell.swift → libdu_shell.dylib (swiftc, Go 콜백은 dynamic_lookup)
#   [2] Go 바이너리 링크 (-export_dynamic으로 콜백 익스포트)
set -euo pipefail
cd "$(dirname "$0")"

echo "[1/2] Swift 네이티브 셸 빌드..."
swiftc -O -parse-as-library -emit-library -o libdu_shell.dylib \
  -import-objc-header du_callbacks.h \
  shell.swift nativeui.swift nativeui2.swift \
  -framework Cocoa -framework WebKit -framework Carbon \
  -framework CoreGraphics -framework CoreFoundation -framework ApplicationServices \
  -Xlinker -install_name -Xlinker @rpath/libdu_shell.dylib \
  -Xlinker -undefined -Xlinker dynamic_lookup
codesign --force --sign - libdu_shell.dylib 2>/dev/null || true

echo "[2/2] Go 바이너리 빌드..."
# -export_dynamic: dockutil_on_* 콜백을 실행파일에서 익스포트 (dylib이 호출)
go build -ldflags="-s -w -extldflags '-Wl,-export_dynamic'" -o dock-util .

echo "완료: goport/dock-util"
