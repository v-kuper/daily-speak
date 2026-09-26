FROM golang:1.26.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/daily-speaking-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/daily-speaking-worker ./cmd/worker

FROM debian:bookworm-slim AS runtime
WORKDIR /app
ENV APP_ADDR=:3000
ENV UPLOADS_DIR=/app/uploads
ENV WHISPER_BACKEND=openai
ENV WHISPER_PYTHON_BIN=/opt/whisper/bin/python
ENV WHISPER_OPENAI_MODEL=base
ENV WHISPER_OPENAI_MODEL_DIR=/app/tools/whisper/openai-models
ENV WHISPER_OPENAI_CACHE_DIR=/app/tools/whisper/cache
ENV WHISPER_FFMPEG_BIN=/usr/bin/ffmpeg
ENV WHISPER_OPENAI_DEVICE=cpu
ENV WHISPER_OPENAI_FP16=false
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg python3 python3-venv \
  && python3 -m venv /opt/whisper \
  && /opt/whisper/bin/pip install --no-cache-dir -U pip openai-whisper \
  && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/daily-speaking-api ./daily-speaking-api
COPY --from=build /out/daily-speaking-worker ./daily-speaking-worker
RUN mkdir -p /app/uploads /app/tools/whisper/openai-models /app/tools/whisper/cache
EXPOSE 3000
CMD ["./daily-speaking-api"]
