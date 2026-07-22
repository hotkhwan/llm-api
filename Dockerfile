FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

ARG VERSION
ARG VCS_REF
RUN test -n "${VERSION}" && test -n "${VCS_REF}" && \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X github.com/hotkhwan/affiliate-api/internal/buildinfo.version=${VERSION} -X github.com/hotkhwan/affiliate-api/internal/buildinfo.commit=${VCS_REF}" \
    -o /out/affiliate-api ./cmd/api

FROM scratch
USER 65532:65532
COPY --from=build --chown=65532:65532 /out/affiliate-api /affiliate-api
EXPOSE 8080
ENTRYPOINT ["/affiliate-api"]
