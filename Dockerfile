# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
	go build -trimpath -ldflags="-s -w" -o /out/openlocal-api ./cmd/api

RUN GOBIN=/out go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -S openlocal \
	&& adduser -S -G openlocal openlocal

WORKDIR /app

COPY --from=builder /out/openlocal-api /usr/local/bin/openlocal-api
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY db/migrations ./db/migrations
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

RUN chmod +x /usr/local/bin/docker-entrypoint.sh \
	&& chown -R openlocal:openlocal /app

EXPOSE 8080

USER openlocal

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["openlocal-api"]
