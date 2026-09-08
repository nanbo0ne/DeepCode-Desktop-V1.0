#!/usr/bin/env bash
# Build and package the Wails desktop app for one platform. Wails cannot
# cross-compile a CGO+webview binary, so this runs on a native runner per target
# (see .github/workflows/release-desktop.yml) and is invoked once per matrix entry.
#
# Output lands in <repo>/dist/ with stable, platform-keyed names that
# desktop/cmd/sign's `manifest` subcommand maps back to update.PlatformKey:
#   macOS:   O.R.C.A-macos-universal.dmg                     (manifest/release artifact)
#            O.R.C.A-darwin-<arch>.zip                       (auxiliary app archive)
#   Windows: O.R.C.A-for-Windows-windows-<arch>-installer.exe (NSIS per-user installer)
#            O.R.C.A-for-Windows-windows-<arch>.zip           (portable human download)
#   Linux:   O.R.C.A-linux-<arch>.deb                        (manifest/release artifact)
#            O.R.C.A-linux-<arch>.tar.gz                     (auxiliary bare binary archive)
#
# Usage: scripts/desktop-build.sh <os/arch> <version> [channel]
#   e.g. scripts/desktop-build.sh darwin/arm64 v1.1.0
#        scripts/desktop-build.sh darwin/arm64 v1.5.0-canary.20260608.42 canary
set -euo pipefail

PLATFORM="${1:?usage: desktop-build.sh <os/arch> <version> [channel]}"
VERSION="${2:?usage: desktop-build.sh <os/arch> <version> [channel]}"
CHANNEL="${3:-stable}"

os="${PLATFORM%/*}"
arch="${PLATFORM#*/}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APPNAME="O.R.C.A"                  # user-facing app/bundle name
BINNAME="Orca"                      # wails.json outputfilename -> linux binary name
ARTIFACT_BASE="O.R.C.A"
[ "$os" = windows ] && ARTIFACT_BASE="O.R.C.A-for-Windows"

# Windows payloads are release inputs, not copies of whichever runtime happens
# to be first on PATH. Keep the Node distribution pinned to the same major line
# used by the release workflow, and verify the official archive before reading
# anything from it. These values come from Node's v22.23.2 SHASUMS256.txt.
NODE_VERSION="v22.23.2"
NODE_RELEASE_BASE="https://nodejs.org/dist/${NODE_VERSION}"

verify_sha256() {
	local file="$1"
	local expected="$2"
	local actual
	actual="$(sha256sum "$file" | awk '{print tolower($1)}')"
	[ "$actual" = "$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')" ] || {
		echo "SHA-256 mismatch for $file: got $actual, expected $expected" >&2
		return 1
	}
}

resolve_absolute_path() {
	local target="$1"
	if [ -d "$target" ]; then
		(cd -P -- "$target" && pwd)
		return
	fi
	local parent base
	parent="$(dirname -- "$target")"
	base="$(basename -- "$target")"
	(cd -P -- "$parent" && printf '%s/%s\n' "$(pwd)" "$base")
}

