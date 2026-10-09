package handlers

import (
	"net/http"
	"time"
)

// parseDateRange reads from/to or start_date/end_date query params.
// Defaults: last 30 days through end of today UTC.
func parseDateRange(r *http.Request) (from, to *time.Time) {
	q := r.URL.Query()
	parse := func(keys ...string) *time.Time {
		for _, k := range keys {
			v := q.Get(k)
			if v == "" {
				continue
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return &t
			}
			if t, err := time.Parse("2006-01-02", v); err == nil {
				return &t
			}
		}
		return nil
	}
	from = parse("from", "start_date", "startDate")
	to = parse("to", "end_date", "endDate")
	now := time.Now().UTC()
	if to == nil {
		end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
		to = &end
	} else {
		// inclusive end of day if date-only
		t := *to
		if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
			to = &t
		}
	}
	if from == nil {
		f := to.AddDate(0, 0, -30)
		f = time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, time.UTC)
		from = &f
	}
	return from, to
}
