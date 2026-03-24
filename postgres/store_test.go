package postgres

import (
	"strings"
	"testing"

	"github.com/pupsourcing/store"
)

// Compile-time interface compliance checks.
var (
	_ store.EventStore            = (*Store)(nil)
	_ store.EventReader           = (*Store)(nil)
	_ store.ScopedEventReader     = (*Store)(nil)
	_ store.GlobalPositionReader  = (*Store)(nil)
	_ store.AggregateStreamReader = (*Store)(nil)
)

func TestWithNotifyChannel(t *testing.T) {
	t.Parallel()

	config := NewStoreConfig(WithNotifyChannel("my_events"))

	if config.NotifyChannel != "my_events" {
		t.Errorf("NotifyChannel = %q, want %q", config.NotifyChannel, "my_events")
	}
}

func TestDefaultStoreConfig_NotifyChannelEmpty(t *testing.T) {
	t.Parallel()

	config := DefaultStoreConfig()

	if config.NotifyChannel != "" {
		t.Errorf("NotifyChannel = %q, want empty (disabled by default)", config.NotifyChannel)
	}
}

func TestBuildReadEventsQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		scope        store.ReadScope
		wantArgs     int
		wantContains []string
	}{
		{
			name:     "no scope filters",
			scope:    store.ReadScope{},
			wantArgs: 2,
			wantContains: []string{
				"WHERE global_position > $1",
				"ORDER BY global_position ASC",
				"LIMIT $2",
			},
		},
		{
			name: "aggregate type filter",
			scope: store.ReadScope{
				AggregateTypes: []string{"User"},
			},
			wantArgs: 3,
			wantContains: []string{
				"WHERE global_position > $1",
				"AND aggregate_type = ANY($2)",
				"LIMIT $3",
			},
		},
		{
			name: "multiple aggregate type filters",
			scope: store.ReadScope{
				AggregateTypes: []string{"User", "Order", "Product"},
			},
			wantArgs: 3,
			wantContains: []string{
				"WHERE global_position > $1",
				"AND aggregate_type = ANY($2)",
				"LIMIT $3",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			query, args := buildReadEventsQuery("events", 41, 25, tt.scope)
			normalized := strings.Join(strings.Fields(query), " ")

			for _, want := range tt.wantContains {
				if !strings.Contains(normalized, want) {
					t.Fatalf("query %q missing %q", normalized, want)
				}
			}

			if len(args) != tt.wantArgs {
				t.Fatalf("len(args) = %d, want %d", len(args), tt.wantArgs)
			}
			if args[0] != int64(41) {
				t.Fatalf("args[0] = %v, want 41", args[0])
			}
			if args[len(args)-1] != 25 {
				t.Fatalf("last arg = %v, want 25", args[len(args)-1])
			}
		})
	}
}
