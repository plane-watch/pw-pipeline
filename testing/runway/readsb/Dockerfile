FROM alpine AS builder

RUN apk add git make gcc musl-dev zlib-dev zstd-dev ncurses-dev && \
    git clone https://github.com/wiedehopf/readsb.git && \
    cd /readsb && \
    make

FROM alpine
RUN apk add --no-cache zstd ncurses
COPY --from=builder /readsb/readsb /usr/local/bin/readsb
COPY --from=builder /readsb/viewadsb /usr/local/bin/viewadsb
COPY ./full-feed.beast /full-feed.beast
CMD [ "readsb", "--device-type", "ifile", "--ifile", "/full-feed.beast", "--throttle", "--net", "--net-connector", "stunnel,30105,beast_out" ]
