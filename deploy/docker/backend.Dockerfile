# One image for every backend role. The final image has no shell and runs as a non-root user.
FROM golang:1.27.3-alpine AS build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /engine ./cmd/engine

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /engine /engine
ENTRYPOINT ["/engine"]
