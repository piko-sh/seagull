package main

import (
	"case_runner/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
)

func outcome(value any, err error) any {
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return value
}

func main() {
	database, err := sql.Open(databaseDriver, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, statement := range []string{
		`CREATE TABLE prices (id BIGINT NOT NULL, amount DECIMAL(10, 2) NOT NULL, wait INTERVAL NOT NULL, tags VARCHAR[] NOT NULL)`,
		`INSERT INTO prices VALUES (1, 9.00, INTERVAL 1 DAY, ['b']), (2, 10.00, INTERVAL 24 HOURS, ['a']), (3, 100.50, INTERVAL 2 HOURS, ['a'])`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	var intervalsEqual bool
	if err := database.QueryRowContext(ctx, `SELECT INTERVAL 1 DAY = INTERVAL 24 HOURS`).Scan(&intervalsEqual); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var distinctNative int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT DISTINCT wait FROM prices)`).Scan(&distinctNative); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	queries := db.New(database)
	ordered, orderedErr := queries.OrderedAmounts(ctx)
	waits, waitsErr := queries.DistinctWaits(ctx)
	amounts, amountsErr := queries.DistinctAmounts(ctx)
	union, unionErr := queries.UnionAmounts(ctx)
	aliasOrdered, aliasOrderedErr := queries.AliasOrderedAmounts(ctx)
	ordinalOrdered, ordinalOrderedErr := queries.OrdinalOrderedAmounts(ctx)
	waitsDirect, waitsDirectErr := queries.DistinctWaitsDirect(ctx)
	unionDirect, unionDirectErr := queries.UnionAmountsDirect(ctx)
	result := map[string]any{
		"ordered_amounts":         outcome(ordered, orderedErr),
		"distinct_waits":          outcome(waits, waitsErr),
		"distinct_amounts":        len(amounts),
		"distinct_amounts_error":  outcome(nil, amountsErr),
		"union_amounts":           outcome(union, unionErr),
		"alias_ordered_amounts":   outcome(aliasOrdered, aliasOrderedErr),
		"ordinal_ordered_amounts": outcome(ordinalOrdered, ordinalOrderedErr),
		"distinct_waits_direct":   outcome(waitsDirect, waitsDirectErr),
		"union_amounts_direct":    outcome(unionDirect, unionDirectErr),
		"native_intervals_equal":  intervalsEqual,
		"native_distinct_waits":   distinctNative,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
