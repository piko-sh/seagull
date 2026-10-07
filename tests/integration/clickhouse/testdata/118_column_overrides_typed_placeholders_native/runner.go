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
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func address(text string) net.IP {
	return net.ParseIP(text)
}

func label(name string) json.RawMessage {
	return json.RawMessage(`{"name":"` + name + `"}`)
}

func run() error {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	options.Settings["mutations_sync"] = 2
	conn, err := clickhouse.Open(options)
	if err != nil {
		return fmt.Errorf("open native connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)
	previous := address("10.9.0.1")

	if err := queries.RecordDevice(ctx, db.RecordDeviceParams{ID: 1, Addr: address("10.0.0.1"), Label: label("exec"), Previous: &previous}); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	copied := []db.CopyDevicesParams{
		{ID: 2, Addr: address("10.0.0.2"), Label: label("copy"), Previous: nil},
		{ID: 3, Addr: address("10.0.0.3"), Label: label("copy"), Previous: &previous},
	}
	if err := queries.CopyDevices(ctx, copied); err != nil {
		return fmt.Errorf("copyfrom: %w", err)
	}
	batched := []db.InsertDevicesParams{
		{ID: 4, Addr: address("10.0.0.4"), Label: label("batch"), Previous: nil},
		{ID: 5, Addr: address("10.0.0.5"), Label: label("batch"), Previous: nil},
	}
	if err := queries.InsertDevices(ctx, batched); err != nil {
		return fmt.Errorf("batch of lone placeholders: %w", err)
	}
	rows := []db.InsertDeviceRowsParams{{ID: 6, Addr: address("10.0.0.6"), Label: label("rows"), Previous: &previous}}
	if err := queries.InsertDeviceRows(ctx, rows); err != nil {
		return fmt.Errorf("batch of rows: %w", err)
	}

	byAddress, err := queries.FindByAddress(ctx, address("10.0.0.3"))
	if err != nil {
		return fmt.Errorf("compare: %w", err)
	}
	byAddresses, err := queries.FindByAddresses(ctx, db.FindByAddressesParams{First: address("10.0.0.1"), Second: address("10.0.0.4")})
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}
	between, err := queries.FindBetween(ctx, db.FindBetweenParams{Low: address("10.0.0.2"), High: address("10.0.0.5"), Label: label("batch")})
	if err != nil {
		return fmt.Errorf("bound: %w", err)
	}
	byPrevious, err := queries.FindByPrevious(ctx, &previous)
	if err != nil {
		return fmt.Errorf("compare nullable: %w", err)
	}
	if err := queries.MoveDevice(ctx, db.MoveDeviceParams{Addr: address("10.0.1.1"), Label: label("moved"), ID: 1}); err != nil {
		return fmt.Errorf("alter update: %w", err)
	}
	devices, err := queries.ListDevices(ctx)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"by_address":   byAddress,
		"by_addresses": byAddresses,
		"between":      between,
		"by_previous":  byPrevious,
		"devices":      devices,
	})
}
