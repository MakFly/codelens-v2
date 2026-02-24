#!/bin/sh
set -e

VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
REPO="MakFly/codelens-v2"

detect_os() {
    case "$(uname -s)" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       echo "linux" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64)   echo "amd64" ;;
        aarch64)  echo "arm64" ;;
        arm64)    echo "arm64" ;;
        *)        echo "amd64" ;;
    esac
}

get_version() {
    if [ "$VERSION" = "latest" ]; then
        curl -sL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/'
    else
        echo "$VERSION"
    fi
}

install_codelens() {
    os=$(detect_os)
    arch=$(detect_arch)
    ver=$(get_version)

    echo "Installing CodeLens v${ver} for ${os}-${arch}..."

    mkdir -p "$INSTALL_DIR"

    archive="codelens_${ver}_${os}_${arch}.tar.gz"
    url="https://github.com/${REPO}/releases/download/v${ver}/${archive}"

    echo "Downloading: $url"
    curl -sL "$url" -o "/tmp/codelens.tar.gz"

    tar -xzf "/tmp/codelens.tar.gz" -C "$INSTALL_DIR"
    rm -f "/tmp/codelens.tar.gz"

    chmod +x "$INSTALL_DIR/codelens"
    [ -f "$INSTALL_DIR/codelens-hook" ] && chmod +x "$INSTALL_DIR/codelens-hook"

    echo ""
    echo "✓ Installed to $INSTALL_DIR"
    echo ""
    echo "Add to your PATH:"
    echo "  export PATH=\$HOME/.local/bin:\$PATH"
    echo ""
    echo "Then run:"
    echo "  codelens --help"
}

install_codelens
