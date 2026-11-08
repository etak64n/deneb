---
title: Webhooks
description: Receive signed events when a video is ready, when it failed, and when its captions are ready, and verify their signature.
---

deneb sends a webhook to your app when a video is ready, when it failed, and when its captions are ready.
This page adds an endpoint, describes the events and their delivery, and verifies their signature.

## Add an endpoint

The webhooks page of the dashboard adds an endpoint, and so does the API.

```sh
# Add an endpoint to deneb at https://video.example.com. KEY is a key of the API.
curl -s https://video.example.com/v1/webhooks \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com/hooks/deneb"}'
```

- The `url` starts with http:// or https://.
- The answer is 201 with `webhook`, whose `secret` appears only in this answer. Your endpoint checks the signature of each event with it.

## Addresses that webhooks do not reach

Webhooks reach any address but the metadata services of clouds, which give out the credentials of the server that asks them.
Without that rule, a key of the API could make deneb fetch them.

- The addresses are the link-local ones, 169.254.0.0/16 and fe80::/10, among them 169.254.169.254, and two outside them: fd00:ec2::254 and 100.100.100.200.
- deneb checks the address that the name of the endpoint resolves to, at each delivery (internal/app/egress.go). A URL that shows such an address fails when you add it.
- Private networks stay open, so that a receiver on the same host, or in the same Docker network, needs no setting.

## Events

deneb sends three events.

1. `video.ready`: the video is ready to play.
2. `video.failed`: the video failed, and its `error` tells why.
3. `video.captions_ready`: the captions of the video are ready. With captions on, this event follows `video.ready`.

An event is a POST of JSON, with the video as `GET /v1/videos/{id}` returns it.

```json
{
  "type": "video.ready",
  "created_at": "2026-11-06T10:58:25.921Z",
  "data": {
    "video": { "id": "vid_7wfd5nhpwadrdxgd7laif6x7ry", "title": "Product tour", "status": "ready" }
  }
}
```

The video in an event has absolute URLs only with `DENEB_PUBLIC_URL`, since no request tells deneb its address when it sends an event.

## Headers

The request of an event carries these headers.

- `Deneb-Event`: the type of the event, such as `video.ready`
- `Deneb-Delivery`: the ID of the delivery, the same in each attempt, for skipping events that arrived twice
- `Deneb-Signature`: `t=<unix seconds>,v1=<signature>`, where the signature is the hex HMAC-SHA256 of `<t>.<body>` with the secret of the endpoint
- `User-Agent`: `deneb/<version>`

## Delivery and retries

- An answer from 200 to 299 within 15 seconds counts as delivered. deneb follows no redirects, since a redirect turns a POST into a GET without its body.
- A delivery that fails runs again after 1 minute, 5 minutes, 30 minutes, 2 hours, and 6 hours, six attempts in all.
- deneb keeps the deliveries for 30 days. The webhooks page of the dashboard and `GET /v1/webhooks/{id}/deliveries` show the latest 50 of each endpoint.
- `last_error` says why an attempt failed in words of deneb: the endpoint could not be reached, did not answer within 15 seconds, answered a status other than 2xx, or is at an address that webhooks do not reach. It never repeats what the endpoint sent. The log of deneb holds the error itself.

## Verify a signature

Check the signature against the body as it arrived, before parsing it.
This function does it in Node.js.

```js
import { createHmac, timingSafeEqual } from "node:crypto";

// Checks a webhook of deneb: the secret of the endpoint, the Deneb-Signature header, and the raw body.
export function verifyDeneb(secret, signature, body) {
  // The header is "t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>">".
  const fields = new Map(signature.split(",").map((part) => part.split("=")));
  const t = fields.get("t") ?? "";
  const v1 = fields.get("v1") ?? "";
  const expected = createHmac("sha256", secret).update(`${t}.${body}`).digest("hex");
  // A signature older than five minutes may be a captured request sent again.
  const recent = Math.abs(Date.now() / 1000 - Number(t)) < 300;
  return recent && v1.length === expected.length && timingSafeEqual(Buffer.from(v1), Buffer.from(expected));
}
```

deneb signs each attempt again with the time it sends it, so a retry passes the check of age too.
