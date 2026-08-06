FROM docker.io/library/golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY scripts/verify-version.sh ./scripts/verify-version.sh
COPY VERSION ./VERSION

ARG VERSION
ARG VCS_REF
RUN ./scripts/verify-version.sh "${VERSION}" "${VCS_REF}" && \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X github.com/hotkhwan/llm-api/internal/buildinfo.version=${VERSION} -X github.com/hotkhwan/llm-api/internal/buildinfo.commit=${VCS_REF}" \
    -o /out/llm-api ./cmd/api

FROM scratch
ARG VERSION
ARG VCS_REF
LABEL org.opencontainers.image.source="https://github.com/hotkhwan/llm-api" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}"
USER 65532:65532
COPY --from=build --chown=65532:65532 /out/llm-api /llm-api
EXPOSE 8080
ENTRYPOINT ["/llm-api"]
