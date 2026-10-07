# syntax=docker/dockerfile:1
# Bridge API, worker and migrations: one static binary on a distroless base.

FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY apps/api/ ./
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/bridge ./cmd/bridge

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/bridge /usr/local/bin/bridge
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/bridge"]
CMD ["serve"]
