#!/usr/bin/env bash
# Package the Go SDK's runtime bundle for one platform: the native core
# (libsmol_go) beside the boot helper, hypervisor libraries and guest rootfs
# that the npm platform package carries (sdk/node/native/<platform>/, from
# sdk/node/scripts/bundle-native.sh). The SDK downloads this tarball from the
# smol release on first local use.
#
#   bundle-runtime.sh <native-dir> <libsmol_go> <platform> <out-dir>
#
# <platform> is darwin-arm64, linux-x64, linux-arm64 or windows-x64. Writes
# <out-dir>/smol-go-runtime-<version>-<platform>.tar.gz, flat.
#
# windows-x64: <native-dir> holds smol-vmm.exe (the smolvm binary renamed),
# krun.dll, libkrunfw.dll and the guest rootfs; <libsmol_go> is smol_go.dll.
# Run it from Git Bash or MSYS2. The bundle must contain no symlinks: creating
# one on Windows needs Developer Mode or elevation, so the SDK could not unpack
# it for an ordinary user.
set -euo pipefail

[ $# -eq 4 ] || { echo "usage: $0 <native-dir> <libsmol_go> <platform> <out-dir>" >&2; exit 2; }
NATIVE="$1"; LIB="$2"; PLAT="$3"; OUT="$4"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(sed -nE 's/^const Version = "([^"]+)"/\1/p' "$SCRIPT_DIR/../assets.go")"
[ -n "$VERSION" ] || { echo "no Version in assets.go" >&2; exit 1; }

HELPER=smol-vmm
case "$PLAT" in
  darwin-arm64) LIBNAME=libsmol_go.dylib ;;
  linux-x64|linux-arm64) LIBNAME=libsmol_go.so ;;
  windows-x64) LIBNAME=smol_go.dll; HELPER=smol-vmm.exe ;;
  *) echo "unsupported platform: $PLAT" >&2; exit 1 ;;
esac

for f in "$NATIVE/$HELPER" "$LIB"; do
  [ -f "$f" ] || { echo "missing $f" >&2; exit 1; }
done
[ -f "$NATIVE/agent-rootfs.tar.zst" ] || [ -f "$NATIVE/agent-rootfs.tar" ] \
  || { echo "missing $NATIVE/agent-rootfs.tar.zst (or .tar)" >&2; exit 1; }

NAME="smol-go-runtime-$VERSION-$PLAT"
STAGE="$(mktemp -d)/$NAME"
mkdir -p "$STAGE" "$OUT"
# -R copies subdirectories such as licenses/; -P keeps
# libkrunfw.dylib -> libkrunfw.5.dylib a symlink.
cp -RP "$NATIVE"/* "$STAGE/"
# The npm package's addon is not part of the Go runtime.
rm -f "$STAGE"/*.node
cp "$LIB" "$STAGE/$LIBNAME"
chmod 755 "$STAGE/$LIBNAME" "$STAGE/$HELPER"
if [ "$PLAT" = windows-x64 ]; then
  for f in krun.dll libkrunfw.dll; do
    [ -f "$STAGE/$f" ] || { echo "missing $NATIVE/$f" >&2; exit 1; }
  done
  if [ -n "$(find "$STAGE" -type l)" ]; then
    echo "windows bundle must not contain symlinks:" >&2
    find "$STAGE" -type l >&2
    exit 1
  fi
fi

tar -C "$STAGE" -czf "$OUT/$NAME.tar.gz" .
echo "$OUT/$NAME.tar.gz ($(du -h "$OUT/$NAME.tar.gz" | cut -f1))"
tar -tzf "$OUT/$NAME.tar.gz" | sed 's/^/  /'
