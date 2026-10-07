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
	"math/big"
	"net/netip"
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

	first := netip.MustParseAddr("10.0.0.1")
	second := netip.MustParseAddr("2001:db8::1")
	mapped := netip.MustParseAddr("::ffff:10.0.0.9")
	wide, _ := new(big.Int).SetString("-170141183460469231731687303715884105728", 10)
	huge, _ := new(big.Int).SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)

	copied := []db.CopyHostsParams{
		{
			ID:             1,
			Keyed:          map[string]netip.Addr{"a": first, "m": mapped},
			KeyedV6:        map[string]netip.Addr{"v6": second, "v4": first},
			Optional:       map[string]*netip.Addr{"set": &first, "unset": nil},
			Listed:         map[string][]netip.Addr{"two": {first, mapped}, "none": {}},
			ListedOptional: map[string][]*netip.Addr{"mixed": {&second, nil}},
			Nested:         map[string]map[string]netip.Addr{"outer": {"inner": second}, "empty": {}},
			Maps:           []map[string]netip.Addr{{"k": first}, {}},
			Holder:         db.HostsHolder{A: 7, M: map[string]netip.Addr{"held": first}},
			Pairs:          map[string]db.HostsPairsValue{"p": {X: 3, IP: first}},
			ListedPairs:    map[string][]db.HostsListedPairsValueItem{"l": {{X: 4, Y: "four"}, {X: 5, Y: "five"}}},
			Unnamed:        map[string][]any{"u": {uint8(6), first}},
			Wide:           map[string]*big.Int{"min": wide, "zero": big.NewInt(0)},
			WideKeyed:      map[*big.Int]string{huge: "max"},
			NullableWide:   map[string]*big.Int{"set": wide, "unset": nil},
			WideLists:      map[string][]*big.Int{"list": {big.NewInt(-1), big.NewInt(2)}},
		},
		{ID: 2},
	}
	if err := queries.CopyHosts(ctx, copied); err != nil {
		return fmt.Errorf("copy hosts: %w", err)
	}
	batched := []db.InsertHostsParams{
		{
			ID:             3,
			Keyed:          map[string]netip.Addr{"b": netip.MustParseAddr("192.0.2.1")},
			KeyedV6:        map[string]netip.Addr{"loopback": netip.IPv6Loopback()},
			Optional:       map[string]*netip.Addr{"unset": nil},
			Listed:         map[string][]netip.Addr{"one": {first}},
			ListedOptional: map[string][]*netip.Addr{"none": nil},
			Nested:         map[string]map[string]netip.Addr{"outer": {"a": first, "b": second}},
			Maps:           []map[string]netip.Addr{{"x": mapped}},
			Holder:         db.HostsHolder{A: 8, M: map[string]netip.Addr{}},
			Pairs:          map[string]db.HostsPairsValue{"q": {X: 9, IP: mapped}, "r": {X: 10, IP: first}},
			ListedPairs:    map[string][]db.HostsListedPairsValueItem{"empty": {}},
			Unnamed:        map[string][]any{},
			Wide:           map[string]*big.Int{},
			WideKeyed:      map[*big.Int]string{big.NewInt(1): "one"},
			NullableWide:   map[string]*big.Int{},
			WideLists:      map[string][]*big.Int{"empty": {}},
		},
	}
	if err := queries.InsertHosts(ctx, batched); err != nil {
		return fmt.Errorf("insert hosts: %w", err)
	}
	if err := queries.CopyHosts(ctx, nil); err != nil {
		return fmt.Errorf("copy no hosts: %w", err)
	}

	hosts, err := queries.ListHosts(ctx)
	if err != nil {
		return fmt.Errorf("list hosts: %w", err)
	}
	typed := map[string]any{
		"mapped_into_ipv4": hosts[0].Keyed["m"].Is4(),
		"ipv4_into_ipv6":   hosts[0].KeyedV6["v4"].Is4In6(),
		"pair_address":     hosts[2].Pairs["q"].IP.String(),
		"unset_is_nil":     hosts[0].Optional["unset"] == nil,
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{"hosts": hosts, "typed": typed})
}
