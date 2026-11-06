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

# whisper-cli of whisper.cpp, for the captions. Its CPU backend comes as one library
# for each level of the instruction sets of the architecture, from plain x86-64 or
# ARMv8.0 up, and whisper-cli loads the best one that the CPU of the host runs. One
# build for all would need what GGML_NATIVE=OFF turns on by default, AVX2 among it,
# and fail on CPUs without it, such as those of many NAS. The libraries live in
# /usr/local/lib/whisper, where the programs look for them.
FROM alpine:3.24 AS whisper
RUN apk add --no-cache build-base cmake git
ARG WHISPER_VERSION=v1.9.5
RUN git clone --depth 1 --branch "${WHISPER_VERSION}" https://github.com/ggml-org/whisper.cpp /whisper \
 && cmake -S /whisper -B /whisper/build -DCMAKE_BUILD_TYPE=Release \
      -DBUILD_SHARED_LIBS=ON -DGGML_BACKEND_DL=ON -DGGML_CPU_ALL_VARIANTS=ON -DGGML_NATIVE=OFF \
      -DGGML_BACKEND_DIR=/usr/local/lib/whisper \
      -DCMAKE_BUILD_WITH_INSTALL_RPATH=ON -DCMAKE_INSTALL_RPATH=/usr/local/lib/whisper \
      -DGGML_OPENMP=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_SERVER=OFF \
 && cmake --build /whisper/build --target whisper-cli --parallel \
 && install -m 0755 /whisper/build/bin/whisper-cli /usr/local/bin/whisper-cli \
 && mkdir -p /usr/local/lib/whisper \
 && find /whisper/build -name '*.so*' -exec cp -P {} /usr/local/lib/whisper/ \;

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
