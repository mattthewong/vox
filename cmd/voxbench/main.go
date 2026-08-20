// Command voxbench measures transcription accuracy and speed across vox's
// speech engines, so claims about one engine beating another are backed by
// numbers from real audio rather than published leaderboards.
//
// Usage:
//
//	voxbench -manifest bench/libri.jsonl -engines base.en,parakeet-v2
//
// The manifest is JSONL, one clip per line:
//
//	{"wav": "clips/0001.wav", "text": "the reference transcript"}
//
// Relative wav paths resolve against the manifest's own directory.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"vox/internal/parakeet"
	"vox/internal/sttmodel"
	"vox/internal/transcribe"
	"vox/internal/whisperserver"
)

type manifestEntry struct {
	WAV  string `json:"wav"`
	Text string `json:"text"`
}

func main() {
	var (
		manifestPath = flag.String("manifest", "", "path to JSONL manifest (required)")
		engineList   = flag.String("engines", "base.en,parakeet-v2", "comma-separated model IDs to compare")
		corpusName   = flag.String("corpus", "", "corpus label for the report (defaults to manifest filename)")
		outPath      = flag.String("out", "", "write markdown report here (default: stdout)")
		verbose      = flag.Bool("v", false, "print per-clip results")
	)
	flag.Parse()

	if *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "error: -manifest is required")
		flag.Usage()
		os.Exit(2)
	}

	entries, err := loadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "error: manifest is empty")
		os.Exit(1)
	}

	corpus := *corpusName
	if corpus == "" {
		corpus = strings.TrimSuffix(filepath.Base(*manifestPath), filepath.Ext(*manifestPath))
	}

	ctx := context.Background()
	var results []*engineResult

	for _, id := range strings.Split(*engineList, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		fmt.Fprintf(os.Stderr, "running %s over %d clips...\n", id, len(entries))
		r, err := runEngine(ctx, id, entries, *verbose)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: engine %s: %v\n", id, err)
			os.Exit(1)
		}
		r.summarize()
		results = append(results, r)
	}

	report := renderMarkdown(corpus, results)
	if *outPath == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*outPath, []byte(report), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
}

// loadManifest reads JSONL entries and resolves relative wav paths against the
// manifest's directory.
func loadManifest(path string) ([]manifestEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	base := filepath.Dir(path)
	var out []manifestEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		var e manifestEntry
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("manifest line %d: %w", line, err)
		}
		if !filepath.IsAbs(e.WAV) {
			e.WAV = filepath.Join(base, e.WAV)
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// runEngine brings up one engine, transcribes every clip, and records timings.
func runEngine(ctx context.Context, modelID string, entries []manifestEntry, verbose bool) (*engineResult, error) {
	m, ok := sttmodel.ByID(modelID)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", modelID)
	}
	if !sttmodel.IsInstalled(m) {
		return nil, fmt.Errorf("model %q is not installed; download it first", modelID)
	}
	modelPath, err := sttmodel.ResolvePath(m)
	if err != nil {
		return nil, err
	}

	res := &engineResult{Engine: modelID}

	var tr transcribe.Transcriber
	loadStart := time.Now()

	switch m.Engine {
	case sttmodel.EngineParakeet:
		// Disable idle unloading so the model stays resident for the run.
		rec := parakeet.New(parakeet.Config{ModelDir: modelPath, IdleTimeout: time.Hour})
		defer rec.Close()
		tr = rec

	case sttmodel.EngineWhisper:
		srv, err := whisperserver.New("127.0.0.1", 2032, os.DevNull)
		if err != nil {
			return nil, err
		}
		if err := srv.Start(ctx, modelPath); err != nil {
			return nil, fmt.Errorf("start whisper-server: %w", err)
		}
		defer srv.Stop(context.Background())
		tr = transcribe.NewClient(srv.URL())

	default:
		return nil, fmt.Errorf("unsupported engine %q", m.Engine)
	}

	// Warm up on the first clip so model load and lazy init are not billed to
	// clip 1's decode time.
	if len(entries) > 0 {
		if wav, err := os.ReadFile(entries[0].WAV); err == nil {
			_, _ = tr.Transcribe(ctx, wav, transcribe.TranscribeOptions{})
		}
	}
	res.LoadTime = time.Since(loadStart)

	for _, e := range entries {
		c := clipResult{Path: e.WAV, Ref: e.Text}

		wav, err := os.ReadFile(e.WAV)
		if err != nil {
			c.Err = err
			res.Clips = append(res.Clips, c)
			continue
		}
		if d, derr := parakeet.Duration(wav); derr == nil {
			c.AudioDur = d
		}

		start := time.Now()
		hyp, err := tr.Transcribe(ctx, wav, transcribe.TranscribeOptions{})
		c.Decode = time.Since(start)
		if err != nil {
			c.Err = err
			res.Clips = append(res.Clips, c)
			continue
		}
		c.Hyp = hyp
		c.WER = wer(e.Text, hyp)
		res.Clips = append(res.Clips, c)

		if verbose {
			fmt.Fprintf(os.Stderr, "  %-40s wer=%.3f decode=%s\n",
				filepath.Base(e.WAV), c.WER, c.Decode.Round(time.Millisecond))
		}
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	res.GoHeapSys = ms.Sys

	return res, nil
}
