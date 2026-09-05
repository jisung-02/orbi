#!/bin/bash
# dock-util 리소스 측정: 바이너리 크기 / RSS(트리 합계) / CPU%(10초 cputime 델타) / 스레드 수
# usage: ./measure.sh <binary> <label>

BIN="$1"
LABEL="$2"
SIZE=$(stat -f%z "$BIN")

"$BIN" > /tmp/dockutil-meas.log 2>&1 &
APP=$!
sleep 25  # 워밍업: WebKit XPC 스폰 + 루프 안정화

tree_rss() {
  local root=$1
  # 루트 + 직계 자식(WebKit XPC) RSS 합계 (KB)
  ps -axo pid=,ppid=,rss= | awk -v r=$root '
    $1==r { root=$1; sum+=$3; next }
    root != "" && $2==root { sum+=$3 }
    END { print sum+0 }'
}

cputime_secs() {
  # "MM:SS.CC" | "H:MM:SS.CC" → 초 (소수)
  ps -o time= -p $1 | awk -F'[:.]' '{
    if (NF==3) printf "%.2f", $1*60+$2+$3/100
    else printf "%.2f", $1*3600+$2*60+$3/100 }'
}

cpu_sample() {
  local pid=$1
  local t1 t2
  t1=$(cputime_secs $pid)
  sleep 10
  t2=$(cputime_secs $pid)
  echo "scale=1; ($t2-$t1)/10*100" | bc
}

echo "===== $LABEL ====="
echo "binary_bytes=$SIZE"
echo "threads_main=$(ps -M $APP 2>/dev/null | wc -l | tr -d ' ')"
for i in 1 2 3; do
  echo "sample$i rss_main_kb=$(ps -o rss= -p $APP | tr -d ' ') rss_tree_kb=$(tree_rss $APP) cpu_pct=$(cpu_sample $APP)"
done
kill $APP 2>/dev/null; wait $APP 2>/dev/null
echo ""
