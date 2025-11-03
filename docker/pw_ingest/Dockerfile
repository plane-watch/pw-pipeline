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
RUN set -x && go build -buildvcs=false -ldflags="-X main.version=${IMAGE_VERSION//\"/}" plane.watch/cmd/pw_ingest

# test binary (make sure it will run on this arch)
RUN ./pw_ingest --help

FROM alpine:3.22.2 AS dev
LABEL org.opencontainers.image.description="Plane.Watch Ingest"

WORKDIR /app
COPY --from=build /app/pw_ingest /app

# limit ourselves to just location updates
ENV PUBLISH_TYPES=location-updates

HEALTHCHECK --interval=15s --timeout=3s CMD wget --spider http://localhost:9602/status

# Command to run
CMD [ "/app/pw_ingest", "daemon" ]

FROM alpine:3.22.2
LABEL org.opencontainers.image.description="Plane.Watch Ingest"

WORKDIR /app
COPY --from=build /app/pw_ingest /app

# limit ourselves to just location updates
ENV PUBLISH_TYPES=location-updates

HEALTHCHECK --interval=15s --timeout=3s CMD wget --spider http://localhost:9602/status

# Command to run
CMD [ "/app/pw_ingest", "daemon" ]
