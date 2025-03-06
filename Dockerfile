FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o loadtest main.go

FROM alpine:latest
COPY --from=builder /app/loadtest /loadtest
ENTRYPOINT ["./loadtest"]