assert_within_dir() {
	local target="$1"
	local root="$2"
	local absolute_target absolute_root
	absolute_root="$(cd -P -- "$root" && pwd)" || {
		echo "cannot resolve safety root: $root" >&2
		return 1
	}
	absolute_target="$(resolve_absolute_path "$target")" || {
		echo "cannot resolve packaging target: $target" >&2
		return 1
	}
	case "$absolute_target" in
		"$absolute_root"/*) return 0 ;;
		*)
			echo "refusing packaging operation outside $absolute_root: $absolute_target" >&2
			return 1
			;;
	esac
}

prepare_windows_installer_resources() {
	[ "$os" = windows ] || return 0

	local res="$ROOT/desktop/build/windows/installer/resources"
	local payload="$ROOT/desktop/build/windows/installer-go/payload"
	mkdir -p "$res"
	assert_within_dir "$res" "$ROOT/desktop/build/windows"
	assert_within_dir "$ROOT/desktop/build/windows/installer-go" "$ROOT/desktop/build/windows"
	mkdir -p "$ROOT/desktop/build/windows/installer-go"
	assert_within_dir "$payload" "$ROOT/desktop/build/windows/installer-go"
	mkdir -p "$payload"

	local node_arch node_archive node_archive_sha node_binary_sha
	case "$arch" in
	amd64)
		node_arch="x64"
		node_archive_sha="1177b4137ba5adaa56354ae40f1080c7450e8ae09cecb47da459d1c52ac99f97"
		node_binary_sha="0d0f5e39f9f3d9587bc19f73eab3c2c9c4903fd02d6dbf9c853dd81b3d95fad4"
		;;
	arm64)
		node_arch="arm64"
		node_archive_sha="fec025a6da31757e3b6af84c5a1628e9d38442ca99a2161091d78f2fcfa35ef3"
		node_binary_sha="97cce5301a815d2dce07ac5bfd1e6039eae88185ec1d10ae4f8cb712f1732878"
		;;
	*)
		echo "unsupported Windows architecture for pinned Node runtime: $arch" >&2
		return 1
		;;
	esac
	node_archive="node-${NODE_VERSION}-win-${node_arch}.zip"
	local node_dest="$payload/node.exe"
	local node_license_dest="$payload/LICENSE.node.txt"
	local cache_root="${ROOT}/.tmp"
	if [ -n "${RUNNER_TEMP:-}" ]; then
		cache_root="$(cygpath -u "$RUNNER_TEMP")"
	fi
	local node_cache_dir="${ORCA_NODE_CACHE_DIR:-$cache_root/orca-node-cache}"
	local node_zip="$node_cache_dir/$node_archive"
	mkdir -p "$node_cache_dir"
	if [ ! -f "$node_zip" ] || ! verify_sha256 "$node_zip" "$node_archive_sha"; then
		rm -f -- "$node_zip"
		echo "==> fetching official Node ${NODE_VERSION} (${node_arch})"
		curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --retry-all-errors \
			"${NODE_RELEASE_BASE}/${node_archive}" -o "$node_zip"
		verify_sha256 "$node_zip" "$node_archive_sha"
	fi

	# Re-extract from the verified archive on every build so an edited or damaged
	# installed payload can never be silently repackaged.
	local node_extract="$payload/.node-extract"
	assert_within_dir "$node_extract" "$payload"
	rm -rf -- "$node_extract"
	mkdir -p "$node_extract"
	powershell.exe -NoProfile -NonInteractive -Command \
		"\$ErrorActionPreference = 'Stop'; Expand-Archive -Force -LiteralPath '$(cygpath -w "$node_zip")' -DestinationPath '$(cygpath -w "$node_extract")'"
	local node_root="$node_extract/node-${NODE_VERSION}-win-${node_arch}"
	[ -f "$node_root/node.exe" ] || { echo "Node archive layout not recognized" >&2; exit 1; }
	[ -f "$node_root/LICENSE" ] || { echo "Node distribution LICENSE missing from archive" >&2; exit 1; }
	cp "$node_root/node.exe" "$node_dest"
	cp "$node_root/LICENSE" "$node_license_dest"
	assert_within_dir "$node_extract" "$payload"
	rm -rf -- "$node_extract"
	verify_sha256 "$node_dest" "$node_binary_sha"
	grep -Fq "Node.js is licensed" "$node_license_dest" || { echo "unexpected Node LICENSE content" >&2; exit 1; }
	grep -Fq "Permission is hereby granted" "$node_license_dest" || { echo "unexpected Node LICENSE content" >&2; exit 1; }

	local cg_dest="$payload/codegraph"
	local codegraph_arch="$node_arch"
	local version go_version asset codegraph_sha codegraph_line
	version="$(awk '/^CODEGRAPH_VERSION[[:space:]]*:=/ { print $3; exit }' "$ROOT/Makefile")"
	[ -n "$version" ] || version="$(awk -F'"' '/Version = / { print $2; exit }' "$ROOT/internal/codegraph/install.go")"
	[ -n "$version" ] || { echo "CODEGRAPH_VERSION not found" >&2; exit 1; }
	go_version="$(awk -F'"' '/Version = / { print $2; exit }' "$ROOT/internal/codegraph/install.go")"
	[ "$version" = "$go_version" ] || { echo "CodeGraph version drift: Makefile=$version Go=$go_version" >&2; exit 1; }
	asset="codegraph-win32-${codegraph_arch}.zip"
	codegraph_line="$(grep -F "\"$asset\"" "$ROOT/internal/codegraph/checksums.go" | head -n1 || true)"
	codegraph_sha="$(printf '%s\n' "$codegraph_line" | sed -nE 's/.*"([0-9a-fA-F]{64})".*/\1/p')"
	[ -n "$codegraph_sha" ] || { echo "CodeGraph checksum not found for $asset" >&2; exit 1; }
	local codegraph_cache_dir="${ORCA_CODEGRAPH_CACHE_DIR:-$cache_root/orca-codegraph-cache}"
	local zip="$codegraph_cache_dir/$asset"
	mkdir -p "$codegraph_cache_dir"
	if [ ! -f "$zip" ] || ! verify_sha256 "$zip" "$codegraph_sha"; then
		rm -f -- "$zip"
		echo "==> fetching immutable CodeGraph ${version} ($asset)"
		curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --retry-all-errors \
			"https://github.com/colbymchenry/codegraph/releases/download/${version}/${asset}" -o "$zip"
		verify_sha256 "$zip" "$codegraph_sha"
	fi

	# Re-extract the complete tree every build. This makes the verified archive,
	# rather than an editable installed directory, the package source of truth.
	local codegraph_extract="$payload/.codegraph-extract"
	assert_within_dir "$cg_dest" "$payload"
	assert_within_dir "$codegraph_extract" "$payload"
	rm -rf -- "$cg_dest" "$codegraph_extract"
	mkdir -p "$codegraph_extract"
	powershell.exe -NoProfile -NonInteractive -Command \
		"\$ErrorActionPreference = 'Stop'; Expand-Archive -Force -LiteralPath '$(cygpath -w "$zip")' -DestinationPath '$(cygpath -w "$codegraph_extract")'"
	local extracted
	extracted="$(find "$codegraph_extract" -mindepth 1 -maxdepth 1 -type d | head -n1)"
	[ -n "$extracted" ] || { echo "CodeGraph archive layout not recognized" >&2; exit 1; }
	assert_within_dir "$extracted" "$codegraph_extract"
	assert_within_dir "$cg_dest" "$payload"
	mv -- "$extracted" "$cg_dest"
	assert_within_dir "$codegraph_extract" "$payload"
	rm -rf -- "$codegraph_extract"
	[ -f "$cg_dest/node.exe" ] || { echo "CodeGraph runtime missing from archive" >&2; exit 1; }
	[ -f "$cg_dest/bin/codegraph.cmd" ] || { echo "CodeGraph launcher missing from archive" >&2; exit 1; }
	[ -f "$cg_dest/lib/package.json" ] || { echo "CodeGraph package metadata missing from archive" >&2; exit 1; }
	grep -Fq "\"version\": \"${version#v}\"" "$cg_dest/lib/package.json" || { echo "CodeGraph version mismatch in archive" >&2; exit 1; }
}

