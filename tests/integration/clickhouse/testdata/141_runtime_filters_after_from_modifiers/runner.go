package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func outcome(value any, err error) any {
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return value
}

func main() {
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `INSERT INTO events VALUES (1, 'a', ['x', 'y'], 1), (2, 'b', ['y'], 1), (2, 'c', ['z'], 2), (3, 'a', [], 1)`); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	queries := db.New(conn)
	finalised, finalisedErr := queries.Finalised().Where("kind", "<>", "a").OrderBy("id", "ASC").All(ctx)
	finalisedCount, finalisedCountErr := queries.Finalised().Where("kind", "<>", "a").Count(ctx)
	sampled, sampledErr := queries.Sampled().Where("kind", "=", "a").OrderBy("id", "ASC").All(ctx)
	unrolled, unrolledErr := queries.Unrolled().Where("tag", "=", "y").OrderBy("id", "ASC").All(ctx)
	unrolledCount, unrolledCountErr := queries.Unrolled().Where("tag", "=", "y").Count(ctx)
	prefiltered, prefilteredErr := queries.Prefiltered().Where("kind", "=", "a").OrderBy("id", "ASC").All(ctx)
	result := map[string]any{
		"finalised":       outcome(finalised, finalisedErr),
		"finalised_count": outcome(finalisedCount, finalisedCountErr),
		"sampled":         outcome(sampled, sampledErr),
		"unrolled":        outcome(unrolled, unrolledErr),
		"unrolled_count":  outcome(unrolledCount, unrolledCountErr),
		"prefiltered":     outcome(prefiltered, prefilteredErr),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
