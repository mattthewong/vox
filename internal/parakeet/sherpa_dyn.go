// Package parakeet: dynamic loading of the sherpa-onnx C library.
//
// This file is why vox does NOT link libsherpa-onnx-c-api.dylib. A normal
// cgo link records an LC_LOAD_DYLIB reference that dyld resolves at every
// process launch, which drags the 27 MiB libonnxruntime.dylib (declared
// minos 26.4) into every vox process — including for users who only ever use
// whisper, and including on macOS versions older than the dylib targets.
//
// Instead, the library is dlopen(3)ed on first Parakeet use and every entry
// point is called through a dlsym(3)ed function pointer. Whisper-only users
// never map the library at all, and an incompatible or missing library
// surfaces as a clear Go error at engine-selection time instead of a dyld
// failure at launch.
//
// The vendored header in csrc/ provides the struct layouts at compile time
// (types only; nothing from it is linked), so cgo derives
// SherpaOnnxOfflineRecognizerConfig from the authoritative source rather
// than a hand-replicated Go mirror that could drift.
package parakeet

/*
#cgo CFLAGS: -I${SRCDIR}/csrc

#include <dlfcn.h>
#include <stdlib.h>
#include "c-api.h"

// Typed trampolines: dlsym returns void*, and calling through a correctly
// typed function pointer is the only portable way to invoke it from cgo.

typedef const SherpaOnnxOfflineRecognizer *(*vox_create_recognizer_fn)(
    const SherpaOnnxOfflineRecognizerConfig *);
typedef void (*vox_destroy_recognizer_fn)(const SherpaOnnxOfflineRecognizer *);
typedef const SherpaOnnxOfflineStream *(*vox_create_stream_fn)(
    const SherpaOnnxOfflineRecognizer *);
typedef void (*vox_destroy_stream_fn)(const SherpaOnnxOfflineStream *);
typedef void (*vox_accept_waveform_fn)(const SherpaOnnxOfflineStream *,
                                       int32_t, const float *, int32_t);
typedef void (*vox_decode_stream_fn)(const SherpaOnnxOfflineRecognizer *,
                                     const SherpaOnnxOfflineStream *);
typedef const SherpaOnnxOfflineRecognizerResult *(*vox_get_result_fn)(
    const SherpaOnnxOfflineStream *);
typedef void (*vox_destroy_result_fn)(const SherpaOnnxOfflineRecognizerResult *);

static const SherpaOnnxOfflineRecognizer *vox_call_create_recognizer(
    void *f, const SherpaOnnxOfflineRecognizerConfig *c) {
  return ((vox_create_recognizer_fn)f)(c);
}
static void vox_call_destroy_recognizer(void *f,
                                        const SherpaOnnxOfflineRecognizer *r) {
  ((vox_destroy_recognizer_fn)f)(r);
}
static const SherpaOnnxOfflineStream *vox_call_create_stream(
    void *f, const SherpaOnnxOfflineRecognizer *r) {
  return ((vox_create_stream_fn)f)(r);
}
static void vox_call_destroy_stream(void *f,
                                    const SherpaOnnxOfflineStream *s) {
  ((vox_destroy_stream_fn)f)(s);
}
static void vox_call_accept_waveform(void *f, const SherpaOnnxOfflineStream *s,
                                     int32_t rate, const float *samples,
                                     int32_t n) {
  ((vox_accept_waveform_fn)f)(s, rate, samples, n);
}
static void vox_call_decode_stream(void *f,
                                   const SherpaOnnxOfflineRecognizer *r,
                                   const SherpaOnnxOfflineStream *s) {
  ((vox_decode_stream_fn)f)(r, s);
}
static const SherpaOnnxOfflineRecognizerResult *vox_call_get_result(
    void *f, const SherpaOnnxOfflineStream *s) {
  return ((vox_get_result_fn)f)(s);
}
static void vox_call_destroy_result(
    void *f, const SherpaOnnxOfflineRecognizerResult *r) {
  ((vox_destroy_result_fn)f)(r);
}
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// sherpaLib holds the dlopen handle and resolved entry points. Loaded at
// most once per process; never unloaded (dlclose of a library holding ONNX
// runtime state is not worth the risk, and the idle-release path frees the
// recognizer, which is where the ~1.7 GiB actually lives).
type sherpaLib struct {
	createRecognizer  unsafe.Pointer
	destroyRecognizer unsafe.Pointer
	createStream      unsafe.Pointer
	destroyStream     unsafe.Pointer
	acceptWaveform    unsafe.Pointer
	decodeStream      unsafe.Pointer
	getResult         unsafe.Pointer
	destroyResult     unsafe.Pointer
}

var (
	libOnce sync.Once
	lib     *sherpaLib
	libErr  error
)

const (
	capiDylib = "libsherpa-onnx-c-api.dylib"
	onnxDylib = "libonnxruntime.dylib"
)

// loadSherpa dlopens the sherpa-onnx C library and resolves the offline
// recognizer entry points. Safe for concurrent use; the result is cached for
// the life of the process, including a load failure (a broken install does
// not become less broken by retrying).
func loadSherpa() (*sherpaLib, error) {
	libOnce.Do(func() {
		lib, libErr = dlopenSherpa()
	})
	return lib, libErr
}

func dlopenSherpa() (*sherpaLib, error) {
	dirs := candidateLibDirs()

	var attempts []string
	var handle unsafe.Pointer
	for _, dir := range dirs {
		// Load libonnxruntime first. In the module-cache layout the c-api
		// dylib references it as @rpath/libonnxruntime.dylib with no rpath
		// of its own; pre-loading it registers its install name with dyld so
		// the reference resolves. In the .app layout the reference was
		// rewritten to @executable_path and this pre-load is harmless.
		onnxPath := filepath.Join(dir, onnxDylib)
		capiPath := filepath.Join(dir, capiDylib)

		cOnnx := C.CString(onnxPath)
		C.dlopen(cOnnx, C.RTLD_NOW|C.RTLD_GLOBAL)
		C.free(unsafe.Pointer(cOnnx))
		// An onnxruntime load failure is not fatal by itself; the c-api load
		// below fails with the authoritative dlerror if it mattered.

		cCapi := C.CString(capiPath)
		handle = C.dlopen(cCapi, C.RTLD_NOW|C.RTLD_GLOBAL)
		C.free(unsafe.Pointer(cCapi))
		if handle != nil {
			break
		}
		if msg := C.dlerror(); msg != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %s", capiPath, C.GoString(msg)))
		} else {
			attempts = append(attempts, capiPath+": dlopen failed")
		}
	}
	if handle == nil {
		return nil, fmt.Errorf(
			"cannot load %s (parakeet is unavailable; whisper still works):\n  %s",
			capiDylib, strings.Join(attempts, "\n  "))
	}

	l := &sherpaLib{}
	for _, s := range []struct {
		name string
		dst  *unsafe.Pointer
	}{
		{"SherpaOnnxCreateOfflineRecognizer", &l.createRecognizer},
		{"SherpaOnnxDestroyOfflineRecognizer", &l.destroyRecognizer},
		{"SherpaOnnxCreateOfflineStream", &l.createStream},
		{"SherpaOnnxDestroyOfflineStream", &l.destroyStream},
		{"SherpaOnnxAcceptWaveformOffline", &l.acceptWaveform},
		{"SherpaOnnxDecodeOfflineStream", &l.decodeStream},
		{"SherpaOnnxGetOfflineStreamResult", &l.getResult},
		{"SherpaOnnxDestroyOfflineRecognizerResult", &l.destroyResult},
	} {
		cName := C.CString(s.name)
		p := C.dlsym(handle, cName)
		C.free(unsafe.Pointer(cName))
		if p == nil {
			return nil, fmt.Errorf("%s is missing %s (version mismatch between the dylib and the vendored c-api.h?)", capiDylib, s.name)
		}
		*s.dst = p
	}
	return l, nil
}

// candidateLibDirs returns directories to probe for the sherpa dylibs, in
// order of preference:
//
//  1. VOX_SHERPA_LIB_DIR — explicit override, tests and unusual setups
//  2. Contents/Frameworks next to the executable — the .app bundle layout
//     produced by packaging/bundle-dylibs.sh
//  3. The Go module cache — dev builds (`make run`, bare `bin/vox`), located
//     by shelling out to `go list`. Dev machines have a Go toolchain; end
//     users run the .app and never reach this.
func candidateLibDirs() []string {
	var dirs []string
	if d := os.Getenv("VOX_SHERPA_LIB_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "..", "Frameworks"))
	}
	if goBin, err := exec.LookPath("go"); err == nil {
		cmd := exec.Command(goBin, "list", "-m", "-f", "{{.Dir}}",
			"github.com/k2-fsa/sherpa-onnx-go-macos")
		cmd.Dir = "" // module resolution works from any dir via GOMODCACHE metadata
		if out, err := runWithTimeout(cmd, 5*time.Second); err == nil {
			modDir := strings.TrimSpace(string(out))
			if modDir != "" {
				dirs = append(dirs, filepath.Join(modDir, "lib", "aarch64-apple-darwin"))
			}
		}
	}
	return dirs
}

// runWithTimeout runs cmd, killing it after d. `go list` normally returns in
// milliseconds; the timeout only guards against a wedged toolchain.
func runWithTimeout(cmd *exec.Cmd, d time.Duration) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := cmd.Output()
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(d):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("timed out after %v", d)
	}
}

// recognizerHandle wraps the C recognizer pointer.
type recognizerHandle struct {
	lib *sherpaLib
	ptr *C.SherpaOnnxOfflineRecognizer
}

// newRecognizerHandle loads the library if needed and constructs a sherpa
// offline recognizer for a nemo_transducer model.
func newRecognizerHandle(modelDir string, numThreads int) (*recognizerHandle, error) {
	l, err := loadSherpa()
	if err != nil {
		return nil, err
	}

	// C strings for the config. sherpa-onnx copies them into std::string
	// during construction, so freeing immediately after create is safe (the
	// upstream Go bindings do the same).
	cs := func(s string) *C.char { return C.CString(s) }
	encoder := cs(filepath.Join(modelDir, "encoder.int8.onnx"))
	decoder := cs(filepath.Join(modelDir, "decoder.int8.onnx"))
	joiner := cs(filepath.Join(modelDir, "joiner.int8.onnx"))
	tokens := cs(filepath.Join(modelDir, "tokens.txt"))
	provider := cs("cpu")
	modelType := cs("nemo_transducer")
	decoding := cs("greedy_search")
	defer func() {
		for _, p := range []*C.char{encoder, decoder, joiner, tokens, provider, modelType, decoding} {
			C.free(unsafe.Pointer(p))
		}
	}()

	// Zero value zeroes every nested sub-config (NULL pointers, 0 ints),
	// which is exactly what the C API expects for unused model families.
	var cfg C.SherpaOnnxOfflineRecognizerConfig
	cfg.feat_config.sample_rate = C.int32_t(sampleRate)
	cfg.feat_config.feature_dim = C.int32_t(featureDim)
	cfg.model_config.transducer.encoder = encoder
	cfg.model_config.transducer.decoder = decoder
	cfg.model_config.transducer.joiner = joiner
	cfg.model_config.tokens = tokens
	cfg.model_config.num_threads = C.int32_t(numThreads)
	cfg.model_config.provider = provider
	cfg.model_config.model_type = modelType
	cfg.decoding_method = decoding

	ptr := C.vox_call_create_recognizer(l.createRecognizer, &cfg)
	if ptr == nil {
		return nil, fmt.Errorf("sherpa-onnx failed to create a recognizer from %s", modelDir)
	}
	return &recognizerHandle{lib: l, ptr: ptr}, nil
}

func (h *recognizerHandle) close() {
	if h.ptr != nil {
		C.vox_call_destroy_recognizer(h.lib.destroyRecognizer, h.ptr)
		h.ptr = nil
	}
}

// decode runs one utterance through the recognizer and returns the text.
// samples must be non-empty; AcceptWaveform reads samples[0] unconditionally.
func (h *recognizerHandle) decode(samples []float32, rate int) (string, error) {
	if len(samples) == 0 {
		return "", fmt.Errorf("no samples")
	}
	stream := C.vox_call_create_stream(h.lib.createStream, h.ptr)
	if stream == nil {
		return "", fmt.Errorf("sherpa-onnx failed to create a stream")
	}
	defer C.vox_call_destroy_stream(h.lib.destroyStream, stream)

	C.vox_call_accept_waveform(h.lib.acceptWaveform, stream,
		C.int32_t(rate), (*C.float)(unsafe.Pointer(&samples[0])), C.int32_t(len(samples)))
	C.vox_call_decode_stream(h.lib.decodeStream, h.ptr, stream)

	res := C.vox_call_get_result(h.lib.getResult, stream)
	if res == nil {
		return "", fmt.Errorf("sherpa-onnx returned a nil result")
	}
	defer C.vox_call_destroy_result(h.lib.destroyResult, res)
	return C.GoString(res.text), nil
}
