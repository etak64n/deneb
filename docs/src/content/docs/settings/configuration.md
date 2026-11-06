---
title: Configuration reference
description: Every environment variable of deneb, its default, and what it changes.
---

deneb reads its settings from environment variables only (internal/config/config.go).
Every setting has a default that works for one container with a volume at /data.

## Example

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
DENEB_WEBHOOK_ALLOW_NETWORKS=
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

## Settings

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
- `DENEB_WEBHOOK_ALLOW_NETWORKS`
  - The private and local networks that webhooks may reach, separated by commas, such as `172.16.0.0/12` or `10.0.0.5`
  - Empty by default, which keeps webhooks to addresses on the internet, so that a key of the API cannot reach the network of deneb through them
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
