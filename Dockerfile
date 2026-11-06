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

# whisper-cli of whisper.cpp, for the captions. GGML_NATIVE=OFF builds for any CPU
# of the architecture rather than only for the CPU of the build machine.
FROM alpine:3.24 AS whisper
RUN apk add --no-cache build-base cmake git
ARG WHISPER_VERSION=v1.9.5
RUN git clone --depth 1 --branch "${WHISPER_VERSION}" https://github.com/ggml-org/whisper.cpp /whisper \
 && cmake -S /whisper -B /whisper/build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
      -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_SERVER=OFF \
 && cmake --build /whisper/build --target whisper-cli --parallel \
 && install -m 0755 /whisper/build/bin/whisper-cli /usr/local/bin/whisper-cli

FROM alpine:3.24
LABEL org.opencontainers.image.title="deneb" \
      org.opencontainers.image.description="A self-hosted video API: uploads, HLS encoding, signed playback, captions, and webhooks" \
      org.opencontainers.image.source="https://github.com/etak64n/deneb" \
      org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache ffmpeg libstdc++ ca-certificates tzdata \
 && adduser -D -H -u 10001 deneb \
 && mkdir /data && chown deneb:deneb /data
COPY --from=whisper /usr/local/bin/whisper-cli /usr/local/bin/whisper-cli
COPY --from=server /out/deneb /usr/local/bin/deneb
USER deneb
ENV DENEB_LISTEN=:8080 \
    DENEB_DATA_DIR=/data \
    DENEB_WHISPER_PATH=/usr/local/bin/whisper-cli
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["deneb", "healthcheck"]
ENTRYPOINT ["deneb"]
