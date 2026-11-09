FROM golang:1.27.3-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY cmd ./cmd
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/kmpoltorak/routetus/internal/cli.Version=${VERSION}" -o /routetus ./cmd/routetus \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /demo-backend ./cmd/demo-backend

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /routetus /routetus
USER 65532:65532
ENTRYPOINT ["/routetus"]

FROM scratch AS demo
COPY --from=build /demo-backend /demo-backend
USER 65532:65532
ENTRYPOINT ["/demo-backend"]

FROM build AS ui-build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /routetus-ui ./cmd/routetus-ui

FROM scratch AS ui
COPY --from=ui-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=ui-build /routetus-ui /routetus-ui
USER 65532:65532
ENTRYPOINT ["/routetus-ui"]

FROM runtime AS production
