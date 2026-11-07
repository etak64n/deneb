---
title: Videos
description: Create, list, get, update, delete, and retry videos through the API, and the fields of the video object.
tableOfContents:
  maxHeadingLevel: 2
---

A video is a file that deneb encodes and plays. These endpoints create, read, change, and delete videos, and each answers with the video object.
Every endpoint on this page needs a key of the API in the header `Authorization: Bearer <key>`.

| Endpoint | Description |
| --- | --- |
| [`POST /v1/videos`](#create-a-video) | Creates a video, and returns the URLs that take its file. |
| [`GET /v1/videos`](#list-videos) | Lists the videos, newest first, a page at a time. |
| [`GET /v1/videos/{id}`](#get-a-video) | Returns a video, with fresh playback URLs once it is ready. |
| [`PATCH /v1/videos/{id}`](#update-a-video) | Changes the title of a video, or whether its URLs are permanent. |
| [`DELETE /v1/videos/{id}`](#delete-a-video) | Deletes a video and its files. |
| [`POST /v1/videos/{id}/retry`](#retry-a-video) | Queues again a video, or its captions, that failed. |

## Create a video

`POST /v1/videos`

Creates a video that waits for its file, and returns the URLs that take the file.
The status of the video is `waiting` until its file arrives.

### Body

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `title` | string | No | The title, of up to 300 characters. deneb removes the spaces at both ends. Empty by default. |
| `permanent_urls` | boolean | No | `true` gives the video playback URLs that never expire. `false` by default. |

### Answer

`201 Created`

| Field | Type | Description |
| --- | --- | --- |
| `video` | [video object](#the-video-object) | The new video, with the status `waiting`. |
| `upload` | [upload object](/deneb/reference/api/uploads/#the-upload-object) | The URLs that take the file, and when their token expires, 24 hours after now. |

### Errors

| Status | When |
| --- | --- |
| 400 | The body is not JSON, has a field that is not in the table, or has a title of more than 300 characters. |
| 401 | The key is missing, wrong, or revoked. |
| 413 | The body is larger than 1 MB. |

### Example

```sh
# Create a video titled Product tour. KEY is a key of the API.
curl -s https://video.example.com/v1/videos \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"title": "Product tour"}'
```

```json
{
  "upload": {
    "url": "https://video.example.com/v1/uploads/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049100.Rk3s9vQpL2mX8tYw1BzN4cHd7eUa0fJg5iKo6nPrSq8",
    "tus": {
      "endpoint": "https://video.example.com/v1/uploads/",
      "metadata": {
        "token": "1794049100.Rk3s9vQpL2mX8tYw1BzN4cHd7eUa0fJg5iKo6nPrSq8",
        "video_id": "vid_7wfd5nhpwadrdxgd7laif6x7ry"
      }
    },
    "expires_at": "2026-11-07T10:58:20Z"
  },
  "video": {
    "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
    "title": "Product tour",
    "permanent_urls": false,
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
  }
}
```

## List videos

`GET /v1/videos`

Lists the videos, newest first, a page at a time.

### Query parameters

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `limit` | integer | No | How many videos the page holds, from 1 to 100. 24 by default. |
| `cursor` | string | No | The `next_cursor` of the previous page, for the page after it. Without it, the page starts with the newest video. |

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `videos` | array of [video objects](#the-video-object) | The videos of the page, newest first. Each ready video has fresh playback URLs. |
| `next_cursor` | string | The cursor of the next page, or an empty string on the last page. |

### Errors

| Status | When |
| --- | --- |
| 400 | `limit` is not a whole number from 1 to 100, or `cursor` is not one that deneb gave. |
| 401 | The key is missing, wrong, or revoked. |

### Example

```sh
# The two newest videos, and the cursor of the next page. KEY is a key of the API.
curl -s "https://video.example.com/v1/videos?limit=2" \
  -H "Authorization: Bearer $KEY"
```

```json
{
  "next_cursor": "MTc5Mzk2MTY2NzAwMDp2aWRfazNuNWI3cXd6MnhoNGZkcjZ0Y20yeWp2cGE",
  "videos": [
    {
      "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
      "title": "Product tour",
      "permanent_urls": false,
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
    },
    {
      "id": "vid_k3n5b7qwz2xh4fdr6tcm2yjvpa",
      "title": "Customer interview",
      "permanent_urls": false,
      "status": "failed",
      "error": "the file is not a video: it has no video track",
      "progress": 0,
      "duration": 0,
      "width": 0,
      "height": 0,
      "source_size": 14820167,
      "renditions": [],
      "captions": { "status": "none" },
      "created_at": "2026-11-06T10:41:07Z",
      "ready_at": null
    }
  ]
}
```

## Get a video

`GET /v1/videos/{id}`

Returns a video. A ready video has playback URLs that deneb signs for this answer, so your app asks for the video each time it shows it.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video, such as `vid_7wfd5nhpwadrdxgd7laif6x7ry`. |

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `video` | [video object](#the-video-object) | The video. |

### Errors

| Status | When |
| --- | --- |
| 401 | The key is missing, wrong, or revoked. |
| 404 | The video does not exist. |

### Example

```sh
# A video and its playback URLs. KEY is a key of the API.
curl -s https://video.example.com/v1/videos/vid_7wfd5nhpwadrdxgd7laif6x7ry \
  -H "Authorization: Bearer $KEY"
```

```json
{
  "video": {
    "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
    "title": "Product tour",
    "permanent_urls": false,
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

## Update a video

`PATCH /v1/videos/{id}`

Changes the fields that the body has, and keeps the others.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video. |

### Body

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `title` | string | No | The new title, of up to 300 characters. deneb removes the spaces at both ends, and an empty string leaves the video without a title. |
| `permanent_urls` | boolean | No | `true` gives the video URLs that never expire. `false` ends them at once, and the video gets signed URLs again. |

Browsers and CDNs may keep the segments and pictures of a video for up to a day. After `permanent_urls` turns `false`, they may still play what they kept.

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `video` | [video object](#the-video-object) | The video after the change. |

### Errors

| Status | When |
| --- | --- |
| 400 | The body is not JSON, has a field that is not in the table, or has a title of more than 300 characters. |
| 401 | The key is missing, wrong, or revoked. |
| 404 | The video does not exist. |
| 413 | The body is larger than 1 MB. |

### Example

```sh
# Rename a video, and give it permanent URLs. KEY is a key of the API.
curl -s -X PATCH https://video.example.com/v1/videos/vid_7wfd5nhpwadrdxgd7laif6x7ry \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"title": "Product tour 2026", "permanent_urls": true}'
```

```json
{
  "video": {
    "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
    "title": "Product tour 2026",
    "permanent_urls": true,
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
      "hls_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/permanent/hls/master.m3u8",
      "poster_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/permanent/poster.jpg",
      "storyboard_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/permanent/storyboard.vtt",
      "captions_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/permanent/captions.vtt",
      "embed_url": "https://video.example.com/embed/vid_7wfd5nhpwadrdxgd7laif6x7ry",
      "expires_at": null
    }
  }
}
```

## Delete a video

`DELETE /v1/videos/{id}`

Deletes a video. deneb stops the work on the video at once, and removes its files a moment later.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video. |

### Answer

`204 No Content`, without a body.

### Errors

| Status | When |
| --- | --- |
| 401 | The key is missing, wrong, or revoked. |
| 404 | The video does not exist. |

### Example

```sh
# Delete a video. KEY is a key of the API.
curl -s -X DELETE https://video.example.com/v1/videos/vid_7wfd5nhpwadrdxgd7laif6x7ry \
  -H "Authorization: Bearer $KEY"
```

## Retry a video

`POST /v1/videos/{id}/retry`

Queues again what failed: the encoding of a failed video, from its file, or else the captions of a ready video whose captions failed.
It takes no body.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video. |

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `video` | [video object](#the-video-object) | The video, queued again, or with its captions queued again. |

### Errors

| Status | When |
| --- | --- |
| 401 | The key is missing, wrong, or revoked. |
| 404 | The video does not exist. |
| 409 | Neither the video nor its captions failed, or the file of the failed video is gone. deneb keeps the file of a failed video for a week. |

### Example

```sh
# Encode a failed video again. KEY is a key of the API.
curl -s -X POST https://video.example.com/v1/videos/vid_7wfd5nhpwadrdxgd7laif6x7ry/retry \
  -H "Authorization: Bearer $KEY"
```

```json
{
  "video": {
    "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
    "title": "Product tour",
    "permanent_urls": false,
    "status": "queued",
    "progress": 0,
    "duration": 0,
    "width": 0,
    "height": 0,
    "source_size": 3096076,
    "renditions": [],
    "captions": { "status": "none" },
    "created_at": "2026-11-06T10:58:20Z",
    "ready_at": null
  }
}
```

## The video object

Every endpoint on this page answers with the video in this form, and so do the events of webhooks.

```json
{
  "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
  "title": "Product tour",
  "permanent_urls": false,
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
```

### Fields

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the video, starting with `vid_`. |
| `title` | string | The title. An empty string when the video has none. |
| `permanent_urls` | boolean | `true` when the playback URLs of the video never expire. |
| `status` | string | Where the video is. The next table lists the statuses. |
| `error` | string | Why the video failed, or why its last attempt failed while it waits for the next. Only when there is an error. |
| `progress` | number | How far the encoding has gone, from 0 to 1, while the video is processing. |
| `duration` | number | The length in seconds. 0 until the video is ready. |
| `width` | integer | The width as displayed, after any rotation. 0 until the video is ready. |
| `height` | integer | The height as displayed, after any rotation. 0 until the video is ready. |
| `source_size` | integer | The size of the uploaded file in bytes. 0 until the file arrives. |
| `renditions` | array | The qualities of the stream, largest first. Empty until the video is ready. |
| `renditions[].name` | string | The name of the quality, such as `720p`. |
| `renditions[].width` | integer | The width of the quality in pixels. |
| `renditions[].height` | integer | The height of the quality in pixels. |
| `renditions[].bitrate` | integer | The target bitrate of video and audio together, in bits per second. |
| `captions.status` | string | Where the captions are. The last table lists the statuses. |
| `captions.language` | string | The language that the captions are in, such as `en`. Only when they are ready. |
| `created_at` | string | When the video was created. |
| `ready_at` | string or null | When the video became ready, or `null` before. |
| `playback` | object | The URLs that play the video. Only when the video is ready. |
| `playback.hls_url` | string | The master playlist of the stream, for any HLS player. |
| `playback.poster_url` | string | A still at a tenth of the length, to show before the video plays. |
| `playback.storyboard_url` | string | A WebVTT file that maps each moment to a picture, for previews on the seek bar. |
| `playback.captions_url` | string | The captions as a WebVTT file. Only when the captions are ready. |
| `playback.embed_url` | string | A page with a player, for an iframe. |
| `playback.expires_at` | string or null | When the signed URLs stop working, or `null` for permanent URLs. |

The [Playback](/deneb/reference/api/playback/) page describes the files behind these URLs, and how long the URLs work.

### Statuses of a video

| Status | Meaning |
| --- | --- |
| `waiting` | The video waits for its file. |
| `queued` | The file arrived, and the video waits for an encoder. A video whose attempt failed for a reason that may pass also waits here, with the reason in `error`. |
| `processing` | An encoder works on the video, and `progress` tells how far it has gone. |
| `ready` | The video plays. deneb sends the event `video.ready`. |
| `failed` | The encoding failed for good, and `error` tells why. deneb sends the event `video.failed`. |

### Statuses of the captions

| Status | Meaning |
| --- | --- |
| `none` | The video has no captions: captions are off, the video has no audio, or the video is not ready yet. |
| `queued` | The video is ready, and its captions wait for whisper.cpp. |
| `processing` | whisper.cpp writes the captions. |
| `ready` | The captions are ready at `playback.captions_url`. deneb sends the event `video.captions_ready`. |
| `failed` | Writing the captions failed. The video still plays, and `POST /v1/videos/{id}/retry` queues the captions again. |
