---
title: Upgrade and back up
description: Replace deneb with the image of a new release, and copy what keeps your videos, keys, and webhooks.
---

This page replaces deneb with the image of a new release, and backs up its data.
Both use the compose file of the [Deploy](/deneb/getting-started/deploy/) page.

## Upgrade

These commands replace the container with the image of the new release.

```sh
# Download the image of the tag in compose.yaml, and recreate the container with it.
docker compose pull
docker compose up -d
```

- The tag `0.3` follows the fixes of 0.3, so a release such as 0.4 needs the tag in compose.yaml changed first.
- deneb updates its database at start (internal/store/migrations).
- The jobs that were running when the old container stopped run again.
- Two deneb cannot use one data folder at once. The second one stops at start, so replace the container rather than run both.

## Back up

- The volume holds the database, deneb.db, with the videos, the keys, the webhooks, and the secret that signs the URLs.
- With local storage, the volume also holds the media, in the media folder. With S3 storage, the bucket holds them.
- Copy deneb.db while deneb is stopped. While deneb runs, SQLite keeps recent changes in deneb.db-wal next to it.
