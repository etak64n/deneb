---
title: API reference
description: The authentication, the errors, the endpoints, and the video object of the API of deneb.
---

The API takes and returns JSON under `/v1`.
This page lists its authentication, its errors, its endpoints, and the video object. Its examples use https://video.example.com as the address of deneb.

## Authentication

A request carries a key of the API in the Authorization header, as `Bearer <key>`. The API keys page of the dashboard creates keys.

```sh
# A key from the API keys page of the dashboard.
KEY=dnb_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# A request with the key: the newest videos.
curl -s https://video.example.com/v1/videos -H "Authorization: Bearer $KEY"
```

The upload URLs and the playback URLs carry tokens of their own, so they need no key.

## Errors

- An error has a status and a body such as `{"error": {"message": "the video does not exist"}}`.
- 400 is input that deneb refuses, 401 a missing or wrong key, and 404 a video or webhook that does not exist.
- 409 is a conflict, such as a second upload for a video. 413 is a body larger than 1 MB.
- 403 answers a key that tries to manage keys. Only the dashboard manages keys.

## Videos

- `POST /v1/videos` creates a video. The body may have a `title` of up to 300 characters. The answer is 201 with `video` and `upload`.
- `GET /v1/videos` lists the videos, newest first, as `videos` and `next_cursor`. `limit` takes 1 to 100, 24 by default. `cursor` takes the `next_cursor` of the previous page, which is empty on the last page.
- `GET /v1/videos/{id}` returns one `video`.
- `PATCH /v1/videos/{id}` changes the `title`, and returns the `video`.
- `DELETE /v1/videos/{id}` deletes a video and answers 204. deneb stops the work on the video and removes its files a moment later.

## Uploads

The `upload` in the answer of `POST /v1/videos` holds two ways to send the file of the video.

- `upload.url` takes the whole file in one PUT, and answers with the video, now queued.
- `upload.tus.endpoint` takes an upload of tus 1.0, with the `video_id` and the `token` of `upload.tus.metadata` in its Upload-Metadata header.
- Every request about the upload of a video needs its token, which works until `upload.expires_at`, 24 hours after the video was created.
- The upload URLs accept requests from any origin, so a browser can send the file itself.

The [Upload videos](/deneb/guides/uploads/) page shows both ways with curl, and from a browser.

## The video object

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

## Webhooks

- `POST /v1/webhooks` adds an endpoint. The body has its `url`, which starts with http:// or https://. The answer is 201 with `webhook`, whose `secret` appears only in this answer.
- `GET /v1/webhooks` lists the endpoints, without their secrets.
- `DELETE /v1/webhooks/{id}` deletes an endpoint and answers 204.
- `GET /v1/webhooks/{id}/deliveries` lists the latest 50 deliveries of an endpoint. A delivery has its `event`, `status` (`pending`, `delivered`, or `failed`), `attempts`, `response_status`, `last_error`, `created_at`, and `delivered_at`.

deneb sends three events to the endpoints, `video.ready`, `video.failed`, and `video.captions_ready`, as POSTs of JSON signed with the secret of each endpoint.
The [Webhooks](/deneb/guides/webhooks/) page describes their body, their headers, and how to verify their signature.

## Health

`GET /healthz` answers 200 while deneb runs. It needs no key.
