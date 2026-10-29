FROM golang:1.26
RUN go install github.com/nats-io/natscli/nats@latest
CMD [ "nats", "sub", ">", "--server", "nats://nats:4222" ]