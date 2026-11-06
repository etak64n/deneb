---
title: Encoding
description: The qualities, the formats, the poster, and the storyboard that deneb makes of each video.
---

deneb encodes each video to HLS (HTTP Live Streaming). HLS splits a video into short segments in several qualities, and players switch between the qualities to fit the connection.
This page lists the qualities, the formats, and the pictures that deneb makes of each video.

## Qualities

deneb makes these qualities, named by the short side of the picture (internal/media/ladder.go).

- 1080p: video at 5,000 kb/s, audio at 192 kb/s
- 720p: video at 2,800 kb/s, audio at 128 kb/s
- 480p: video at 1,400 kb/s, audio at 128 kb/s
- 360p: video at 800 kb/s, audio at 96 kb/s

deneb skips each quality larger than the source, since a larger quality would cost bandwidth and show no more detail.

- A source smaller than 360p gets one quality of its own size.
- Vertical videos keep their shape. The 1080p quality of a 9:16 video is 1080 pixels wide and 1920 tall.
- deneb follows the rotation that phones record, and the shape of pixels that are not square, such as those of HDV.

## Formats

- The video is H.264 with the High profile, and the audio is AAC in stereo at 48 kHz.
- The segments are MPEG-TS, four seconds long, which every HLS player plays: Safari, hls.js, Video.js, and the players of iOS and Android.
- Key frames fall every two seconds, and the qualities line up, so a player can switch at any segment.
- deneb does not tone-map HDR. An HDR source becomes 8-bit SDR as it is, so its colors look flat.

## Poster and storyboard

- The poster is a still at a tenth of the length, at most 1280 pixels wide.
- The storyboard is one sheet of pictures of 160 by 90 pixels, one every two seconds or more, and at most 100. A WebVTT file, the format of timed text for web players, maps each moment to its picture.

## Failures

An encoding that fails for a reason that may pass, such as a bucket that did not answer, runs again after one minute and after ten minutes.
A file that is not a video fails at once.
