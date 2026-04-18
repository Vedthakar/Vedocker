#!/usr/bin/env bash
set -euo pipefail

ALPINE_VERSION="${ALPINE_VERSION:-3.20.3}"
ARCH="${ARCH:-aarch64}"
BASE_URL="https://dl-cdn.alpinelinux.org/alpine/v3.20/releases/${ARCH}"
TARBALL="alpine-minirootfs-${ALPINE_VERSION}-${ARCH}.tar.gz"
DOWNLOAD_URL="${BASE_URL}/${TARBALL}"

INSTALL_ROOT="/var/lib/minicontainer"
IMAGE_ROOT="${INSTALL_ROOT}/images/alpine"
CACHE_DIR="${INSTALL_ROOT}/cache"
TMP_DIR="$(mktemp -d)"
ARCH_UNAME="$(uname -m)"

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

echo "==> minicontainer Alpine bootstrap"
echo "==> checking architecture"

case "${ARCH_UNAME}" in
  aarch64|arm64)
    ;;
  *)
    echo "error: this script is intended for Linux ARM64/aarch64, but found '${ARCH_UNAME}'" >&2
    exit 1
    ;;
esac

if [[ "${EUID}" -ne 0 ]]; then
  echo "error: run this script with sudo" >&2
  exit 1
fi

echo "==> creating directories"
mkdir -p "${IMAGE_ROOT}" "${CACHE_DIR}"

if [[ -x "${IMAGE_ROOT}/bin/sh" ]]; then
  echo "==> existing Alpine rootfs detected at ${IMAGE_ROOT}"
  echo "==> skipping download and extraction"
  exit 0
fi

ARCHIVE_PATH="${CACHE_DIR}/${TARBALL}"
PARTIAL_PATH="${ARCHIVE_PATH}.partial"

if [[ ! -f "${ARCHIVE_PATH}" ]]; then
  echo "==> downloading Alpine minirootfs"
  echo "==> ${DOWNLOAD_URL}"
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --proto '=https' --tlsv1.2 "${DOWNLOAD_URL}" -o "${PARTIAL_PATH}"
  elif command -v wget >/dev/null 2>&1; then
    wget --https-only --tries=3 -O "${PARTIAL_PATH}" "${DOWNLOAD_URL}"
  else
    echo "error: neither curl nor wget is installed" >&2
    exit 1
  fi
  mv "${PARTIAL_PATH}" "${ARCHIVE_PATH}"
else
  echo "==> using cached archive ${ARCHIVE_PATH}"
fi

echo "==> preparing clean rootfs directory"
rm -rf "${IMAGE_ROOT}"
mkdir -p "${IMAGE_ROOT}"

echo "==> extracting rootfs to ${IMAGE_ROOT}"
tar -xzf "${ARCHIVE_PATH}" -C "${IMAGE_ROOT}" --numeric-owner

echo "==> ensuring basic directories exist"
mkdir -p "${IMAGE_ROOT}/proc" "${IMAGE_ROOT}/dev" "${IMAGE_ROOT}/tmp"
chmod 1777 "${IMAGE_ROOT}/tmp"

echo "==> Alpine rootfs is ready"
echo "==> target: ${IMAGE_ROOT}"
