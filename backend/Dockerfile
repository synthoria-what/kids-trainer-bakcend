FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/safe-route ./cmd/server

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app \
    && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/safe-route /app/safe-route
RUN mkdir -p /app/data && chown -R app:app /app
USER app
EXPOSE 8080
ENTRYPOINT ["/app/safe-route"]
