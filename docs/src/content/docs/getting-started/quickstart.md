---
title: Quick start
description: Run deneb on your machine with Docker, create its administrator, and play a first video.
---

This page runs deneb on your machine with one `docker run`, and plays a first video in its dashboard.
It needs Docker only.

## Run deneb

These commands start deneb, and read the setup code from its log.

```sh
# Run deneb on port 8080, with its database and videos in a volume named deneb-data.
docker run -d --name deneb -p 8080:8080 -v deneb-data:/data ghcr.io/etak64n/deneb
# Read the setup code that deneb writes to its log until it has an administrator.
docker logs deneb
```

- `-d` runs the container in the background, and `--name deneb` names it for `docker logs`.
- `-p 8080:8080` publishes the port of the dashboard and the API.
- `-v deneb-data:/data` keeps the database, the uploads, and the videos when the container is replaced.
- `-e DENEB_CAPTIONS=true`, added before the image, turns on captions.

## Create the administrator

The log has a line such as this one.

```json
{"time":"2026-11-06T10:58:20Z","level":"INFO","msg":"deneb has no administrator yet; create one in the dashboard with this setup code","setup_code":"K7QM-2XPA"}
```

Open http://localhost:8080, and create the administrator with an email, a password, and the setup code.
The setup code shows that you can read the log of the server. Without it, the first person to reach a new server could claim it.

## Play a first video

Drop a video on the videos page of the dashboard. The video plays once its state is Ready.

## Next steps

- [Upload videos](/deneb/guides/uploads/) sends videos from your app through the API, with a key from the API keys page.
- [Deploy](/deneb/getting-started/deploy/) runs deneb on a server under your own domain.
