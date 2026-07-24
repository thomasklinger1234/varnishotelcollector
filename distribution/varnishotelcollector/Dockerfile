ARG VARNISH_VERSION

FROM varnish:${VARNISH_VERSION}

USER root

RUN set -ex; \
    curl -s http://pki.ottogroup.com/Ottogroup-Root-CA-v01.pem.txt > /usr/local/share/ca-certificates/Ottogroup-Root-CA-v01.crt; \
    curl -s http://pki.ottogroup.com/Ottogroup-Issuing0201-CA-v01.crt > /usr/local/share/ca-certificates/Ottogroup-Issuing0201-CA-v01.crt; \
    update-ca-certificates --fresh;

RUN set -e; \
    apt-get update -y; \
    apt-get install -y \
        make \
        golang-go \
        varnish-dev

RUN go env -w GOCACHE=/go-cache
RUN go env -w GOMODCACHE=/gomod-cache

WORKDIR /src

COPY go.work .
COPY go.mod .
COPY internal internal
COPY package_test.go package_test.go
COPY distribution distribution
COPY receiver receiver
COPY Makefile Makefile
COPY Makefile.Common Makefile.Common

RUN --mount=type=cache,target=/go-cache --mount=type=cache,target=/gomod-cache \
  make gomoddownload

RUN --mount=type=cache,target=/go-cache --mount=type=cache,target=/gomod-cache \
  make distribution

FROM varnish:${VARNISH_VERSION}

COPY --from=0 /src/build/varnishotelcollector /usr/sbin/varnishotelcollector
COPY distribution/varnishotelcollector/config.yaml /etc/varnishotelcollector/config.yaml

ENTRYPOINT [ "/usr/sbin/varnishotelcollector" ]
CMD ["--config", "/etc/varnishotelcollector/config.yaml" ]
