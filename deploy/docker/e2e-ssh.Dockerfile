# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS build

ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY tests/e2e/sshserver ./tests/e2e/sshserver
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/argus-e2e-ssh ./tests/e2e/sshserver

FROM alpine:3.22.1
RUN apk add --no-cache iproute2
COPY --from=build /out/argus-e2e-ssh /usr/local/bin/argus-e2e-ssh
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/argus-e2e-ssh"]
