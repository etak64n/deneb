# deneb

deneb is a self-hosted video API that runs as one Docker image, as an alternative to hosted services such as Mux.
You upload a video, and deneb encodes it to HLS in several qualities, writes captions with Whisper, and returns signed URLs to play or embed it.
This README explains what deneb does, how it works, how to run it, and how to use its API.

## Contents

- [deneb](#deneb)
  - [In short](#in-short)
  - [How it works](#how-it-works)
    - [The path of a video](#the-path-of-a-video)
    - [Design choices](#design-choices)
  - [Features](#features)
    - [Encoding](#encoding)
    - [Uploads](#uploads)
    - [Captions](#captions)
    - [Playback](#playback)
    - [Webhooks](#webhooks)
    - [Dashboard](#dashboard)
    - [Accounts and security](#accounts-and-security)
    - [Limits](#limits)
    - [What deneb does not do](#what-deneb-does-not-do)
  - [Quick start](#quick-start)
  - [Deploy](#deploy)
    - [Docker Compose behind a proxy](#docker-compose-behind-a-proxy)
    - [S3-compatible storage](#s3-compatible-storage)
    - [Turn on captions](#turn-on-captions)
    - [Upgrade](#upgrade)
    - [Back up](#back-up)
  - [API](#api)
    - [Authentication and errors](#authentication-and-errors)
    - [Upload a video](#upload-a-video)
    - [Upload from a browser](#upload-from-a-browser)
    - [Videos](#videos)
    - [The video object](#the-video-object)
    - [Playback URLs](#playback-urls)
    - [Manage webhooks](#manage-webhooks)
    - [Verify a webhook](#verify-a-webhook)
  - [Configuration reference](#configuration-reference)
    - [Example](#example)
    - [Settings](#settings)
  - [Development](#development)
    - [Build from source](#build-from-source)
    - [Tests](#tests)
    - [Releases](#releases)
  - [Project layout](#project-layout)
  - [License](#license)

## In short

- deneb is one Go program with SQLite, ffmpeg, and whisper.cpp in one image. It needs no database server, no queue, and no cloud service.
- Your app creates videos through a JSON API. Your servers or the browsers of your users then upload the files with tus, a protocol for resumable uploads.
- deneb encodes each video to HLS (HTTP Live Streaming). HLS splits a video into short segments in several qualities, and players switch between the qualities to fit the connection.
- Playback URLs are signed and expire. An embed page plays a video in an iframe on any site.
- Webhooks tell your app when a video is ready, when it failed, and when its captions are ready.
- A dashboard uploads, plays, and deletes videos, and manages the keys of the API and the webhooks.
- The media stay on the volume of deneb, or in an S3-compatible bucket.

A typical use is the videos of your own product, such as lessons, demos, or recordings, which you upload once and play in your site or app.

## How it works

deneb takes a file, turns it into a stream in the background, and serves the stream to players under signed URLs.

### The path of a video

A video goes through six steps.

1. Your server creates a video with `POST /v1/videos`. deneb answers with the video and an upload URL, whose token works for 24 hours.
2. Your server, or the browser of your user, sends the file to the upload URL over tus. The token lets a browser upload without your API key.
3. When the last part arrives, deneb records the upload and queues the video.
4. An encoder runs ffmpeg once over the file and writes every quality, then a poster and a storyboard for previews on the seek bar. It stores them, and the file itself, in the storage.
5. The video is ready, and deneb sends `video.ready` to your webhooks. With captions on, whisper.cpp then transcribes the audio, and deneb sends `video.captions_ready`.
6. Your server reads the video and gets its playback URLs. Players fetch the playlists and segments from deneb with them until they expire.

### Design choices

- deneb is one program in one image (cmd/deneb). SQLite keeps the videos, the jobs, and the keys, and the image has ffmpeg and whisper.cpp. A `docker run` with one volume is a whole deployment.
- Uploads use tus (internal/server/uploads.go). A file of several gigabytes resumes where a lost connection stopped it, rather than starting over.
- Each video has one upload, under the ID of the video. The file stays where it arrived until the encoder has stored it, so a restart of deneb loses no upload.
- One ffmpeg run writes every quality (internal/media/hls.go). deneb decodes the file once rather than once per quality.
- Key frames fall every two seconds, and segments are four seconds long. The qualities line up, so a player can switch at any segment.
- The segments are MPEG-TS, which every HLS player plays: Safari, hls.js, Video.js, and the players of iOS and Android.
- No quality is larger than its source (internal/media/ladder.go). A larger quality would cost bandwidth and show no more detail.
- A playback URL carries its signed token in the path (internal/app/app.go). The relative URLs inside the playlists then carry the token too.
- The playback URLs of a video stay the same for an hour, so players and CDNs can reuse what they fetched.
- With S3 storage, the media go through deneb by default (internal/storage/s3.go). Browsers then need no route to the endpoint of the bucket, and the bucket needs no CORS rules, the rules that let pages of other sites read its files. `DENEB_S3_REDIRECTS=true` sends players to the bucket instead, which spares the bandwidth of deneb.
- Captions run on the CPU of the server (internal/media/captions.go). The audio of your videos goes to no other service.
- The first administrator needs a setup code from the log (internal/app/auth.go). Without it, the first person to reach a new server could claim it.
- Every setting is an environment variable with a default (internal/config/config.go). deneb checks every setting at start and names each one that is wrong.

## Features

This section lists what deneb does, and its limits.

### Encoding

- deneb makes these qualities, named by the short side of the picture (internal/media/ladder.go). It skips each one larger than the source.
  - 1080p: video at 5,000 kb/s, audio at 192 kb/s
  - 720p: video at 2,800 kb/s, audio at 128 kb/s
  - 480p: video at 1,400 kb/s, audio at 128 kb/s
  - 360p: video at 800 kb/s, audio at 96 kb/s
- A source smaller than 360p gets one quality of its own size.
- Vertical videos keep their shape. The 1080p quality of a 9:16 video is 1080 pixels wide and 1920 tall.
- deneb follows the rotation that phones record, and the shape of pixels that are not square, such as those of HDV.
- The video is H.264 with the High profile, and the audio is AAC in stereo at 48 kHz.
- The poster is a still at a tenth of the length, at most 1280 pixels wide.
- The storyboard is one sheet of pictures of 160 by 90 pixels, one every two seconds or more, and at most 100. A WebVTT file, the format of timed text for web players, maps each moment to its picture.
- An encoding that fails for a reason that may pass, such as a bucket that did not answer, runs again after one minute and after ten minutes. A file that is not a video fails at once.

### Uploads

- deneb takes uploads over tus 1.0 (tusd). Any tus client works, such as tus-js-client in browsers and Node.js.
- A file can be up to 50 GB, which `DENEB_MAX_UPLOAD_GB` changes.
- A video takes one file. An upload for a video that has its file gets 409.
- The dashboard uploads three files at a time, in parts of 64 MiB, and resumes them after a lost connection.
- deneb deletes the videos whose file never arrived after seven days, with what arrived of their uploads.

### Captions

- Captions are off by default. `DENEB_CAPTIONS=true` turns them on for every video with audio.
- deneb downloads the whisper.cpp model on first use from Hugging Face (huggingface.co/ggerganov/whisper.cpp), into the models folder of the volume.
- The models are tiny, base, small, medium, large-v3, and large-v3-turbo, and base is the default. A larger model is more accurate and slower.
- deneb detects the spoken language by default. `DENEB_CAPTIONS_LANGUAGE` sets one, such as `en` or `ja`.
- The captions are a WebVTT file. The player of deneb shows them, and the API returns their URL.

### Playback

- A ready video has the URLs of its master playlist, its poster, its storyboard, its captions, and its embed page.
- The URLs expire at the time in `expires_at`. They last at least `DENEB_PLAYBACK_TTL_HOURS`, 24 by default, rounded up to the hour.
- The playback URLs answer any origin (CORS), so players on other sites can fetch the stream.
- Browsers and CDNs may keep segments and pictures for a day, and playlists for a minute. deneb marks errors as not to be kept.
- The embed page is a player on its own page, for an iframe on any site. The dashboard shows the iframe code of each video, in the shape of the video.

### Webhooks

- deneb sends these events: `video.ready`, `video.failed`, and `video.captions_ready`.
- The video in an event has absolute URLs only with `DENEB_PUBLIC_URL`, since no request tells deneb its address when it sends an event.
- Each event is a POST with a JSON body, signed with the secret of the endpoint in the `Deneb-Signature` header.
- An answer from 200 to 299 within 15 seconds counts as delivered. deneb follows no redirects, since a redirect turns a POST into a GET without its body.
- A delivery that fails runs again after 1 minute, 5 minutes, 30 minutes, 2 hours, and 6 hours, six attempts in all.
- deneb keeps the deliveries for 30 days, and the dashboard shows the latest 50 of each endpoint.

### Dashboard

- On the first visit, the dashboard creates the administrator with the setup code from the log.
- The videos page lists every video with its state, and uploads the files that you choose or drop on the page.
- The page of a video plays it, and shows its details, its playback URL, its embed code, and its qualities. It renames and deletes the video.
- The API keys page creates and revokes keys. A new key appears once.
- The webhooks page adds and deletes endpoints, and shows the deliveries of each. A new signing secret appears once.

### Accounts and security

- deneb has one administrator. deneb writes a setup code to its log until the administrator exists, and creating the administrator needs that code.
- A password needs at least 12 characters, and deneb stores it with Argon2id.
- A session lasts 30 days. Dashboard requests that change something need a header that other sites cannot send, which stops cross-site request forgery.
- An address may try to sign in 10 times in 10 minutes. deneb counts an IPv6 address by its /64 network.
- Keys of the API start with `dnb_` and appear once. deneb keeps only their SHA-256, and a key cannot create or delete keys.
- Upload and playback tokens are HMAC-SHA256 with a secret that deneb creates and keeps in its database, or `DENEB_SECRET`.

### Limits

These constants set the limits.

```go
// internal/server/api.go: the pages of GET /v1/videos, and the length of titles
defaultPageSize = 24
maxPageSize     = 100
maxTitleLength  = 300

// internal/app/app.go and internal/app/auth.go: how long upload URLs and sessions last
const UploadTTL = 24 * time.Hour
const SessionTTL = 30 * 24 * time.Hour
const MinPasswordLength = 12

// internal/app/jobs.go: retries, the workers of webhooks, and what deneb keeps
var webhookRetries = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
var mediaRetries = []time.Duration{time.Minute, 10 * time.Minute}
const lightWorkers = 4
const deliveryHistory = 30 * 24 * time.Hour
const staleAfter = 7 * 24 * time.Hour

// internal/media/hls.go and internal/media/images.go: segments and the storyboard
const SegmentSeconds = 4
tileWidth  = 160
tileHeight = 90
maxTiles   = 100

// internal/server/server.go: sign-in attempts per address
newLimiter(10, 10*time.Minute)
```

### What deneb does not do

- deneb does not stream live video.
- deneb has no public playback. Every playback URL expires, so your app asks deneb for the video when it shows it.
- deneb has no DRM, the encryption that only licensed players can undo. Signed URLs keep the stream from people who do not have a URL, not from people who do.
- deneb does not tone-map HDR. An HDR source becomes 8-bit SDR as it is, so its colors look flat.
- deneb offers no MP4 downloads of its encodings.
- deneb encodes on the CPU only.
- deneb has one administrator, with no other accounts or roles.
- deneb counts no views.

## Quick start

These commands run deneb on your machine with Docker.

```sh
# Run deneb on port 8080, with its database and videos in a volume named deneb-data.
docker run -d --name deneb -p 8080:8080 -v deneb-data:/data ghcr.io/etak64n/deneb
# Read the setup code that deneb writes to its log until it has an administrator.
docker logs deneb
```

- `-p 8080:8080` publishes the port of the dashboard and the API.
- `-v deneb-data:/data` keeps the database, the uploads, and the videos when the container is replaced.
- `-e DENEB_CAPTIONS=true`, added before the image, turns on captions.

The log has a line such as this one.

```json
{"time":"2026-11-06T10:58:20Z","level":"INFO","msg":"deneb has no administrator yet; create one in the dashboard with this setup code","setup_code":"K7QM-2XPA"}
```

Open http://localhost:8080, and create the administrator with an email, a password, and the setup code.
Then drop a video on the page. The video plays once its state is Ready.

## Deploy

This section runs deneb on a server under your own domain.

### Docker Compose behind a proxy

compose.yaml runs deneb behind a reverse proxy that serves your domain with TLS, such as Caddy or nginx.

```yaml
# deneb behind a reverse proxy that serves https://video.example.com with TLS and
# forwards to port 8080 of this host. Replace the address with your own.
services:
  deneb:
    # 0.1 follows the fixes of 0.1; latest follows every release.
    image: ghcr.io/etak64n/deneb:0.1
    restart: unless-stopped
    ports:
      # Only the proxy on this host reaches deneb, so the X-Forwarded-For it sets can be trusted.
      - "127.0.0.1:8080:8080"
    environment:
      DENEB_PUBLIC_URL: https://video.example.com
      DENEB_TRUST_PROXY: "true"
      DENEB_CAPTIONS: "true"
    volumes:
      - deneb-data:/data

volumes:
  deneb-data:
```

- `DENEB_PUBLIC_URL` is the address that viewers and API clients use. deneb builds every URL that it returns from it, and marks the session cookie as Secure when it starts with https.
- `DENEB_TRUST_PROXY` makes deneb read the address of each client from the headers of the proxy. The limit of sign-in attempts then counts each client rather than the proxy.
- The port listens on 127.0.0.1 only. A client that reached deneb directly could otherwise forge those headers.

```sh
# Start deneb in the background, and read the setup code from its log.
docker compose up -d
docker compose logs deneb
```

The proxy needs two settings.

1. The proxy accepts request bodies of 64 MiB, the parts in which the dashboard uploads. nginx accepts 1 MB by default, so raise `client_max_body_size`. The proxy of Cloudflare accepts at least 100 MB.
2. The proxy forwards to `127.0.0.1:8080`, with the X-Forwarded-For, X-Forwarded-Proto, and X-Forwarded-Host headers.

`GET /healthz` answers 200 while deneb runs. The image checks it with `deneb healthcheck`.

### S3-compatible storage

deneb can keep the media in a bucket of Amazon S3, Cloudflare R2, or another S3-compatible service, instead of on its volume.
The database stays on the volume either way.

```yaml
# Settings of deneb for a bucket of Cloudflare R2. Add them to the environment of compose.yaml.
DENEB_STORAGE: s3
DENEB_S3_ENDPOINT: https://<account-id>.r2.cloudflarestorage.com
DENEB_S3_REGION: auto
DENEB_S3_BUCKET: videos
DENEB_S3_ACCESS_KEY_ID: <access key id>
DENEB_S3_SECRET_ACCESS_KEY: <secret access key>
```

The values differ by service.

- Cloudflare R2: the endpoint `https://<account-id>.r2.cloudflarestorage.com`, and the region `auto`.
- Amazon S3: no endpoint, the region of the bucket such as `us-east-1`, and `DENEB_S3_PATH_STYLE: "false"`.
- MinIO, RustFS, and other servers that you run: their URL as the endpoint, and the region that they are set to, often `us-east-1`.

deneb lists the bucket at start, and stops with an error when it cannot.
The key needs to list, read, write, and delete objects in the bucket.

With `DENEB_S3_REDIRECTS: "true"`, deneb sends players to signed URLs of the bucket for segments and pictures.

- Browsers then fetch from the endpoint itself, so the endpoint must be reachable from them.
- The CORS rules of the bucket must allow GET and HEAD from any origin.
- Playlists and captions still come from deneb, because the URLs inside them point back to deneb.

A lifecycle rule that aborts incomplete multipart uploads after a day keeps the bucket clean.
A restart of deneb in the middle of storing a large file leaves the parts that it sent.

### Turn on captions

`DENEB_CAPTIONS: "true"` writes captions for every video with audio.

- The first video downloads the model, so its captions take longer.
- Transcription takes CPU time after each encoding. `DENEB_WORKERS` sets how many videos deneb works on at once, and `DENEB_THREADS` how many threads each one uses.
- base is a good start. small and larger models are more accurate, and much slower on a CPU.

### Upgrade

These commands replace the container with the image of the new release.

```sh
# Download the image of the tag in compose.yaml, and recreate the container with it.
docker compose pull
docker compose up -d
```

- deneb updates its database at start (internal/store/migrations).
- The jobs that were running when the old container stopped run again.
- Two deneb cannot use one data folder at once. The second one stops at start, so replace the container rather than run both.

### Back up

- The volume holds the database, deneb.db, with the videos, the keys, the webhooks, and the secret that signs the URLs.
- With local storage, the volume also holds the media, in the media folder. With S3 storage, the bucket holds them.
- Copy deneb.db while deneb is stopped. While deneb runs, SQLite keeps recent changes in deneb.db-wal next to it.

## API

The API takes and returns JSON under `/v1`.

### Authentication and errors

A request carries a key of the API in the Authorization header. The API keys page of the dashboard creates keys.

```sh
# The address of deneb, and a key from its API keys page. The examples below use both.
DENEB=https://video.example.com
KEY=dnb_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

- An error has a status and a body such as `{"error": {"message": "the video does not exist"}}`.
- 400 is input that deneb refuses, 401 a missing or wrong key, and 404 a video or webhook that does not exist.
- 409 is a conflict, such as a second upload for a video. 413 is a body larger than 1 MB.
- 403 answers a key that tries to manage keys. Only the dashboard manages keys.

### Upload a video

An upload creates the video with one call of the API, then sends its file over tus.

```sh
# Create a video. The answer holds the video and where to send its file.
curl -s "$DENEB/v1/videos" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"title": "Product tour"}'
```

```json
{
  "video": {
    "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
    "title": "Product tour",
    "status": "waiting",
    "progress": 0,
    "duration": 0,
    "width": 0,
    "height": 0,
    "source_size": 0,
    "renditions": [],
    "captions": { "status": "none" },
    "created_at": "2026-11-06T10:58:20Z",
    "ready_at": null
  },
  "upload": {
    "url": "https://video.example.com/v1/uploads/",
    "metadata": {
      "video_id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
      "token": "1794049100.m3Qp0ZkS7mWcT9vYbL2xN4aE8rJ1uFgD6iO5tKqVwXs"
    },
    "expires_at": "2026-11-07T10:58:20Z"
  }
}
```

Any tus client sends the file. These commands send it with curl.

```sh
# The video and the token from the answer above.
VIDEO_ID=vid_7wfd5nhpwadrdxgd7laif6x7ry
TOKEN=1794049100.m3Qp0ZkS7mWcT9vYbL2xN4aE8rJ1uFgD6iO5tKqVwXs
# tus takes each value of the metadata in base64.
b64() { printf %s "$1" | base64 | tr -d '\n'; }

# Create the upload. deneb answers 201, with the URL of the upload in the Location header.
curl -si -X POST "$DENEB/v1/uploads/" \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Length: $(wc -c < clip.mp4 | tr -d ' ')" \
  -H "Upload-Metadata: video_id $(b64 "$VIDEO_ID"),token $(b64 "$TOKEN"),filename $(b64 clip.mp4)"

# Send the file from its start. The URL of the upload ends with the ID of the video.
curl -X PATCH "$DENEB/v1/uploads/$VIDEO_ID" \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Offset: 0" \
  -H "Content-Type: application/offset+octet-stream" \
  --data-binary @clip.mp4
```

- `Upload-Length` is the size of the file in bytes. tus needs it before the file.
- `Upload-Metadata` names the video and its token. `filename` is optional, and deneb keeps it with the upload.
- `Upload-Offset: 0` sends the file from its first byte. After a lost connection, a tus client asks deneb for the offset and sends the rest.

The video becomes `queued` when the last byte arrives, then `processing`, then `ready`.

### Upload from a browser

Your server creates the video with its key, and passes the `upload` of the answer to the browser.
The browser then sends the file to deneb itself, and never sees your key.

```js
// In the browser. `created` is the answer of POST /v1/videos that your server made.
import { Upload } from "tus-js-client";

const upload = new Upload(file, {
  endpoint: created.upload.url,
  // video_id and token name the video; the file name is kept with the upload.
  metadata: { ...created.upload.metadata, filename: file.name },
  // Parts of 64 MiB pass proxies that limit the size of a request.
  chunkSize: 64 * 1024 * 1024,
  onProgress: (sent, total) => console.log(`${Math.floor((sent / total) * 100)}%`),
  onSuccess: () => console.log("uploaded"),
});
upload.start();
```

The upload URL accepts requests from any origin, so the page can be on another domain than deneb.

### Videos

- `POST /v1/videos` creates a video. The body may have a `title` of up to 300 characters. The answer is 201 with `video` and `upload`.
- `GET /v1/videos` lists the videos, newest first, as `videos` and `next_cursor`. `limit` takes 1 to 100, 24 by default. `cursor` takes the `next_cursor` of the previous page, which is empty on the last page.
- `GET /v1/videos/{id}` returns one `video`.
- `PATCH /v1/videos/{id}` changes the `title`, and returns the `video`.
- `DELETE /v1/videos/{id}` deletes a video and answers 204. deneb stops the work on the video and removes its files a moment later.

### The video object

```json
{
  "video": {
    "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
    "title": "Product tour",
    "status": "ready",
    "progress": 1,
    "duration": 8,
    "width": 1280,
    "height": 720,
    "source_size": 3096076,
    "renditions": [
      { "name": "720p", "width": 1280, "height": 720, "bitrate": 2928000 },
      { "name": "480p", "width": 852, "height": 480, "bitrate": 1528000 },
      { "name": "360p", "width": 640, "height": 360, "bitrate": 896000 }
    ],
    "captions": { "status": "ready", "language": "en" },
    "created_at": "2026-11-06T10:58:20Z",
    "ready_at": "2026-11-06T10:58:25Z",
    "playback": {
      "hls_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/hls/master.m3u8",
      "poster_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/poster.jpg",
      "storyboard_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/storyboard.vtt",
      "captions_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/captions.vtt",
      "embed_url": "https://video.example.com/embed/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4",
      "expires_at": "2026-11-07T11:00:00Z"
    }
  }
}
```

- `status` is `waiting` for the file, `queued`, `processing`, `ready`, or `failed`. `error` tells why a video failed.
- `progress` goes from 0 to 1 while the video is processing.
- `duration` is in seconds. `width` and `height` are the size as displayed, after any rotation.
- `source_size` is the size of the uploaded file in bytes.
- `renditions` lists the qualities, largest first. `bitrate` is the target of video and audio together, in bits per second.
- `captions.status` is `none`, `queued`, `processing`, `ready`, or `failed`. `captions.language` is the language of ready captions.
- `playback` appears once the video is ready. `captions_url` appears once the captions are ready.

### Playback URLs

- `hls_url` plays in any HLS player, such as hls.js, Video.js, or the player of Safari, iOS, and Android.
- `embed_url` is a page with a player, for an iframe.
- The URLs work until `expires_at`. Ask deneb for the video when your app shows it, and use the URLs of that answer.

```html
<!-- The embed URL of the video in an iframe, in the shape of the video. -->
<iframe
  src="https://video.example.com/embed/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4"
  title="Product tour"
  style="width: 100%; aspect-ratio: 16/9; border: 0"
  allow="autoplay; fullscreen; picture-in-picture"
  allowfullscreen
></iframe>
```

### Manage webhooks

- `POST /v1/webhooks` adds an endpoint. The body has its `url`, which starts with http:// or https://. The answer is 201 with `webhook`, whose `secret` appears only in this answer.
- `GET /v1/webhooks` lists the endpoints, without their secrets.
- `DELETE /v1/webhooks/{id}` deletes an endpoint and answers 204.
- `GET /v1/webhooks/{id}/deliveries` lists the latest 50 deliveries of an endpoint. A delivery has its `event`, `status` (`pending`, `delivered`, or `failed`), `attempts`, `response_status`, `last_error`, `created_at`, and `delivered_at`.

An event is a POST of JSON, with the video as `GET /v1/videos/{id}` returns it.

```json
{
  "type": "video.ready",
  "created_at": "2026-11-06T10:58:25Z",
  "data": {
    "video": { "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry", "title": "Product tour", "status": "ready" }
  }
}
```

The request carries these headers.

- `Deneb-Event`: the type of the event, such as `video.ready`
- `Deneb-Delivery`: the ID of the delivery, the same in each attempt, for skipping events that arrived twice
- `Deneb-Signature`: `t=<unix seconds>,v1=<signature>`, where the signature is the hex HMAC-SHA256 of `<t>.<body>` with the secret of the endpoint
- `User-Agent`: `deneb/<version>`

### Verify a webhook

Check the signature against the body as it arrived, before parsing it.
This function does it in Node.js.

```js
import { createHmac, timingSafeEqual } from "node:crypto";

// Checks a webhook of deneb: the secret of the endpoint, the Deneb-Signature header, and the raw body.
export function verifyDeneb(secret, signature, body) {
  // The header is "t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">".
  const fields = new Map(signature.split(",").map((part) => part.split("=")));
  const t = fields.get("t") ?? "";
  const v1 = fields.get("v1") ?? "";
  const expected = createHmac("sha256", secret).update(`${t}.${body}`).digest("hex");
  // A signature older than five minutes may be a captured request sent again.
  const recent = Math.abs(Date.now() / 1000 - Number(t)) < 300;
  return recent && v1.length === expected.length && timingSafeEqual(Buffer.from(v1), Buffer.from(expected));
}
```

deneb signs each attempt again with the time it sends it, so a retry passes the check of age too.

## Configuration reference

deneb reads its settings from environment variables only (internal/config/config.go).
Every setting has a default that works for one container with a volume at /data.

### Example

This is every setting with its default. Set only the ones you change.

```sh
# Settings of deneb, with their defaults.
DENEB_LISTEN=:8080
DENEB_DATA_DIR=/data
DENEB_PUBLIC_URL=
DENEB_SECRET=
DENEB_TRUST_PROXY=false
DENEB_WORKERS=1
DENEB_THREADS=0
DENEB_MAX_UPLOAD_GB=50
DENEB_PLAYBACK_TTL_HOURS=24
DENEB_CAPTIONS=false
DENEB_CAPTIONS_MODEL=base
DENEB_CAPTIONS_LANGUAGE=auto
DENEB_STORAGE=local
DENEB_S3_ENDPOINT=
DENEB_S3_REGION=auto
DENEB_S3_BUCKET=
DENEB_S3_ACCESS_KEY_ID=
DENEB_S3_SECRET_ACCESS_KEY=
DENEB_S3_PATH_STYLE=true
DENEB_S3_REDIRECTS=false
DENEB_FFMPEG_PATH=ffmpeg
DENEB_FFPROBE_PATH=ffprobe
DENEB_WHISPER_PATH=whisper-cli
```

### Settings

deneb checks every setting at start, and stops with one message that names each wrong setting.
A setting of true or false also takes 1 or 0, yes or no, and on or off.

- `DENEB_LISTEN`
  - The address of the HTTP server
  - `:8080` by default
- `DENEB_DATA_DIR`
  - The folder of the database, the uploads in progress, the work of the encoders, the Whisper models, and the media with local storage
  - `/data` by default, the volume of the image
- `DENEB_PUBLIC_URL`
  - The address that viewers and API clients use, such as `https://video.example.com`
  - deneb builds every URL that it returns from it. Without it, deneb uses the scheme and host of each request.
  - The URLs in webhooks are absolute only with it, since no request tells deneb its address when it sends an event.
- `DENEB_SECRET`
  - The secret that signs upload and playback tokens, at least 32 characters, such as the output of `openssl rand -hex 32`
  - Without it, deneb creates one on its first start and keeps it in the database.
  - A new secret ends every URL that deneb gave out.
- `DENEB_TRUST_PROXY`
  - true makes deneb read the address of each client, and the scheme and host, from X-Forwarded-For, X-Forwarded-Proto, and X-Forwarded-Host
  - Set it only behind a proxy that sets these headers, since a client could forge them otherwise.
  - false by default
- `DENEB_WORKERS`
  - How many videos deneb encodes or transcribes at once
  - 1 by default
- `DENEB_THREADS`
  - How many CPU threads each encoder uses, 0 for every CPU that the container may use
  - 0 by default
- `DENEB_MAX_UPLOAD_GB`
  - The largest upload, in gigabytes of 1,000,000,000 bytes
  - 50 by default
- `DENEB_PLAYBACK_TTL_HOURS`
  - How long playback URLs last at least, in hours
  - 24 by default
- `DENEB_CAPTIONS`
  - true writes captions for every video with audio
  - false by default
- `DENEB_CAPTIONS_MODEL`
  - The whisper.cpp model: tiny, base, small, medium, large-v3, or large-v3-turbo
  - base by default
- `DENEB_CAPTIONS_LANGUAGE`
  - The spoken language as a code such as `en` or `ja`, or `auto` to detect it
  - auto by default
- `DENEB_STORAGE`
  - `local` keeps the media in the media folder of `DENEB_DATA_DIR`, and `s3` in a bucket
  - local by default
- `DENEB_S3_ENDPOINT`
  - The URL of the S3-compatible service, empty for Amazon S3
- `DENEB_S3_REGION`
  - The region of the bucket. Amazon S3 needs a real one, such as `us-east-1`.
  - auto by default, the region of Cloudflare R2
- `DENEB_S3_BUCKET`, `DENEB_S3_ACCESS_KEY_ID`, `DENEB_S3_SECRET_ACCESS_KEY`
  - The bucket and the key, which s3 storage needs
- `DENEB_S3_PATH_STYLE`
  - true puts the bucket in the path of each URL, and false in the host name, as Amazon S3 prefers
  - true by default
- `DENEB_S3_REDIRECTS`
  - true sends players to signed URLs of the bucket for segments and pictures, which spares the bandwidth of deneb
  - The endpoint must be reachable from browsers, and the bucket must allow GET and HEAD from any origin in its CORS rules.
  - false by default
- `DENEB_FFMPEG_PATH`, `DENEB_FFPROBE_PATH`, `DENEB_WHISPER_PATH`
  - The programs that deneb runs
  - The image has all three, and sets `DENEB_WHISPER_PATH` to `/usr/local/bin/whisper-cli`.

## Development

deneb needs Go 1.27 and Node.js 24 to build, and ffmpeg and ffprobe to run. Captions also need whisper.cpp.

### Build from source

The Go program embeds the dashboard, so the dashboard builds first.

```sh
# Install the dependencies of the dashboard and build it into web/dist.
pnpm --dir web install
pnpm --dir web build
# Build the program, with the dashboard inside.
go build -o deneb ./cmd/deneb
# Run it with its data in a folder of this directory.
DENEB_DATA_DIR=./data ./deneb
```

- pnpm is the version that `packageManager` in web/package.json names.
- `pnpm --dir web dev` serves the dashboard with live reload, and forwards the API to a deneb on port 8080.
- A program built without the dashboard runs, and shows a message in its place.

### Tests

- `go test ./...` runs the tests of the Go packages. The tests of internal/media run ffmpeg, and skip where it is not installed.
- `pnpm --dir web typecheck` checks the types of the dashboard.
- `test/e2e.sh <image> local` runs an image end to end: it uploads a video, waits for its encoding and captions, fetches what a player fetches, and checks the signatures of the webhooks. It needs Docker, curl, jq, and Python.
- `test/e2e.sh <image> s3` does the same with a bucket of RustFS, and needs the AWS CLI.
- `test/browser/run.sh <image>` uses the dashboard in Google Chrome, and keeps a screenshot of each step in test/browser/screenshots. It needs Node.js, Google Chrome, espeak-ng, and `pnpm --dir test/browser install` first.

The CI workflow (.github/workflows/ci.yml) runs them all on every push to main and every pull request.

### Releases

A tag such as `v0.2.0` runs .github/workflows/release.yml.

- The workflow builds the image for amd64 and arm64, each on a runner of its own architecture, which is faster than emulation.
- It publishes the image to ghcr.io/etak64n/deneb as 0.2.0, 0.2, and latest.
- A prerelease such as `v0.3.0-rc.1` is published under its own version only.

## Project layout

- cmd/deneb/: the program, with `deneb version` and `deneb healthcheck`
- internal/config/: the settings from environment variables
- internal/server/: HTTP: the API, uploads with tusd, playback, the embed page, and the dashboard
- internal/app/: videos, jobs, accounts, tokens, and webhooks
- internal/store/: the SQLite database and its migrations
- internal/storage/: the volume and S3-compatible buckets
- internal/media/: ffprobe, ffmpeg, and whisper.cpp
- web/: the dashboard and the embed page, in React with Vite and Video.js
- test/: the end-to-end tests of the image
- Dockerfile: the image, with ffmpeg and whisper.cpp
- compose.yaml: an example of deneb behind a proxy

## License

deneb is released under the MIT License (LICENSE).

The image also contains software under its own licenses, among them FFmpeg, under GPL-2.0-or-later as Alpine Linux builds it, and whisper.cpp, under MIT.
deneb runs FFmpeg as a separate program. The Whisper models that deneb downloads are under MIT.
