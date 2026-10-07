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
	"reflect"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
	"case_runner/kinds"
)

const seed = `INSERT INTO probes (id, label, amount, ratio, flag, note, payload, optional_label, optional_amount, tags, host, optional_host, address, pair, keyed) VALUES
	(1, 'alpha', 7, 2.5, true, '{"n":1}', '\x01', 'beta', 9, ['x', 'y'], '10.0.0.1', '10.9.0.1', '192.0.2.1', ('alpha', 1), {'alpha': 1}),
	(2, 'gamma', 8, 3.5, false, '{"n":2}', '\x02', 'beta', 9, ['z'], '10.0.0.2', '10.9.0.1', '192.0.2.2', ('gamma', 2), {'gamma': 2}),
	(3, 'delta', 9, 4.5, true, '{"n":3}', '\x03', NULL, NULL, [], '10.0.0.3', NULL, '192.0.2.3', ('delta', 3), {'delta': 3})`

type cells map[string]string

func (c cells) expect(name string, want any, read func() (any, error)) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c[name] = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	got, err := read()
	switch {
	case err != nil:
		c[name] = err.Error()
	case !reflect.DeepEqual(got, want):
		c[name] = fmt.Sprintf("read %#v, want %#v", got, want)
	default:
		c[name] = "ok"
	}
}

