# Build a static reference-server binary in a reproducible Go toolchain image.
FROM golang:1.23.2-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ascp-server ./cmd/ascp-server

# Distroless contains CA certificates and runs as an unprivileged user. The
# reference server itself has no shell or package manager in the final image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ascp-server /ascp-server
ENV ASCP_ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/ascp-server"]
