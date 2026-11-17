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
		`INSERT INTO items (id, tags, other) VALUES (1, '{b}', '{z}'), (2, '{a}', '{a}'), (3, '{a}', '{y}')`,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	ordered, orderedErr := queries.OrderedTags(ctx)
	distinct, distinctErr := queries.DistinctTags(ctx)
	union, unionErr := queries.UnionTags(ctx)
	grouped, groupedErr := queries.GroupedTags(ctx)

	result := map[string]any{
		"ordered_tags":  outcome(ordered, orderedErr),
		"distinct_tags": outcome(distinct, distinctErr),
		"union_tags":    outcome(union, unionErr),
		"grouped_tags":  outcome(grouped, groupedErr),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
