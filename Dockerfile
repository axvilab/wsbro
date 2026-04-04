FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o wsbro .

FROM alpine:3.22

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/wsbro .

EXPOSE 6001

ENTRYPOINT ["./wsbro"]