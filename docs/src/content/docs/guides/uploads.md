---
title: Upload videos
description: Create a video through the API, and send its file in one PUT, with tus, which resumes after a lost connection, or from the browser of your user.
---

An upload takes two requests: one creates the video, and one sends its file to the upload URL that the answer holds.
This page sends the file in three ways: in one PUT, with tus, which resumes after a lost connection, and from the browser of your user.

## Before you start

The examples on this page send their requests to deneb at https://video.example.com. Replace it with the address of your deneb.
A request to the API also carries a key in the Authorization header. The API keys page of the dashboard creates keys, and shows each new key once.

```sh
# A key from the API keys page of the dashboard. The examples below send it.
KEY=dnb_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

## Upload in one PUT

These commands create a video, and send its file to its upload URL.

```sh
# Create a video, and keep the URL where its file goes.
UPLOAD_URL=$(curl -s https://video.example.com/v1/videos \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"title": "Product tour"}' | jq -r .upload.url)

# Send the file there in one PUT. deneb answers with the video, now queued.
curl -X PUT -T clip.mp4 "$UPLOAD_URL"
```

- `-d` sends the title as the JSON body of a POST, and `jq -r .upload.url` keeps the upload URL of the answer.
- `-X PUT -T` sends the file as the body of a PUT.
- The upload URL carries a token for this video only, so it needs no key of the API. It works for 24 hours.
- The video becomes `queued` when the file has arrived, then `processing`, then `ready`.

The answer of `POST /v1/videos` holds the video and two ways to upload its file.

```json
{
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
    "created_at": "2026-11-06T10:58:20.483Z",
    "ready_at": null
  },
  "upload": {
    "url": "https://video.example.com/v1/uploads/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049100.m3Qp0ZkS7mWcT9vYbL2xN4aE8rJ1uFgD6iO5tKqVwXs",
    "tus": {
      "endpoint": "https://video.example.com/v1/uploads/",
      "metadata": {
        "video_id": "vid_7wfd5nhpwadrdxgd7laif6x7ry",
        "token": "1794049100.m3Qp0ZkS7mWcT9vYbL2xN4aE8rJ1uFgD6iO5tKqVwXs"
      }
    },
    "expires_at": "2026-11-07T10:58:20Z"
  }
}
```

A PUT sends the whole file in one request, which a proxy in front of deneb may refuse for a large file.
The proxy of Cloudflare, for one, accepts 100 MB. Send larger files with tus.

## Resumable uploads

tus sends a file in parts, and resumes after a lost connection where it stopped.
Any tus client works, such as tus-js-client in browsers and Node.js. These commands show the requests with curl.

```sh
# The video and the token of upload.tus.metadata in the answer above.
VIDEO_ID=vid_7wfd5nhpwadrdxgd7laif6x7ry
TOKEN=1794049100.m3Qp0ZkS7mWcT9vYbL2xN4aE8rJ1uFgD6iO5tKqVwXs
# tus takes each value of the metadata in base64.
b64() { printf %s "$1" | base64 | tr -d '\n'; }

# Create the upload at upload.tus.endpoint. deneb answers 201, with the URL of the
# upload in the Location header: the upload URL of the video, with its token.
curl -si -X POST https://video.example.com/v1/uploads/ \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Length: $(wc -c < clip.mp4 | tr -d ' ')" \
  -H "Upload-Metadata: video_id $(b64 "$VIDEO_ID"),token $(b64 "$TOKEN"),filename $(b64 clip.mp4)"

# Send the file from its start to the URL of the Location header.
curl -X PATCH "https://video.example.com/v1/uploads/$VIDEO_ID?token=$TOKEN" \
  -H "Tus-Resumable: 1.0.0" \
  -H "Upload-Offset: 0" \
  -H "Content-Type: application/offset+octet-stream" \
  --data-binary @clip.mp4
```

- `-si` prints the headers of the answer, among them Location.
- `Upload-Length` is the size of the file in bytes. tus needs it before the file.
- `Upload-Metadata` names the video and its token. `filename` is optional, and deneb keeps it with the upload.
- `Upload-Offset: 0` sends the file from its first byte. After a lost connection, a tus client asks deneb for the offset and sends the rest.

## Upload from a browser

Your server creates the video with its key, and passes the `upload` of the answer to the browser.
The browser then sends the file to deneb itself, and never sees your key.

```js
// In the browser. `created` is the answer of POST /v1/videos that your server made.
import { Upload } from "tus-js-client";

const upload = new Upload(file, {
  endpoint: created.upload.tus.endpoint,
  // video_id and token name the video; the file name is kept with the upload.
  metadata: { ...created.upload.tus.metadata, filename: file.name },
  // Parts of 64 MiB pass proxies that limit the size of a request.
  chunkSize: 64 * 1024 * 1024,
  onProgress: (sent, total) => console.log(`${Math.floor((sent / total) * 100)}%`),
  onSuccess: () => console.log("uploaded"),
});
upload.start();
```

- The upload URLs accept requests from any origin, so the page can be on another domain than deneb.
- A page can also PUT a small file with `fetch(created.upload.url, { method: "PUT", body: file })`. tus suits large files, since it resumes them.

## Limits of uploads

- A file can be up to 50 GB, which `DENEB_MAX_UPLOAD_GB` changes.
- A video takes one file. An upload for a video that has its file gets 409.
- One upload of a video runs at a time. A second PUT while one sends its file gets 409, and a PUT stops a tus upload of the video that has not finished.
- An upload that the free disk of deneb cannot take gets 507, since deneb keeps about 1 GB free for its database.
- deneb deletes the videos whose file never arrived after seven days, with what arrived of their uploads.
- The dashboard uploads three files at a time, in parts of 64 MiB, and resumes them after a lost connection.
