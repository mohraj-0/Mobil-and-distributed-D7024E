FROM golang:1.22.1 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY kademlia ./kademlia
COPY metadata ./metadata
COPY registry ./registry

RUN CGO_ENABLED=0 GOOS=linux go build -o kadlab ./cmd/simshell

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/kadlab .
COPY docker/kadlab-shell.sh /usr/local/bin/kadlab-shell
RUN sed -i 's/\r$//' /usr/local/bin/kadlab-shell && chmod +x /usr/local/bin/kadlab-shell && mkdir -p /etc/kadlab

CMD ["./kadlab"]
