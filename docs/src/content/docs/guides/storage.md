---
title: S3-compatible storage
description: Keep the media of deneb in a bucket of Amazon S3, Cloudflare R2, or another S3-compatible service, instead of on its volume.
---

deneb can keep the media in a bucket of Amazon S3, Cloudflare R2, or another S3-compatible service, instead of on its volume.
The database stays on the volume either way.

## Settings

These settings point deneb at a bucket of Cloudflare R2.

```yaml
# Settings of deneb for a bucket of Cloudflare R2. Add them to the environment of compose.yaml.
DENEB_STORAGE: s3
DENEB_S3_ENDPOINT: https://<account-id>.r2.cloudflarestorage.com
DENEB_S3_REGION: auto
DENEB_S3_BUCKET: videos
DENEB_S3_ACCESS_KEY_ID: <access key id>
DENEB_S3_SECRET_ACCESS_KEY: <secret access key>
```

The values differ by service.

- Cloudflare R2: the endpoint `https://<account-id>.r2.cloudflarestorage.com`, and the region `auto`.
- Amazon S3: no endpoint, the region of the bucket such as `us-east-1`, and `DENEB_S3_PATH_STYLE: "false"`.
- MinIO, RustFS, and other servers that you run: their URL as the endpoint, and the region that they are set to, often `us-east-1`.

## Permissions

deneb lists the bucket at start, and stops with an error when it cannot.
The key needs to list, read, write, and delete objects in the bucket.

## How players reach the media

By default, the media go through deneb (internal/storage/s3.go).
Browsers then need no route to the endpoint of the bucket, and the bucket needs no CORS rules, the rules that let pages of other sites read its files.

With `DENEB_S3_REDIRECTS: "true"`, deneb sends players to signed URLs of the bucket for segments and pictures, which spares the bandwidth of deneb.

- Browsers then fetch from the endpoint itself, so the endpoint must be reachable from them.
- The CORS rules of the bucket must allow GET and HEAD from any origin.
- Playlists and captions still come from deneb, because the URLs inside them point back to deneb.

## Clean up incomplete uploads

A restart of deneb in the middle of storing a large file leaves the parts that it sent.
A lifecycle rule that aborts incomplete multipart uploads after a day keeps the bucket clean.
