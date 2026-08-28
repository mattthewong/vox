#!/usr/bin/env bash
# Copy the sherpa-onnx dylibs into the .app so Parakeet works on machines
# that are not this one.
#
# sherpa-onnx-go ships dylibs, not static archives. The vox binary no longer
# links them (internal/parakeet dlopens the c-api at first use, precisely so
# whisper-only users never map libonnxruntime), but the .app must still carry
# the libraries for dlopen to find via Contents/Frameworks.
#
# The inter-library reference uses @loader_path, not @executable_path: the
# c-api dylib is loaded by dlopen from arbitrary processes (the app, test
# binaries, voxbench), and @loader_path resolves relative to the dylib
# itself, which keeps the pair relocatable no matter who loads it.
# @executable_path was tried first and breaks any loader that is not the
# bundled app.
#
# install_name_tool invalidates code signatures, so this script signs last:
# inner dylibs first, then the enclosing bundle. Nothing may sign before it.
set -euo pipefail

APP="${1:?usage: bundle-dylibs.sh <path-to-.app> [bundle-id]}"
BUNDLE_ID="${2:-}"
MACOS_DIR="$APP/Contents/MacOS"
FRAMEWORKS="$APP/Contents/Frameworks"
BINARY="$MACOS_DIR/vox"

[ -x "$BINARY" ] || { echo "no binary at $BINARY" >&2; exit 1; }

# Locate the dylibs via the module cache rather than hardcoding a version.
# Download first so go list can resolve the directory on a fresh clone.
go mod download github.com/k2-fsa/sherpa-onnx-go-macos >/dev/null
MODDIR="$(go list -m -f '{{.Dir}}' github.com/k2-fsa/sherpa-onnx-go-macos)"
LIBDIR="$MODDIR/lib/aarch64-apple-darwin"
[ -d "$LIBDIR" ] || { echo "no dylib dir at $LIBDIR" >&2; exit 1; }

mkdir -p "$FRAMEWORKS"

# libsherpa-onnx-cxx-api.dylib is unused; copying it would inflate the
# bundle for nothing.
LIBS=(libsherpa-onnx-c-api.dylib libonnxruntime.dylib)

for lib in "${LIBS[@]}"; do
	# Module cache files are 0444; clear the destination so cp cannot fail on
	# a read-only leftover, then make the copy writable for install_name_tool.
	rm -f "$FRAMEWORKS/$lib"
	cp "$LIBDIR/$lib" "$FRAMEWORKS/$lib"
	chmod u+w "$FRAMEWORKS/$lib"
	# Relocatable install name. Nothing links these anymore, so the id only
	# matters for the c-api -> onnxruntime dependency below.
	install_name_tool -id "@loader_path/$lib" "$FRAMEWORKS/$lib"
done

# The binary itself carries no sherpa references since the dlopen change, so
# there is nothing to rewrite on it. Assert that rather than assume it: a
# reappearing link would resurrect the launch-time minos 26.4 dependency this
# design exists to avoid.
if otool -L "$BINARY" | grep -qE 'sherpa|onnxruntime'; then
	echo "error: $BINARY links sherpa/onnxruntime directly; the dlopen design requires it not to" >&2
	otool -L "$BINARY" | grep -E 'sherpa|onnxruntime' >&2
	exit 1
fi

# libonnxruntime is referenced by libsherpa-onnx-c-api. @loader_path makes it
# resolve next to the c-api dylib regardless of the loading process.
install_name_tool -change "@rpath/libonnxruntime.dylib" \
	"@loader_path/libonnxruntime.dylib" \
	"$FRAMEWORKS/libsherpa-onnx-c-api.dylib"

# Fail the build rather than ship a bundle that only runs here. Catches both
# a surviving @rpath reference and any absolute path into the module cache.
check_self_contained() {
	local target="$1" bad
	bad="$(otool -L "$target" | tail -n +2 | grep -E '@rpath/|/pkg/mod/' || true)"
	if [ -n "$bad" ]; then
		echo "error: $target still references unbundled libraries:" >&2
		echo "$bad" >&2
		exit 1
	fi
}
for target in "$BINARY" "${LIBS[@]/#/$FRAMEWORKS/}"; do
	check_self_contained "$target"
done

# Sign inner libraries before the enclosing bundle, or the outer signature
# is invalidated by the later inner ones.
for lib in "${LIBS[@]}"; do
	codesign --force --sign - "$FRAMEWORKS/$lib"
done
if [ -n "$BUNDLE_ID" ]; then
	# Keep the identifier stable so macOS TCC preserves Accessibility and
	# Microphone grants across rebuilds.
	codesign --force --sign - --identifier "$BUNDLE_ID" "$APP"
else
	codesign --force --sign - "$APP"
fi

echo "bundled: ${LIBS[*]}"
otool -L "$FRAMEWORKS/libsherpa-onnx-c-api.dylib" | sed -n '2,$p'
