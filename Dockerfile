# syntax=docker/dockerfile:1

# Base images are pinned by tag for readability. For production, pin by
# digest as well (e.g. golang:1.27-trixie@sha256:...) so a re-pushed tag
# cannot change what you build; Dependabot/Renovate can keep digests fresh.

# ---- build stage -----------------------------------------------------------
FROM golang:1.27-trixie AS build
WORKDIR /src

# Download modules first so this layer is cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd/ cmd/
COPY internal/ internal/

# CGO disabled -> fully static binary that runs on distroless/static.
# -trimpath removes local paths; -s -w strips debug info.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime stage ---------------------------------------------------------
# distroless/static: no shell, no package manager, only CA certs, tzdata and
# /etc/passwd. The :nonroot variant runs as UID/GID 65532.
FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/server /server

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/server"]
