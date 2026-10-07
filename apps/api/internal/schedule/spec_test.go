package schedule

import (
	"slices"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// runs returns the next n runs after start.
func runs(t *testing.T, sp Spec, start time.Time, n int) []time.Time {
	t.Helper()
	loc, err := sp.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	var out []time.Time
	after := start
	for range n {
		next, ok := sp.Next(after, loc)
		if !ok {
			break
		}
		if !next.After(after) {
			t.Fatalf("Next(%s) = %s, not after", after, next)
		}
		out = append(out, next.UTC())
		after = next
	}
	return out
}

func expectRuns(t *testing.T, name string, got []time.Time, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d runs %v, want %v", name, len(got), got, want)
	}
	for i := range want {
		if !got[i].Equal(utc(want[i])) {
			t.Errorf("%s: run %d = %s, want %s", name, i, got[i].Format(time.RFC3339), want[i])
		}
	}
}

func TestDailyAcrossNewYorkSpringForward(t *testing.T) {
	// Clocks go from 02:00 EST to 03:00 EDT on 2026-03-08.
	sp := Spec{Kind: KindDaily, At: "09:00", TimeZone: "America/New_York"}
	expectRuns(t, "09:00", runs(t, sp, utc("2026-03-06T12:00:00Z"), 4),
		"2026-03-06T14:00:00Z", // EST, UTC-5
		"2026-03-07T14:00:00Z",
		"2026-03-08T13:00:00Z", // EDT, UTC-4: still 09:00 on the wall
		"2026-03-09T13:00:00Z",
	)
	// 02:30 does not exist on 2026-03-08: it runs at 03:30 EDT, once.
	sp.At = "02:30"
	expectRuns(t, "02:30 gap", runs(t, sp, utc("2026-03-07T12:00:00Z"), 3),
		"2026-03-08T07:30:00Z", // 03:30 EDT
		"2026-03-09T06:30:00Z", // 02:30 EDT
		"2026-03-10T06:30:00Z",
	)
	loc := mustLoc(t, "America/New_York")
	if got := utc("2026-03-08T07:30:00Z").In(loc).Format("15:04 MST"); got != "03:30 EDT" {
		t.Fatalf("gap run is %s", got)
	}
}

func TestDailyAcrossNewYorkFallBack(t *testing.T) {
	// Clocks go from 02:00 EDT back to 01:00 EST on 2026-11-01: 01:30 happens twice.
	sp := Spec{Kind: KindDaily, At: "01:30", TimeZone: "America/New_York"}
	expectRuns(t, "01:30 repeated", runs(t, sp, utc("2026-10-31T12:00:00Z"), 3),
		"2026-11-01T05:30:00Z", // the first 01:30 (EDT) only
		"2026-11-02T06:30:00Z", // 01:30 EST
		"2026-11-03T06:30:00Z",
	)
	sp.At = "09:00"
	expectRuns(t, "09:00", runs(t, sp, utc("2026-10-31T00:00:00Z"), 3),
		"2026-10-31T13:00:00Z",
		"2026-11-01T14:00:00Z",
		"2026-11-02T14:00:00Z",
	)
}

func TestDailyAcrossLondonChanges(t *testing.T) {
	// Spring: 01:00 GMT becomes 02:00 BST on 2026-03-29. Autumn: 02:00 BST
	// becomes 01:00 GMT on 2026-10-25.
	sp := Spec{Kind: KindDaily, At: "01:30", TimeZone: "Europe/London"}
	expectRuns(t, "01:30 spring gap", runs(t, sp, utc("2026-03-28T12:00:00Z"), 2),
		"2026-03-29T01:30:00Z", // 02:30 BST
		"2026-03-30T00:30:00Z", // 01:30 BST
	)
	expectRuns(t, "01:30 autumn repeat", runs(t, sp, utc("2026-10-24T12:00:00Z"), 2),
		"2026-10-25T00:30:00Z", // the first 01:30 (BST)
		"2026-10-26T01:30:00Z", // 01:30 GMT
	)
	sp.At = "09:00"
	expectRuns(t, "09:00", runs(t, sp, utc("2026-03-28T00:00:00Z"), 2),
		"2026-03-28T09:00:00Z",
		"2026-03-29T08:00:00Z",
	)
}

