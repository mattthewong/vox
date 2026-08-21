//go:build vox_dylib_source

// Package parakeet: this file is never built. Its sole purpose is to keep
// github.com/k2-fsa/sherpa-onnx-go-macos in go.mod after the code stopped
// importing it for linking.
//
// The module is still load-bearing in two places that do not show up as Go
// imports:
//
//   - packaging/bundle-dylibs.sh resolves it with `go list -m` to find the
//     dylibs it copies into the .app bundle
//   - internal/parakeet/sherpa_dyn.go resolves it the same way at runtime as
//     the dev-build dlopen fallback, and csrc/c-api.h is vendored from it
//
// Without this file, `go mod tidy` would drop the module and both of those
// would silently break on the next fresh checkout.
package parakeet

import _ "github.com/k2-fsa/sherpa-onnx-go-macos"
