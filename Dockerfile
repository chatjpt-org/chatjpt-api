FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/chatjpt-api ./cmd/chatjpt-api

FROM alpine:3.22

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S chatjpt \
    && adduser -S -G chatjpt -h /nonexistent -s /sbin/nologin chatjpt

COPY --from=build /out/chatjpt-api /usr/local/bin/chatjpt-api

USER chatjpt
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/chatjpt-api"]
CMD ["serve"]
