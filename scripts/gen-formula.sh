#!/usr/bin/env bash
# Homebrew 탭 포뮬러 생성기
#
# 사용법:
#   gen-formula.sh <version> <sha256> <asset> <repo> <out-dir> [--prerelease]
#
# 예:
#   gen-formula.sh 1.2.3 abc123... dock-util-1.2.3-aarch64-macos.tar.gz chaejisung/dock-util tap/Formula
#   gen-formula.sh 1.2.3-beta abc123... dock-util-1.2.3-beta-aarch64-macos.tar.gz chaejisung/dock-util tap/Formula --prerelease
#
# 안정 버전: Formula/dock-util.rb 갱신 (brew upgrade 대상)
# 프리릴리스: Formula/dock-util@<version>.rb 추가 (latest 아님, brew install dock-util@<version>)
set -euo pipefail

VERSION="${1:?version 필요}"
SHA256="${2:?sha256 필요}"
ASSET="${3:?asset 필요}"
REPO="${4:?repo 필요}"
OUT="${5:?출력 디렉터리 필요}"
PRERELEASE="${6:-}"

mkdir -p "$OUT"
URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"

if [ "$PRERELEASE" != "--prerelease" ]; then
  cat > "${OUT}/dock-util.rb" <<RUBY
# frozen_string_literal: true

class DockUtil < Formula
  desc "Glass widget orb that lives next to the macOS Dock"
  homepage "https://github.com/${REPO}"
  version "${VERSION}"
  url "${URL}"
  sha256 "${SHA256}"

  depends_on arch: :arm64
  depends_on macos: :ventura

  def install
    bin.install "dock-util"
  end

  def caveats
    <<~EOS
      Run: dock-util
      On first launch, grant Accessibility permission if prompted.
    EOS
  end

  livecheck do
    url :stable
    regex(/^v?(\d+\.\d+\.\d+)$/i)
    strategy :github_latest
  end
end
RUBY
  echo "생성: ${OUT}/dock-util.rb (안정본 ${VERSION})"
fi

if [ "$PRERELEASE" = "--prerelease" ]; then
  # 1.2.3-beta → DockUtilAT123Beta (Homebrew 버전 포뮬러 클래스 명명 규칙)
  NUM="$(printf '%s' "$VERSION" | tr -d '.')"
  CLASS="DockUtilAT$(printf '%s' "$VERSION" | tr -cd '[:alnum:]' | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"

  cat > "${OUT}/dock-util@${VERSION}.rb" <<RUBY
# frozen_string_literal: true

class ${CLASS} < Formula
  desc "Glass widget orb (prerelease ${VERSION})"
  homepage "https://github.com/${REPO}"
  version "${VERSION}"
  url "${URL}"
  sha256 "${SHA256}"

  depends_on arch: :arm64
  depends_on macos: :ventura

  def install
    bin.install "dock-util" => "dock-util@${VERSION}"
  end

  def caveats
    <<~EOS
      Prerelease build. Run: dock-util@${VERSION}
      This is versioned and does not affect the latest stable formula.
    EOS
  end
end
RUBY
  echo "생성: ${OUT}/dock-util@${VERSION}.rb (프리릴리스, 클래스 ${CLASS})"
fi
