package main

// Benchmark: the Go port. Mirrors bench/bench_ts.mjs — same workload, same
// iteration count, same reported metrics, so the numbers are comparable.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	_ "time/tzdata"

	"github.com/portmortem/cronparser/cronparser"
)

var workload = []struct {
	expr string
	tz   string
}{
	{"*/5 * * * *", "UTC"},
	{"0 0 * * *", "America/New_York"},
	{"30 1 * * *", "America/New_York"},      // fall-back case
	{"0 2 * * *", "Australia/Lord_Howe"},    // 30-min DST
	{"30 1 * * *", "Antarctica/Troll"},      // 2-hour DST
	{"0 0 L * *", "UTC"},                    // last day of month
	{"0 0 * * 5#3", "Europe/Berlin"},        // nth weekday
	{"*/15 9-17 * * 1-5", "Asia/Kolkata"},
}

const nextPer = 10

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p / 100 * float64(len(sorted)))
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func main() {
	iters := flag.Int("iters", 20000, "iterations")
	flag.Parse()

	// Warm up: fill tz caches and let the allocator settle.
	for i := 0; i < 2000; i++ {
		w := workload[i%len(workload)]
		e, err := cronparser.Parse(w.expr, cronparser.Options{TZ: w.tz, CurrentDate: "2026-06-15T12:00:00"})
		if err != nil {
			fmt.Fprintln(os.Stderr, "warmup:", err)
			os.Exit(1)
		}
		for k := 0; k < nextPer; k++ {
			if _, err := e.Next(); err != nil {
				break
			}
		}
	}

	// Windows' time.Now() has ~1ms granularity, so timing a single
	// parse+10×Next() (which takes single-digit microseconds) reads as either 0
	// or 1000us — the p50 comes out as literally 0. Time a BATCH and report the
	// per-operation average of each batch instead, so each sample spans enough
	// wall clock to be resolvable.
	const batch = 200
	nBatches := *iters / batch
	if nBatches < 1 {
		nBatches = 1
	}
	samples := make([]float64, nBatches)
	for b := 0; b < nBatches; b++ {
		t0 := time.Now()
		for i := 0; i < batch; i++ {
			w := workload[(b*batch+i)%len(workload)]
			e, err := cronparser.Parse(w.expr, cronparser.Options{TZ: w.tz, CurrentDate: "2026-06-15T12:00:00"})
			if err != nil {
				fmt.Fprintln(os.Stderr, "parse:", err)
				os.Exit(1)
			}
			for k := 0; k < nextPer; k++ {
				if _, err := e.Next(); err != nil {
					break
				}
			}
		}
		elapsed := float64(time.Since(t0).Nanoseconds()) / 1000.0
		samples[b] = elapsed / float64(batch) // us per operation
	}

	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	out := map[string]any{
		"impl":                  "cronparser-go (port)",
		"runtime":               runtime.Version(),
		"iterations":            *iters,
		"batchSize":             batch,
		"samplesAreBatchMeans":  true,
		"nextCallsPerIteration": nextPer,
		"latency_us": map[string]float64{
			"mean": round2(mean),
			"p50":  round2(percentile(sorted, 50)),
			"p90":  round2(percentile(sorted, 90)),
			"p99":  round2(percentile(sorted, 99)),
			"p999": round2(percentile(sorted, 99.9)),
			"max":  round2(sorted[len(sorted)-1]),
		},
		"memory_bytes": map[string]uint64{
			// Sys is the closest analogue to RSS: total memory obtained from
			// the OS. HeapAlloc is the live heap.
			"sys":        ms.Sys,
			"heapAlloc":  ms.HeapAlloc,
			"totalAlloc": ms.TotalAlloc,
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
