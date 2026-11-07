---
title: Overview
description: deneb is a self-hosted video API that runs as one Docker image. These pages explain how to run it, how to use its API, and how it works.
---

deneb is a self-hosted video API that runs as one Docker image.
You upload a video, and deneb encodes it to HLS in several qualities, writes captions with Whisper, and returns signed URLs to play or embed it.
These pages explain how to run deneb, how to use its API, and how it works.

The source and the releases are on [GitHub](https://github.com/etak64n/deneb). deneb is released under the MIT License.
The image also contains FFmpeg, under GPL-2.0-or-later as Alpine Linux builds it, and whisper.cpp, under MIT. deneb runs FFmpeg as a separate program, and the Whisper models that it downloads are under MIT.

## Getting started

- [Quick start](/deneb/getting-started/quickstart/): deneb on your machine with one `docker run`, and a first video in its dashboard
- [Deploy](/deneb/getting-started/deploy/): deneb on a server under your own domain, with Docker Compose behind a reverse proxy

## Guides

- [Upload videos](/deneb/guides/uploads/): a file in one PUT, with tus, which resumes after a lost connection, or from the browser of your user
- [Play videos](/deneb/guides/playback/): the URLs of a ready video, expiring or permanent, and the embed page
- [Webhooks](/deneb/guides/webhooks/): the events, their delivery and retries, and how to verify their signature
- [Captions](/deneb/guides/captions/): captions with whisper.cpp, its models and languages, and their cost in CPU time
- [S3-compatible storage](/deneb/guides/storage/): the media in a bucket of Amazon S3, Cloudflare R2, or another S3-compatible service
- [Dashboard and accounts](/deneb/guides/dashboard/): the pages of the dashboard, its one administrator, and how deneb protects them
- [Upgrade and back up](/deneb/guides/operations/): a new release, and what to copy to keep your data

## Settings

- [Configuration reference](/deneb/settings/configuration/): every environment variable, its default, and what it changes

## Reference

- [API reference](/deneb/reference/api/): every endpoint at a glance, with the authentication, the errors, and the pages of lists
- [Videos](/deneb/reference/api/videos/): create, list, get, update, delete, and retry videos, and the fields of the video object
- [Uploads](/deneb/reference/api/uploads/): the file of a video in one PUT, or with tus, and the fields of the upload object
- [Playback](/deneb/reference/api/playback/): the files of the playback URLs, the embed page, and how long the URLs work
- [Webhooks](/deneb/reference/api/webhooks/): the endpoints that receive the events, their deliveries, and the fields of the events
- [Encoding](/deneb/reference/encoding/): the qualities, the formats, the poster, and the storyboard that deneb makes

## Internals

- [Architecture](/deneb/internals/architecture/): the path of a video through deneb, and the reasons behind its design

## Project

- [Limits](/deneb/project/limits/): what deneb does not do, and the constants that set its limits
- [Development](/deneb/project/development/): building deneb from source, its tests, its releases, and the layout of its code
