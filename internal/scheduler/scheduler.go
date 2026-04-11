package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cronField represents a single field in a cron expression.
type cronField struct {
	wildcard bool
	values   []int
	step     int
}

// matches reports whether val satisfies this field.
func (f cronField) matches(val int) bool {
	if f.wildcard {
		if f.step > 0 {
			return val%f.step == 0
		}
		return true
	}
	for _, v := range f.values {
		if v == val {
			return true
		}
	}
	return false
}

// cronExpr holds the five fields of a simplified cron expression.
type cronExpr struct {
	minute, hour, dom, month, dow cronField
}

// matches reports whether t satisfies all five fields.
func (e cronExpr) matches(t time.Time) bool {
	return e.minute.matches(t.Minute()) &&
		e.hour.matches(t.Hour()) &&
		e.dom.matches(t.Day()) &&
		e.month.matches(int(t.Month())) &&
		e.dow.matches(int(t.Weekday()))
}

// parseField parses a single cron field string.
// Supported formats:
//   - "*"    → wildcard
//   - "*/N"  → wildcard with step N
//   - "N"    → single value
//   - "N,M"  → comma-separated values
//
// min and max define the valid range for values.
func parseField(s string, min, max int) (cronField, error) {
	if s == "*" {
		return cronField{wildcard: true}, nil
	}

	if strings.HasPrefix(s, "*/") {
		stepStr := s[2:]
		step, err := strconv.Atoi(stepStr)
		if err != nil || step <= 0 {
			return cronField{}, fmt.Errorf("invalid step in field %q", s)
		}
		return cronField{wildcard: true, step: step}, nil
	}

	// Comma-separated values.
	parts := strings.Split(s, ",")
	var values []int
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return cronField{}, fmt.Errorf("invalid value %q in field %q", p, s)
		}
		if v < min || v > max {
			return cronField{}, fmt.Errorf("value %d out of range [%d, %d] in field %q", v, min, max, s)
		}
		values = append(values, v)
	}
	return cronField{values: values}, nil
}

// parseCron parses a 5-field cron expression string.
// Fields: minute hour dom month dow
func parseCron(expr string) (cronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cronExpr{}, fmt.Errorf("cron expression must have exactly 5 fields, got %d", len(fields))
	}

	minute, err := parseField(fields[0], 0, 59)
	if err != nil {
		return cronExpr{}, fmt.Errorf("minute field: %w", err)
	}
	hour, err := parseField(fields[1], 0, 23)
	if err != nil {
		return cronExpr{}, fmt.Errorf("hour field: %w", err)
	}
	dom, err := parseField(fields[2], 1, 31)
	if err != nil {
		return cronExpr{}, fmt.Errorf("dom field: %w", err)
	}
	month, err := parseField(fields[3], 1, 12)
	if err != nil {
		return cronExpr{}, fmt.Errorf("month field: %w", err)
	}
	dow, err := parseField(fields[4], 0, 6)
	if err != nil {
		return cronExpr{}, fmt.Errorf("dow field: %w", err)
	}

	return cronExpr{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

// job pairs a name with its cron schedule.
type job struct {
	name     string
	schedule cronExpr
}

// Scheduler runs registered jobs on a cron schedule, checking every 30 seconds.
type Scheduler struct {
	jobs []job
	fn   func(name string)
	stop chan struct{}
	wg   sync.WaitGroup
}

// New creates a new Scheduler. fn is called with the job name whenever a job fires.
func New(fn func(name string)) *Scheduler {
	return &Scheduler{
		fn:   fn,
		stop: make(chan struct{}),
	}
}

// Add registers a new job with the given name and cron expression string.
func (s *Scheduler) Add(name, cronExprStr string) error {
	expr, err := parseCron(cronExprStr)
	if err != nil {
		return err
	}
	s.jobs = append(s.jobs, job{name: name, schedule: expr})
	return nil
}

// Start launches the background goroutine that checks jobs every 30 seconds.
func (s *Scheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case t := <-ticker.C:
				for _, j := range s.jobs {
					if j.schedule.matches(t) {
						s.fn(j.name)
					}
				}
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}
