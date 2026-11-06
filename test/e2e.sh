#!/usr/bin/env bash
# Runs a deneb image the way its users do. It starts a container, creates the
# administrator and a key of the API, uploads a short video over tus, waits for
# the encoding and the captions, fetches what a player fetches, and checks the
# signed webhook that deneb sends when the video is ready.
#
# Usage: test/e2e.sh <image> [local|s3]
# With s3, deneb keeps the media in a bucket of RustFS, an S3-compatible server.
# Needs docker, curl, jq, and python3, and the AWS CLI for s3.
set -euo pipefail

image=${1:?usage: test/e2e.sh <image> [local|s3]}
storage=${2:-local}
work=$(mktemp -d)
base=http://127.0.0.1:8080
container=deneb-e2e
bucket_container=deneb-e2e-bucket
passed=false

cleanup() {
  if [[ $passed != true ]]; then
    echo "--- deneb log ---"
    docker logs "$container" 2>&1 | tail -n 100 || true
    echo "--- webhook receiver log ---"
    cat "$work/receiver.log" 2>/dev/null || true
  fi
  docker rm -f "$container" "$bucket_container" >/dev/null 2>&1 || true
  [[ -n ${receiver_pid:-} ]] && kill "$receiver_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# A receiver of webhooks on the host, which writes each request it gets to a file.
cat > "$work/receiver.py" <<'PY'
import http.server, json, sys

out = sys.argv[1]

class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers["Content-Length"])).decode()
        with open(out, "a") as f:
            f.write(json.dumps({"signature": self.headers["Deneb-Signature"], "event": self.headers["Deneb-Event"], "body": body}) + "\n")
        self.send_response(204)
        self.end_headers()

http.server.HTTPServer(("0.0.0.0", 9876), Handler).serve_forever()
PY
python3 "$work/receiver.py" "$work/hooks.jsonl" >"$work/receiver.log" 2>&1 &
receiver_pid=$!

echo "Make a test video with the ffmpeg of the image"
# With s3, the video is larger than a part of a multipart upload, 16 MiB, so that its
# file goes to the bucket in parts.
rate=()
[[ $storage == s3 ]] && rate=(-b:v 24M -minrate 24M -maxrate 24M -bufsize 24M -x264-params nal-hrd=cbr)
docker run --rm --user "$(id -u):$(id -g)" -v "$work:/out" --entrypoint ffmpeg "$image" \
  -hide_banner -loglevel error -f lavfi -i testsrc2=size=1280x720:rate=30:duration=8 \
  -f lavfi -i sine=frequency=440:duration=8 -c:v libx264 "${rate[@]}" -pix_fmt yuv420p -c:a aac /out/clip.mp4
if [[ $storage == s3 ]]; then
  (( $(stat -c %s "$work/clip.mp4") > 16 * 1024 * 1024 )) || fail "the clip is not larger than a part"
fi

case $storage in
  local)
    run=(-p 8080:8080)
    ;;
  s3)
    echo "Start RustFS with a bucket"
    # deneb shares the network of the host here, so that the bucket has the same
    # address for deneb and for the redirects that curl follows.
    export AWS_ACCESS_KEY_ID=rustfsadmin AWS_SECRET_ACCESS_KEY=rustfsadmin AWS_DEFAULT_REGION=us-east-1
    s3=(aws --endpoint-url http://127.0.0.1:9000 s3api)
    docker run -d --name "$bucket_container" -p 9000:9000 rustfs/rustfs:1.0.4 >/dev/null
    for _ in $(seq 60); do
      "${s3[@]}" create-bucket --bucket deneb >/dev/null 2>&1 && break
      sleep 1
    done
    "${s3[@]}" head-bucket --bucket deneb || fail "RustFS has no bucket"
    run=(--network host -e DENEB_STORAGE=s3 -e DENEB_S3_ENDPOINT=http://127.0.0.1:9000 -e DENEB_S3_BUCKET=deneb
      -e DENEB_S3_REGION=us-east-1 -e DENEB_S3_ACCESS_KEY_ID=rustfsadmin -e DENEB_S3_SECRET_ACCESS_KEY=rustfsadmin)
    ;;
  *)
    fail "unknown storage $storage"
    ;;
