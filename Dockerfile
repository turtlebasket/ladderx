ARG VERSION=build

FROM ghcr.io/nixos/nix:2.34.8 AS build
ARG VERSION
WORKDIR /src

COPY . .

RUN nix --extra-experimental-features "nix-command flakes" \
    --option filter-syscalls false \
    develop path:. -c sh -c \
    'ladder-assets && CGO_ENABLED=0 GOOS=linux go build -ldflags="-X ladder/handlers.version=${VERSION}" -o /tmp/ladder ./cmd'

FROM gcr.io/distroless/static-debian13:nonroot AS release

WORKDIR /app

COPY --from=build /tmp/ladder .

#EXPOSE 8080

#ENTRYPOINT ["/usr/bin/dumb-init", "--"]

ENTRYPOINT ["/app/ladder"]