func (c cells) record(name string, write func() error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c[name] = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	if err := write(); err != nil {
		c[name] = err.Error()
		return
	}
	c[name] = "ok"
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return err
	}
	defer conn.Close()
	queries := db.New(conn)
	results := cells{}

	if err := conn.Exec(ctx, seed); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	beta := kinds.Label("beta")
	nine := kinds.Count(9)
	theta := kinds.Label("theta")
	ninety := kinds.Count(90)
	gateway := json.Number("10.9.0.1")
	router := json.Number("10.9.1.1")
	results.record("exec", func() error {
		return queries.RecordProbe(ctx, db.RecordProbeParams{ID: 11, Label: "written 11", Amount: 110, Ratio: 11.25, Flag: true, Note: json.RawMessage(`{"n":11}`), Payload: kinds.Blob{11}, OptionalLabel: &theta, OptionalAmount: &ninety, Tags: []kinds.Label{"w", "11"}, Host: json.Number("10.0.1.11"), OptionalHost: &router, Address: netip.MustParseAddr("198.51.100.11"), Pair: db.ProbesPair{Name: "written", Rank: 11}, Keyed: map[kinds.Label]kinds.Count{"w": 11}})
	})
	results.record("batch", func() error {
		return queries.RecordProbes(ctx, []db.RecordProbesParams{
			{ID: 12, Label: "written 12", Amount: 120, Ratio: 12.25, Flag: true, Note: json.RawMessage(`{"n":12}`), Payload: kinds.Blob{12}, OptionalLabel: &theta, OptionalAmount: &ninety, Tags: []kinds.Label{"w", "12"}, Host: json.Number("10.0.1.12"), OptionalHost: &router, Address: netip.MustParseAddr("198.51.100.12"), Pair: db.ProbesPair{Name: "written", Rank: 12}, Keyed: map[kinds.Label]kinds.Count{"w": 12}},
			{ID: 13, Label: "written 13", Amount: 130, Ratio: 13.25, Flag: true, Note: json.RawMessage(`{"n":13}`), Payload: kinds.Blob{13}, OptionalLabel: nil, OptionalAmount: nil, Tags: []kinds.Label{}, Host: json.Number("10.0.1.13"), OptionalHost: nil, Address: netip.MustParseAddr("198.51.100.13"), Pair: db.ProbesPair{Name: "written", Rank: 13}, Keyed: map[kinds.Label]kinds.Count{"w": 13}},
		})
	})
	results.record("copyfrom", func() error {
		return queries.CopyProbes(ctx, []db.CopyProbesParams{
			{ID: 14, Label: "written 14", Amount: 140, Ratio: 14.25, Flag: true, Note: json.RawMessage(`{"n":14}`), Payload: kinds.Blob{14}, OptionalLabel: &theta, OptionalAmount: &ninety, Tags: []kinds.Label{"w", "14"}, Host: json.Number("10.0.1.14"), OptionalHost: &router, Address: netip.MustParseAddr("198.51.100.14"), Pair: db.ProbesPair{Name: "written", Rank: 14}, Keyed: map[kinds.Label]kinds.Count{"w": 14}},
			{ID: 15, Label: "written 15", Amount: 150, Ratio: 15.25, Flag: true, Note: json.RawMessage(`{"n":15}`), Payload: kinds.Blob{15}, OptionalLabel: nil, OptionalAmount: nil, Tags: []kinds.Label{}, Host: json.Number("10.0.1.15"), OptionalHost: nil, Address: netip.MustParseAddr("198.51.100.15"), Pair: db.ProbesPair{Name: "written", Rank: 15}, Keyed: map[kinds.Label]kinds.Count{"w": 15}},
		})
	})
	results.record("batch rows", func() error {
		return queries.RecordProbeRows(ctx, []db.RecordProbeRowsParams{
			{ID: 16, Label: "written 16", Amount: 160, Ratio: 16.25, Flag: true, Note: json.RawMessage(`{"n":16}`), Payload: kinds.Blob{16}, OptionalLabel: &theta, OptionalAmount: &ninety, Tags: []kinds.Label{"w", "16"}, Host: json.Number("10.0.1.16"), OptionalHost: &router, Address: netip.MustParseAddr("198.51.100.16"), Pair: db.ProbesPair{Name: "written", Rank: 16}, Keyed: map[kinds.Label]kinds.Count{"w": 16}},
		})
	})

	results.expect("parameter label", []db.FindByLabelRow{{ID: 1}}, func() (any, error) { return queries.FindByLabel(ctx, "alpha") })
	results.expect("parameter amount", []db.FindByAmountRow{{ID: 1}}, func() (any, error) { return queries.FindByAmount(ctx, 7) })
	results.expect("parameter ratio", []db.FindByRatioRow{{ID: 1}}, func() (any, error) { return queries.FindByRatio(ctx, 2.5) })
	results.expect("parameter flag", []db.FindByFlagRow{{ID: 2}}, func() (any, error) { return queries.FindByFlag(ctx, false) })
	results.expect("parameter note", []db.FindByNoteRow{{ID: 1}}, func() (any, error) { return queries.FindByNote(ctx, json.RawMessage(`{"n":1}`)) })
	results.expect("parameter payload", []db.FindByPayloadRow{{ID: 1}}, func() (any, error) { return queries.FindByPayload(ctx, kinds.Blob{1}) })
	results.expect("parameter optional label", []db.FindByOptionalLabelRow{{ID: 1}, {ID: 2}}, func() (any, error) { return queries.FindByOptionalLabel(ctx, &beta) })
	results.expect("parameter tags", []db.FindByTagsRow{{ID: 1}}, func() (any, error) { return queries.FindByTags(ctx, []kinds.Label{"x", "y"}) })
	results.expect("parameter host", []db.FindByHostRow{{ID: 1}}, func() (any, error) { return queries.FindByHost(ctx, json.Number("10.0.0.1")) })
	results.expect("parameter optional host", []db.FindByOptionalHostRow{{ID: 1}, {ID: 2}}, func() (any, error) { return queries.FindByOptionalHost(ctx, &gateway) })
	results.expect("parameter address", []db.FindByAddressRow{{ID: 1}}, func() (any, error) { return queries.FindByAddress(ctx, netip.MustParseAddr("192.0.2.1")) })

	results.expect("scan label", kinds.Label("alpha"), func() (any, error) {
		row, err := queries.ReadLabel(ctx, 1)
		return row.Label, err
	})
	results.expect("scan amount", kinds.Count(7), func() (any, error) {
		row, err := queries.ReadAmount(ctx, 1)
		return row.Amount, err
	})
	results.expect("scan ratio", kinds.Ratio(2.5), func() (any, error) {
		row, err := queries.ReadRatio(ctx, 1)
		return row.Ratio, err
	})
	results.expect("scan flag", kinds.Flag(true), func() (any, error) {
		row, err := queries.ReadFlag(ctx, 1)
		return row.Flag, err
	})
	results.expect("scan note", json.RawMessage(`{"n":1}`), func() (any, error) {
		row, err := queries.ReadNote(ctx, 1)
		return row.Note, err
	})
	results.expect("scan payload", kinds.Blob{1}, func() (any, error) {
		row, err := queries.ReadPayload(ctx, 1)
		return row.Payload, err
	})
	results.expect("scan optional label", &beta, func() (any, error) {
		row, err := queries.ReadOptionalLabel(ctx, 1)
		return row.OptionalLabel, err
	})
	results.expect("scan optional label null", (*kinds.Label)(nil), func() (any, error) {
		row, err := queries.ReadOptionalLabel(ctx, 3)
		return row.OptionalLabel, err
	})
	results.expect("scan optional amount", &nine, func() (any, error) {
		row, err := queries.ReadOptionalAmount(ctx, 1)
		return row.OptionalAmount, err
	})
	results.expect("scan optional amount null", (*kinds.Count)(nil), func() (any, error) {
		row, err := queries.ReadOptionalAmount(ctx, 3)
		return row.OptionalAmount, err
	})
	results.expect("scan tags", []kinds.Label{"x", "y"}, func() (any, error) {
		row, err := queries.ReadTags(ctx, 1)
		return row.Tags, err
	})
	results.expect("scan host", json.Number("10.0.0.1"), func() (any, error) {
		row, err := queries.ReadHost(ctx, 1)
		return row.Host, err
	})
	results.expect("scan optional host", &gateway, func() (any, error) {
		row, err := queries.ReadOptionalHost(ctx, 1)
		return row.OptionalHost, err
	})
	results.expect("scan optional host null", (*json.Number)(nil), func() (any, error) {
		row, err := queries.ReadOptionalHost(ctx, 3)
		return row.OptionalHost, err
	})
	results.expect("scan address", netip.MustParseAddr("192.0.2.1"), func() (any, error) {
		row, err := queries.ReadAddress(ctx, 1)
		return row.Address, err
	})
	results.expect("scan pair", db.ProbesPair{Name: "alpha", Rank: 1}, func() (any, error) {
		row, err := queries.ReadPair(ctx, 1)
		return row.Pair, err
	})
	results.expect("scan keyed", map[kinds.Label]kinds.Count{"alpha": 1}, func() (any, error) {
		row, err := queries.ReadKeyed(ctx, 1)
		return row.Keyed, err
	})

	var probes any
	results.expect("written note", []db.FindByNoteRow{{ID: 11}}, func() (any, error) { return queries.FindByNote(ctx, json.RawMessage(`{"n":11}`)) })
	results.expect("written host", []db.FindByHostRow{{ID: 11}}, func() (any, error) { return queries.FindByHost(ctx, json.Number("10.0.1.11")) })
	results.expect("written optional host", []db.FindByOptionalHostRow{{ID: 11}, {ID: 12}, {ID: 14}, {ID: 16}}, func() (any, error) { return queries.FindByOptionalHost(ctx, &router) })
	results.record("scan row", func() error {
		_, err := queries.ListProbes(ctx)
		return err
	})
	results.record("scan plain row", func() error {
		rows, err := queries.ListPlainProbes(ctx)
		probes = rows
		return err
	})

	return json.NewEncoder(os.Stdout).Encode(map[string]any{"cells": results, "probes": probes})
}
