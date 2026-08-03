//go:build offsets

package main

// Verify that Go's tzdata 2025c and Node's ICU 2025b agree on the *offsets*
// for every transition used in the findings. If they agree, the observed
// library divergences are real behavioral differences, not tzdata skew.

import (
	"fmt"
	"time"

	_ "time/tzdata"
)

func main() {
	type probe struct {
		zone   string
		before string
		after  string
		label  string
	}
	probes := []probe{
		{"Antarctica/Troll", "2026-03-29T00:00:00", "2026-03-29T23:00:00", "Troll spring 2h"},
		{"Australia/Lord_Howe", "2026-10-04T00:00:00", "2026-10-04T23:00:00", "Lord Howe spring 30m"},
		{"Australia/Lord_Howe", "2026-04-05T00:00:00", "2026-04-05T23:00:00", "Lord Howe fall 30m"},
		{"Pacific/Chatham", "2026-09-27T00:00:00", "2026-09-27T23:00:00", "Chatham spring"},
		{"America/New_York", "2026-03-08T00:00:00", "2026-03-08T23:00:00", "NY spring 1h"},
		{"America/New_York", "2026-11-01T00:00:00", "2026-11-01T23:00:00", "NY fall 1h"},
	}

	fmt.Println("zone | label | offsetBefore(min) | offsetAfter(min) | delta(min)")
	for _, p := range probes {
		loc, err := time.LoadLocation(p.zone)
		if err != nil {
			fmt.Printf("%s | LoadLocation error %v\n", p.zone, err)
			continue
		}
		b, _ := time.ParseInLocation("2006-01-02T15:04:05", p.before, loc)
		a, _ := time.ParseInLocation("2006-01-02T15:04:05", p.after, loc)
		_, ob := b.Zone()
		_, oa := a.Zone()
		fmt.Printf("%s | %s | %d | %d | %d\n", p.zone, p.label, ob/60, oa/60, (oa-ob)/60)
	}
}
