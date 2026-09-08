#!/usr/bin/env bash
# Inspect the actual read-only image, not just the source bundle or DMG exit code.
set -euo pipefail

dmg="${1:?DMG path required}"
version="${2:?numeric version required}"
arch="${3:?architecture required}"
[ "$(uname -s)" = Darwin ] || { echo 'DMG verification requires macOS' >&2; exit 1; }
hdiutil verify "$dmg"
mountpoint="$(mktemp -d "${TMPDIR:-/tmp}/orca-dmg-check.XXXXXX")"
attached=false
cleanup() {
	local status=$?
	trap - EXIT
	if [ "$attached" = true ]; then
		hdiutil detach "$mountpoint" >/dev/null || status=1
	fi
	rmdir "$mountpoint" || status=1
	exit "$status"
}
trap cleanup EXIT
hdiutil attach -readonly -nobrowse -mountpoint "$mountpoint" "$dmg" >/dev/null
attached=true
app="$mountpoint/O.R.C.A.app"
plist="$app/Contents/Info.plist"
plutil -lint "$plist"
[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$plist")" = "$version" ]
[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "$plist")" = "$version" ]
[ "$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$plist")" = Orca ]
[ -x "$app/Contents/MacOS/Orca" ]
[ -s "$app/Contents/Resources/iconfile.icns" ]
[ -L "$mountpoint/Applications" ]
[ "$(readlink "$mountpoint/Applications")" = /Applications ]
case "$arch" in
	universal) lipo -verify_arch x86_64 arm64 "$app/Contents/MacOS/Orca" ;;
	amd64) lipo -verify_arch x86_64 "$app/Contents/MacOS/Orca" ;;
	arm64) lipo -verify_arch arm64 "$app/Contents/MacOS/Orca" ;;
	*) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac
codesign --verify --deep --strict "$app"
echo "Verified DMG bundle: $version ($arch), plist, executable, icon, Applications link and code signature"
