FROM golang:1-alpine AS builder

ADD . .

RUN go build -o /ipinfo main.go

FROM alpine:latest

COPY --from=builder /ipinfo /ipinfo

RUN apk add --no-cache ca-certificates

ENTRYPOINT ["/ipinfo"]