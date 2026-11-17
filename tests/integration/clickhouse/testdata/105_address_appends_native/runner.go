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
	"net/netip"
	"os"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

type refusal struct {
	Path           string `json:"path"`
	Parameter      string `json:"parameter"`
	Value          string `json:"value"`
	Reason         string `json:"reason"`
	Refused        bool   `json:"refused"`
	NamesParameter bool   `json:"names_parameter"`
}

type mutation struct {
	change    func(*db.CopyAddressesParams)
	parameter string
	value     string
}

type path struct {
	write  func(context.Context, []db.CopyAddressesParams) error
	name   string
	atomic bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return fmt.Errorf("open native connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)
	paths := writingPaths(queries)

	for index, writing := range paths {
		rows := []db.CopyAddressesParams{validRow(uint64(index*2+1), false), validRow(uint64(index*2+2), true)}
		if err := writing.write(ctx, rows); err != nil {
			return fmt.Errorf("%s valid rows: %w", writing.name, err)
		}
	}

	var refusals []refusal
	for _, writing := range paths {
		for _, change := range mutations() {
			invalid := validRow(100, false)
			change.change(&invalid)
			rows := []db.CopyAddressesParams{validRow(99, false), invalid}
			if !writing.atomic {
				rows = rows[1:]
			}
			err := writing.write(ctx, rows)
			refusals = append(refusals, describe(writing.name, change, err))
		}
	}

	addresses, err := queries.ListAddresses(ctx)
	if err != nil {
		return fmt.Errorf("list addresses: %w", err)
	}
	typed := map[string]any{
		"mapped_host_is_ipv4":  addresses[1].Host.Is4(),
		"mapped_alias_is_ipv4": addresses[1].Aliases["mapped"].Is4(),
		"ipv4_peer_is_mapped":  addresses[0].Peer.Is4In6(),
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"addresses": addresses, "refusals": refusals, "typed": typed})
}

func writingPaths(queries *db.Queries) []path {
	return []path{
		{name: "copyfrom", atomic: true, write: func(ctx context.Context, rows []db.CopyAddressesParams) error {
			return queries.CopyAddresses(ctx, rows)
		}},
		{name: "batch of lone placeholders", atomic: true, write: func(ctx context.Context, rows []db.CopyAddressesParams) error {
			converted := make([]db.InsertAddressesParams, len(rows))
			for index, row := range rows {
				converted[index] = db.InsertAddressesParams(row)
			}
			return queries.InsertAddresses(ctx, converted)
		}},
		{name: "batch of rows", atomic: true, write: func(ctx context.Context, rows []db.CopyAddressesParams) error {
			converted := make([]db.InsertAddressRowsParams, len(rows))
			for index, row := range rows {
				converted[index] = db.InsertAddressRowsParams(row)
			}
			return queries.InsertAddressRows(ctx, converted)
		}},
		{name: "exec", write: func(ctx context.Context, rows []db.CopyAddressesParams) error {
			for _, row := range rows {
				if err := queries.RecordAddress(ctx, db.RecordAddressParams(row)); err != nil {
					return err
				}
			}
			return nil
		}},
	}
}

func validRow(id uint64, mapped bool) db.CopyAddressesParams {
	address := func(last int) netip.Addr {
		if mapped {
			return netip.MustParseAddr(fmt.Sprintf("::ffff:10.0.%d.%d", id, last))
		}
		return netip.MustParseAddr(fmt.Sprintf("10.0.%d.%d", id, last))
	}
	gateway := address(2)
	routed := address(5)
	fallback := address(10)
	return db.CopyAddressesParams{
		ID:        id,
		Host:      address(1),
		Peer:      address(12),
		Gateway:   &gateway,
		Relay:     nil,
		Hops:      []netip.Addr{address(3), address(4)},
		Routes:    []*netip.Addr{&routed, nil},
		Link:      db.AddressesLink{Near: address(6), Far: netip.MustParseAddr("2001:db8::7")},
		Origin:    []any{address(8), uint8(id)},
		Aliases:   map[string]netip.Addr{"mapped": address(9)},
		Fallbacks: map[string]*netip.Addr{"set": &fallback, "unset": nil},
		Mirrors:   map[string]netip.Addr{"v4": address(11), "v6": netip.MustParseAddr("2001:db8::b")},
	}
}

func mutations() []mutation {
	zero := netip.Addr{}
	wide := netip.MustParseAddr("2001:db8::1")
	return []mutation{
		{parameter: "host", value: "zero", change: func(row *db.CopyAddressesParams) { row.Host = zero }},
		{parameter: "host", value: "ipv6", change: func(row *db.CopyAddressesParams) { row.Host = wide }},
		{parameter: "peer", value: "zero", change: func(row *db.CopyAddressesParams) { row.Peer = zero }},
		{parameter: "gateway", value: "zero", change: func(row *db.CopyAddressesParams) { row.Gateway = &zero }},
		{parameter: "gateway", value: "ipv6", change: func(row *db.CopyAddressesParams) { row.Gateway = &wide }},
		{parameter: "relay", value: "zero", change: func(row *db.CopyAddressesParams) { row.Relay = &zero }},
		{parameter: "hops", value: "ipv6", change: func(row *db.CopyAddressesParams) { row.Hops = append(row.Hops, wide) }},
		{parameter: "routes", value: "zero", change: func(row *db.CopyAddressesParams) { row.Routes = append(row.Routes, &zero) }},
		{parameter: "link", value: "ipv6", change: func(row *db.CopyAddressesParams) { row.Link.Near = wide }},
		{parameter: "link", value: "zero", change: func(row *db.CopyAddressesParams) { row.Link.Far = zero }},
		{parameter: "origin", value: "ipv6", change: func(row *db.CopyAddressesParams) { row.Origin = []any{wide, uint8(1)} }},
		{parameter: "aliases", value: "ipv6", change: func(row *db.CopyAddressesParams) { row.Aliases["wide"] = wide }},
		{parameter: "fallbacks", value: "zero", change: func(row *db.CopyAddressesParams) { row.Fallbacks["zero"] = &zero }},
		{parameter: "mirrors", value: "zero", change: func(row *db.CopyAddressesParams) { row.Mirrors["zero"] = zero }},
	}
}

func describe(pathName string, change mutation, err error) refusal {
	described := refusal{Path: pathName, Parameter: change.parameter, Value: change.value, Refused: err != nil}
	if err == nil {
		return described
	}
	message := err.Error()
	described.NamesParameter = strings.Contains(message, change.parameter)
	switch {
	case strings.Contains(message, "holds no address"):
		described.Reason = "no address"
	case strings.Contains(message, "which an IPv4 value cannot hold"):
		described.Reason = "an IPv6 address for an IPv4 value"
	case strings.Contains(message, "is not an IP address"):
		described.Reason = "not an address"
	default:
		described.Reason = message
	}
	return described
}
