FROM golang:1.26.1-alpine AS builder

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,id=frux-go-modules,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,id=frux-go-modules,target=/go/pkg/mod \
    --mount=type=cache,id=frux-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/frux-api ./cmd/feed
RUN --mount=type=cache,id=frux-go-modules,target=/go/pkg/mod \
    --mount=type=cache,id=frux-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/frux-worker ./cmd/worker
RUN --mount=type=cache,id=frux-go-modules,target=/go/pkg/mod \
    --mount=type=cache,id=frux-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/frux-multimodal-provider ./cmd/multimodal-provider

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates ffmpeg tzdata

COPY --from=builder /out/frux-api ./frux-api
COPY --from=builder /out/frux-worker ./frux-worker
COPY --from=builder /out/frux-multimodal-provider ./frux-multimodal-provider
COPY configs ./configs

RUN mkdir -p uploads

EXPOSE 8080 8099

CMD ["./frux-api"]
