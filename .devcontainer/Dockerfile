FROM mcr.microsoft.com/devcontainers/base:bullseye

ARG TARGETARCH
ARG DEBIAN_FRONTEND=noninteractive

# uname typically returns either x86_64 or aarch64
# Install protoc (x86_64 || aarch_64)
RUN PB_REL="https://github.com/protocolbuffers/protobuf/releases" && \
  NAME="protoc" && VERSION="22.0" && \
  ARCH=$(uname -m); \
  if [ $ARCH = 'aarch64' ]; then ARCH='aarch_64'; fi; \
  curl -L -o ${NAME}.zip ${PB_REL}/download/v${VERSION}/${NAME}-${VERSION}-linux-${ARCH}.zip && \
  unzip ${NAME}.zip -d /tmp && \
  cp -R /tmp/include/google /usr/local/include/ && \
  chmod -R +xr /usr/local/include/google && \
  cp /tmp/bin/* /usr/local/bin/ && \
  chmod +x /usr/local/bin/${NAME} && \
  rm -f ${NAME}.zip
