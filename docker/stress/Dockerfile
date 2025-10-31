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
RUN set -x && go build -buildvcs=false -ldflags="-X main.version=${IMAGE_VERSION//\"/}" plane.watch/cmd/stress

# test binary (make sure it will run on this arch)
RUN ./stress --help

FROM alpine:latest AS dev
LABEL org.opencontainers.image.description="Plane.Watch Stress Test"

WORKDIR /app
COPY --from=build /app/stress /app

# Command to run
CMD [ "/app/stress" ]
