---
title: Playback
description: The files that the playback URLs of a ready video serve, the details for a player, the embed page, and how long the URLs work.
tableOfContents:
  maxHeadingLevel: 2
---

A ready video plays from URLs under `/play`, and in the page under `/embed`. The `playback` of the video object holds these URLs, so your app takes them from the API rather than building them.
They carry the playback token of the video in place of a key of the API, so any player and any site can use them.

| Endpoint | Description |
| --- | --- |
| [`GET /play/{id}/{token}/{file}`](#play-a-video) | Serves the playlists, segments, pictures, and captions of a ready video. |
| [`GET /play/{id}/{token}/info.json`](#get-the-details-for-a-player) | Returns the title, length, size, and captions of a video, for a player. |
| [`GET /embed/{id}`](#embed-a-video) | Serves a page with a player, for an iframe on any site. |

## Play a video

`GET /play/{id}/{token}/{file}`

Serves a file of a ready video. The playlists name the other files by relative URLs, which then carry the token too.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video. |
| `token` | string | The playback token of the video, or `permanent` for a video with permanent URLs. |
| `file` | string | The file. The next table lists them. |

### Files

| File | Content | Kept by browsers and CDNs |
| --- | --- | --- |
| `hls/master.m3u8` | The master playlist, which lists the qualities. This is `hls_url`. | 1 minute |
| `hls/index_{quality}.m3u8` | The playlist of one quality, such as `hls/index_720p.m3u8`. | 1 minute |
| `hls/{quality}_{number}.ts` | A segment of four seconds of one quality, in MPEG-TS. | 1 day |
| `poster.jpg` | A still at a tenth of the length. This is `poster_url`. | 1 day |
| `storyboard.vtt` | WebVTT that maps each moment to a part of `storyboard.jpg`. This is `storyboard_url`. | 1 minute |
| `storyboard.jpg` | The pictures of the storyboard, in one image. | 1 day |
| `captions.vtt` | The captions as WebVTT, once they are ready. This is `captions_url`. | 1 minute |

- The files take `Range` requests, and answer any origin with CORS, so a player on any site can read them.
- deneb marks an error as not to be kept, so a fixed URL works at once.
- With S3 storage and `DENEB_S3_REDIRECTS=true`, deneb answers the segments and pictures with 302 to a signed URL of the bucket, which saves the bandwidth of deneb. It still serves the playlists and the WebVTT files itself.

### Errors

| Status | When |
| --- | --- |
| 403 | The token is wrong or expired, or the token is `permanent` and the video has no permanent URLs. |
| 404 | The video has no such file, as before it is ready, or the path names no file of the list. |

### Example

```sh
# The master playlist of a video. HLS_URL is playback.hls_url of the video.
curl -s "$HLS_URL"
```

## Get the details for a player

`GET /play/{id}/{token}/info.json`

Returns what a player shows beside the stream. The embed page reads it, and a player of your own can too.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video. |
| `token` | string | The playback token of the video, or `permanent`. |

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `title` | string | The title of the video. |
| `duration` | number | The length in seconds. |
| `width` | integer | The width as displayed. |
| `height` | integer | The height as displayed. |
| `captions` | boolean | `true` when the captions are ready at `captions.vtt`. |
| `captions_language` | string | The language of the captions, such as `en`, or an empty string without captions. |

### Errors

| Status | When |
| --- | --- |
| 403 | The token is wrong or expired, or the video has no permanent URLs. |
| 404 | The video does not exist, or is not ready. |

### Example

```json
{
  "title": "Product tour",
  "duration": 8,
  "width": 1280,
  "height": 720,
  "captions": true,
  "captions_language": "en"
}
```

## Embed a video

`GET /embed/{id}?token={token}`

Serves a page with a player, which sites put in an iframe. This is `embed_url`.
The page shows the poster at once, then plays the stream through the `/play` URLs of the same token.

### Parameters

| Parameter | In | Type | Required | Description |
| --- | --- | --- | --- | --- |
| `id` | path | string | Yes | The ID of the video. |
| `token` | query | string | No | The playback token of the video. A video with permanent URLs needs none. |

### Answer

`200 OK`, with the HTML of the page. Any site may frame it.

With a token that is wrong or expired, or without a token for a video that has no permanent URLs, the page still loads, with 403. It then says that the video is not available, rather than leaving an error in the frame of another site.

### Example

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

## How long the URLs work

| URLs | How long they work |
| --- | --- |
| Signed | Until `playback.expires_at`: at least `DENEB_PLAYBACK_TTL_HOURS`, 24 by default, rounded up to the hour. The URLs stay the same for that hour, so players and CDNs reuse what they fetched. |
| Permanent | Until the video turns `permanent_urls` off, which ends them at once. They hold `permanent` in place of the token, and the embed URL has no token. |

Each answer of the API signs the URLs again, so a page that your server builds always has URLs that work.
The [Play videos](/deneb/guides/playback/) page explains when to use each kind.
