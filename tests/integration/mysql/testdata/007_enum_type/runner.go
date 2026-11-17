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
	connectionString := os.Getenv("DATABASE_URL")

	conn, err := sql.Open("mysql", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	queries := db.New(conn)

	err = queries.InsertTicket(ctx, db.InsertTicketParams{
		Title:    "Fix login bug",
		Priority: "high",
		Status:   "open",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertTicket 1:", err)
		os.Exit(1)
	}

	err = queries.InsertTicket(ctx, db.InsertTicketParams{
		Title:    "Update documentation",
		Priority: "low",
		Status:   "in_progress",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertTicket 2:", err)
		os.Exit(1)
	}

	err = queries.InsertTicket(ctx, db.InsertTicketParams{
		Title:    "Database migration",
		Priority: "high",
		Status:   "open",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertTicket 3:", err)
		os.Exit(1)
	}

	err = queries.InsertTicket(ctx, db.InsertTicketParams{
		Title:    "Add unit tests",
		Priority: "medium",
		Status:   "closed",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertTicket 4:", err)
		os.Exit(1)
	}

	ticket, err := queries.GetTicket(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetTicket:", err)
		os.Exit(1)
	}

	highPriority, err := queries.ListByPriority(ctx, "high")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListByPriority:", err)
		os.Exit(1)
	}

	allTickets, err := queries.ListAll(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListAll:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"ticket":        ticket,
		"high_priority": highPriority,
		"all_tickets":   allTickets,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
