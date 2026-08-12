# syntax=docker/dockerfile:1.7

FROM golang:1.25-bookworm AS build
WORKDIR /src/apps/api
COPY apps/api/go.mod apps/api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY apps/api ./
ARG APP_VERSION="dev"
ARG APP_COMMIT=""
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags "-s -w -X lanqin-email-api/internal/app.BuildVersion=${APP_VERSION} -X lanqin-email-api/internal/app.BuildCommit=${APP_COMMIT}" \
      -o /out/lanqin-api ./cmd/server

FROM debian:bookworm-slim
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean && \
    apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/lanqin-api /usr/local/bin/lanqin-api
COPY deploy/docker-compose.yml /usr/share/newszxcn-email/deploy/docker-compose.yml
EXPOSE 8080 465 587
CMD ["lanqin-api"]
