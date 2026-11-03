# ---- build stage: full Go toolchain, discarded after compilation ----
FROM golang:1.26.7-alpine AS build
WORKDIR /src

# Copy the module files first so the dependency layer is cached until they change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api .

# ---- runtime stage: a few MB of Alpine and one static binary ----
FROM alpine:3.22
RUN adduser -D -u 10001 app
COPY --from=build /out/api /usr/local/bin/api
USER app

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["api"]