copy_stable_windows_installer() {
	local source="$1"
	local destination="$2"
	local temporary="${destination}.copying"
	local before after copied attempt

	# Wails/NSIS can return before the installer writer releases its final
	# compressed block. Publish only after the source and copied hashes are stable.
	for attempt in $(seq 1 30); do
		before="$(sha256sum "$source" | awk '{print $1}')"
		sleep 1
		after="$(sha256sum "$source" | awk '{print $1}')"
		[ "$before" = "$after" ] || continue

		rm -f "$temporary"
		cp "$source" "$temporary"
		sleep 1
		after="$(sha256sum "$source" | awk '{print $1}')"
		copied="$(sha256sum "$temporary" | awk '{print $1}')"
		if [ "$before" = "$after" ] && [ "$after" = "$copied" ]; then
			mv -f "$temporary" "$destination"
			return 0
		fi
	done

	rm -f "$temporary"
	echo "NSIS installer did not become stable: $source" >&2
	return 1
}

verify_windows_installer_archive() {
	local installer="$1"
	case "$installer" in
		*.exe) go -C "$ROOT/desktop" run ./cmd/nsischeck "$installer" ;;
	esac
	local seven_zip=""

	if command -v 7z >/dev/null 2>&1; then
		seven_zip="$(command -v 7z)"
	elif command -v 7z.exe >/dev/null 2>&1; then
		seven_zip="$(command -v 7z.exe)"
	elif [ -x "/c/Program Files/7-Zip/7z.exe" ]; then
		seven_zip="/c/Program Files/7-Zip/7z.exe"
	fi

	if [ -z "$seven_zip" ]; then
		echo "warning: 7-Zip not found; skipped Windows archive test" >&2
		return 0
	fi

	echo "==> testing Windows archive integrity"
	"$seven_zip" t "$installer"
}

# Bounded acquisition/staging mode for release review. It deliberately skips
# version stamping and Wails/frontend compilation.
if [ "${ORCA_PACKAGING_PREP_ONLY:-}" = "1" ]; then
	[ "$os" = windows ] || { echo "ORCA_PACKAGING_PREP_ONLY requires a Windows target" >&2; exit 1; }
	prepare_windows_installer_resources
	echo "==> verified Windows payload staged; prep-only mode complete"
	exit 0
fi

cd "$ROOT/desktop"

