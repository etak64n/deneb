---
title: Play videos
description: Get the URLs of a ready video, signed or public, play its stream in any HLS player, and embed it in an iframe on any site.
---

A ready video has URLs that play its stream in any HLS player, and an embed page for an iframe on any site.
The URLs are signed and expire, or, for a public video, never expire. This page gets them, explains both kinds, and embeds a video.

## Get the playback URLs

A signed URL expires, so your app asks deneb for the video when it shows it, and uses the URLs of that answer.

```sh
# The playback URLs of a video, from deneb at https://video.example.com. KEY is a key of the API.
curl -s https://video.example.com/v1/videos/vid_7wfd5nhpwadrdxgd7laif6x7ry \
  -H "Authorization: Bearer $KEY" | jq .video.playback
```

```json
{
  "hls_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/hls/master.m3u8",
  "poster_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/poster.jpg",
  "storyboard_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/storyboard.vtt",
  "captions_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/captions.vtt",
  "embed_url": "https://video.example.com/embed/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4",
  "expires_at": "2026-11-07T11:00:00Z"
}
```

`playback` appears once the video is ready, and `captions_url` once its captions are ready.

## The URLs

- `hls_url` is the master playlist of the stream. It plays in any HLS player, such as hls.js, Video.js, or the player of Safari, iOS, and Android.
- `poster_url` is a still at a tenth of the length, for the player to show before it plays.
- `storyboard_url` is a WebVTT file that maps each moment to a picture, for previews on the seek bar.
- `captions_url` is the captions, as a WebVTT file.
- `embed_url` is a page with a player, for an iframe.

## How long the URLs last

The URLs expire, so a URL that someone copies stops working, while the video itself stays.
Each answer of the API carries fresh URLs, so a page that your server builds always has URLs that work.

- The URLs work until `expires_at`. They last at least `DENEB_PLAYBACK_TTL_HOURS`, 24 by default, rounded up to the hour.
- A page that pastes an embed code once and has no server, such as a blog post, needs URLs that never expire. The next section makes a video public for it.
- The playback URLs of a video stay the same for an hour, so players and CDNs can reuse what they fetched.
- A playback URL carries its signed token in the path (internal/app/app.go). The relative URLs inside the playlists then carry the token too.

## Public URLs

A public video plays under URLs that never expire, for a page that pastes an embed code once.
A video is private by default. This request makes it public.

```sh
# Make a video public. KEY is a key of the API.
curl -s -X PATCH https://video.example.com/v1/videos/vid_7wfd5nhpwadrdxgd7laif6x7ry \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"public": true}'
```

- `POST /v1/videos` takes `"public": true` as well, and the page of a video in the dashboard has a Make public button.
- The URLs of a public video hold `public` in place of the token, such as https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/public/hls/master.m3u8. Its embed URL has no token, and `expires_at` is null.
- Anyone with these URLs can play the video, and an embed code with them keeps working.
- `"public": false` ends them at once, since deneb checks the video on each request (internal/app/app.go). Browsers and CDNs may still serve the segments and pictures that they keep, for up to a day.

## Embed a video

The embed page plays a video in an iframe on any site. The dashboard shows the iframe code of each video, in the shape of the video.

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

## Other sites and caches

- The playback URLs answer any origin (CORS), so players on other sites can fetch the stream.
- Browsers and CDNs may keep segments and pictures for a day, and playlists for a minute. deneb marks errors as not to be kept.
