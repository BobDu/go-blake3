#!/usr/bin/env bash
# 量化 #6(hash_neon 用 SHA3 VEOR3)的收益
#   基线 = hash-critical-path (PR 48)      待测 = eor3-measure (PR 48 + EOR3)
set -euo pipefail
REPO=https://github.com/BobDu/go-blake3.git
BASE_BRANCH=hash-critical-path
TEST_BRANCH=eor3-measure
SMALL='BLAKE3/Entire/(0001_block|0004_block|0016_block|0002_kib)$'
LARGE='BLAKE3/Entire/(0004_kib|0064_kib|1024_kib)$'
REPS=20

D=$(mktemp -d); cd "$D"; echo "工作目录 $D"
sysctl -n machdep.cpu.brand_string 2>/dev/null || lscpu | grep -i 'BIOS Model' || uname -m
go version; echo

echo "[1/5] 拉取两臂 ..."
git clone -q "$REPO" before
git -C before fetch -q origin "$BASE_BRANCH"; git -C before checkout -q FETCH_HEAD
cp -R before after
git -C after fetch -q origin "$TEST_BRANCH"; git -C after checkout -q FETCH_HEAD
if diff -q before/internal/alg/hash/hash_neon/impl_arm64.s \
           after/internal/alg/hash/hash_neon/impl_arm64.s >/dev/null 2>&1; then
  echo "  !!! 两臂 .s 相同,实验作废"; exit 1
fi
nb=$(wc -l < before/internal/alg/hash/hash_neon/impl_arm64.s)
na=$(wc -l < after/internal/alg/hash/hash_neon/impl_arm64.s)
echo "  两臂确有差异 ✓  impl_arm64.s: $nb → $na 行 (差 $((na-nb)),期望 +896)"
[ $((na-nb)) -eq 896 ] || { echo "  !!! 行数差不是 896,作废"; exit 1; }

echo "[2/5] 下载依赖并编译 ..."
( cd before && go mod download && go build ./... ); echo "  完成 ✓"

echo "[3/5] 派发探针 ..."
cat > before/zz_d_test.go <<'EOF'
package blake3

import (
	"testing"

	"github.com/zeebo/blake3/internal/consts"
)

func TestZZD(t *testing.T) {
	t.Logf("DISPATCH HasNEON=%v HasSVE2=%v", consts.HasNEON, consts.HasSVE2)
}
EOF
( cd before && go test -run TestZZD -v . 2>&1 | grep DISPATCH | sed 's/^/  /' )
rm -f before/zz_d_test.go

echo "[4/5] 编译并跑全量测试 ..."
for a in before after; do ( cd "$a" && go test -count=1 . >/dev/null && go test -c -o "$D/$a.test" . ); echo "  $a ✓"; done

echo "[5/5] 基准 $REPS 轮,约 12 分钟"
: > b.txt; : > a.txt
for ((r=0; r<REPS; r++)); do
  printf '\r  第 %2d/%d 轮' $((r+1)) "$REPS"
  if (( r % 2 == 0 )); then ORDER="before after"; else ORDER="after before"; fi
  for x in $ORDER; do
    [ "$x" = before ] && F=b.txt || F=a.txt
    GOMAXPROCS=1 "$D/$x.test" -test.run=XXX -test.bench="$SMALL" -test.benchtime=3s    -test.cpu=1 2>/dev/null | grep '^Benchmark' >> "$F" || true
    GOMAXPROCS=1 "$D/$x.test" -test.run=XXX -test.bench="$LARGE" -test.benchtime=500ms -test.cpu=1 2>/dev/null | grep '^Benchmark' >> "$F" || true
  done
done
echo
for f in b.txt a.txt; do n=$(grep -c '^Benchmark' "$f"); [ "$n" -eq 140 ] || { echo "!!! $f 有 $n 行,期望 140"; exit 1; }; done
echo "  行数守恒 ✓"

echo; echo "════════════ 把下面整块发回给 Mino ════════════"
echo "CPU: $(sysctl -n machdep.cpu.brand_string 2>/dev/null || lscpu | grep -i 'BIOS Model' | sed 's/.*: *//')"
echo "GO:  $(go version)"
for f in b.txt:PR48 a.txt:PR48+EOR3; do
  awk -v tag="${f#*:}" '{split($1,p,"/"); s=p[3]; sub(/-[0-9]+$/,"",s); ns[s]=ns[s]" "$3}
    END{ n=split("0001_block 0004_block 0016_block 0002_kib 0004_kib 0064_kib 1024_kib",o," ")
      for(i=1;i<=n;i++){ s=o[i]; if(!(s in ns))continue
        c=split(ns[s],v," "); for(j=1;j<c;j++)for(k=j+1;k<=c;k++)if(v[k]+0<v[j]+0){t=v[j];v[j]=v[k];v[k]=t}
        printf "%-11s %-11s n=%2d  min=%s med=%s max=%s\n", tag, s, c, v[1], v[int((c+1)/2)], v[c] } }' "${f%%:*}"
done
echo "════════════════════════════════════════════════"
echo; echo "原始数据: $D/b.txt  $D/a.txt"
