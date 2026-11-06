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
- A download that receives nothing for 2 minutes stops, and runs again with the job (internal/media/captions.go).

## Languages

deneb detects the spoken language by default. `DENEB_CAPTIONS_LANGUAGE` sets one, such as `en` or `ja`.

## CPU time

Transcription takes CPU time after each encoding.
`DENEB_WORKERS` sets how many videos deneb works on at once, and `DENEB_THREADS` how many threads each one uses.

- whisper.cpp runs at a lower priority than deneb, so the API and playback take the CPU first (internal/media/priority_unix.go).
- A transcription stops after ten times the length of the video, and at least an hour, which only one that hangs takes (internal/app/process.go).
- On amd64, the image has whisper.cpp built twice, once with AVX2 and once for any x86-64, and runs the build that the CPU can (Dockerfile). The captions then work on CPUs without AVX2 too, such as those of many NAS.

## Memory

whisper.cpp holds its model in memory while it transcribes a video.
The README of whisper.cpp gives this memory for each model.

- tiny: about 273 MB
- base: about 388 MB
- small: about 852 MB
- medium: about 2.1 GB
- large: about 3.9 GB. The README gives one figure for its large models, and none for large-v3-turbo.

Each worker that `DENEB_WORKERS` adds transcribes with a model of its own, so two workers can need twice the memory.
A limit on the memory of the container, such as `mem_limit` in compose.yaml, needs room for the model.

- Without the room, the system stops whisper.cpp when it runs out of memory.
- deneb then runs the transcription again after 1 minute, 10 minutes, 1 hour, and 6 hours (internal/app/jobs.go), and marks the captions of the video as failed after the fifth attempt.
- `POST /v1/videos/{id}/retry`, or Retry on the page of the video in the dashboard, runs failed captions again, once the container has room.

## Where the captions appear

- The captions are a WebVTT file, the format of timed text for web players.
- The player of deneb shows them, and the API returns their URL as `captions_url` once they are ready.
- deneb sends `video.captions_ready` to your webhooks when they are ready.
- `captions.status` of the video is `none`, `queued`, `processing`, `ready`, or `failed`, and `captions.language` is the language of ready captions.
