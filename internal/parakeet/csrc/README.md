# Vendored sherpa-onnx C API header

`c-api.h` is copied verbatim from
`github.com/k2-fsa/sherpa-onnx-go-macos@v1.13.6` (Apache License 2.0,
Copyright 2023 Xiaomi Corporation). See `LICENSE` in this directory.

## Why it is vendored

`internal/parakeet` deliberately does NOT link against
`libsherpa-onnx-c-api.dylib`. Linking creates an `LC_LOAD_DYLIB` reference
that dyld resolves at every launch, which forces the 27 MiB
`libonnxruntime.dylib` (declared `minos 26.4`) into every vox process,
including for users who only ever use whisper. Instead the library is
`dlopen`ed on first Parakeet use and called through `dlsym`ed function
pointers.

The header is still needed at compile time so cgo derives the config struct
layouts from the authoritative source instead of hand-replicated Go structs.
Types only; no symbols from it are linked.

## Version coupling

This header MUST match the dylib version bundled by
`packaging/bundle-dylibs.sh`, which resolves the same module. When bumping
`sherpa-onnx-go-macos` in go.mod, re-copy the header:

    cp "$(go list -m -f '{{.Dir}}' github.com/k2-fsa/sherpa-onnx-go-macos)/c-api.h" \
       internal/parakeet/csrc/c-api.h
