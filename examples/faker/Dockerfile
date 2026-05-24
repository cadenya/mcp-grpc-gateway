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
    -o /out/faker \
    ./examples/faker

FROM gcr.io/distroless/static-debian12:nonroot

EXPOSE 50051

USER nonroot:nonroot
ENTRYPOINT ["/faker"]

COPY --from=build /out/faker /faker
