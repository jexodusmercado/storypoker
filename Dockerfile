FROM golang:1.26-alpine AS server-builder
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/storypoker .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=server-builder /out/storypoker /usr/local/bin/storypoker
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/storypoker"]
