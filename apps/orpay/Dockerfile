# Build from the repository root:
#   docker build -f apps/orpay/Dockerfile --target backend .
#   docker build -f apps/orpay/Dockerfile --target web .

FROM golang:1.26-alpine AS backend-build
WORKDIR /src
COPY node/ node/
COPY apps/orpay/backend/ apps/orpay/backend/
WORKDIR /src/apps/orpay/backend
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/orpay-backend .

FROM node:22-alpine AS web-build
WORKDIR /src
COPY apps/orpay/frontend/package.json apps/orpay/frontend/package-lock.json ./
RUN npm ci
COPY apps/orpay/frontend/ ./
RUN npm run build

FROM alpine:3.20 AS backend
RUN adduser -D -u 10001 orpay && mkdir /data && chown orpay /data
COPY --from=backend-build /out/orpay-backend /usr/local/bin/
USER orpay
VOLUME /data
EXPOSE 4000
ENTRYPOINT ["orpay-backend", "-db", "/data/orpay-users.json"]

# Caddy serves the built app and terminates HTTPS.
FROM caddy:2-alpine AS web
COPY --from=web-build /src/dist /srv
COPY deploy/Caddyfile /etc/caddy/Caddyfile
