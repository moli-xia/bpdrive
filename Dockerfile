FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bpdrive ./cmd/bpdrive

FROM alpine:3.20

RUN addgroup -S bpdrive && adduser -S -G bpdrive bpdrive
WORKDIR /app

COPY --from=builder /out/bpdrive /app/bpdrive
COPY web ./web
COPY data/config.example.json /app/data/config.example.json

ENV BPDRIVE_ADDR=:8088
ENV BPDRIVE_DATA=/app/data

RUN chown -R bpdrive:bpdrive /app
USER bpdrive

EXPOSE 8088
VOLUME ["/app/data"]

CMD ["/app/bpdrive"]
