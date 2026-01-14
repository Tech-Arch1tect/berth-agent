#!/bin/sh
set -e

if command -v grype >/dev/null 2>&1; then
    echo "Grype already installed"
    exit 0
fi

ARCH=$(uname -m)
case "$ARCH" in
    x86_64) GRYPE_ARCH="amd64" ;;
    aarch64|arm64) GRYPE_ARCH="arm64" ;;
    *) echo "Unsupported: $ARCH"; exit 1 ;;
esac

GRYPE_VERSION=$(curl -s https://api.github.com/repos/anchore/grype/releases/latest | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')

if [ -z "$GRYPE_VERSION" ]; then
    echo "Failed to fetch grype version"
    exit 1
fi

echo "Installing grype v${GRYPE_VERSION} for linux/${GRYPE_ARCH}"

mkdir -p /usr/local/bin

curl -sSfL "https://github.com/anchore/grype/releases/download/v${GRYPE_VERSION}/grype_${GRYPE_VERSION}_linux_${GRYPE_ARCH}.tar.gz" \
    | tar -xz -C /usr/local/bin grype

chmod +x /usr/local/bin/grype
echo "Grype installed successfully"
