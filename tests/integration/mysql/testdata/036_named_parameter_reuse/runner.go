// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	boats := []db.AddBoatParams{
		{ID: 1, Name: "Puffin", Nickname: "Puff"},
		{ID: 2, Name: "Kittiwake", Nickname: "Puffin"},
		{ID: 3, Name: "Gannet", Nickname: "Plunger"},
		{ID: 4, Name: "Tern", Nickname: "Arctic"},
	}
	for _, boat := range boats {
		if err := queries.AddBoat(ctx, boat); err != nil {
			return fmt.Errorf("add boat %d: %w", boat.ID, err)
		}
	}

	byName, err := queries.FindBoatsByTerm(ctx, "Puffin")
	if err != nil {
		return fmt.Errorf("find by term: %w", err)
	}
	byJoined, err := queries.FindBoatsByTerm(ctx, "Gannet-Plunger")
	if err != nil {
		return fmt.Errorf("find by joined term: %w", err)
	}
	numbered, err := queries.FindBoatsNumbered(ctx, db.FindBoatsNumberedParams{Name: "Puffin", ID: 4})
	if err != nil {
		return fmt.Errorf("find numbered: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"byName":   byName,
		"byJoined": byJoined,
		"numbered": numbered,
	})
}
