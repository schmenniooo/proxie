FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Shown by `proxie version`; the release workflow sets both.
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/schmenniooo/proxie/internal/cli.version=${VERSION} -X github.com/schmenniooo/proxie/internal/cli.commit=${COMMIT}" \
    -o /proxie ./cmd/proxie

FROM gcr.io/distroless/static:nonroot
# On PATH (distroless sets /usr/local/bin), so `proxie doctor` works in the
# container.
COPY --from=build /proxie /usr/local/bin/proxie
EXPOSE 8080
# The CLI is the entrypoint: `docker run <image> doctor` runs another command.
ENTRYPOINT ["/usr/local/bin/proxie"]
CMD ["serve"]
