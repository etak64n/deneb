---
title: Development
description: Build deneb from source, run its tests, work on this site, release deneb, and find your way in its code.
---

deneb needs Go 1.27 and Node.js 24 to build, and ffmpeg and ffprobe to run. Captions also need whisper.cpp.
This page builds deneb from source, runs its tests, and describes this site, the releases, and the layout of the code.

## Build from source

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

## Tests

- `go test ./...` runs the tests of the Go packages. The tests of internal/media run ffmpeg, and skip where it is not installed.
- `pnpm --dir web typecheck` checks the types of the dashboard.
- `test/e2e.sh <image> local` runs an image end to end: it uploads a video, waits for its encoding and captions, fetches what a player fetches, and checks the signatures of the webhooks. It needs Docker, curl, jq, and Python.
- `test/e2e.sh <image> s3` does the same with a bucket of RustFS, and needs the AWS CLI.
- `test/browser/run.sh <image>` uses the dashboard in Google Chrome, and keeps a screenshot of each step in test/browser/out/dashboard. It needs Node.js, Google Chrome, espeak-ng, and `pnpm --dir test/browser install` first.
- `test/browser/run.sh <image> demo.mjs` records a short demo of the API, and `test/browser/gif.sh <image> test/browser/out/demo` turns it into a GIF and an MP4. The Demo workflow (.github/workflows/demo.yml) does both when you start it.

The CI workflow (.github/workflows/ci.yml) runs them all on every push to main and every pull request, and the end-to-end tests on amd64 and arm64.

## This site

This site is in docs/, built with Starlight, the documentation theme of Astro.

```sh
# Install the dependencies of the site, and serve it with live reload.
pnpm --dir docs install
pnpm --dir docs exec astro dev
```

- The pages are the Markdown files in docs/src/content/docs. docs/astro.config.mjs lists them in the sidebar.
- The Docs workflow (.github/workflows/docs.yml) builds the site on every push to main, so a broken page fails before a release.
- The same workflow publishes the site to GitHub Pages for every release, so that the site describes the released version. A manual run of the workflow publishes it too.

## Releases

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
- docs/: this site, in Starlight
- test/: the end-to-end tests of the image
- Dockerfile: the image, with ffmpeg and whisper.cpp
- compose.yaml: an example of deneb behind a proxy
