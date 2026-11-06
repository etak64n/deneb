---
title: Play videos
description: Get the signed URLs of a ready video, play its stream in any HLS player, and embed it in an iframe on any site.
---

A ready video has signed URLs that play its stream in any HLS player, and an embed page for an iframe on any site.
This page gets the URLs, explains how long they last, and embeds a video.

## Get the playback URLs

Every playback URL expires, so your app asks deneb for the video when it shows it, and uses the URLs of that answer.

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

- The URLs work until `expires_at`. They last at least `DENEB_PLAYBACK_TTL_HOURS`, 24 by default, rounded up to the hour.
- The playback URLs of a video stay the same for an hour, so players and CDNs can reuse what they fetched.
- A playback URL carries its signed token in the path (internal/app/app.go). The relative URLs inside the playlists then carry the token too.

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
