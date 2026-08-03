package main

// Emitter for the Go port. Reads corpus/cases.json and writes the shared
// conformance shape, so corpus/diff.mjs can compare it against the TypeScript
// original without either side knowing about the other.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	_ "time/tzdata" // pin the tz database; do not inherit the host's

	"github.com/portmortem/cronparser/cronparser"
)

type Case struct {
	ID   string `json:"id"`
	Expr string `json:"expr"`
	TZ   string `json:"tz"`
	From string `json:"from"`
	N    int    `json:"n"`
	Dir  string `json:"dir"`
	Why  string `json:"why"`
}

type Corpus struct {
	Cases []Case `json:"cases"`
}

type Fire struct {
	EpochMS   int64  `json:"epoch_ms"`
	Local     string `json:"local"`
	OffsetMin int    `json:"offset_min"`
}

type Result struct {
	ID    string  `json:"id"`
	Fires []Fire  `json:"fires"`
	Error *string `json:"error"`
}

func main() {
	corpusPath := flag.String("corpus", "../corpus/cases.json", "path to cases.json")
	flag.Parse()

	raw, err := os.ReadFile(*corpusPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read corpus:", err)
		os.Exit(1)
	}
	var corpus Corpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		fmt.Fprintln(os.Stderr, "parse corpus:", err)
		os.Exit(1)
	}

	results := make([]Result, 0, len(corpus.Cases))
	for _, c := range corpus.Cases {
		r := Result{ID: c.ID, Fires: []Fire{}}

		expr, err := cronparser.Parse(c.Expr, cronparser.Options{
			TZ:          c.TZ,
			CurrentDate: c.From,
		})
		if err != nil {
			e := err.Error()
			r.Error = &e
			results = append(results, r)
			continue
		}

		for i := 0; i < c.N; i++ {
			var t time.Time
			var err error
			if c.Dir == "prev" {
				t, err = expr.Prev()
			} else {
				t, err = expr.Next()
			}
			if err != nil {
				e := err.Error()
				r.Error = &e
				break
			}
			_, off := t.Zone()
			r.Fires = append(r.Fires, Fire{
				EpochMS:   t.UnixMilli(),
				Local:     t.Format("2006-01-02 15:04:05"),
				OffsetMin: off / 60,
			})
		}
		results = append(results, r)
	}

	out := map[string]any{
		"impl":    "cronparser-go (port)",
		"lang":    "go",
		"version": "0.1.0",
		"runtime": runtime.Version(),
		"tzdata":  "2025c (GOROOT/lib/time, embedded via time/tzdata)",
		"results": results,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}
