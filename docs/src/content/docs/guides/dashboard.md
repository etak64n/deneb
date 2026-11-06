---
title: Dashboard and accounts
description: The pages of the dashboard of deneb, its one administrator, and how deneb protects the accounts, the keys, and the tokens.
---

The dashboard of deneb uploads, plays, and deletes videos, and manages the keys of the API and the webhooks.
This page describes its pages, its one administrator, and how deneb protects them.

## The first visit

On the first visit, the dashboard creates the administrator with the setup code from the log.
deneb writes the setup code to its log until the administrator exists (internal/app/auth.go). Without it, the first person to reach a new server could claim it.

## Pages

- The videos page lists every video with its state, and uploads the files that you choose or drop on the page. It uploads three files at a time, in parts of 64 MiB, and resumes them after a lost connection.
- The page of a video plays it, and shows its details, its playback URL, its embed code, and its qualities. It renames and deletes the video.
- The API keys page creates and revokes keys. A new key appears once.
- The webhooks page adds and deletes endpoints, and shows the deliveries of each. A new signing secret appears once.

## Accounts and security

- deneb has one administrator, with no other accounts or roles.
- A password needs at least 12 characters, and deneb stores it with Argon2id.
- A session lasts 30 days. Dashboard requests that change something need a header that other sites cannot send, which stops cross-site request forgery.
- An address may try to sign in 10 times in 10 minutes. deneb counts an IPv6 address by its /64 network.
- Keys of the API start with `dnb_` and appear once. deneb keeps only their SHA-256, and a key cannot create or delete keys.
- Upload and playback tokens are HMAC-SHA256 with a secret that deneb creates and keeps in its database, or `DENEB_SECRET`.
