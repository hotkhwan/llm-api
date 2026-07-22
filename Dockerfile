FROM docker.io/library/golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS build

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
