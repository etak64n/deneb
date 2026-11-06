#!/usr/bin/env bash
# Turns the recording of demo.mjs into a GIF for READMEs and an MP4 for posts, with
# the ffmpeg of a deneb image, and keeps a still every three seconds to look at.
#
# Usage: test/browser/gif.sh <image> <folder that holds demo.webm>
set -euo pipefail

image=${1:?usage: test/browser/gif.sh <image> <folder>}
dir=$(cd "${2:?usage: test/browser/gif.sh <image> <folder>}" && pwd)
[[ -f $dir/demo.webm ]] || { echo "no demo.webm in $dir" >&2; exit 1; }

ffmpeg() {
  docker run --rm --user "$(id -u):$(id -g)" -v "$dir:/work" -w /work --entrypoint ffmpeg "$image" -hide_banner -loglevel error -y "$@"
}
# An MP4 keeps every frame at a fraction of the size of a GIF.
ffmpeg -i demo.webm -c:v libx264 -pix_fmt yuv420p -crf 22 -preset slow -movflags +faststart -an demo.mp4
# A GIF of 960 pixels at 12 frames a second, with a palette made for this recording,
# which keeps the colors of the dashboard without banding.
ffmpeg -i demo.webm -vf "fps=12,scale=960:-1:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" demo.gif
ffmpeg -i demo.webm -vf "fps=1/3,scale=960:-1" still_%02d.png
ls -l "$dir"
