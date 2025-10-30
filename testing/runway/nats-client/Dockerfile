FROM golang:1.25
RUN go install github.com/nats-io/natscli/nats@latest
CMD [ "nats", "sub", ">", "--server", "nats://nats:4222" ]