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
	"github.com/google/uuid"

	"case_runner/db"
)

const seed = `INSERT INTO devices (id, public_id, owner, last_ip4, last_ip6, embedding) VALUES (1, 'f47ac10b-58cc-4372-a567-0e02b2c3d479', NULL, NULL, '::1', VEC_FromText('[1,2,3]'))`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	owner := uuid.MustParse("0b3c9b1e-6a4e-4b8a-9c39-6f1a2b3c4d5e")
	device := uuid.MustParse("9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d")
	address := "192.0.2.10"
	output.record("add_device", func() (any, error) {
		return "ok", queries.AddDevice(ctx, db.AddDeviceParams{
			ID: 2, PublicID: device, Owner: &owner, LastIp4: &address, LastIp6: "2001:db8::10", Embedding: "[0.5,1,1.5]",
		})
	})
	output.record("list_devices", func() (any, error) { return queries.ListDevices(ctx) })
	output.record("addresses", func() (any, error) { return queries.Addresses(ctx, device) })
	output.record("seeded_addresses", func() (any, error) {
		return queries.Addresses(ctx, uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"))
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
