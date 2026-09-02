# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openusenet ./cmd/openusenet

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 9 news \
    && useradd --uid 9 --gid 9 --home-dir /var/lib/openusenet --create-home --shell /usr/sbin/nologin news \
    && mkdir -p /var/spool/openusenet/archive \
    && chown -R news:news /var/lib/openusenet /var/spool/openusenet
COPY --from=build /out/openusenet /usr/local/bin/openusenet
USER news
EXPOSE 119 8080
ENTRYPOINT ["openusenet"]
CMD ["serve"]
