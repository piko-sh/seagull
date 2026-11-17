package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"case_runner/db"
)

func outcome(value any, err error) any {
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return value
}

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{
		`CREATE TABLE events (id INTEGER PRIMARY KEY, created_at DATETIME NOT NULL, label TEXT NOT NULL)`,
		`INSERT INTO events (id, created_at, label) VALUES (1, '2024-05-06 07:08:09', '2024-05-06 07:08:09'), (2, '2025-01-02 03:04:05', '2025-01-02 03:04:05')`,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	latest, latestErr := queries.LatestEvent(ctx)
	earliest, earliestErr := queries.EarliestEvent(ctx)
	coalesced, coalescedErr := queries.CoalescedTimes(ctx)
	cast, castErr := queries.CastLabels(ctx)
	direct, directErr := queries.DirectTimes(ctx)

	result := map[string]any{
		"latest_event":    outcome(latest, latestErr),
		"earliest_event":  outcome(earliest, earliestErr),
		"coalesced_times": outcome(coalesced, coalescedErr),
		"cast_labels":     outcome(cast, castErr),
		"direct_times":    outcome(direct, directErr),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
