---
title: Architecture
description: The path of a video through deneb, from the API to the player, and the reasons behind the design of deneb.
---

deneb takes a file, turns it into a stream in the background, and serves the stream to players under signed URLs.
This page follows a video through deneb, and gives the reasons behind its design.

## The path of a video

A video goes through six steps.

1. Your server creates a video with `POST /v1/videos`. deneb answers with the video and its upload URL, whose token works for 24 hours.
2. Your server, or the browser of your user, sends the file to the upload URL in one PUT, or with tus. The token lets a browser upload without your API key.
3. When the last part arrives, deneb records the upload and queues the video.
4. An encoder runs ffmpeg once over the file and writes every quality, then a poster and a storyboard for previews on the seek bar. It stores them, and the file itself, in the storage.
5. The video is ready, and deneb sends `video.ready` to your webhooks. With captions on, whisper.cpp then transcribes the audio, and deneb sends `video.captions_ready`.
6. Your server reads the video and gets its playback URLs. Players fetch the playlists and segments from deneb with them until they expire.

## Design choices

- deneb is one program in one image (cmd/deneb). SQLite keeps the videos, the jobs, and the keys, and the image has ffmpeg and whisper.cpp. A `docker run` with one volume is a whole deployment.
- A file arrives in one PUT to a signed URL, as files go into buckets, or with tus (internal/server/uploads.go). tus resumes a file of several gigabytes where a lost connection stopped it, rather than starting over.
- Each video has one upload, under the ID of the video, and every request about it needs its token. The file stays where it arrived until the encoder has stored it, so a restart of deneb loses no upload.
- One ffmpeg run writes every quality (internal/media/hls.go). deneb decodes the file once rather than once per quality.
- ffmpeg and whisper.cpp run at a niceness of 10, below deneb itself (internal/media/priority_unix.go). The API and playback then take the CPU first while a video encodes, and with nothing else to do, the encoders still take all of it.
- Key frames fall every two seconds, and segments are four seconds long. The qualities line up, so a player can switch at any segment.
- The segments are MPEG-TS, which every HLS player plays: Safari, hls.js, Video.js, and the players of iOS and Android.
- No quality is larger than its source (internal/media/ladder.go). A larger quality would cost bandwidth and show no more detail.
- A playback URL carries its signed token in the path (internal/app/app.go). The relative URLs inside the playlists then carry the token too.
- The playback URLs of a video stay the same for an hour, so players and CDNs can reuse what they fetched.
- Permanent URLs hold `permanent` in place of the token (internal/app/app.go). deneb reads the video on each request with them, so switching it back to expiring URLs ends them at once, unlike a signed URL that lasts until it expires.
- With S3 storage, the media go through deneb by default (internal/storage/s3.go). Browsers then need no route to the endpoint of the bucket, and the bucket needs no CORS rules, the rules that let pages of other sites read its files. `DENEB_S3_REDIRECTS=true` sends players to the bucket instead, which spares the bandwidth of deneb.
- Captions run on the CPU of the server (internal/media/captions.go). The audio of your videos goes to no other service.
- whisper.cpp in the image comes with its code for each level of the instructions of the architecture, and loads the one that its CPU runs (Dockerfile). The captions then work on any CPU of the architecture, and as fast as each allows.
- A job that fails for a reason that may pass runs again for about seven hours, and the file of a video that still fails stays for a week (internal/app/jobs.go). A long outage then costs no upload, and a retry needs no new one.
- A stop of deneb puts the jobs that ran back in the queue, without counting them as attempts. A crash counts, so that a job that brings deneb down every time fails in the end.
- Webhooks reach no link-local address, where the metadata services of clouds give out credentials (internal/app/egress.go). A key of the API then cannot make deneb fetch them, while a receiver on the same host or network needs no setting.
- The first administrator needs a setup code from the log (internal/app/auth.go). Without it, the first person to reach a new server could claim it.
- Every setting is an environment variable with a default (internal/config/config.go). deneb checks every setting at start and names each one that is wrong.
