# syntax=docker/dockerfile:1.7

FROM golang:1.26.7-alpine AS build
WORKDIR /src
COPY deploy/updater/go.mod ./
COPY deploy/updater/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/newszxcn-updater .

FROM docker:28.5.2-cli
RUN apk add --no-cache ca-certificates docker-cli-compose
LABEL org.opencontainers.image.title="NewSzxcn Email updater"
COPY --from=build /out/newszxcn-updater /usr/local/bin/newszxcn-updater
EXPOSE 8080
ENTRYPOINT ["newszxcn-updater"]
