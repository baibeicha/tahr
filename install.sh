#!/usr/bin/env bash
set -e

# Tahr IDE Universal Installer for Linux and macOS
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/baibeicha/tahr/main/install.sh | bash

REPO="baibeicha/tahr"
GITHUB_URL="https://github.com/${REPO}"

# ANSI Colors
BOLD="$(tput bold 2>/dev/null || echo '')"
GREEN="$(tput setaf 2 2>/dev/null || echo '')"
CYAN="$(tput setaf 6 2>/dev/null || echo '')"
YELLOW="$(tput setaf 3 2>/dev/null || echo '')"
RED="$(tput setaf 1 2>/dev/null || echo '')"
RESET="$(tput sgr0 2>/dev/null || echo '')"

info() {
    printf "${CYAN}==>${RESET} ${BOLD}%s${RESET}\n" "$1"
}

success() {
    printf "${GREEN}✔${RESET} ${BOLD}%s${RESET}\n" "$1"
}

warn() {
    printf "${YELLOW}⚠${RESET} %s\n" "$1"
}

error() {
    printf "${RED}✖ Error:${RESET} %s\n" "$1" >&2
    exit 1
}

# 1. Detect OS
OS_RAW="$(uname -s)"
case "${OS_RAW}" in
    Linux*)     OS="linux" ;;
    Darwin*)   OS="darwin" ;;
    *)          error "Unsupported operating system: ${OS_RAW}. Tahr supports Linux and macOS." ;;
esac

# 2. Detect Architecture
ARCH_RAW="$(uname -m)"
case "${ARCH_RAW}" in
    x86_64|amd64)   ARCH="amd64" ;;
    arm64|aarch64)  ARCH="arm64" ;;
    *)              error "Unsupported architecture: ${ARCH_RAW}. Tahr supports amd64 and arm64." ;;
esac

info "Detected platform: ${BOLD}${OS}/${ARCH}${RESET}"

# 3. Detect latest release tag
LATEST_TAG=""
if command -v curl >/dev/null 2>&1; then
    LATEST_TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
elif command -v wget >/dev/null 2>&1; then
    LATEST_TAG=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
fi

TARBALL="tahr-${OS}-${ARCH}.tar.gz"

if [ -n "${LATEST_TAG}" ]; then
    info "Target release: ${BOLD}${LATEST_TAG}${RESET}"
    DOWNLOAD_URL="${GITHUB_URL}/releases/download/${LATEST_TAG}/${TARBALL}"
else
    info "Target release: ${BOLD}latest${RESET}"
    DOWNLOAD_URL="${GITHUB_URL}/releases/latest/download/${TARBALL}"
fi

# 4. Download archive
TEMP_DIR=$(mktemp -d -t tahr-install-XXXXXX)
cleanup() {
    rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT

info "Downloading ${DOWNLOAD_URL}..."
if command -v curl >/dev/null 2>&1; then
    curl -fSL "${DOWNLOAD_URL}" -o "${TEMP_DIR}/${TARBALL}" || error "Failed to download ${DOWNLOAD_URL}"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "${TEMP_DIR}/${TARBALL}" "${DOWNLOAD_URL}" || error "Failed to download ${DOWNLOAD_URL}"
else
    error "Neither curl nor wget found. Please install curl or wget."
fi

# 5. Extract binary
info "Extracting archive..."
tar -xzf "${TEMP_DIR}/${TARBALL}" -C "${TEMP_DIR}"

BINARY_PATH="${TEMP_DIR}/tahr"
if [ ! -f "${BINARY_PATH}" ]; then
    BINARY_PATH=$(find "${TEMP_DIR}" -type f -name "tahr" -perm -111 2>/dev/null | head -n 1)
fi

if [ ! -f "${BINARY_PATH}" ]; then
    error "Binary 'tahr' not found in downloaded archive."
fi

chmod +x "${BINARY_PATH}"

# 6. Determine target installation directory
INSTALL_DIR="/usr/local/bin"
USE_SUDO=0

if [ -w "${INSTALL_DIR}" ]; then
    USE_SUDO=0
elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    USE_SUDO=1
elif [ -e /dev/tty ] && command -v sudo >/dev/null 2>&1; then
    USE_SUDO=1
else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
    USE_SUDO=0
fi

info "Installing binary to ${BOLD}${INSTALL_DIR}/tahr${RESET}..."
if [ "${USE_SUDO}" -eq 1 ]; then
    if [ -e /dev/tty ]; then
        sudo mv "${BINARY_PATH}" "${INSTALL_DIR}/tahr" < /dev/tty
    else
        sudo mv "${BINARY_PATH}" "${INSTALL_DIR}/tahr"
    fi
else
    mv "${BINARY_PATH}" "${INSTALL_DIR}/tahr"
fi

success "Tahr IDE has been successfully installed to ${INSTALL_DIR}/tahr!"

# 7. Check PATH and verification
case ":${PATH}:" in
    *:"${INSTALL_DIR}":*) ;;
    *)
        warn "${INSTALL_DIR} is not in your PATH."
        printf "Add it to your shell profile (~/.bashrc, ~/.zshrc):\n"
        printf "  ${BOLD}export PATH=\"%s:\$PATH\"${RESET}\n\n" "${INSTALL_DIR}"
        ;;
esac

if command -v "${INSTALL_DIR}/tahr" >/dev/null 2>&1; then
    VER_STR=$("${INSTALL_DIR}/tahr" --version 2>/dev/null || echo "")
    if [ -n "${VER_STR}" ]; then
        printf "%s\n\n" "${VER_STR}"
    fi
fi

printf "⚡ Launch Tahr by running: ${GREEN}${BOLD}tahr${RESET}\n\n"
