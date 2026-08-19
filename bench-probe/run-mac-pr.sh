#!/bin/bash
# PR「avoid a stack copy when building the compress state」的 M3 Pro 表。
# 三臂 × 两种构建;base/pr 都直接从远端 clone 已发布的 commit,不在本地打补丁。
#
#   base    zeebo/blake3 @ d130e92        (upstream master)
#   pr      BobDu/go-blake3 @ cd455bc     (PR 的那个 commit,原样)
#   padded  base + 布局扰动               (对照臂 = 布局地板;相对 base 应为 ~)
#
# 用法:  bash run-mac-pr.sh              # n=20, benchtime=1s, 约 16 分钟
#         ROUNDS=8 BT=500ms bash run-mac-pr.sh    # 想先快看一眼
set -euo pipefail

ROUNDS="${ROUNDS:-20}"; BT="${BT:-1s}"
WORK="${WORK:-$(mktemp -d /tmp/b3pr.XXXXXX)}"
ARMS="base pr padded"
F=internal/alg/compress/compress_pure/compress.go

echo "════════════════════════════════════════════"
sysctl -n machdep.cpu.brand_string 2>/dev/null || echo "(未知 CPU)"
echo "物理核 $(sysctl -n hw.physicalcpu 2>/dev/null || echo ?) / 逻辑核 $(sysctl -n hw.logicalcpu 2>/dev/null || echo ?)"
command -v go >/dev/null || { echo "!!! 没找到 go"; exit 1; }
go version
echo "工作目录 $WORK   轮数 $ROUNDS   benchtime $BT"
echo "════════════════════════════════════════════"
echo "⚠️  插电、关掉浏览器/编译/Docker,让这个终端保持前台(前台进程才拿 P 核)"
echo "⚠️  benchtime 默认 1s(是 AWS 那轮的两倍)—— Mac 上小尺寸行离散度容易失控,不要下调"
echo

BS="$(go env GOPATH)/bin/benchstat"
if [ ! -x "$BS" ]; then
  echo "==> 装 benchstat(可能触发 Go 工具链下载,有输出才算正常)"
  go install golang.org/x/perf/cmd/benchstat@latest
  [ -x "$BS" ] || { echo "!!! benchstat 装失败,期望 $BS"; exit 1; }
fi

cd "$WORK"
echo "==> clone upstream @ d130e92"
git clone -q https://github.com/zeebo/blake3.git base && ( cd base && git checkout -q d130e92 )
echo "==> clone PR commit @ cd455bc"
git clone -q https://github.com/BobDu/go-blake3.git pr && ( cd pr && git checkout -q cd455bc )
cp -R base padded
python3 - <<'PYEOF'
F='padded/internal/alg/compress/compress_pure/compress.go'
s=open(F).read()
PAD=''.join('//go:noinline\nfunc pad%d(x uint32) uint32 { return x*%d + %d }\n\n'%(i,2*i+3,i+7) for i in range(9)) \
    +'var PadSink uint32\n\nfunc init() { PadSink = %s }\n\n'%('+'.join('pad%d(PadSink)'%i for i in range(9)))
assert s.count('func Compress(')==1
open(F,'w').write(s.replace('func Compress(', PAD+'func Compress(',1))
PYEOF
gofmt -w "padded/$F"

# ── 闸门 1:pr 与 base 必须真有差异(否则 commit 取错)
if diff -q "base/$F" "pr/$F" >/dev/null; then echo "!!! pr 与 base 无差异,commit 取错"; exit 1; fi
echo "==> 改动行数 $(diff "base/$F" "pr/$F" | grep -c '^[<>]')"

mkdir -p out
# ── 闸门 2:三臂 × 两种 tag 都要 gofmt 干净 + 全量测试通过
for a in $ARMS; do
  [ -z "$( cd "$a" && gofmt -l . )" ] || { echo "!!! $a gofmt 不干净"; exit 1; }
  for tg in "" "-tags=purego"; do
    ( cd "$a" && go test -count=1 $tg . >/dev/null ) || { echo "!!! $a $tg 测试失败"; exit 1; }
    sfx=$( [ -z "$tg" ] && echo def || echo pure )
    ( cd "$a" && go test -c $tg -o "$WORK/out/$a.$sfx.test" . )
  done
  echo "  $a: gofmt 干净、default+purego 测试通过"
done

# ── 闸门 3:padded 必须真挪了位、且语义未变(只允许重定位差异)
python3 - <<'PYEOF'
import subprocess,sys,re
def info(b):
    nm=subprocess.run(['go','tool','nm',b],capture_output=True,text=True).stdout
    a=[l.split()[0] for l in nm.split('\n') if l.rstrip().endswith('compress_pure.Compress')]
    od=subprocess.run(['go','tool','objdump','-s','compress_pure\\.Compress$',b],capture_output=True,text=True).stdout
    ins=[]
    for l in od.split('\n'):
        if 'PCDATA' in l or 'FUNCDATA' in l or l.startswith('TEXT') or not l.strip(): continue
        fs=[x.strip() for x in l.split('\t') if x.strip()]
        if len(fs)>=4: ins.append(fs[3])
    return (a[0] if a else None), ins
