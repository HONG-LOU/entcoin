#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || ! $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "usage: $0 VERSION" >&2
    exit 2
fi

version=$1
project=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bin="$project/build/bin"
stage=$(mktemp -d "${TMPDIR:-/tmp}/entcoin-linux-package.XXXXXX")
trap 'rm -rf "$stage"' EXIT
chmod 0755 "$stage"

command -v node >/dev/null || { echo "Node.js 22 is required" >&2; exit 1; }
project_version=$(node -p "require('$project/frontend/package.json').version")
if [[ $version != "$project_version" ]]; then
    echo "release version $version does not match project version $project_version" >&2
    exit 1
fi

command -v go >/dev/null || { echo "Go 1.26.5 is required" >&2; exit 1; }
go version | grep -q 'go1\.26\.5' || { echo "Go 1.26.5 is required" >&2; exit 1; }
command -v wails >/dev/null || { echo "Wails v2.13.0 is required" >&2; exit 1; }
wails version | grep -q 'v2\.13\.0' || { echo "Wails v2.13.0 is required" >&2; exit 1; }
command -v dpkg-deb >/dev/null || { echo "dpkg-deb is required" >&2; exit 1; }

cd "$project"
wails build -clean -trimpath -platform linux/amd64 -tags webkit2_41 -o entcoin-linux-amd64
go build -trimpath -ldflags="-s -w" -o "$bin/entcoin-cli-linux-amd64" ./cmd/entcoin
go build -trimpath -ldflags="-s -w" -o "$bin/entpay-linux-amd64" ./cmd/entpay

install -d -m 0755 \
    "$stage/DEBIAN" \
    "$stage/usr" \
    "$stage/usr/bin" \
    "$stage/usr/share" \
    "$stage/usr/share/applications" \
    "$stage/usr/share/icons" \
    "$stage/usr/share/icons/hicolor" \
    "$stage/usr/share/icons/hicolor/512x512" \
    "$stage/usr/share/icons/hicolor/512x512/apps"
install -m 0755 "$bin/entcoin-linux-amd64" "$stage/usr/bin/entcoin"
install -m 0755 "$bin/entcoin-cli-linux-amd64" "$stage/usr/bin/entcoin-cli"
install -m 0644 "$project/deploy/linux-desktop/entcoin.desktop" \
    "$stage/usr/share/applications/entcoin.desktop"
install -m 0644 "$project/build/appicon.png" \
    "$stage/usr/share/icons/hicolor/512x512/apps/entcoin.png"
install -m 0755 "$project/deploy/linux-desktop/postinst" "$stage/DEBIAN/postinst"
install -m 0755 "$project/deploy/linux-desktop/postrm" "$stage/DEBIAN/postrm"
installed_size=$(du -sk "$stage/usr" | cut -f1)
sed \
    -e "s/@VERSION@/$version/g" \
    -e "s/@INSTALLED_SIZE@/$installed_size/g" \
    "$project/deploy/linux-desktop/control" > "$stage/DEBIAN/control"

if unsafe_directory=$(find "$stage" -type d -perm /022 -print -quit) && [[ -n $unsafe_directory ]]; then
    echo "package directory is group/other writable: $unsafe_directory" >&2
    exit 1
fi

package="$bin/entcoin_${version}_amd64.deb"
dpkg-deb --build --root-owner-group "$stage" "$package"

(
    cd "$bin"
    sha256sum \
        entcoin-linux-amd64 \
        entcoin-cli-linux-amd64 \
        entpay-linux-amd64 \
        "$(basename "$package")" > SHA256SUMS-linux.txt
)

echo "Built: $bin/entcoin-linux-amd64"
echo "Built: $bin/entcoin-cli-linux-amd64"
echo "Built: $bin/entpay-linux-amd64"
echo "Built: $package"
