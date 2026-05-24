FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/mcp-grpc-gateway \
    ./cmd/mcp-grpc-gateway

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="mcp-grpc-gateway"
LABEL org.opencontainers.image.description="Stateless HTTP MCP gateway for reflected gRPC services"
LABEL org.opencontainers.image.source="https://github.com/cadenya/mcp-grpc-gateway"

EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/mcp-grpc-gateway"]

COPY --from=build /out/mcp-grpc-gateway /mcp-grpc-gateway
