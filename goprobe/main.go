package main

import (
	"fmt"
	"time"

	_ "time/tzdata" // embed the tz database so results are reproducible

	"github.com/robfig/cron/v3"
)

func run(label, tzName, spec, from string, n int) {
	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("spec=%q tz=%s from=%s\n", spec, tzName, from)

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		fmt.Printf("  LoadLocation error: %v\n", err)
		return
	}

	// Standard 5-field parser, matching cron-parser's default dialect.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(spec)
	if err != nil {
		fmt.Printf("  Parse error: %v\n", err)
		return
	}

	t, err := time.ParseInLocation("2006-01-02T15:04:05", from, loc)
	if err != nil {
		fmt.Printf("  ParseInLocation error: %v\n", err)
		return
	}

	for i := 0; i < n; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			fmt.Printf("  <zero time — no further occurrences>\n")
			return
		}
		fmt.Printf("  %s\n", t.Format("2006-01-02 15:04 -07:00"))
	}
}

func main() {
	fmt.Println("Go:", "robfig/cron v3.0.1")

	// The two cases where cron-parser (TS) skips a day entirely.
	run("Antarctica/Troll  daily 01:30  (2h spring-forward)",
		"Antarctica/Troll", "30 1 * * *", "2026-03-27T00:00:00", 5)

	run("Australia/Lord_Howe  daily 02:00  (30m spring-forward)",
		"Australia/Lord_Howe", "0 2 * * *", "2026-10-02T00:00:00", 5)

	run("America/New_York  daily 02:30  (1h spring-forward, CONTROL)",
		"America/New_York", "30 2 * * *", "2026-03-06T00:00:00", 5)

	// Hourly across the transitions.
	run("Antarctica/Troll  hourly",
		"Antarctica/Troll", "0 * * * *", "2026-03-28T22:00:00", 8)

	run("Australia/Lord_Howe  hourly",
		"Australia/Lord_Howe", "0 * * * *", "2026-10-04T00:00:00", 8)

	// ---- FALL-BACK: the repeated hour. Does a job run once or twice? ----
	// This is the behavior the research flagged as undocumented in robfig/cron.
	fmt.Println("\n\n########## FALL-BACK (repeated hour) ##########")

	run("America/New_York  daily 01:30  (1h fall-back; 01:30 occurs TWICE)",
		"America/New_York", "30 1 * * *", "2026-10-31T00:00:00", 4)

	run("America/New_York  hourly across fall-back",
		"America/New_York", "0 * * * *", "2026-11-01T00:00:00", 6)

	run("Australia/Lord_Howe  daily 01:45 (30m fall-back; 01:45 occurs TWICE)",
		"Australia/Lord_Howe", "45 1 * * *", "2026-04-04T00:00:00", 4)

	// ---- DOM/DOW semantics: POSIX says OR when both are restricted ----
	fmt.Println("\n\n########## DOM/DOW OR-vs-AND ##########")
	run("'1 2 3 * 5' — POSIX: 3rd of month OR every Friday",
		"UTC", "1 2 3 * 5", "2026-01-01T00:00:00", 8)
}
