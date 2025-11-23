FROM alpine:3.22.2 AS mlat-server-builder

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
    python3 -m venv /opt/mlat-python-venv && \
    source /opt/mlat-python-venv/bin/activate && \
      pip3 install -U pip && \
      pip3 install numpy scipy pykalman python-graph-core uvloop ujson Cython "setuptools<71" && \
    python3 setup.py build_ext --inplace

FROM alpine:3.22.2

ENV MKL_NUM_THREADS=1 \
    NUMEXPR_NUM_THREADS=1 \
    OMP_NUM_THREADS=1

COPY --from=mlat-server-builder /opt/mlat-server /opt/mlat-server
COPY --from=mlat-server-builder /opt/mlat-python-venv /opt/mlat-python-venv

RUN apk add --no-cache \
      python3 \
      && \
    /opt/mlat-python-venv/bin/python3 /opt/mlat-server/mlat-server -h

ENTRYPOINT [ "/opt/mlat-python-venv/bin/python3" ]
CMD [ "/opt/mlat-server/mlat-server", "--client-listen", "0.0.0.0:12346", "--basestation-listen", "0.0.0.0:30003", "--motd", "plane.watch", "--work-dir", "/tmp", "--status-interval", "3600" ]
