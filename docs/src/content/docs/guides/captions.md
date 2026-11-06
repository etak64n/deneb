---
title: Captions
description: Write captions for every video with whisper.cpp on the CPU of the server, and choose its model and its language.
---

deneb can write captions for every video with audio, with whisper.cpp on the CPU of the server.
Captions are off by default. This page turns them on, and explains the models, the languages, and their cost in CPU time.

## Turn on captions

`DENEB_CAPTIONS=true` writes captions for every video with audio.

```yaml
# The environment of deneb in compose.yaml, with captions on.
environment:
  DENEB_CAPTIONS: "true"
```

With `docker run`, add `-e DENEB_CAPTIONS=true` before the image.
Captions run on the CPU of the server (internal/media/captions.go), so the audio of your videos goes to no other service.

## Models

- The models are tiny, base, small, medium, large-v3, and large-v3-turbo, and base is the default. `DENEB_CAPTIONS_MODEL` sets one.
- A larger model is more accurate and slower. base is a good start, and small and larger models are much slower on a CPU.
- deneb downloads the model on first use from Hugging Face (huggingface.co/ggerganov/whisper.cpp), into the models folder of the volume. The captions of the first video therefore take longer.

## Languages

deneb detects the spoken language by default. `DENEB_CAPTIONS_LANGUAGE` sets one, such as `en` or `ja`.

## CPU time

Transcription takes CPU time after each encoding.
`DENEB_WORKERS` sets how many videos deneb works on at once, and `DENEB_THREADS` how many threads each one uses.

## Where the captions appear

- The captions are a WebVTT file, the format of timed text for web players.
- The player of deneb shows them, and the API returns their URL as `captions_url` once they are ready.
- deneb sends `video.captions_ready` to your webhooks when they are ready.
- `captions.status` of the video is `none`, `queued`, `processing`, `ready`, or `failed`, and `captions.language` is the language of ready captions.
