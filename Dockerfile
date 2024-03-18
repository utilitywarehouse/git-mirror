# Build the manager binary
FROM golang:1.22-alpine as builder
ARG TARGETOS
ARG TARGETARCH

RUN os=$(go env GOOS) && arch=$(go env GOARCH) \
      && apk --no-cache add git

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY . .

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN go test -v -cover ./... \
    && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o git-mirror ./cmd/app

FROM alpine:3.18

ENV USER_ID=65532

RUN adduser -S -H -u $USER_ID git-mirror \
      && apk --no-cache add ca-certificates git openssh-client

WORKDIR /
COPY --from=builder /workspace/git-mirror .

ENV USER=git-mirror
# Setting HOME ensures git can write config file .gitconfig.
ENV HOME=/tmp

USER $USER_ID

ENTRYPOINT ["/git-mirror"]