# Stamp the version resource (Windows file properties, macOS CFBundleVersion) from
# the tag. Wails feeds info.productVersion into goversioninfo and NSIS's
# VIFileVersion, both of which demand a strictly numeric X.X.X, so strip the
# leading "v" AND any prerelease suffix (a `-rc1` tag would otherwise abort the
# installer build). The full tag still rides in ldflags for the in-app version.
numver="${VERSION#v}"; numver="${numver%%-*}"
node -e 'const fs=require("fs"),f="wails.json",j=JSON.parse(fs.readFileSync(f,"utf8"));j.info.productVersion=process.argv[1];fs.writeFileSync(f,JSON.stringify(j,null,2)+"\n")' "$numver"

prepare_windows_installer_resources

# NSIS installer is Windows-only (Wails requires a single windows target for -nsis).
build_args=(-clean -platform "$PLATFORM" -ldflags "-X main.version=$VERSION -X main.channel=$CHANNEL")
[ "$os" = windows ] && build_args+=(-nsis -webview2 embed)
# Link cgo against WebKitGTK 4.1: 4.0 (libwebkit2gtk-4.0.so.37) is gone on
# Ubuntu 24.04+/Fedora 40+, while 4.1 ships from Ubuntu 22.04 onward.
[ "$os" = linux ] && build_args+=(-tags webkit2_41)

echo "==> wails build ${build_args[*]}"
wails build "${build_args[@]}"

mkdir -p "$ROOT/dist"

case "$os" in
darwin)
	# Wails names the bundle after the project name, which is independent from the
	# executable outputfilename. Discover the single generated bundle, then
	# normalize its public name to O.R.C.A.app.
	staging=$(mktemp -d)
	app="$staging/${APPNAME}.app"
	generated_app=$(find build/bin -maxdepth 1 -type d -name "*.app" -print -quit)
	[ -n "$generated_app" ] || { echo "no macOS app bundle found in build/bin" >&2; exit 1; }
	cp -R "$generated_app" "$app"
	# Reject invalid bundle metadata before signing or archiving it.
	plutil -lint "$app/Contents/Info.plist"

	# Two signing paths, selected by HAS_APPLE_CERT (set by release-desktop.yml when
	# the APPLE_* secrets are present). With a real Developer ID cert + notarization
	# key we sign with a hardened runtime, notarize, and staple — a downloaded build
	# then opens with no Gatekeeper prompt. Without it we ad-hoc sign as before (still
	# un-notarized; users clear the quarantine attribute per desktop/README.md). The
	# fallback keeps fork/local builds working with no secrets configured.
	if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
		identity="$(security find-identity -v -p codesigning | awk -F'"' '/Developer ID Application/{print $2; exit}')"
		[ -n "$identity" ] || { echo "HAS_APPLE_CERT=true but no 'Developer ID Application' identity found in the keychain" >&2; exit 1; }
		echo "==> codesign (Developer ID): $identity"
		codesign --force --deep --timestamp --options runtime \
			--entitlements "$ROOT/desktop/build/darwin/entitlements.plist" \
			-s "$identity" "$app"
		# notarytool wants an archive, not a bare bundle: zip the .app, submit, wait,
		# then staple the ticket back onto the bundle so it verifies offline.
		ditto -c -k --keepParent "$app" "$staging/notarize.zip"
		echo "==> notarytool submit (app)"
		xcrun notarytool submit "$staging/notarize.zip" \
			--key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY_ID" \
			--issuer "$APPLE_API_ISSUER_ID" --wait
		xcrun stapler staple "$app"
	else
		# Ad-hoc cuts the "is damaged" error somewhat but is NOT notarized; users may
		# still need `xattr -dr com.apple.quarantine` (see desktop/README.md).
		codesign --force --deep -s - "$app"
	fi
	codesign --verify --deep --strict "$app"

	if [ "$arch" = universal ]; then
		# One universal .app covers Intel + Apple Silicon; publish it under both
		# manifest keys so the updater's darwin-arm64/darwin-amd64 lookup finds it
		# (avoids a scarce macos-13 Intel runner).
		ditto -c -k --keepParent "$app" "$ROOT/dist/${ARTIFACT_BASE}-darwin-arm64.zip"
		ditto -c -k --keepParent "$app" "$ROOT/dist/${ARTIFACT_BASE}-darwin-amd64.zip"
	else
		ditto -c -k --keepParent "$app" "$ROOT/dist/${ARTIFACT_BASE}-darwin-${arch}.zip"
	fi
	# A drag-to-Applications .dmg for the manifest and first-time human download.
	# Named -universal so it is one artifact for both darwin manifest keys. The .zip
	# remains an auxiliary app archive. create-dmg can exit nonzero
	# while still writing the image, so gate on the file existing, not the exit code.
	dmgsrc=$(mktemp -d)
	cp -R "$app" "$dmgsrc/${APPNAME}.app"
	dmg="$ROOT/dist/${ARTIFACT_BASE}-macos-universal.dmg"
	create-dmg \
		--volname "$APPNAME" \
		--window-size 540 380 \
		--icon-size 110 \
		--icon "${APPNAME}.app" 150 190 \
		--app-drop-link 390 190 \
		--no-internet-enable \
		"$dmg" "$dmgsrc" || true
	[ -f "$dmg" ] || { echo "create-dmg did not produce $dmg" >&2; exit 1; }
	# The .dmg is a separately-downloaded artifact, so sign + notarize + staple the
	# disk image itself too — the stapled .app inside isn't enough for the image.
	if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
		codesign --force --timestamp -s "$identity" "$dmg"
		echo "==> notarytool submit (dmg)"
		xcrun notarytool submit "$dmg" \
			--key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY_ID" \
			--issuer "$APPLE_API_ISSUER_ID" --wait
		xcrun stapler staple "$dmg"
	fi
	bash "$ROOT/scripts/test-desktop-dmg.sh" "$dmg" "$numver" "$arch"
	rm -rf "$staging" "$dmgsrc"
	;;
