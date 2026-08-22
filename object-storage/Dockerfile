FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/object-storage ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates && \
    addgroup -S okf && adduser -S okf -G okf && \
    mkdir -p /data/objects && chown -R okf:okf /data/objects
COPY --from=build /out/object-storage /object-storage
USER okf:okf
EXPOSE 9000
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["wget", "-q", "-O", "/dev/null", "http://localhost:9000/healthz"]
ENTRYPOINT ["/object-storage"]