func TestMonthlyClampsToMonthEnd(t *testing.T) {
	sp := Spec{Kind: KindMonthly, At: "10:00", DayOfMonth: 31, TimeZone: "UTC"}
	expectRuns(t, "day 31", runs(t, sp, utc("2026-01-15T00:00:00Z"), 5),
		"2026-01-31T10:00:00Z",
		"2026-02-28T10:00:00Z",
		"2026-03-31T10:00:00Z",
		"2026-04-30T10:00:00Z",
		"2026-05-31T10:00:00Z",
	)
	sp.DayOfMonth = 30
	expectRuns(t, "day 30 in a leap year", runs(t, sp, utc("2028-02-01T00:00:00Z"), 2),
		"2028-02-29T10:00:00Z",
		"2028-03-30T10:00:00Z",
	)
	sp.DayOfMonth = 15
	expectRuns(t, "day 15 after this month's run", runs(t, sp, utc("2026-06-15T10:00:00Z"), 1),
		"2026-07-15T10:00:00Z",
	)
}

func TestWeeklyDaySets(t *testing.T) {
	sp := Spec{Kind: KindWeekly, At: "08:00", Days: []string{"friday", "MON", "wed", "mon"}, TimeZone: "Asia/Kolkata"}
	if _, err := sp.Normalize(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sp.Days, []string{"mon", "wed", "fri"}) {
		t.Fatalf("days normalized to %v", sp.Days)
	}
	// 2026-10-07 is a Wednesday. 08:00 IST is 02:30 UTC.
	if wd := utc("2026-10-07T00:00:00Z").Weekday(); wd != time.Wednesday {
		t.Fatalf("2026-10-07 is a %s", wd)
	}
	expectRuns(t, "mon/wed/fri", runs(t, sp, utc("2026-10-07T03:00:00Z"), 4),
		"2026-10-09T02:30:00Z", // Fri
		"2026-10-12T02:30:00Z", // Mon
		"2026-10-14T02:30:00Z", // Wed
		"2026-10-16T02:30:00Z", // Fri
	)
	// Exactly at a run time, the next run is the following one.
	expectRuns(t, "at the run instant", runs(t, sp, utc("2026-10-09T02:30:00Z"), 1), "2026-10-12T02:30:00Z")

	sun := Spec{Kind: KindWeekly, At: "23:59", Days: []string{"sun"}, TimeZone: "America/New_York"}
	expectRuns(t, "sunday late", runs(t, sun, utc("2026-03-01T00:00:00Z"), 2),
		"2026-03-02T04:59:00Z", // Sun 2026-03-01 23:59 EST
		"2026-03-09T03:59:00Z", // Sun 2026-03-08 23:59 EDT
	)
}

func TestOnce(t *testing.T) {
	sp := Spec{Kind: KindOnce, At: "18:45", Date: "2026-12-24", TimeZone: "Asia/Kolkata", Days: []string{"mon"}, DayOfMonth: 3}
	loc, err := sp.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if sp.Days != nil || sp.DayOfMonth != 0 {
		t.Fatalf("fields for other kinds kept: %+v", sp)
	}
	next, ok := sp.Next(utc("2026-10-07T00:00:00Z"), loc)
	if !ok || !next.Equal(utc("2026-12-24T13:15:00Z")) {
		t.Fatalf("once: %s %v", next, ok)
	}
	if _, ok := sp.Next(next, loc); ok {
		t.Fatal("a one-off ran twice")
	}
}

func TestNormalizeRejects(t *testing.T) {
	for name, sp := range map[string]Spec{
		"kind":          {Kind: "hourly", At: "09:00", TimeZone: "UTC"},
		"at":            {Kind: KindDaily, At: "9:00", TimeZone: "UTC"},
		"at range":      {Kind: KindDaily, At: "24:00", TimeZone: "UTC"},
		"zone":          {Kind: KindDaily, At: "09:00", TimeZone: "Mars/Olympus"},
		"local zone":    {Kind: KindDaily, At: "09:00", TimeZone: "Local"},
		"no zone":       {Kind: KindDaily, At: "09:00"},
		"weekly no day": {Kind: KindWeekly, At: "09:00", TimeZone: "UTC"},
		"weekly bad":    {Kind: KindWeekly, At: "09:00", Days: []string{"someday"}, TimeZone: "UTC"},
		"monthly 0":     {Kind: KindMonthly, At: "09:00", TimeZone: "UTC"},
		"monthly 32":    {Kind: KindMonthly, At: "09:00", DayOfMonth: 32, TimeZone: "UTC"},
		"once no date":  {Kind: KindOnce, At: "09:00", TimeZone: "UTC"},
		"once bad date": {Kind: KindOnce, At: "09:00", Date: "2026-02-30", TimeZone: "UTC"},
	} {
		if _, err := sp.Normalize(); err == nil {
			t.Errorf("%s: accepted %+v", name, sp)
		}
	}
}

func TestDescribe(t *testing.T) {
	sp := Spec{Kind: KindWeekly, At: "09:30", Days: []string{"mon", "fri"}, TimeZone: "Asia/Kolkata"}
	if got := sp.Describe(); got != "Every Mon, Fri at 09:30 (Asia/Kolkata)" {
		t.Fatal(got)
	}
}
