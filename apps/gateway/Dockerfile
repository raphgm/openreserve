# Build from the repository root: docker build -f apps/gateway/Dockerfile .
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY node/ node/
COPY apps/gateway/ apps/gateway/
WORKDIR /src/apps/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 gw && mkdir /data && chown gw /data
COPY --from=build /out/gateway /usr/local/bin/
USER gw
VOLUME /data
EXPOSE 4100
ENTRYPOINT ["gateway"]
