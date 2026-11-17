package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

func outcome(value any, err error) any {
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return value
}

func main() {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{
		fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")),
		`INSERT INTO a (id, x) VALUES (1, 10), (2, 20)`,
		`INSERT INTO b (id, a_id, y) VALUES (1, 1, 100), (2, 99, 200)`,
		`INSERT INTO c (id, b_id, z) VALUES (1, 1, 1000), (2, 77, 2000)`,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	right, rightErr := queries.NestedRight(ctx)
	full, fullErr := queries.NestedFull(ctx)
	derived, derivedErr := queries.NestedDerived(ctx)
	offset, offsetErr := queries.CountOffset(ctx)
	limitZero, limitZeroErr := queries.CountLimitZero(ctx)
	fetchZero, fetchZeroErr := queries.CountFetchZero(ctx)
	plain, plainErr := queries.CountPlain(ctx)

	result := map[string]any{
		"nested_right":     outcome(right, rightErr),
		"nested_full":      outcome(full, fullErr),
		"nested_derived":   outcome(derived, derivedErr),
		"count_offset":     outcome(offset, offsetErr),
		"count_limit_zero": outcome(limitZero, limitZeroErr),
		"count_fetch_zero": outcome(fetchZero, fetchZeroErr),
		"count_plain":      outcome(plain, plainErr),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
