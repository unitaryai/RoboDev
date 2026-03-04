FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o fake-agent .

FROM scratch
COPY --from=builder /build/fake-agent /fake-agent
USER 10000
ENTRYPOINT ["/fake-agent"]
