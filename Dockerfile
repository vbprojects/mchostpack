# Production builds should override these arguments with immutable digest references.
ARG GO_IMAGE=golang:1.24-bookworm
ARG JAVA17_IMAGE=eclipse-temurin:17-jre-jammy
ARG MINECRAFT_IMAGE=itzg/minecraft-server:java21

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/hostpackd ./cmd/hostpackd

FROM ${JAVA17_IMAGE} AS java17

FROM ${MINECRAFT_IMAGE}
USER root
COPY --from=build /out/hostpackd /usr/local/bin/hostpackd
COPY --from=java17 /opt/java/openjdk /opt/java17
RUN mv /opt/java/openjdk /opt/java21 \
    && ln -s /opt/java21 /opt/java/openjdk \
    && rm -rf /data \
    && ln -s /state/runtime/current /data
COPY config /app/config
COPY --chmod=0755 scripts/container-entrypoint.sh /usr/local/bin/hostpack-entrypoint
RUN chmod 0755 /usr/local/bin/hostpackd
EXPOSE 25565/tcp
# The upstream image uses /data as WORKDIR. Here /data points into the mounted
# volume and its per-pack target does not exist until hostpackd initializes it.
# Fly must be able to establish the working directory before the entrypoint can
# run, so bootstrap from a path that always exists.
WORKDIR /
ENTRYPOINT ["/usr/local/bin/hostpack-entrypoint"]
CMD ["serve"]
