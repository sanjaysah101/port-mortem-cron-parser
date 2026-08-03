//go:build emit

package main

// Emitter: robfig/cron v3.0.1 (Go)
// Reads corpus/cases.json, emits results in the shared conformance shape.

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	_ "time/tzdata"

	"github.com/robfig/cron/v3"
)

type Case struct {
	ID   string `json:"id"`
	Expr string `json:"expr"`
	TZ   string `json:"tz"`
	From string `json:"from"`
	N    int    `json:"n"`
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
	raw, err := os.ReadFile("../corpus/cases.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "read corpus:", err)
		os.Exit(1)
	}
	var corpus Corpus
	if err := json.Unmarshal(raw, &corpus); err != nil {
		fmt.Fprintln(os.Stderr, "parse corpus:", err)
		os.Exit(1)
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	results := make([]Result, 0, len(corpus.Cases))
	for _, c := range corpus.Cases {
		r := Result{ID: c.ID, Fires: []Fire{}}

		loc, err := time.LoadLocation(c.TZ)
		if err != nil {
			e := "LoadLocation: " + err.Error()
			r.Error = &e
			results = append(results, r)
			continue
		}
		sched, err := parser.Parse(c.Expr)
		if err != nil {
			e := "Parse: " + err.Error()
			r.Error = &e
			results = append(results, r)
			continue
		}
		t, err := time.ParseInLocation("2006-01-02T15:04:05", c.From, loc)
		if err != nil {
			e := "ParseInLocation: " + err.Error()
			r.Error = &e
			results = append(results, r)
			continue
		}

		for i := 0; i < c.N; i++ {
			t = sched.Next(t)
			if t.IsZero() {
				e := "zero time (no further occurrences)"
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
		"impl":    "robfig/cron",
		"lang":    "go",
		"version": "v3.0.1",
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
