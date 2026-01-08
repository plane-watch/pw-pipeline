FROM alpine:3.23.2 AS certmaker

WORKDIR /ssl

COPY /docker/runway/cert.cnf /ssl/cert.cnf

RUN set -x && \
    apk add --no-cache openssl && \
    openssl genrsa -out /ssl/cert.key 2048 && \
    openssl req -new -key /ssl/cert.key -out /ssl/cert.csr -config /ssl/cert.cnf -batch && \
    openssl x509 -req -days 3650 -in /ssl/cert.csr -signkey /ssl/cert.key -out /ssl/cert.crt && \
    openssl x509 -in /ssl/cert.crt -noout -text

FROM golang:1.25 AS build

# Set necessary environmet variables needed for our image
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOAMD64=v3

ARG IMAGE_VERSION=dev

WORKDIR /app

# prefill the build steps
COPY go.mod /app
COPY go.sum /app
RUN go mod download
RUN go mod verify

# Copy the code into the container
COPY . /app/

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Build the application
RUN set -x && go build -buildvcs=false -ldflags="-X main.version=${IMAGE_VERSION//\"/}" plane.watch/cmd/runway

# test binary (make sure it will run on this arch)
RUN ./runway --help

FROM alpine:3.23.2 AS dev
LABEL org.opencontainers.image.description="Plane.Watch Runway"

WORKDIR /app

# copy in app
COPY --from=build /app/runway /app

# copy in self-signed cert
COPY --from=certmaker --chmod=600 /ssl/cert.key /app/cert.key
COPY --from=certmaker --chmod=644 /ssl/cert.crt /app/cert.crt

HEALTHCHECK --interval=15s --timeout=3s CMD wget --spider http://localhost:9602/status

# Command to run
ENTRYPOINT [ "/app/runway" ]
CMD [ "daemon" ]

FROM alpine:3.23.2
LABEL org.opencontainers.image.description="Plane.Watch Runway"

WORKDIR /app

# copy in app
COPY --from=build /app/runway /app

# copy in self-signed cert
COPY --from=certmaker --chmod=600 /ssl/cert.key /app/cert.key
COPY --from=certmaker --chmod=644 /ssl/cert.crt /app/cert.crt

HEALTHCHECK --interval=15s --timeout=3s CMD wget --spider http://localhost:9602/status

# Command to run
ENTRYPOINT [ "/app/runway" ]
CMD [ "daemon" ]
