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
		`CREATE TABLE items (id BIGINT NOT NULL, label VARCHAR NOT NULL, note VARCHAR)`,
		`CREATE VIEW renamed_items (item_id) AS SELECT id, label, note FROM items`,
		`INSERT INTO items VALUES (1, 'first', NULL), (2, 'second', 'kept')`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	queries := db.New(database)
	viewItems, viewErr := queries.ViewItems(ctx)
	cteItems, cteErr := queries.CTEItems(ctx)
	derivedItems, derivedErr := queries.DerivedItems(ctx)
	result := map[string]any{
		"view_items":    outcome(viewItems, viewErr),
		"cte_items":     outcome(cteItems, cteErr),
		"derived_items": outcome(derivedItems, derivedErr),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
