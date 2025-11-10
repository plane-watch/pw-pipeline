FROM golang:1.25 AS pw-ingest-builder

# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOAMD64=v3

ARG IMAGE_VERSION=dev

WORKDIR /app

COPY . /app/
RUN go mod download
RUN go mod verify

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Build the application
RUN set -x && go build -buildvcs=false -ldflags="-X main.version=${IMAGE_VERSION//\"/}" plane.watch/cmd/pw_ingest

# test binary (make sure it will run on this arch)
RUN ./pw_ingest --help

FROM alpine:3.22.2 AS mlat-server-builder

ENV VENV=/opt/mlat-python-venv

WORKDIR /opt/mlat-server

RUN apk update && \
    apk add \
      gcc \
      git \
      musl-dev \
      py3-pip \
      python3-dev \
      && \
    git clone https://github.com/wiedehopf/mlat-server.git /opt/mlat-server && \
    rm -rf $VENV && \
    python3 -m venv $VENV && \
    source $VENV/bin/activate && \
      pip3 install -U pip && \
      pip3 install numpy scipy pykalman python-graph-core uvloop ujson Cython "setuptools<71" && \
    python3 setup.py build_ext --inplace

FROM alpine:3.22.2

ENV VENV=/opt/mlat-python-venv \
    MKL_NUM_THREADS=1 \
    NUMEXPR_NUM_THREADS=1 \
    OMP_NUM_THREADS=1 \
    S6_LOGGING=0 \
    S6_VERBOSITY=1 \
    S6_OVERLAY_VERSION=3.2.1.0

ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz /tmp
RUN tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-x86_64.tar.xz /tmp
RUN tar -C / -Jxpf /tmp/s6-overlay-x86_64.tar.xz

COPY --from=mlat-server-builder /opt/mlat-server /opt/mlat-server
COPY --from=mlat-server-builder /opt/mlat-python-venv /opt/mlat-python-venv
COPY --from=pw-ingest-builder /app/pw_ingest /usr/local/bin/pw_ingest
COPY /docker/mlat-server/rootfs/ /

RUN apk add --no-cache \
      bash \
      gawk \
      python3 \
      sed \
      && \
    $VENV/bin/python3 /opt/mlat-server/mlat-server -h && \
    /usr/local/bin/pw_ingest -h

ENTRYPOINT [ "/init" ]
