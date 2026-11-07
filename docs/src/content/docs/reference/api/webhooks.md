---
title: Webhooks
description: Add, list, and delete the endpoints that receive the events of deneb, list their deliveries, and the fields of the events and their headers.
tableOfContents:
  maxHeadingLevel: 2
---

A webhook endpoint is a URL of your app that receives the events of deneb, such as a video that became ready. These endpoints manage the webhook endpoints and show their deliveries.
Every endpoint on this page needs a key of the API in the header `Authorization: Bearer <key>`.

| Endpoint | Description |
| --- | --- |
| [`POST /v1/webhooks`](#add-an-endpoint) | Adds an endpoint that receives the events. |
| [`GET /v1/webhooks`](#list-endpoints) | Lists the endpoints. |
| [`DELETE /v1/webhooks/{id}`](#delete-an-endpoint) | Deletes an endpoint. |
| [`GET /v1/webhooks/{id}/deliveries`](#list-deliveries) | Lists the latest 50 deliveries of an endpoint. |

## Add an endpoint

`POST /v1/webhooks`

Adds an endpoint, and creates the secret that signs its events. Every event goes to every endpoint.

### Body

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `url` | string | Yes | The URL that receives the events, of up to 2,048 characters. It starts with `https://` or `http://`, and may not point to an address where the metadata services of clouds answer: a link-local address, `fd00:ec2::254`, or `100.100.100.200`. |

### Answer

`201 Created`

| Field | Type | Description |
| --- | --- | --- |
| `webhook` | [webhook object](#the-webhook-object) | The new endpoint, with its `secret`, which only this answer shows. |

### Errors

| Status | When |
| --- | --- |
| 400 | The body is not JSON or has another field, or the URL is not one that the table allows. |
| 401 | The key is missing, wrong, or revoked. |
| 413 | The body is larger than 1 MB. |

### Example

```sh
# Add https://example.com/hooks/deneb as an endpoint. KEY is a key of the API.
curl -s https://video.example.com/v1/webhooks \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/hooks/deneb"}'
```

```json
{
  "webhook": {
    "id": "wh_q4kz7tq2vfa3xn5gw6hd2bpe3m",
    "url": "https://example.com/hooks/deneb",
    "secret": "whsec_3Jx8pQ2vN7mK4tL9wR6yB1cF5hD0sG2a",
    "created_at": "2026-11-06T10:50:00Z"
  }
}
```

## List endpoints

`GET /v1/webhooks`

Lists every endpoint, newest first, without their secrets.

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `webhooks` | array of [webhook objects](#the-webhook-object) | The endpoints, without `secret`. |

### Errors

| Status | When |
| --- | --- |
| 401 | The key is missing, wrong, or revoked. |

### Example

```sh
# Every endpoint, newest first. KEY is a key of the API.
curl -s https://video.example.com/v1/webhooks \
  -H "Authorization: Bearer $KEY"
```

```json
{
  "webhooks": [
    {
      "id": "wh_q4kz7tq2vfa3xn5gw6hd2bpe3m",
      "url": "https://example.com/hooks/deneb",
      "created_at": "2026-11-06T10:50:00Z"
    },
    {
      "id": "wh_3fhq7xk2mzc5dnr4tpw6ybv2le",
      "url": "https://staging.example.com/hooks/deneb",
      "created_at": "2026-11-06T10:46:30Z"
    }
  ]
}
```

## Delete an endpoint

`DELETE /v1/webhooks/{id}`

Deletes an endpoint. deneb sends it no more events.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the endpoint, starting with `wh_`. |

### Answer

`204 No Content`, without a body.

### Errors

| Status | When |
| --- | --- |
| 401 | The key is missing, wrong, or revoked. |
| 404 | The endpoint does not exist. |

## List deliveries

`GET /v1/webhooks/{id}/deliveries`

Lists the latest 50 deliveries of an endpoint, newest first. deneb keeps the deliveries for 30 days.

### Path parameters

| Parameter | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the endpoint. |

### Answer

`200 OK`

| Field | Type | Description |
| --- | --- | --- |
| `deliveries` | array of [delivery objects](#the-delivery-object) | The deliveries, newest first. |

### Errors

| Status | When |
| --- | --- |
| 401 | The key is missing, wrong, or revoked. |
| 404 | The endpoint does not exist. |

### Example

```sh
# The deliveries of an endpoint, newest first. KEY is a key of the API.
curl -s https://video.example.com/v1/webhooks/wh_q4kz7tq2vfa3xn5gw6hd2bpe3m/deliveries \
  -H "Authorization: Bearer $KEY"
```

```json
{
  "deliveries": [
    {
      "id": "whd_6tq3nbx7kfr2wzh5dmy4cpl7ve",
      "event": "video.captions_ready",
      "status": "pending",
      "attempts": 1,
      "response_status": 503,
      "last_error": "the endpoint answered 503 Service Unavailable",
      "created_at": "2026-11-06T10:58:31Z",
      "delivered_at": null
    },
    {
      "id": "whd_2m6xk4qf7vbt3yh5nczr6d3jpa",
      "event": "video.ready",
      "status": "delivered",
      "attempts": 1,
      "response_status": 200,
      "created_at": "2026-11-06T10:58:25Z",
      "delivered_at": "2026-11-06T10:58:25Z"
    }
  ]
}
```

## The webhook object

`POST /v1/webhooks` answers with the endpoint in this form. `GET /v1/webhooks` leaves out `secret`.

```json
{
  "id": "wh_q4kz7tq2vfa3xn5gw6hd2bpe3m",
  "url": "https://example.com/hooks/deneb",
  "secret": "whsec_3Jx8pQ2vN7mK4tL9wR6yB1cF5hD0sG2a",
  "created_at": "2026-11-06T10:50:00Z"
}
```

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the endpoint, starting with `wh_`. |
| `url` | string | The URL that receives the events. |
| `secret` | string | The secret that signs the events, starting with `whsec_`. Only in the answer of `POST /v1/webhooks`. |
| `created_at` | string | When the endpoint was added. |

## The delivery object

A delivery is one event on its way to one endpoint, with every attempt to send it.

```json
{
  "id": "whd_2m6xk4qf7vbt3yh5nczr6d3jpa",
  "event": "video.ready",
  "status": "delivered",
  "attempts": 1,
  "response_status": 200,
  "created_at": "2026-11-06T10:58:25Z",
  "delivered_at": "2026-11-06T10:58:25Z"
}
```

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | The ID of the delivery, starting with `whd_`. Each attempt sends it as the header `Deneb-Delivery`. |
| `event` | string | The type of the event, such as `video.ready`. |
| `status` | string | `pending` while an attempt is due, `delivered` once the endpoint answered with 2xx, or `failed` after the last attempt failed. |
| `attempts` | integer | How many attempts deneb made. |
| `response_status` | integer | The status of the last answer of the endpoint, or 0 when it gave none. |
| `last_error` | string | Why the last attempt failed. Only when it failed. |
| `created_at` | string | When the event happened. |
| `delivered_at` | string or null | When the endpoint took the event, or `null` before. |

## Events

deneb sends each event as a POST of JSON to every endpoint.

| Event | When |
| --- | --- |
| `video.ready` | A video became ready to play. |
| `video.failed` | A video failed for good, and its `error` tells why. |
| `video.captions_ready` | The captions of a video are ready. With captions on, this event follows `video.ready`. |

### Body

```json
{
  "created_at": "2026-11-06T10:58:25Z",
  "data": {
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
      "captions": { "status": "queued" },
      "created_at": "2026-11-06T10:58:20Z",
      "ready_at": "2026-11-06T10:58:25Z",
      "playback": {
        "hls_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/hls/master.m3u8",
        "poster_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/poster.jpg",
        "storyboard_url": "https://video.example.com/play/vid_7wfd5nhpwadrdxgd7laif6x7ry/1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4/storyboard.vtt",
        "embed_url": "https://video.example.com/embed/vid_7wfd5nhpwadrdxgd7laif6x7ry?token=1794049200.yA0AfKH5xhRVDS6eSfMAx1-KxPIYimj_UbPRtnMSrt4",
        "expires_at": "2026-11-07T11:00:00Z"
      }
    }
  },
  "type": "video.ready"
}
```

| Field | Type | Description |
| --- | --- | --- |
| `type` | string | The event, as the table above names it. |
| `created_at` | string | When the event happened. |
| `data.video` | [video object](/deneb/reference/api/videos/#the-video-object) | The video, as `GET /v1/videos/{id}` returns it. Its URLs are absolute only with `DENEB_PUBLIC_URL`, since no request tells deneb its address when it sends an event. |

### Headers

| Header | Description |
| --- | --- |
| `Deneb-Event` | The type of the event, such as `video.ready`. |
| `Deneb-Delivery` | The ID of the delivery, the same in each attempt, so that your app can skip an event that arrived twice. |
| `Deneb-Signature` | `t=<unix seconds>,v1=<signature>`, where the signature is the hex HMAC-SHA256 of `<t>.<body>` with the secret of the endpoint. |
| `User-Agent` | `deneb/<version>`. |

An answer from 200 to 299 within 15 seconds counts as delivered. A delivery that fails runs again after 1 minute, 5 minutes, 30 minutes, 2 hours, and 6 hours, six attempts in all.
The [Webhooks](/deneb/guides/webhooks/) guide verifies a signature, and explains which addresses webhooks do not reach.