bad=0; hard=0
for sfx in ('def','pure'):
    # 脚本已 cd 到 WORK,直接用相对路径,不依赖环境变量
    ab,ib=info('out/base.%s.test'%sfx)
    ap,ip=info('out/padded.%s.test'%sfx)
    ar,ir=info('out/pr.%s.test'%sfx)
    ok_addr=bool(ab and ap and ab!=ap); ok_cnt=len(ib)==len(ip) and len(ib)>0
    ok_mnem=[x.split()[0] for x in ib]==[y.split()[0] for y in ip]
    diffs=[(x,y) for x,y in zip(ib,ip) if x!=y]
    # 函数挪位会改变跳转目标与 PC 相对寻址的数值编码(amd64 印绝对地址;arm64 是
    # ADRP+ADD 两条一组,跨页时页号变一页、页内偏移补回填充量)。不按助记符白名单
    # 放行 —— 那要求预知每个平台的寻址惯用法,已两次漏判。改为规范化:把所有数字
    # 字面量掩掉后必须逐条相同,即「唯一的差别是数值」= 纯重定位。寄存器分配变化、
    # 指令替换都会在这一步暴露。
    mask=lambda t: re.sub(r'(?<![A-Za-z0-9])(0x[0-9a-f]+|\d+)', 'N', t)
    reloc=[mask(x) for x in ib]==[mask(y) for y in ip]
    ok_pr=ir!=ib
    print("  [%s] base@0x%s(%d) padded@0x%s(%d) pr(%d)  挪位:%s 条数:%s 助记符:%s 差异仅数值(重定位):%s(%d 行) pr有改动:%s"
          %(sfx,ab,len(ib),ap,len(ip),len(ir),
            "OK" if ok_addr else "FAIL","OK" if ok_cnt else "FAIL","OK" if ok_mnem else "FAIL",
            "OK" if reloc else "FAIL",len(diffs),"OK" if ok_pr else "FAIL"))
    for x,y in diffs[:4]: print("        base: %-34s padded: %s"%(x,y))
    if not(ok_addr and ok_cnt and ok_mnem and reloc): bad=1
    if not ok_pr: hard=1
if bad: print("  ⚠️  padded 对照臂校验未通过 —— 只影响能不能引用布局地板,base/pr 的数照跑")
sys.exit(hard)
PYEOF
echo "  (以上仅校验 padded 辅助臂;base/pr 的测量不依赖它)"

S='BLAKE3/Entire/(0001_block|0004_block|0016_block|0002_kib|0004_kib|0064_kib|1024_kib)$'
NARM=$( echo $ARMS | wc -w | tr -d ' ' )
for a in $ARMS; do for c in def pure; do : > "out/$a.$c.e"; : > "out/$a.$c.c"; done; done
echo
for r in $( seq 0 $((ROUNDS-1)) ); do
  printf '\r  第 %2d/%d 轮 ' $((r+1)) "$ROUNDS"
  ORD=$( echo $ARMS | awk -v r=$r -v n=$NARM '{k=r%n; for(i=1;i<=n;i++) printf "%s ", $(((k+i-1)%n)+1)}' )
  for a in $ORD; do for c in def pure; do
    GOMAXPROCS=1 "out/$a.$c.test" -test.run=XXX -test.bench="$S"                -test.benchtime=$BT -test.cpu=1 2>/dev/null | grep '^Benchmark' >> "out/$a.$c.e"
    GOMAXPROCS=1 "out/$a.$c.test" -test.run=XXX -test.bench='BenchmarkCompress$' -test.benchtime=$BT -test.cpu=1 2>/dev/null | grep '^Benchmark' >> "out/$a.$c.c"
  done; done
done
echo; echo
# ── 闸门 4:行数守恒(期望值先算)
EE=$(( ROUNDS * 7 ))
for a in $ARMS; do for c in def pure; do
  n=$( grep -c '^Benchmark' "out/$a.$c.e" || true ); [ "$n" -eq "$EE" ]     || { echo "!!! $a.$c.e $n 行,期望 $EE"; exit 1; }
  n=$( grep -c '^Benchmark' "out/$a.$c.c" || true ); [ "$n" -eq "$ROUNDS" ] || { echo "!!! $a.$c.c $n 行,期望 $ROUNDS"; exit 1; }
done; done
echo "  行数守恒 ✓"
for c in def pure; do
  echo "@@@@@@@@ ${c}_kernel"; "$BS" master="out/base.$c.c" pr="out/pr.$c.c" padded="out/padded.$c.c" 2>&1 | sed -n '/sec\/op/,/^$/p'
  echo "@@@@@@@@ ${c}_entire"; "$BS" master="out/base.$c.e" pr="out/pr.$c.e" padded="out/padded.$c.e" 2>&1 | sed -n '/B\/s/,/^$/p'
done
echo
echo "⚠️  出表前必查:任何一行 ± 超过 5% 就要重跑那一行(离散度失控时 ~ 表示没测准,不是无差异)"
echo "原始数据在 $WORK/out/"
