---
title: Uploads
description: Send the file of a video in one PUT, or resumably with tus, to the URLs that POST /v1/videos returns, and the fields of the upload object.
tableOfContents:
  maxHeadingLevel: 2
---

The file of a video goes to the URLs in the `upload` of `POST /v1/videos`, in one PUT or with tus, a protocol for uploads that resume after a lost connection.
These URLs carry the upload token of the video instead of a key of the API, so the browser of your user can send the file itself.

| Endpoint | Description |
| --- | --- |
| [`PUT /v1/uploads/{id}`](#upload-a-file-in-one-put) | Takes the whole file of a video in one request. |
| [`POST /v1/uploads/`](#create-a-tus-upload) | Creates a resumable upload in the tus protocol. |
| [`PATCH /v1/uploads/{id}`](#send-a-part) | Sends a part of the file of a tus upload. |
| [`HEAD /v1/uploads/{id}`](#read-the-offset) | Tells how much of the file of a tus upload has arrived. |
| [`DELETE /v1/uploads/{id}`](#cancel-a-tus-upload) | Removes a tus upload that has not finished. |

## The upload object

`POST /v1/videos` answers with the video and this object.

```json
{
  "url": "https://video.example.com/v1/uploads/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049100.Rk3s9vQpL2mX8tYw1BzN4cHd7eUa0fJg5iKo6nPrSq8",
  "tus": {
    "endpoint": "https://video.example.com/v1/uploads/",
    "metadata": {
      "token": "1794049100.Rk3s9vQpL2mX8tYw1BzN4cHd7eUa0fJg5iKo6nPrSq8",
      "video_id": "vid_7wfd5nhpwadrdxgd7laif6x7ry"
    }
  },
  "expires_at": "2026-11-07T10:58:20Z"
}
```

| Field | Type | Description |
| --- | --- | --- |
| `url` | string | The URL that takes the whole file in one PUT, with the token in its query. |
| `tus.endpoint` | string | Where a tus client creates the upload. |
| `tus.metadata.video_id` | string | The ID of the video, which the tus upload names in its metadata. |
| `tus.metadata.token` | string | The upload token, which the tus upload names in its metadata. |
| `expires_at` | string | When the token stops working: 24 hours after the video was created. |

- A video has one upload at a time. A PUT stops a tus upload of the video that has not finished, and takes its place. A second PUT while the first one sends its file gets 409.
- A finished upload is the file that deneb encodes. Neither a PUT nor tus can replace it, and deleting the video removes it.
- A file can be as large as `DENEB_MAX_UPLOAD_GB`, 50 GB by default.
- The upload URLs accept requests from any origin, with CORS, so that a page of your site can send the file.

## Upload a file in one PUT

`PUT /v1/uploads/{id}?token={token}`

Takes the whole file of the video as the body of one request, and queues the video once the last byte arrives.
This is the `url` of the upload object, as it is.

### Parameters

| Parameter | In | Type | Required | Description |
| --- | --- | --- | --- | --- |
| `id` | path | string | Yes | The ID of the video. |
| `token` | query | string | Yes | The upload token of the video. |
| `Content-Length` | header | integer | No | The size of the file in bytes. deneb checks it against `DENEB_MAX_UPLOAD_GB` and the free disk before it reads the file. |

### Body

The bytes of the video file, such as an MP4, as they are. deneb reads the format from the bytes, so the request needs no `Content-Type`.

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `video` | [video object](/deneb/reference/api/videos/#the-video-object) | The video, with the status `queued`. |

### Errors

| Status | When |
| --- | --- |
| 400 | The body is empty, or the connection stopped before the end of the file. |
| 401 | The token is missing, wrong, or expired. |
| 404 | The video does not exist, or was deleted while the file arrived. |
| 409 | Another upload of the video is sending its file, or the video already has its file. |
| 413 | The file is larger than `DENEB_MAX_UPLOAD_GB`. |
| 507 | The disk of deneb has no room for the file. |

### Example

```sh
# Send clip.mp4 to the upload URL of a video. UPLOAD_URL is upload.url of POST /v1/videos.
curl -s -T clip.mp4 "$UPLOAD_URL" | jq .video.status
```

## Create a tus upload

`POST /v1/uploads/`

Creates a resumable upload in tus 1.0.0. Any tus client can do it, such as tus-js-client in a browser.

### Headers

| Header | Required | Description |
| --- | --- | --- |
| `Tus-Resumable` | Yes | `1.0.0`. |
| `Upload-Length` | Yes | The size of the file in bytes, at most `DENEB_MAX_UPLOAD_GB`. |
| `Upload-Metadata` | Yes | The pairs of `tus.metadata`, each as the key, a space, and the value in base64, separated by commas: `video_id` and `token`. It may also hold the `filename`. |

### Answer

`201 Created`, with the header `Location`: the URL of the upload, which the next requests use.
The URL holds the ID of the video and the token, such as https://video.example.com/v1/uploads/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049100.Rk3s9vQpL2mX8tYw1BzN4cHd7eUa0fJg5iKo6nPrSq8.
An earlier tus upload of the video that did not finish, as when the client lost the answer to its POST, gives way to this one.

### Errors

| Status | When |
| --- | --- |
| 401 | The metadata has no `video_id` or `token`, or the token is wrong or expired. |
| 404 | The video does not exist. |
| 409 | The video already has its file. |
| 413 | `Upload-Length` is larger than `DENEB_MAX_UPLOAD_GB`. |
| 507 | The disk of deneb has no room for the file. |

### Example

```sh
# Create a tus upload for clip.mp4. VIDEO_ID and TOKEN are tus.metadata of POST /v1/videos.
b64() { printf %s "$1" | base64; }
curl -s -D - -o /dev/null -X POST https://video.example.com/v1/uploads/ \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Length: $(wc -c < clip.mp4)" \
  -H "Upload-Metadata: video_id $(b64 "$VIDEO_ID"),token $(b64 "$TOKEN"),filename $(b64 clip.mp4)" | grep -i '^location'
```

## Send a part

`PATCH /v1/uploads/{id}?token={token}`

Appends a part of the file at the offset that the request names. When the last byte arrives, deneb queues the video before it answers.
This is the `Location` of the upload.

### Headers

| Header | Required | Description |
| --- | --- | --- |
| `Tus-Resumable` | Yes | `1.0.0`. |
| `Upload-Offset` | Yes | Where the part starts, in bytes: the offset that the upload has reached. |
| `Content-Type` | Yes | `application/offset+octet-stream`. |

### Answer

`204 No Content`, with the header `Upload-Offset`: the offset after the part.

### Errors

| Status | When |
| --- | --- |
| 401 | The token is missing, wrong, or expired. |
| 404 | The upload does not exist. |
| 409 | `Upload-Offset` is not the offset that the upload has reached. |

## Read the offset

`HEAD /v1/uploads/{id}?token={token}`

Tells how much of the file has arrived, so that a client resumes after a lost connection from there.

### Headers

| Header | Required | Description |
| --- | --- | --- |
| `Tus-Resumable` | Yes | `1.0.0`. |

### Answer

`200 OK`, without a body, with the headers `Upload-Offset`, the bytes that arrived, and `Upload-Length`, the size of the file.

## Cancel a tus upload

`DELETE /v1/uploads/{id}?token={token}`

Removes a tus upload that has not finished, and the part of the file that arrived. The video keeps waiting for its file.

### Headers

| Header | Required | Description |
| --- | --- | --- |
| `Tus-Resumable` | Yes | `1.0.0`. |

### Answer

`204 No Content`.

### Errors

| Status | When |
| --- | --- |
| 401 | The token is missing, wrong, or expired. |
| 409 | The upload finished. Delete the video instead. |

The [Upload videos](/deneb/guides/uploads/) page sends a file both ways with curl, and from a browser.
