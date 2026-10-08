FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openreception ./cmd/server

FROM alpine:3.22

RUN addgroup -S assistant && adduser -S -G assistant assistant && mkdir -p /data && chown assistant:assistant /data
COPY --from=build /out/openreception /usr/local/bin/openreception
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /zoneinfo.zip

ENV PORT=3000
ENV ZONEINFO=/zoneinfo.zip
EXPOSE 3000
VOLUME ["/data"]

USER assistant

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:3000/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/openreception"]
