# syntax=docker/dockerfile:1

# ---------- build ----------
FROM golang:1.27-bookworm AS build

WORKDIR /src

# Dependencies in their own layer: only rebuilt when go.mod/go.sum change.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# CGO is off: photos are processed in pure Go (internal/storage), so the
# binary is static and runs on distroless.
# -trimpath keeps build machine paths out of the binary.
# -s -w drop the symbol table and DWARF: a much smaller binary.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/ ./cmd/api ./cmd/migrate ./cmd/adduser

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/api /out/migrate /out/adduser /

# The unprivileged user comes from the :nonroot tag.
# CMD rather than ENTRYPOINT so Fly's release_command ("/migrate") and
# docker compose can swap the binary.
EXPOSE 8080
CMD ["/api"]

# ---------------------------------------------------------------------
# Photos are resized in pure Go and written as JPEG. Moving to libvips
# (govips) for WebP would make files about a quarter smaller, at the cost
# of cgo and this image:
#
#   build:    RUN apt-get update && apt-get install -y libvips-dev
#             RUN CGO_ENABLED=1 go build ...
#
#   runtime:  FROM debian:bookworm-slim   (distroless/static will not do:
#             RUN apt-get install -y libvips42   the binary stops being static)
#
# That takes the image from ~20 MB to ~150 MB. Only the ImageProcessor
# adapter would change.
# ---------------------------------------------------------------------
