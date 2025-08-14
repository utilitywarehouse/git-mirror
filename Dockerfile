# Build the manager binary
FROM golang:1.25-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

# '--repository' flag used to install latest git v 2.49
# can be removed once alpine is updated to 3.22
RUN apk --no-cache add git openssh-client --repository=https://dl-cdn.alpinelinux.org/alpine/edge/main

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
RUN go test -v -cover ./... && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o git-mirror

FROM alpine:3.22

ENV USER_ID=65532

RUN adduser -S -H -u $USER_ID app-user \
      && apk --no-cache add ca-certificates git openssh-client

WORKDIR /

COPY --from=builder /workspace/git-mirror .

ENV USER=app-user

USER $USER_ID

ENTRYPOINT ["/git-mirror"]
