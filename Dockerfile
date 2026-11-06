# syntax=docker/dockerfile:1

# The dashboard and the embed page.
FROM node:24-slim AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN npm install --global "$(node -p 'require("./package.json").packageManager')" \
 && pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm exec vite build

# The deneb binary, with the dashboard inside.
FROM golang:1.27-alpine3.24 AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=web /src/web/dist/ web/dist/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/deneb ./cmd/deneb

# whisper-cli of whisper.cpp, for the captions. On x86, GGML_NATIVE=OFF turns on AVX2,
# FMA, F16C, and BMI2, which many CPUs lack, such as those of many NAS. On amd64 the
# image therefore has two builds, one with those instructions and one for any x86-64,
# and /usr/local/bin/whisper-cli runs the one that the CPU of the host can. On arm64,
# GGML_NATIVE=OFF builds for any ARMv8, and one build is enough.
FROM alpine:3.24 AS whisper
RUN apk add --no-cache build-base cmake git
ARG WHISPER_VERSION=v1.9.5
RUN git clone --depth 1 --branch "${WHISPER_VERSION}" https://github.com/ggml-org/whisper.cpp /whisper
ARG TARGETARCH
# build makes whisper-cli as /usr/local/lib/whisper/whisper-cli-<name>, with the
# options of cmake that follow the name. The build that any CPU can run starts once,
# so that a build that cannot run fails here rather than on a host.
RUN set -eu; \
    build() { \
      name=$1; shift; \
      cmake -S /whisper -B "/whisper/build-$name" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_NATIVE=OFF \
        -DGGML_OPENMP=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_SERVER=OFF "$@"; \
      cmake --build "/whisper/build-$name" --target whisper-cli --parallel "$(nproc)"; \
      install -D -m 0755 "/whisper/build-$name/bin/whisper-cli" "/usr/local/lib/whisper/whisper-cli-$name"; \
    }; \
    if [ "$TARGETARCH" = amd64 ]; then \
      build avx2; \
      build x86-64 -DGGML_SSE42=OFF -DGGML_AVX=OFF -DGGML_AVX2=OFF -DGGML_FMA=OFF -DGGML_F16C=OFF -DGGML_BMI2=OFF; \
      /usr/local/lib/whisper/whisper-cli-x86-64 --help >/dev/null; \
    else \
      build any; \
      /usr/local/lib/whisper/whisper-cli-any --help >/dev/null; \
    fi
COPY --chmod=0755 <<'EOF' /usr/local/bin/whisper-cli
#!/bin/sh
# Runs the build of whisper-cli that the CPU of the host can run. On amd64, the build
# with AVX2, FMA, F16C, and BMI2 needs all four, and the one for any x86-64 runs on
# the other CPUs. arm64 has one build.
dir=/usr/local/lib/whisper
if [ -x "$dir/whisper-cli-avx2" ]; then
  flags=" $(grep -m1 '^flags' /proc/cpuinfo) "
  for need in avx2 fma f16c bmi2; do
    case "$flags" in *" $need "*) ;; *) exec "$dir/whisper-cli-x86-64" "$@" ;; esac
  done
  exec "$dir/whisper-cli-avx2" "$@"
fi
exec "$dir/whisper-cli-any" "$@"
EOF

FROM alpine:3.24
LABEL org.opencontainers.image.title="deneb" \
      org.opencontainers.image.description="A self-hosted video API: uploads, HLS encoding, signed playback, captions, and webhooks" \
      org.opencontainers.image.source="https://github.com/etak64n/deneb" \
      org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache ffmpeg libstdc++ ca-certificates tzdata \
 && adduser -D -H -u 10001 deneb \
 && mkdir /data && chown deneb:deneb /data
COPY --from=whisper /usr/local/bin/whisper-cli /usr/local/bin/whisper-cli
COPY --from=whisper /usr/local/lib/whisper /usr/local/lib/whisper
COPY --from=server /out/deneb /usr/local/bin/deneb
USER deneb
ENV DENEB_LISTEN=:8080 \
    DENEB_DATA_DIR=/data \
    DENEB_WHISPER_PATH=/usr/local/bin/whisper-cli
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["deneb", "healthcheck"]
ENTRYPOINT ["deneb"]
