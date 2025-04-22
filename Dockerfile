FROM golang:1.23-alpine3.19 AS build

ENV CGO_ENABLED=1

RUN apk add --no-cache \
    # Important: required for go-sqlite3
    gcc \
    # Required for Alpine
    musl-dev

WORKDIR /app

COPY . /app/

RUN go mod tidy && \
    go install -ldflags='-s -w -extldflags "-static"' ./cmd/api/main.go

FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache \
    ca-certificates \
    curl \
    bash \
    sqlite \
    tzdata

COPY --from=build /go/bin/main /app/main
COPY n3.json /app/n3.json
COPY n4.json /app/n4.json
COPY n5.json /app/n5.json

CMD [ "/app/main" ]