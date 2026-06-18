FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dpdrive ./cmd/dpdrive

FROM alpine:3.20

RUN addgroup -S dpdrive && adduser -S -G dpdrive dpdrive
WORKDIR /app

COPY --from=builder /out/dpdrive /app/dpdrive
COPY web ./web
COPY data/config.example.json /app/data/config.example.json

ENV DPDRIVE_ADDR=:8088
ENV DPDRIVE_DATA=/app/data

RUN chown -R dpdrive:dpdrive /app
USER dpdrive

EXPOSE 8088
VOLUME ["/app/data"]

CMD ["/app/dpdrive"]
