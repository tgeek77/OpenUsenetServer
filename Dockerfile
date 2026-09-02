# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openusenet ./cmd/openusenet

# Ubuntu 24.04 ships Perl 5.38+, required by cleanfeed-ng.
FROM ubuntu:24.04
ARG CLEANFEED_NG_VERSION=2026-07-03-rc1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 9 news \
    && useradd --uid 9 --gid 9 --home-dir /var/lib/openusenet --create-home --shell /usr/sbin/nologin news \
    && mkdir -p /var/spool/openusenet/archive /var/spool/openusenet/cleanfeed \
    && chown -R news:news /var/lib/openusenet /var/spool/openusenet

COPY scripts/install-cleanfeed.sh /tmp/install-cleanfeed.sh
COPY scripts/cleanfeed-filter.pl /tmp/cleanfeed-filter.pl
COPY docker/cleanfeed/cleanfeed.local /tmp/cleanfeed.local
ENV CLEANFEED_NG_VERSION=${CLEANFEED_NG_VERSION} \
    CLEANFEED_LOCAL_SRC=/tmp/cleanfeed.local \
    CLEANFEED_FILTER_SRC=/tmp/cleanfeed-filter.pl
RUN chmod +x /tmp/install-cleanfeed.sh \
    && /tmp/install-cleanfeed.sh \
    && rm -rf /tmp/install-cleanfeed.sh /tmp/cleanfeed.local /tmp/cleanfeed-filter.pl \
    && chown -R news:news /usr/local/lib/cleanfeed-ng /usr/local/lib/openusenet

COPY --from=build /out/openusenet /usr/local/bin/openusenet

ENV OPENUSENET_CLEANFEED=true \
    OPENUSENET_CLEANFEED_MODE=reject \
    OPENUSENET_CLEANFEED_SCRIPT=/usr/local/lib/cleanfeed-ng/cleanfeed \
    OPENUSENET_CLEANFEED_CONFIG_DIR=/usr/local/lib/cleanfeed-ng/etc \
    OPENUSENET_CLEANFEED_COMMAND="perl /usr/local/lib/openusenet/cleanfeed-filter.pl"

USER news
EXPOSE 119 8080
ENTRYPOINT ["openusenet"]
CMD ["serve"]
