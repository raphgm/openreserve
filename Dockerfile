FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY node/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openreserved ./cmd/openreserved \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orctl ./cmd/orctl

FROM alpine:3.20
RUN adduser -D -u 10001 orp && apk add --no-cache ca-certificates wget && mkdir /data && chown orp /data
VOLUME /data
COPY --from=builder /out/ /usr/local/bin/
USER orp
WORKDIR /home/orp
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s CMD wget -qO- http://localhost:8080/healthz || exit 1
ENTRYPOINT ["openreserved"]
