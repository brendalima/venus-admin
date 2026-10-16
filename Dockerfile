# syntax=docker/dockerfile:1

# ---------- build ----------
FROM golang:1.27-bookworm AS build

WORKDIR /src

# Dependencies in their own layer: only rebuilt when go.mod/go.sum change.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# CGO is off while there is no libvips (see the note at the end).
# -trimpath keeps build machine paths out of the binary.
# -s -w drop the symbol table and DWARF: a much smaller binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/server ./cmd/server

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /server

# The unprivileged user comes from the :nonroot tag.
EXPOSE 8080
ENTRYPOINT ["/server"]

# ---------------------------------------------------------------------
# When image processing with govips lands, this image changes, because
# libvips is a C library and needs cgo:
#
#   build:    FROM golang:1.27-bookworm
#             RUN apt-get update && apt-get install -y libvips-dev
#             RUN CGO_ENABLED=1 go build ...
#
#   runtime:  FROM debian:bookworm-slim   (distroless/static will not do:
#             RUN apt-get install -y libvips42   the binary stops being static)
#
# That takes the image from ~20 MB to ~150 MB. The cgo-free alternative is
# in the plan: resize with disintegration/imaging and write optimised JPEG,
# losing ~25% compression against WebP.
# ---------------------------------------------------------------------