windows)
	# `wails build -nsis` writes the installer under build/bin. Wails versions
	# Wails versions use the user-facing O.R.C.A installer name; older versions
	# may still emit a generic `...installer.exe` name.
	installer=$(find build/bin -maxdepth 1 -type f \( -iname "*setup*.exe" -o -iname "*installer*.exe" \) | head -n1 || true)
	[ -n "$installer" ] || { echo "no NSIS installer found in build/bin" >&2; exit 1; }
	packaged_installer="$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}-installer.exe"
	copy_stable_windows_installer "$installer" "$packaged_installer"
	verify_windows_installer_archive "$packaged_installer"
	portable=$(find build/bin -maxdepth 1 -type f -name "*.exe" ! -iname "*setup*.exe" ! -iname "*installer*.exe" | head -n1 || true)
	[ -n "$portable" ] || { echo "no portable Windows exe found in build/bin" >&2; exit 1; }
	staging=$(mktemp -d)
	staging_parent="$(dirname -- "$staging")"
	cp "$portable" "$staging/Orca.exe"
	payload="$ROOT/desktop/build/windows/installer-go/payload"
	[ -f "$payload/node.exe" ] || { echo "portable payload is missing pinned node.exe" >&2; exit 1; }
	[ -f "$payload/LICENSE.node.txt" ] || { echo "portable payload is missing Node LICENSE" >&2; exit 1; }
	[ -f "$payload/codegraph/bin/codegraph.cmd" ] || { echo "portable payload is missing CodeGraph launcher" >&2; exit 1; }
	cp "$payload/node.exe" "$staging/node.exe"
	cp "$payload/LICENSE.node.txt" "$staging/LICENSE.node.txt"
	cp -R "$payload/codegraph" "$staging/codegraph"
	staging_win=$(cygpath -w "$staging")
	zip_win=$(cygpath -w "$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}.zip")
	powershell.exe -NoProfile -NonInteractive -Command "Compress-Archive -Force -Path '${staging_win}\\*' -DestinationPath '$zip_win'"
	verify_windows_installer_archive "$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}.zip"
	assert_within_dir "$staging" "$staging_parent"
	rm -rf -- "$staging"
	# V2 updater compatibility aliases. They intentionally retain the previous
	# asset names for one major release while all current UI uses O.R.C.A.
	cp "$packaged_installer" "$ROOT/dist/DeepSeek-Orca-windows-${arch}-installer.exe"
	cp "$ROOT/dist/${ARTIFACT_BASE}-windows-${arch}.zip" "$ROOT/dist/DeepSeek-Orca-windows-${arch}.zip"
	;;
linux)
	tar -czf "$ROOT/dist/${ARTIFACT_BASE}-linux-${arch}.tar.gz" -C build/bin "$BINNAME"
	# Also build the manifest/release .deb for Debian/Ubuntu users (goreleaser/nfpm;
	# see desktop/build/linux/nfpm.yaml). The tar.gz remains an auxiliary bare-binary
	# archive. nfpm reads
	# $DEB_VERSION/$DEB_ARCH — dpkg wants a strict numeric version, so reuse numver.
	DEB_VERSION="$numver" DEB_ARCH="$arch" \
		nfpm package --config build/linux/nfpm.yaml --packager deb \
		--target "$ROOT/dist/${ARTIFACT_BASE}-linux-${arch}.deb"
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac

echo "==> packaged into dist/:"
ls -la "$ROOT/dist"
