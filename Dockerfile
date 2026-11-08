FROM golang:1.27.3-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY cmd ./cmd
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/kmpoltorak/routeqtus/internal/cli.Version=${VERSION}" -o /routeqtus ./cmd/routeqtus \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /demo-backend ./cmd/demo-backend

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /routeqtus /routeqtus
COPY --from=build /demo-backend /demo-backend
USER 65532:65532
ENTRYPOINT ["/routeqtus"]
