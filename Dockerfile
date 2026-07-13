ARG GO_VERSION=1.25

# --- Web UI build ---
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
# postinstall (below) vendors swagger-ui-dist assets via this script, so it
# must be present before `npm ci` runs, ahead of the full `COPY web/ .` -
# keeps the dependency-install layer cacheable across unrelated source changes.
COPY web/scripts ./scripts
RUN --mount=type=cache,target=/root/.npm \
    npm ci
COPY web/ .
RUN npm run build

# --- Go build ---
FROM golang:${GO_VERSION} AS build
WORKDIR /src

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x

RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,target=. \
    CGO_ENABLED=0 go build -o /bin/server .

# --- Final image ---
FROM alpine:latest AS final

# pbzip2 would speed up the first-run index build but is gone from the
# Alpine repos; the server falls back to sequential bzip2 and the resulting
# index cache makes it a one-time cost.
RUN --mount=type=cache,target=/var/cache/apk \
    apk --update add \
    ca-certificates \
    tzdata \
    && \
    update-ca-certificates

ARG UID=10001
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    appuser
USER appuser

COPY --from=build /bin/server /server
COPY --from=web /web/dist /web

ENV DUMP_PATH=/dump \
    WEB_DIR=/web \
    PORT=9095

EXPOSE 9095

ENTRYPOINT [ "/server" ]
