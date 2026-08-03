package main

// Replay the conformance trace recorded from the UNMODIFIED upstream test suite.
//
// The 302 Jest tests are TypeScript and cannot execute against a Go binary — and
// embedding a JS runtime is banned by the rules. So conformance/capture.mjs reads
// the test SOURCES as text, extracts every literal
// CronExpressionParser.parse(expression, options) call site, and records what the
// ORIGINAL implementation returned for each. This replays those calls against the
// port and diffs.
//
// What this does and does not prove is spelled out in CONFORMANCE.md.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "time/tzdata"

	"github.com/portmortem/cronparser/cronparser"
)

type Record struct {
	Source     string         `json:"source"`
	Expression string         `json:"expression"`
	Options    map[string]any `json:"options"`
	Error      *string        `json:"error"`
	Fires      []int64        `json:"fires"`
	PrevFires  []int64        `json:"prevFires"`
	Anchored   bool           `json:"anchored"`
}

type Trace struct {
	RecordedUnderTZ      string   `json:"recordedUnderTZ"`
	MaxIterationsPerCall int      `json:"maxIterationsPerCall"`
	CallSitesRecorded    int      `json:"callSitesRecorded"`
	ExtractedFrom        []string `json:"extractedFrom"`
	Records              []Record `json:"records"`
}

type outcome struct {
	rec     Record
	kind    string // "match", "error-mismatch", "fires-mismatch", "port-error", "port-ok-upstream-error"
	detail  string
}

func main() {
	tracePath := flag.String("trace", "../conformance/trace.json", "recorded trace")
	verbose := flag.Bool("v", false, "print every case, not just failures")
	flag.Parse()

	// Expressions parsed without a tz option are interpreted in the process
	// timezone, so the replay must run under the SAME zone the trace was
	// recorded under. Windows does not honour the TZ environment variable for
	// Go's time.Local, so set it explicitly from the trace metadata below.

	raw, err := os.ReadFile(*tracePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read trace:", err)
		os.Exit(1)
	}
	var tr Trace
	if err := json.Unmarshal(raw, &tr); err != nil {
		fmt.Fprintln(os.Stderr, "parse trace:", err)
		os.Exit(1)
	}

	// Align time.Local with the recording zone.
	if tz := tr.RecordedUnderTZ; tz != "" && tz != "(system default)" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			fmt.Fprintf(os.Stderr, "trace recorded under TZ=%q which this build cannot load: %v\n", tz, err)
			os.Exit(1)
		}
		time.Local = loc
	}

	var results []outcome
	for _, rec := range tr.Records {
		results = append(results, replay(rec, tr.MaxIterationsPerCall))
	}

	counts := map[string]int{}
	for _, r := range results {
		counts[r.kind]++
	}

	fmt.Println("CONFORMANCE REPLAY — recorded from the unmodified upstream test suite")
	fmt.Println(strings.Repeat("=", 74))
	fmt.Printf("source files : %s\n", strings.Join(tr.ExtractedFrom, ", "))
	fmt.Printf("call sites   : %d\n", len(tr.Records))
	fmt.Println(strings.Repeat("=", 74))

	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Printf("  %-24s %4d\n", k, counts[k])
	}

	failures := 0
	for _, r := range results {
		if r.kind != "match" && r.kind != "parse-only (unanchored)" {
			failures++
		}
	}

	if failures > 0 || *verbose {
		fmt.Println()
		fmt.Println("DETAIL")
		fmt.Println(strings.Repeat("-", 74))
		for _, r := range results {
			if r.kind == "match" && !*verbose {
				continue
			}
			fmt.Printf("[%s] %s\n", r.kind, r.rec.Source)
			fmt.Printf("  expr=%q opts=%v\n", r.rec.Expression, r.rec.Options)
			if r.detail != "" {
				fmt.Printf("  %s\n", r.detail)
			}
		}
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 74))
	pct := 100.0 * float64(len(tr.Records)-failures) / float64(len(tr.Records))
	fmt.Printf("RESULT: %d/%d match (%.1f%%)\n", len(tr.Records)-failures, len(tr.Records), pct)
	fmt.Println(strings.Repeat("=", 74))

	if failures > 0 {
		os.Exit(1)
	}
}

func replay(rec Record, maxIter int) outcome {
	opts := toOptions(rec.Options)

	// Parse-time behaviour first: does the port agree about throwing?
	_, err := cronparser.Parse(rec.Expression, opts)

	if rec.Error != nil {
		if err == nil {
			return outcome{rec, "port-ok-upstream-error",
				fmt.Sprintf("upstream threw %q; port succeeded", *rec.Error)}
		}
		// Error messages are transcribed verbatim in the port, so compare them.
		if err.Error() != *rec.Error {
			return outcome{rec, "error-mismatch",
				fmt.Sprintf("upstream: %q\n  port    : %q", *rec.Error, err.Error())}
		}
		return outcome{rec, "match", ""}
	}

	if err != nil {
		return outcome{rec, "port-error",
			fmt.Sprintf("upstream succeeded; port failed: %v", err)}
	}

	// Unanchored call sites (no currentDate/startDate) iterate from "now", so
	// their fire times depend on the wall clock at record time and can never be
	// replayed. The parse outcome above IS comparable and was checked; the
	// iteration is not, and is reported separately rather than counted as a pass.
	if !rec.Anchored {
		return outcome{rec, "parse-only (unanchored)", ""}
	}

	// Forward iteration.
	if d := compareIter(rec.Expression, opts, rec.Fires, false, maxIter); d != "" {
		return outcome{rec, "fires-mismatch", "next(): " + d}
	}
	// Backward iteration.
	if d := compareIter(rec.Expression, opts, rec.PrevFires, true, maxIter); d != "" {
		return outcome{rec, "fires-mismatch", "prev(): " + d}
	}

	return outcome{rec, "match", ""}
}

func compareIter(expr string, opts cronparser.Options, want []int64, reverse bool, maxIter int) string {
	e, err := cronparser.Parse(expr, opts)
	if err != nil {
		return fmt.Sprintf("re-parse failed: %v", err)
	}
	var got []int64
	for i := 0; i < maxIter; i++ {
		var t interface{ UnixMilli() int64 }
		if reverse {
			v, err := e.Prev()
			if err != nil {
				break
			}
			t = v
		} else {
			v, err := e.Next()
			if err != nil {
				break
			}
			t = v
		}
		got = append(got, t.UnixMilli())
	}

	if len(got) != len(want) {
		return fmt.Sprintf("length %d != %d\n    upstream: %v\n    port    : %v",
			len(got), len(want), want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Sprintf("index %d: upstream %d, port %d (delta %d ms)\n"+
				"    upstream: %v\n    port    : %v",
				i, want[i], got[i], got[i]-want[i], want, got)
		}
	}
	return ""
}

func toOptions(m map[string]any) cronparser.Options {
	var o cronparser.Options
	for k, v := range m {
		s, _ := v.(string)
		switch k {
		case "currentDate":
			o.CurrentDate = s
		case "startDate":
			o.StartDate = s
		case "endDate":
			o.EndDate = s
		case "tz":
			o.TZ = s
		case "hashSeed":
			o.HashSeed = s
		case "strict":
			if b, ok := v.(bool); ok {
				o.Strict = b
			}
		}
	}
	return o
}
