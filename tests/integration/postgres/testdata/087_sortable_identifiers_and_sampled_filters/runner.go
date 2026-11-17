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
		`INSERT INTO items (id, code, "Mixed", "select", "a.b") VALUES (1, 10, 'B', 'y', 'p'), (2, 9, 'A', 'x', 'q'), (3, 100, 'C', 'z', 'r')`,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	byMixed, byMixedErr := queries.SortedOdd(ctx, db.SortedOddParams{OrderBy: db.SortedOddOrderByMixed, OrderByDirection: db.OrderDesc})
	bySelect, bySelectErr := queries.SortedOdd(ctx, db.SortedOddParams{OrderBy: db.SortedOddOrderBySelect, OrderByDirection: db.OrderAsc})
	sampled, sampledErr := queries.Sampled().Where("code", ">", 9).OrderBy("id", "ASC").All(ctx)
	sampledCount, sampledCountErr := queries.Sampled().Where("code", ">", 9).Count(ctx)

	result := map[string]any{
		"sorted_by_mixed_desc":   outcome(byMixed, byMixedErr),
		"sorted_by_reserved_asc": outcome(bySelect, bySelectErr),
		"sampled_filtered":       outcome(sampled, sampledErr),
		"sampled_count":          outcome(sampledCount, sampledCountErr),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
