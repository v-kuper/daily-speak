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
ENV FFMPEG_BINARY_PATH=/usr/bin/ffmpeg
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg \
  && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/daily-speaking-api ./daily-speaking-api
COPY --from=build /out/daily-speaking-worker ./daily-speaking-worker
RUN mkdir -p /app/uploads
EXPOSE 3000
CMD ["./daily-speaking-api"]