esac

echo "Start deneb with $storage storage"
docker run -d --name "$container" "${run[@]}" --add-host=host.docker.internal:host-gateway \
  -e DENEB_CAPTIONS=true -e DENEB_CAPTIONS_MODEL=tiny -e DENEB_CAPTIONS_LANGUAGE=en \
  "$image" >/dev/null
for _ in $(seq 60); do
  curl -fsS "$base/healthz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$base/healthz" >/dev/null || fail "deneb did not start"

echo "Create the administrator with the setup code from the log, and a key of the API"
code=""
for _ in $(seq 20); do
  code=$(docker logs "$container" 2>&1 | jq -R -r 'fromjson? | select(.setup_code != null) | .setup_code' | tail -n 1)
  [[ -n $code ]] && break
  sleep 0.5
done
[[ -n $code ]] || fail "deneb wrote no setup code to its log"
status=$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Deneb-Client: e2e' -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"correct horse battery","setup_code":"AAAA-AAAA"}' "$base/auth/setup")
[[ $status == 403 ]] || fail "a wrong setup code answered $status"
curl -fsS -c "$work/cookies" -H 'X-Deneb-Client: e2e' -H 'Content-Type: application/json' \
  -d "{\"email\":\"admin@example.com\",\"password\":\"correct horse battery\",\"setup_code\":\"$code\"}" "$base/auth/setup" >/dev/null
key=$(curl -fsS -b "$work/cookies" -H 'X-Deneb-Client: e2e' -H 'Content-Type: application/json' \
  -d '{"name":"e2e"}' "$base/v1/api-keys" | jq -r .api_key.secret)
[[ $key == dnb_* ]] || fail "unexpected key $key"
api=(-fsS -H "Authorization: Bearer $key" -H 'Content-Type: application/json')

secret=$(curl "${api[@]}" -d '{"url":"http://host.docker.internal:9876/hook"}' "$base/v1/webhooks" | jq -r .webhook.secret)
[[ $secret == whsec_* ]] || fail "unexpected webhook secret $secret"

echo "Create a video and upload its file over tus"
created=$(curl "${api[@]}" -d '{"title":"E2E clip"}' "$base/v1/videos")
id=$(jq -r .video.id <<<"$created")
token=$(jq -r .upload.metadata.token <<<"$created")
upload_url=$(jq -r .upload.url <<<"$created")
size=$(stat -c %s "$work/clip.mp4")
b64() { printf %s "$1" | base64 -w0; }
metadata="video_id $(b64 "$id"),token $(b64 "$token"),filename $(b64 clip.mp4)"

status=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Tus-Resumable: 1.0.0' -H "Upload-Length: $size" \
  -H "Upload-Metadata: video_id $(b64 "$id"),token $(b64 "1.forged")" "$upload_url")
[[ $status == 401 ]] || fail "an upload with a forged token answered $status"

location=$(curl -fsS -D - -o /dev/null -X POST -H 'Tus-Resumable: 1.0.0' -H "Upload-Length: $size" \
  -H "Upload-Metadata: $metadata" "$upload_url" | tr -d '\r' | awk 'tolower($1) == "location:" { print $2 }')
[[ $location == "$base/v1/uploads/"* ]] || fail "unexpected upload location $location"
curl -fsS -X PATCH -H 'Tus-Resumable: 1.0.0' -H 'Upload-Offset: 0' -H 'Content-Type: application/offset+octet-stream' \
  --data-binary "@$work/clip.mp4" "$location" >/dev/null

echo "Wait for the encoding and the captions"
for _ in $(seq 150); do
  video=$(curl "${api[@]}" "$base/v1/videos/$id")
  status=$(jq -r .video.status <<<"$video")
  captions=$(jq -r .video.captions.status <<<"$video")
  [[ $status == failed ]] && fail "the encoding failed: $(jq -r .video.error <<<"$video")"
  [[ $status == ready && ($captions == ready || $captions == failed) ]] && break
  sleep 2
