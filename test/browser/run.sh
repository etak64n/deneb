#!/usr/bin/env bash
# Runs the dashboard of a deneb image in Google Chrome. It makes a few clips, one of
# them spoken, starts a container and a receiver of webhooks, and runs a script of
# this folder against it: dashboard.mjs, the default, uses the dashboard the way an
# administrator does and keeps a screenshot of each step; demo.mjs records a short
# demo. What the script writes goes to test/browser/out/<script>.
#
# Usage: test/browser/run.sh <image> [dashboard.mjs|demo.mjs]
# Needs docker, jq, python3, espeak-ng, Node.js, and Google Chrome, and
# `pnpm --dir test/browser install` before.
set -euo pipefail

image=${1:?usage: test/browser/run.sh <image> [dashboard.mjs|demo.mjs]}
script=${2:-dashboard.mjs}
here=$(cd "$(dirname "$0")" && pwd)
out=$here/out/${script%.mjs}
work=$(mktemp -d)
container=deneb-browser
passed=false

cleanup() {
  if [[ $passed != true ]]; then
    echo "--- deneb log ---"
    docker logs "$container" 2>&1 | tail -n 100 || true
  fi
  docker rm -f "$container" >/dev/null 2>&1 || true
  [[ -n ${receiver_pid:-} ]] && kill "$receiver_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

# A receiver of webhooks that accepts every request.
cat > "$work/receiver.py" <<'PY'
import http.server

class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers["Content-Length"]))
        self.send_response(204)
        self.end_headers()

http.server.HTTPServer(("0.0.0.0", 9876), Handler).serve_forever()
PY
python3 "$work/receiver.py" >/dev/null 2>&1 &
receiver_pid=$!

echo "Make the clips"
mkdir "$work/clips"
espeak-ng -v en-us -s 135 -w "$work/speech.wav" \
  "Welcome. Upload a video, and it is encoded, captioned, and ready to stream in minutes."
ffmpeg() {
  docker run --rm --user "$(id -u):$(id -g)" -v "$work:/work" -w /work --entrypoint ffmpeg "$image" -hide_banner -loglevel error "$@"
}
# Large cells that change six times a second, which a GIF of the demo stores cheaply.
ffmpeg -f lavfi -i "life=s=160x90:r=6:mold=8:ratio=0.12:life_color=#9cc3ff:death_color=#1a2c44:mold_color=#2c4a70,scale=1280:720:flags=neighbor,fps=30" \
  -i speech.wav -filter_complex "[1:a]apad[a]" -map 0:v -map "[a]" -t 10 \
  -c:v libx264 -pix_fmt yuv420p -c:a aac "clips/Welcome to deneb.mp4"
ffmpeg -f lavfi -i "testsrc2=s=1920x1080:r=30:d=12" -f lavfi -i "sine=f=330:d=12" \
  -c:v libx264 -pix_fmt yuv420p -c:a aac "clips/Product tour.mp4"
ffmpeg -f lavfi -i "cellauto=s=720x1280:r=30:rule=110" -f lavfi -i "sine=f=440" -t 8 \
  -vf "format=yuv420p" -c:v libx264 -c:a aac "clips/Vertical teaser.mp4"
ffmpeg -f lavfi -i "gradients=s=1280x720:r=30:d=8:speed=0.03:nb_colors=3:c0=0x1a2c44:c1=0x3a6ea5:c2=0x9cc3ff" \
  -c:v libx264 -pix_fmt yuv420p "clips/Silent loop.mp4"

echo "Start deneb"
# CAPTIONS_MODEL picks another Whisper model than the small and fast tiny, such as base for the demo.
docker run -d --name "$container" -p 8080:8080 --add-host=host.docker.internal:host-gateway \
  -e DENEB_CAPTIONS=true -e DENEB_CAPTIONS_MODEL="${CAPTIONS_MODEL:-tiny}" "$image" >/dev/null
for _ in $(seq 60); do
  curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1 && break
  sleep 1
done
# The administrator is created with the setup code that deneb writes to its log.
for _ in $(seq 20); do
  code=$(docker logs "$container" 2>&1 | jq -R -r 'fromjson? | select(.setup_code != null) | .setup_code' | tail -n 1)
  [[ -n $code ]] && break
  sleep 0.5
done

rm -rf "$out"
DENEB_SETUP_CODE=$code node "$here/$script" http://127.0.0.1:8080 "$work/clips" "$out"
passed=true
