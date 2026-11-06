---
title: Deploy
description: Run deneb on a server under your own domain, with Docker Compose behind a reverse proxy that serves the domain with TLS.
---

This page runs deneb on a server under your own domain, with Docker Compose.
A reverse proxy in front of deneb serves the domain with TLS, such as Caddy or nginx.

## The compose file

compose.yaml in the repository runs deneb behind the proxy.

```yaml
# deneb behind a reverse proxy that serves https://video.example.com with TLS and
# forwards to port 8080 of this host. Replace the address with your own.
services:
  deneb:
    # 0.4 follows the fixes of 0.4; latest follows every release.
    image: ghcr.io/etak64n/deneb:0.4
    restart: unless-stopped
    # deneb takes up to 20 seconds to finish the requests that run and to put its jobs
    # back in the queue. Docker would kill it after 10.
    stop_grace_period: 30s
    ports:
      # Only the proxy on this host reaches deneb, so the X-Forwarded-For it sets can be trusted.
      - "127.0.0.1:8080:8080"
    environment:
      DENEB_PUBLIC_URL: https://video.example.com
      DENEB_TRUST_PROXY: "true"
      DENEB_CAPTIONS: "true"
    volumes:
      - deneb-data:/data

volumes:
  deneb-data:
```

- `DENEB_PUBLIC_URL` is the address that viewers and API clients use. deneb builds every URL that it returns from it, and marks the session cookie as Secure when it starts with https.
- `DENEB_TRUST_PROXY` makes deneb read the address of each client from the headers of the proxy. The limit of sign-in attempts then counts each client rather than the proxy.
- The port listens on 127.0.0.1 only. A client that reached deneb directly could otherwise forge those headers.
- `DENEB_CAPTIONS` turns on captions. The [Captions](/deneb/guides/captions/) page explains their cost in CPU time.

## Start deneb

These commands start deneb, and read the setup code from its log.

```sh
# Start deneb in the background, and read the setup code from its log.
docker compose up -d
docker compose logs deneb
```

Open the address of `DENEB_PUBLIC_URL`, and create the administrator with an email, a password, and the setup code.

## Set up the proxy

The proxy needs two settings.

1. The proxy accepts request bodies of 64 MiB, the parts in which the dashboard uploads. nginx accepts 1 MB by default, so raise `client_max_body_size`. The proxy of Cloudflare accepts at least 100 MB.
2. The proxy forwards to `127.0.0.1:8080`, with the X-Forwarded-For, X-Forwarded-Proto, and X-Forwarded-Host headers.

## Check its health

`GET /healthz` answers 200 while deneb runs. The image checks it with `deneb healthcheck`.
