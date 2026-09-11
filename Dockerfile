# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/meaningforge/metis/version.Version=${VERSION} -X github.com/meaningforge/metis/version.Commit=${COMMIT} -X github.com/meaningforge/metis/version.Date=${BUILD_DATE}" \
    -o /out/metis ./cmd/metis

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S metis && adduser -S -G metis metis
COPY --from=build /out/metis /usr/local/bin/metis
COPY LICENSE NOTICE THIRD_PARTY_NOTICES /usr/share/licenses/metis/
COPY licenses/ /usr/share/licenses/metis/licenses/
RUN cd /usr/share/licenses/metis && sha256sum -c licenses/SHA256SUMS >/dev/null
USER metis
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/metis"]
CMD ["serve", "--config", "/etc/metis/metis.yaml", "--addr", ":8080"]
