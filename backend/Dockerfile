FROM golang:1.26.2-alpine
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /daily-speaking-api ./cmd/api
CMD ["/daily-speaking-api"]
