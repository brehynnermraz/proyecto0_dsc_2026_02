FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/worker ./cmd/worker

# distroless: sin shell ni curl para el healthcheck de Compose, por eso
# cmd/worker/main.go trae su propio modo cliente (-healthcheck) que consulta
# /healthz contra su propio servidor de salud (METRICS_ADDR, default :9090).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/worker /worker
USER nonroot:nonroot
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/worker", "-healthcheck"]
ENTRYPOINT ["/worker"]
