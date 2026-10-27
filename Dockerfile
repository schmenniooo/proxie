FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /proxie ./cmd/proxie

FROM gcr.io/distroless/static:nonroot
COPY --from=build /proxie /proxie
EXPOSE 8080
ENTRYPOINT ["/proxie"]
