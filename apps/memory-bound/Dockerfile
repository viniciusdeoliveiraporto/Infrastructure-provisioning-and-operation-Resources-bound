# Build:  docker build -t memory-bound apps/memory-bound
# Run:    docker run --rm -p 8080:8080 memory-bound
#         docker run --rm -p 8080:8080 memory-bound -addr :8080 -admin -populate-entries 100000

FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY demo ./demo

# Static binary: no libc needed in the runtime image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM scratch

COPY --from=build /out/server /server

USER 65532:65532

EXPOSE 8080

# Arguments passed to `docker run <image> ...` replace CMD and are appended to
# the entrypoint, so every server flag is available.
ENTRYPOINT ["/server"]
CMD ["-addr", ":8080"]
