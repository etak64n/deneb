---
title: API reference
description: Every endpoint of the API of deneb at a glance, and what the endpoints have in common, from authentication to errors.
---

The API of deneb takes and returns JSON at the address of deneb. The examples use https://video.example.com as that address.
This page lists every endpoint, and what the endpoints have in common. The pages of videos, uploads, playback, and webhooks describe each endpoint with its parameters, its answer, and its errors.

## Endpoints

### Videos

| Endpoint | Description |
| --- | --- |
| [`POST /v1/videos`](/deneb/reference/api/videos/#create-a-video) | Creates a video, and returns the URLs that take its file. |
| [`GET /v1/videos`](/deneb/reference/api/videos/#list-videos) | Lists the videos, newest first, a page at a time. |
| [`GET /v1/videos/{id}`](/deneb/reference/api/videos/#get-a-video) | Returns a video, with fresh playback URLs once it is ready. |
| [`PATCH /v1/videos/{id}`](/deneb/reference/api/videos/#update-a-video) | Changes the title of a video, or whether its URLs are permanent. |
| [`DELETE /v1/videos/{id}`](/deneb/reference/api/videos/#delete-a-video) | Deletes a video and its files. |
| [`POST /v1/videos/{id}/retry`](/deneb/reference/api/videos/#retry-a-video) | Queues again a video, or its captions, that failed. |

### Uploads

| Endpoint | Description |
| --- | --- |
| [`PUT /v1/uploads/{id}`](/deneb/reference/api/uploads/#upload-a-file-in-one-put) | Takes the whole file of a video in one request. |
| [`POST /v1/uploads/`](/deneb/reference/api/uploads/#create-a-tus-upload) | Creates a resumable upload in the tus protocol. |
| [`PATCH /v1/uploads/{id}`](/deneb/reference/api/uploads/#send-a-part) | Sends a part of the file of a tus upload. |
| [`HEAD /v1/uploads/{id}`](/deneb/reference/api/uploads/#read-the-offset) | Tells how much of the file of a tus upload has arrived. |
| [`DELETE /v1/uploads/{id}`](/deneb/reference/api/uploads/#cancel-a-tus-upload) | Removes a tus upload that has not finished. |

### Playback

| Endpoint | Description |
| --- | --- |
| [`GET /play/{id}/{token}/{file}`](/deneb/reference/api/playback/#play-a-video) | Serves the playlists, segments, pictures, and captions of a ready video. |
| [`GET /play/{id}/{token}/info.json`](/deneb/reference/api/playback/#get-the-details-for-a-player) | Returns the title, length, size, and captions of a video, for a player. |
| [`GET /embed/{id}`](/deneb/reference/api/playback/#embed-a-video) | Serves a page with a player, for an iframe on any site. |

### Webhooks

| Endpoint | Description |
| --- | --- |
| [`POST /v1/webhooks`](/deneb/reference/api/webhooks/#add-an-endpoint) | Adds an endpoint that receives the events. |
| [`GET /v1/webhooks`](/deneb/reference/api/webhooks/#list-endpoints) | Lists the endpoints. |
| [`DELETE /v1/webhooks/{id}`](/deneb/reference/api/webhooks/#delete-an-endpoint) | Deletes an endpoint. |
| [`GET /v1/webhooks/{id}/deliveries`](/deneb/reference/api/webhooks/#list-deliveries) | Lists the latest 50 deliveries of an endpoint. |

### Health

| Endpoint | Description |
| --- | --- |
| [`GET /healthz`](#health) | Answers 200 while deneb runs. |

## Authentication

Each kind of endpoint takes a credential of its own.

| Endpoints | Credential |
| --- | --- |
| `/v1/videos`, `/v1/webhooks` | A key of the API, in the header `Authorization: Bearer <key>`. |
| `/v1/uploads` | The upload token of the video, in the URL or in the metadata of tus. `POST /v1/videos` returns it. |
| `/play`, `/embed` | The playback token of the video in the URL, or `permanent` for a video with permanent URLs. The video object holds these URLs. |
| `/healthz` | None. |

```sh
# A key from the API keys page of the dashboard.
KEY=dnb_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
# The newest videos, with the key.
curl -s https://video.example.com/v1/videos -H "Authorization: Bearer $KEY"
```

- A key starts with `dnb_`. The API keys page of the dashboard creates keys, and shows each one once.
- A missing, wrong, or revoked key gets 401.
- A key cannot create, list, or revoke keys: `/v1/api-keys` answers a key with 403. Only the administrator manages keys, in the dashboard.
- The upload and playback tokens let a browser upload and play without a key, so your app never hands its key to a browser.

## Requests and answers

- A body is JSON of at most 1 MB. A field that the endpoint does not know gets 400, so a misspelled field does not pass unnoticed.
- An empty body counts as an object without fields.
- An answer of the API is JSON that browsers and proxies do not keep (`Cache-Control: no-store`). An answer of 204 has no body.
- An answer holds its object under its name, such as `{"video": {...}}`, and a list under the plural, such as `{"videos": [...]}`.
- Times are in RFC 3339, in UTC, such as `2026-11-06T10:58:20Z`. A time that has not come, such as the `ready_at` of a video that is not ready, is `null`.
- IDs start with the kind of their object: `vid_` for videos, `wh_` for webhook endpoints, and `whd_` for deliveries.
- A path under `/v1/` that names no endpoint gets 404 with the message `no such endpoint`.

## Errors

An error has a status, and a body whose message says what went wrong.

```json
{ "error": { "message": "the title can be at most 300 characters" } }
```

| Status | Meaning |
| --- | --- |
| 400 | deneb refuses the input: the body is not JSON or has an unknown field, or a value is outside its range. |
| 401 | The key of the API, or the upload token, is missing, wrong, revoked, or expired. |
| 403 | A key asked for what only the dashboard may do, or a playback URL is invalid or expired. |
| 404 | The video, the webhook endpoint, or the path does not exist. |
| 409 | The request conflicts with the state of the video, such as a second upload of its file. |
| 413 | The body is larger than 1 MB, or the file larger than `DENEB_MAX_UPLOAD_GB`. |
| 500 | Something went wrong on the server. The log of deneb tells what. |
| 507 | The disk of deneb has no room for the upload. deneb keeps about 1 GB free for its database. |

The errors that the tus protocol finds come in plain text, such as `ERR_VIDEO_UPLOADED: the video already has its file`, and so do the errors of the playback URLs.

## Pages of lists

`GET /v1/videos` returns the videos a page at a time, newest first.

| Parameter | Description |
| --- | --- |
| `limit` | How many videos a page holds, from 1 to 100. 24 by default. |
| `cursor` | The `next_cursor` of the previous page, for the page after it. Without it, the list starts with the newest video. |

The answer has `next_cursor`, which is an empty string on the last page.

```sh
# Every video, 100 at a time: the ID, the status, and the title of each. KEY is a key of the API.
cursor=""
while :; do
  page=$(curl -s "https://video.example.com/v1/videos?limit=100&cursor=$cursor" -H "Authorization: Bearer $KEY")
  echo "$page" | jq -r '.videos[] | "\(.id) \(.status) \(.title)"'
  cursor=$(echo "$page" | jq -r .next_cursor)
  [ -z "$cursor" ] && break
done
```

The other lists come whole: `GET /v1/webhooks` lists every endpoint, and `GET /v1/webhooks/{id}/deliveries` the latest 50 deliveries.

## Health

`GET /healthz` answers 200 with `{"status": "ok"}` while deneb runs. It needs no key, so a load balancer or a monitor can ask it.
