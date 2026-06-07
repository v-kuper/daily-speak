FROM node:22-alpine AS next-deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM next-deps AS next-build
WORKDIR /app
COPY . .
RUN mkdir -p public && npm run build

FROM golang:1.26.2-alpine AS go-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/daily-speaking-api ./cmd/api

FROM node:22-bookworm-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3001
ENV HOSTNAME=127.0.0.1
ENV APP_ADDR=:3000
ENV NEXT_UPSTREAM_URL=http://127.0.0.1:3001
ENV UPLOADS_DIR=/app/uploads
ENV WHISPER_BACKEND=openai
ENV WHISPER_PYTHON_BIN=/opt/whisper/bin/python
ENV WHISPER_OPENAI_MODEL=base.en
ENV WHISPER_OPENAI_MODEL_DIR=/app/tools/whisper/openai-models
ENV WHISPER_OPENAI_CACHE_DIR=/app/tools/whisper/cache
ENV WHISPER_FFMPEG_BIN=/usr/bin/ffmpeg
ENV WHISPER_OPENAI_DEVICE=cpu
ENV WHISPER_OPENAI_FP16=false
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 python3-venv \
  && python3 -m venv /opt/whisper \
  && /opt/whisper/bin/pip install --no-cache-dir -U pip \
  && /opt/whisper/bin/pip install --no-cache-dir -U openai-whisper \
  && rm -rf /var/lib/apt/lists/*
COPY --from=next-build /app/.next/standalone ./
COPY --from=next-build /app/.next/static ./.next/static
COPY --from=next-build /app/public ./public
COPY --from=go-build /out/daily-speaking-api ./daily-speaking-api
COPY docker-entrypoint.sh ./docker-entrypoint.sh
RUN mkdir -p /app/uploads /app/tools/whisper/openai-models /app/tools/whisper/cache \
  && chmod +x ./docker-entrypoint.sh ./daily-speaking-api
EXPOSE 3000
CMD ["./docker-entrypoint.sh"]
