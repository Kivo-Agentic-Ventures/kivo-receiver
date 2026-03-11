FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /receiver ./cmd/receiver

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /receiver /receiver
COPY config.yaml /config.yaml

EXPOSE 8080
CMD ["/receiver"]
