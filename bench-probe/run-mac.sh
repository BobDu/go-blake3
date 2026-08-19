#!/bin/bash
# out 职责分离 vs 逐元素写:五臂对照。macOS 版(无 taskset / 无 lscpu / 不依赖 python3)。
#
#   base        upstream d130e92 原样(*out = [16]uint32{...} 复合字面量)
#   elem        逐元素直写 *out,rcompress 签名不动,out 仍是 in/out
#   elem_split  逐元素写局部 s,rcompress(&s, block, out),out 只做输出
#   fused       不建状态数组,状态从头就在寄存器里,out 只做输出(零额外缓冲)
#   padded      base + 布局扰动 —— 对照臂,它相对 base 应该是 "~"
#
# 用法:  bash run-mac.sh            # 默认 20 轮
#         ROUNDS=8 bash run-mac.sh  # 想快点
set -euo pipefail

ROUNDS="${ROUNDS:-20}"
BT="${BT:-500ms}"
HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="${WORK:-$(mktemp -d /tmp/b3split.XXXXXX)}"
ARMS="base elem elem_split fused padded"
F=internal/alg/compress/compress_pure/compress.go

echo "════════════════════════════════════════════"
sysctl -n machdep.cpu.brand_string 2>/dev/null || echo "(未知 CPU)"
echo "物理核 $(sysctl -n hw.physicalcpu 2>/dev/null || echo ?) / 逻辑核 $(sysctl -n hw.logicalcpu 2>/dev/null || echo ?)"
command -v go >/dev/null || { echo "!!! 没找到 go,先装 Go(brew install go)"; exit 1; }
go version
echo "工作目录 $WORK   轮数 $ROUNDS   benchtime $BT"
echo "════════════════════════════════════════════"
echo "⚠️  请插电、关掉浏览器/编译等后台负载,并让这个终端保持前台(前台进程才拿 P 核)"
echo

# ---- benchstat ----
BS="$(go env GOPATH)/bin/benchstat"
if [ ! -x "$BS" ]; then
  echo "==> 没有 benchstat,现在安装(可能触发 Go 工具链下载,请耐心等,有输出才算正常)"
  go install golang.org/x/perf/cmd/benchstat@latest
  [ -x "$BS" ] || { echo "!!! benchstat 安装失败,期望路径 $BS"; exit 1; }
fi
echo "==> benchstat: $BS"

# ---- 取 upstream 基座 ----
cd "$WORK"
echo "==> clone zeebo/blake3 @ d130e92"
git clone -q https://github.com/zeebo/blake3.git src
( cd src && git checkout -q d130e92 )

# 闸门 0:基座里的那个文件必须与 variants/base.go 一字不差,否则基座对不上,后面全白测
if ! diff -q "src/$F" "$HERE/variants/base.go" >/dev/null; then
  echo "!!! 基座的 $F 与 variants/base.go 不一致 —— 基座版本对不上,中止"; exit 1
fi
echo "==> 基座校验 ✓(variants/base.go 就是 upstream 原样)"

mkdir -p out
for a in $ARMS; do
  cp -R src "arm_$a"
  cp "$HERE/variants/$a.go" "arm_$a/$F"
done

# ---- 闸门 1:每臂都要能编译 + 全量测试通过 + gofmt 干净 ----
echo
for a in $ARMS; do
  printf '  %-11s gofmt=%s  ' "$a" "$( cd "arm_$a" && gofmt -l . | wc -l | tr -d ' ' )"
  ( cd "arm_$a" && go test -count=1 . 2>&1 | tail -1 )
  ( cd "arm_$a" && go test -c -o "$WORK/out/$a.test" . )
done

# ---- 闸门 2:各臂的 Compress 必须真的不同;padded 的 rcompress 地址必须真的挪了 ----
echo
for a in $ARMS; do
  n=$( go tool objdump -s 'compress_pure\.Compress$' "out/$a.test" 2>/dev/null | grep -vE 'PCDATA|FUNCDATA' | tail -n +2 | grep -c . || true )
  addr=$( go tool nm "out/$a.test" | grep 'compress_pure\.rcompress$' | awk '{print $1}' )
  printf '  静态 %-11s Compress %4s 条指令   rcompress @ 0x%s\n' "$a" "$n" "$addr"
done
A_BASE=$( go tool nm out/base.test   | grep 'compress_pure\.rcompress$' | awk '{print $1}' )
A_PAD=$(  go tool nm out/padded.test | grep 'compress_pure\.rcompress$' | awk '{print $1}' )
[ "$A_BASE" != "$A_PAD" ] || { echo "!!! padded 臂地址没变,布局对照无效,中止"; exit 1; }
echo "  布局对照有效(地址不同)✓"

# ---- 跑分:轮内轮转臂序,抵消热漂移 ----
S='BLAKE3/Entire/(0001_block|0004_block|0016_block|0002_kib|0004_kib|0064_kib|1024_kib)$'
NARM=$( echo $ARMS | wc -w | tr -d ' ' )
for a in $ARMS; do : > "out/$a.e.txt"; : > "out/$a.c.txt"; done
echo
for r in $( seq 0 $((ROUNDS-1)) ); do
  printf '\r  第 %2d/%d 轮 ' $((r+1)) "$ROUNDS"
  ORD=$( echo $ARMS | awk -v r=$r -v n=$NARM '{k=r%n; for(i=1;i<=n;i++) printf "%s ", $(((k+i-1)%n)+1)}' )
  for a in $ORD; do
    GOMAXPROCS=1 "out/$a.test" -test.run=XXX -test.bench="$S"                -test.benchtime=$BT -test.cpu=1 2>/dev/null | grep '^Benchmark' >> "out/$a.e.txt"
    GOMAXPROCS=1 "out/$a.test" -test.run=XXX -test.bench='BenchmarkCompress$' -test.benchtime=$BT -test.cpu=1 2>/dev/null | grep '^Benchmark' >> "out/$a.c.txt"
  done
done
echo; echo

# ---- 闸门 3:行数守恒(期望值先算出来再比,不是"看着像就行")----
EXP_E=$(( ROUNDS * 7 )); EXP_C=$ROUNDS
for a in $ARMS; do
  ne=$( grep -c '^Benchmark' "out/$a.e.txt" || true ); nc=$( grep -c '^Benchmark' "out/$a.c.txt" || true )
  [ "$ne" -eq "$EXP_E" ] || { echo "!!! $a.e 有 $ne 行,期望 $EXP_E —— 数据不全,中止"; exit 1; }
  [ "$nc" -eq "$EXP_C" ] || { echo "!!! $a.c 有 $nc 行,期望 $EXP_C —— 数据不全,中止"; exit 1; }
done
echo "  行数守恒 ✓ (Entire ${EXP_E}/臂,内核 ${EXP_C}/臂)"

ARGS=""; for a in $ARMS; do ARGS="$ARGS $a=out/$a.c.txt"; done
echo; echo "@@@@@@ BenchmarkCompress(内核,最敏感)"
"$BS" $ARGS 2>&1 | sed -n '/sec\/op/,/^$/p'
ARGS=""; for a in $ARMS; do ARGS="$ARGS $a=out/$a.e.txt"; done
echo "@@@@@@ Entire(默认构建,arm64 走 NEON/pure 混合)"
"$BS" $ARGS 2>&1 | sed -n '/B\/s/,/^$/p'
echo
echo "原始数据留在 $WORK/out/ ,不用了可以自己删"
