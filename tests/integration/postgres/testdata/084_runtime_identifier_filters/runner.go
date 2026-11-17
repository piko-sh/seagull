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
	castText, castTextErr := queries.CastAlias().Where("code_text", ">", "9").OrderBy("id", "ASC").All(ctx)
	mixed, mixedErr := queries.OddNames().Where("Mixed", "=", "A").All(ctx)
	reserved, reservedErr := queries.OddNames().Where("select", "=", "y").All(ctx)
	dotted, dottedErr := queries.OddNames().Where("a.b", "=", "q").All(ctx)
	byMixed, byMixedErr := queries.OddNames().OrderBy("Mixed", "DESC").All(ctx)
	byDotted, byDottedErr := queries.OddNames().OrderBy("a.b", "DESC").All(ctx)
	counted, countedErr := queries.OddNames().Where("select", "<>", "y").Count(ctx)

	result := map[string]any{
		"cast_alias_text_comparison": outcome(castText, castTextErr),
		"mixed_case_filter":          outcome(mixed, mixedErr),
		"reserved_filter":            outcome(reserved, reservedErr),
		"dotted_filter":              outcome(dotted, dottedErr),
		"mixed_case_order":           outcome(byMixed, byMixedErr),
		"dotted_order":               outcome(byDotted, byDottedErr),
		"reserved_count":             outcome(counted, countedErr),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
