FROM golang:1.26.1-alpine AS builder

ARG GOPROXY

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/frux-api ./cmd/feed
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/frux-worker ./cmd/worker

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates ffmpeg tzdata

COPY --from=builder /out/frux-api ./frux-api
COPY --from=builder /out/frux-worker ./frux-worker
COPY configs ./configs

RUN mkdir -p uploads

EXPOSE 8080

CMD ["./frux-api"]
