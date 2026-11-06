---
title: Limits
description: What deneb does not do, and the constants in its code that set its limits.
---

This page lists what deneb does not do, and the constants in its code that set its limits.
A limit that a setting changes, such as the largest upload, is on the [Configuration reference](/deneb/settings/configuration/) page instead.

## What deneb does not do

- deneb does not stream live video.
- deneb has no DRM, the encryption that only licensed players can undo. Signed URLs keep the stream from people who do not have a URL, not from people who do.
- deneb does not tone-map HDR. An HDR source becomes 8-bit SDR as it is, so its colors look flat.
- deneb offers no MP4 downloads of its encodings.
- deneb encodes on the CPU only.
- deneb runs on one server, and does not spread its work over several. A second deneb stops at start when another one uses the data folder (cmd/deneb/lock_unix.go), since both would take the same jobs. A larger server and `DENEB_WORKERS` encode more at once, and a CDN in front of deneb, or `DENEB_S3_REDIRECTS`, spreads the load of playback.
- deneb has one administrator, with no other accounts or roles.
- deneb counts no views.

## Constants

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
