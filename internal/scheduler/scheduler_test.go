package scheduler

import (
	"testing"
	"time"
)

func TestParseCron(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{name: "specific minute/hour/dow", expr: "0 3 * * 0", wantErr: false},
		{name: "step wildcard", expr: "*/5 * * * *", wantErr: false},
		{name: "first of month", expr: "0 0 1 * *", wantErr: false},
		{name: "bad expression", expr: "bad", wantErr: true},
		{name: "empty expression", expr: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCron(tc.expr)
			if (err != nil) != tc.wantErr {
				t.Errorf("parseCron(%q) error = %v, wantErr = %v", tc.expr, err, tc.wantErr)
			}
		})
	}
}

func TestCronFieldMatches(t *testing.T) {
	t.Run("wildcard matches all", func(t *testing.T) {
		f := cronField{wildcard: true}
		for _, v := range []int{0, 15, 30, 59} {
			if !f.matches(v) {
				t.Errorf("wildcard should match %d", v)
			}
		}
	})

	t.Run("wildcard with step", func(t *testing.T) {
		f := cronField{wildcard: true, step: 5}
		if !f.matches(0) {
			t.Error("step=5 should match 0")
		}
		if !f.matches(15) {
			t.Error("step=5 should match 15")
		}
		if f.matches(7) {
			t.Error("step=5 should not match 7")
		}
	})

	t.Run("value list", func(t *testing.T) {
		f := cronField{values: []int{1, 15, 30}}
		if !f.matches(1) {
			t.Error("should match 1")
		}
		if !f.matches(15) {
			t.Error("should match 15")
		}
		if f.matches(2) {
			t.Error("should not match 2")
		}
	})
}

func TestCronExprMatches(t *testing.T) {
	// "0 3 * * 0" — minute=0, hour=3, dom=*, month=*, dow=0 (Sunday)
	expr, err := parseCron("0 3 * * 0")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	// Sunday 2024-01-07 03:00
	sunday := time.Date(2024, 1, 7, 3, 0, 0, 0, time.UTC)
	if !expr.matches(sunday) {
		t.Error("should match Sunday 03:00")
	}

	// Monday 2024-01-08 03:00 — dow mismatch
	monday := time.Date(2024, 1, 8, 3, 0, 0, 0, time.UTC)
	if expr.matches(monday) {
		t.Error("should not match Monday 03:00")
	}
}

func TestSchedulerStartStop(t *testing.T) {
	fired := make([]string, 0)
	s := New(func(name string) {
		fired = append(fired, name)
	})

	if err := s.Add("test-job", "*/5 * * * *"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	s.Start()
	time.Sleep(100 * time.Millisecond)
	s.Stop() // must not panic
}

func TestSchedulerAddInvalidCron(t *testing.T) {
	s := New(func(name string) {})
	if err := s.Add("bad-job", "not a cron"); err == nil {
		t.Error("expected error for invalid cron expression")
	}
}