done
echo "$video" | jq .video
[[ $status == ready ]] || fail "the video is $status"
[[ $captions == ready ]] || fail "the captions are $captions"
[[ $(jq '.video.renditions | length' <<<"$video") == 3 ]] || fail "a 720p video should have 3 renditions"
[[ $(jq -r .video.captions.language <<<"$video") == en ]] || fail "the captions are not in English"

echo "Fetch what a player fetches, without the key"
hls=$(jq -r .video.playback.hls_url <<<"$video")
root=${hls%hls/master.m3u8}
master=$(curl -fsS "$hls")
grep -q '#EXT-X-STREAM-INF' <<<"$master" || fail "the master playlist lists no qualities"
# The first line that is neither a tag nor empty names a playlist, and in a playlist, a segment.
variant=$(grep -m1 -v -e '^#' -e '^$' <<<"$master")
playlist=$(curl -fsS "${root}hls/$variant")
grep -q '#EXT-X-ENDLIST' <<<"$playlist" || fail "the playlist $variant is not complete"
segment=$(grep -m1 -v -e '^#' -e '^$' <<<"$playlist")
curl -fsSL -o "$work/segment.ts" "${root}hls/$segment"
[[ $(stat -c %s "$work/segment.ts") -gt 1000 ]] || fail "the segment $segment is empty"
[[ $(curl -fsSL -o /dev/null -w '%{content_type}' "$(jq -r .video.playback.poster_url <<<"$video")") == image/jpeg ]] || fail "the poster is not a JPEG"
curl -fsS "$(jq -r .video.playback.storyboard_url <<<"$video")" | grep -q 'storyboard.jpg#xywh=' || fail "the storyboard has no cues"
curl -fsS "$(jq -r .video.playback.captions_url <<<"$video")" | head -1 | grep -q WEBVTT || fail "the captions are not WebVTT"
curl -fsS "$(jq -r .video.playback.embed_url <<<"$video")" | grep -q '<div id="root">' || fail "the embed page did not load"
[[ $(curl -s -o /dev/null -w '%{http_code}' "$base/play/$id/1.forged/hls/master.m3u8") == 403 ]] \
  || fail "a forged playback token was accepted"

echo "Check the signed webhooks"
for _ in $(seq 30); do
  [[ -f $work/hooks.jsonl ]] && grep -q video.captions_ready "$work/hooks.jsonl" && break
  sleep 1
done
python3 - "$work/hooks.jsonl" "$secret" "$id" <<'PY'
import hashlib, hmac, json, sys

path, secret, video_id = sys.argv[1:]
events = []
for line in open(path):
    hook = json.loads(line)
    fields = dict(part.split("=", 1) for part in hook["signature"].split(","))
    expected = hmac.new(secret.encode(), (fields["t"] + "." + hook["body"]).encode(), hashlib.sha256).hexdigest()
    assert hmac.compare_digest(expected, fields["v1"]), "a webhook has a wrong signature"
    body = json.loads(hook["body"])
    assert body["type"] == hook["event"], body
    assert body["data"]["video"]["id"] == video_id, body
    events.append(body["type"])
assert events == ["video.ready", "video.captions_ready"], events
print("webhooks:", events)
PY

echo "Delete the video"
curl "${api[@]}" -X DELETE "$base/v1/videos/$id" >/dev/null
# A job removes the files a moment after.
for _ in $(seq 20); do
  [[ $(curl -s -o /dev/null -w '%{http_code}' "$hls") == 404 ]] && break
  sleep 0.5
done
[[ $(curl -s -o /dev/null -w '%{http_code}' "$hls") == 404 ]] || fail "the stream of a deleted video still plays"
if [[ $storage == s3 ]]; then
  left=$("${s3[@]}" list-objects-v2 --bucket deneb --prefix "videos/$id/" --query 'length(Contents || `[]`)')
  [[ $left == 0 ]] || fail "the bucket keeps $left files of the deleted video"
fi

passed=true
echo "PASS"
