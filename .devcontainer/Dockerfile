# See here for image contents: https://github.com/microsoft/vscode-dev-containers/tree/v0.202.5/containers/go/.devcontainer/base.Dockerfile

# [Choice] Go version (use -bullseye variants on local arm64/Apple Silicon): 1, 1.16, 1.17, 1-bullseye, 1.16-bullseye, 1.17-bullseye, 1-buster, 1.16-buster, 1.17-buster
ARG VARIANT="1.18-bullseye"
FROM mcr.microsoft.com/vscode/devcontainers/go:0-${VARIANT}

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive

# [Choice] Node.js version: none, lts/*, 16, 14, 12, 10
ARG NODE_VERSION="none"
RUN if [ "${NODE_VERSION}" != "none" ]; then su vscode -c "umask 0002 && . /usr/local/share/nvm/nvm.sh && nvm install ${NODE_VERSION} 2>&1"; fi

# [Optional] Uncomment this section to install additional OS packages.
# RUN apt-get update && export DEBIAN_FRONTEND=noninteractive \
#     && apt-get -y install --no-install-recommends protobuf-compiler

# Install buf
RUN BIN="/usr/local/bin" && \
  VERSION="1.0.0-rc10" && \
  BINARY_NAME="buf" && \
  curl -sSL \
  "https://github.com/bufbuild/buf/releases/download/v${VERSION}/${BINARY_NAME}-$(uname -s)-$(uname -m)" \
  -o "${BIN}/${BINARY_NAME}" && \
  chmod +x "${BIN}/${BINARY_NAME}"

# we get x86_64 and aarch64
# Install protoc (x86_64 || aarch_64)
RUN PB_REL="https://github.com/protocolbuffers/protobuf/releases" && \
  VERSION="21.5" && \
  ARCH=$(uname -m); \
  if [ $ARCH = 'aarch64' ]; then ARCH='aarch_64'; fi; \
  curl -curl -LO $PB_REL/download/v${VERSION}/protoc-${VERSION}-linux-${ARCH}.zip && \
  unzip protoc-${VERSION}-linux-${ARCH}.zip -d /tmp && \
  cp -R /tmp/include/google /usr/local/include/ && \
  chmod -R +xr /usr/local/include/google && \
  cp /tmp/bin/* /usr/local/bin/ && \
  chmod +x /usr/local/bin/protoc && \
  rm -f protoc-${VERSION}-linux-${ARCH}.zip

USER vscode

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28.1
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2

RUN sudo chmod g+w -R /go
