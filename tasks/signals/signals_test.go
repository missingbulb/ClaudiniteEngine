package signals

import "testing"

func TestReadLocalTellsAnUnreadableRetentionFromAnAbsentOne(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]map[string]any
		days       *float64
		unreadable bool
	}{
		{"absent", map[string]map[string]any{"acme-pack": {}}, nil, false},
		{"null", map[string]map[string]any{"acme-pack": {"retention_days": nil}}, nil, false},
		{"a number", map[string]map[string]any{"acme-pack": {"retention_days": 7.0}}, ptr(7), false},
		{"a string", map[string]map[string]any{"acme-pack": {"retention_days": "7"}}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := ReadLocal(t.TempDir(), []string{"acme-pack"}, func(id string) map[string]any { return c.config[id] })
			if (l.RetentionDays == nil) != (c.days == nil) || (c.days != nil && *l.RetentionDays != *c.days) || l.RetentionUnreadable != c.unreadable {
				t.Fatalf("days %v, unreadable %v", l.RetentionDays, l.RetentionUnreadable)
			}
		})
	}
}

func ptr(v float64) *float64 { return &v }
