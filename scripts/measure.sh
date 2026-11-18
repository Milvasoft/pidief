#!/usr/bin/env bash
# Measures image size, idle memory, and memory/latency under concurrent HTML->PDF load.
# Usage: scripts/measure.sh [image] [concurrency] [requests]
set -euo pipefail
IMAGE=${1:-pidief:dev}
C=${2:-8}
N=${3:-40}
NAME=pidief-measure
PORT=3999
docker rm -f $NAME >/dev/null 2>&1 || true
docker run -d --name $NAME -p $PORT:3000 --shm-size 256m -e PIDIEF_CONCURRENCY=4 $IMAGE >/dev/null
trap 'docker rm -f $NAME >/dev/null' EXIT
for _ in $(seq 1 60); do curl -sf localhost:$PORT/health/ready >/dev/null && break; sleep 0.5; done
echo "image size: $(docker image inspect $IMAGE --format '{{.Size}}' | awk '{printf "%.0f MiB", $1/1048576}')"
sleep 2
echo "idle (chromium warm): $(docker stats --no-stream --format '{{.MemUsage}}' $NAME)"
DIR=$(mktemp -d)
{
  echo '<!doctype html><html lang="tr"><head><meta charset="utf-8"><title>Hak Ediş</title><style>body{font-family:Onest} td,th{border:1px solid #999;padding:3px}</style></head><body><h1>Hak Ediş Raporu – Şantiye</h1><table><thead><tr><th>Poz</th><th>Açıklama</th><th>Tutar</th></tr></thead><tbody>'
  for i in $(seq 1 300); do echo "<tr><td>15.$i</td><td>Çelik donatı ğüşiöç İĞÜŞÖÇ $i</td><td>₺1.234,56</td></tr>"; done
  echo '</tbody></table></body></html>'
} > $DIR/index.html
PEAK=0
( while true; do docker stats --no-stream --format '{{.MemUsage}}' $NAME 2>/dev/null | awk '{print $1}' >> $DIR/mem; done ) &
SP=$!
START=$(date +%s.%N)
seq 1 $N | xargs -P $C -I{} curl -s -o /dev/null -w '%{http_code} %{time_total}\n' -F "files=@$DIR/index.html" -F pageNumbers=true localhost:$PORT/v1/html > $DIR/res
END=$(date +%s.%N)
kill $SP 2>/dev/null || true
echo "requests: $N, concurrency: $C, ok: $(grep -c '^200' $DIR/res), wall: $(awk -v a=$START -v b=$END 'BEGIN{printf "%.1f", b-a}') s"
sort -k2 -n $DIR/res | awk '{a[NR]=$2} END {printf "latency p50 %.2fs p95 %.2fs max %.2fs\n", a[int(NR*0.5)], a[int(NR*0.95)], a[NR]}'
echo "peak memory under load: $(sort -h $DIR/mem | tail -1)"
curl -s -o $DIR/out.pdf -F "files=@$DIR/index.html" localhost:$PORT/v1/html
echo "one result: $(curl -s --data-binary @$DIR/out.pdf -H 'Content-Type: application/pdf' localhost:$PORT/v1/info | grep -m1 pageCount | tr -d ' ,') , $(wc -c < $DIR/out.pdf) bytes"
