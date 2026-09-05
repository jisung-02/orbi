#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT
clang -c tests/callbacks.c -o "$TEST_DIR/callbacks.o"
swiftc -parse-as-library -import-objc-header du_callbacks.h \
  shell.swift nativeui.swift nativeui2.swift tests/native_test.swift tests/drag_fixture.swift "$TEST_DIR/callbacks.o" \
  -framework Cocoa -framework WebKit -framework Carbon -framework ApplicationServices \
  -o "$TEST_DIR/native-tests"
"$TEST_DIR/native-tests"
