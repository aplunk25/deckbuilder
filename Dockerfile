# syntax=docker/dockerfile:1

# ---- build stage ----
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
WORKDIR /src

# Download modules first so this layer is cached when only source changes.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/deckbuilder .

# ---- runtime stage ----
# distroless/static has CA certificates (needed for HTTPS to the API) and runs
# as a non-root user, with no shell or package manager.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/deckbuilder /usr/local/bin/deckbuilder

# Card cache and decks live here; mount a volume to keep them between runs.
ENV RIFTBOUND_HOME=/data
VOLUME /data

ENTRYPOINT ["deckbuilder"]
CMD ["help"]
