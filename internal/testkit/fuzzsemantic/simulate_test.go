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

package fuzzsemantic

import (
	"slices"
	"strconv"
	"strings"
)

type fault string

const (
	faultNone            fault = ""
	faultNotKeepsNull    fault = "NOT keeps rows whose predicate is NULL"
	faultTwoValued       fault = "the predicate treats NULL as FALSE everywhere"
	faultInIgnoresNull   fault = "IN ignores NULL list elements outside ClickHouse"
	faultSumOffByOne     fault = "SUM adds one in the NOT partition"
	faultMinIsMax        fault = "MIN returns the maximum in the WHERE partition"
	faultCountStar       fault = "COUNT(*) skips NULL a in the NULL partition"
	faultCommonCount     fault = "the CTE count drops one row"
	faultRowNumberZero   fault = "row_number starts at zero"
	faultRunningReverse  fault = "the running total runs backwards"
	faultLagCurrent      fault = "LAG returns the current identifier"
	faultGroupSum        fault = "GROUP BY totals add one"
	faultWindowPrefilter fault = "the window builder's Where filters before numbering"
	faultLeftIsInner     fault = "LEFT JOIN drops unmatched items"
	faultFullIsLeft      fault = "FULL JOIN drops unmatched peers"
	faultSemiDuplicates  fault = "IN repeats items once per matching peer"
	faultExistsAll       fault = "EXISTS ignores its correlation"
	faultUnionAllDedup   fault = "UNION ALL removes duplicates"
	faultUnionKeepsDups  fault = "UNION keeps duplicates"
	faultExceptAll       fault = "EXCEPT keeps duplicates"
	faultIntersectAll    fault = "INTERSECT ignores the second branch"
	faultBareSetDistinct fault = "ClickHouse's bare INTERSECT and EXCEPT remove duplicates"
	faultOffsetShift     fault = "OFFSET skips one extra row"
	faultLimitIgnored    fault = "the builder ignores Limit"
	faultCountPlusOne    fault = "the builder Count adds one"
	faultBuilderWhere    fault = "the builder Where uses >= for >"
	faultDescNullsSplit  fault = "the descending builder order puts NULL keys in the middle"
	faultLimitPrefilter  fault = "the limited builder's Where filters before the query limits its rows"
)

func simulate(c Case, mistake fault) []Observation {
	calls := c.Calls()
	observations := make([]Observation, 0, len(calls))
	for _, current := range calls {
		observation := Observation{Label: current.Label, Class: "", Error: "", Rows: nil, Count: 0}
		observation.Rows, observation.Count = simulateCall(&c, current, mistake)
		observations = append(observations, observation)
	}
	return observations
}

func simulateCall(c *Case, current Call, mistake fault) ([][]Cell, int64) {
	label := current.Label
	switch {
	case strings.HasPrefix(label, labelBuilderPage):
		offset, _ := strconv.Atoi(strings.TrimPrefix(label, labelBuilderPage))
		limit := c.PageSize
		if mistake == faultLimitIgnored {
			limit = len(c.Items)
		}
		return page(simulateOrdered(c, c.Descending, mistake), limit, offset), 0
	case strings.HasPrefix(label, labelPage):
		offset, _ := strconv.Atoi(strings.TrimPrefix(label, labelPage))
		if mistake == faultOffsetShift {
			offset++
		}
		return page(simulateOrdered(c, false, faultNone), c.PageSize, offset), 0
	}
	switch label {
	case labelWhere, labelNot, labelNull, labelAll, labelStaticWhere:
		return simulatePartition(c, label, mistake), 0
	case labelAggregateWhere, labelAggregateNot, labelAggregateNull, labelAggregateAll:
		return [][]Cell{simulateAggregate(c, label, mistake)}, 0
	case labelCommonCount, labelDerivedCount:
		partition := map[string]string{labelCommonCount: labelWhere, labelDerivedCount: labelNot}[label]
		count := len(simulatePartition(c, partition, faultNone))
		if mistake == faultCommonCount && label == labelCommonCount && count > 0 {
			count--
		}
		return [][]Cell{{count64(count)}}, 0
	case labelWindow:
		return simulateWindow(c, mistake), 0
	case labelGroupSum:
		return simulateGroups(c, mistake), 0
	case labelWindowFull, labelWindowFiltered:
		return simulateWindowBuilder(c, label == labelWindowFiltered, mistake), 0
	case labelLimitFull, labelLimitWhere, labelLimitByFull, labelLimitByWhere:
		return simulateLimited(c, label, mistake), 0
	case labelLimitCount, labelLimitByCount:
		read := map[string]string{labelLimitCount: labelLimitWhere, labelLimitByCount: labelLimitByWhere}[label]
		return nil, int64(len(simulateLimited(c, read, mistake)))
	case labelBuilderFull:
		return simulateOrdered(c, c.Descending, mistake), 0
	case labelPageFull:
		return simulateOrdered(c, false, faultNone), 0
	case labelBuilderCount:
		count := int64(len(c.Items))
		if mistake == faultCountPlusOne {
			count++
		}
		return nil, count
	case labelBuilderWhere:
		var rows [][]Cell
		for _, item := range c.Items {
			threshold := c.Parameter
			if mistake == faultBuilderWhere {
				threshold--
			}
			if compare(item.A, new(threshold), greater) == truthTrue {
				rows = append(rows, itemCells(item)[:3])
			}
		}
		return rows, 0
	}
	return simulateJoinOrCompound(c, label, mistake), 0
}

func simulateTruth(c *Case, item Item, mistake fault) truth {
	value := c.truthOf(item)
	switch mistake {
	case faultTwoValued:
		if value == truthUnknown {
			return truthFalse
		}
	case faultInIgnoresNull:
		if c.Predicate == len(predicates)-1 && value == truthUnknown {
			return truthFalse
		}
	default:
	}
	return value
}

func simulatePartition(c *Case, label string, mistake fault) [][]Cell {
	var rows [][]Cell
	for _, item := range c.Items {
		value := simulateTruth(c, item, mistake)
		keep := false
		switch label {
		case labelWhere:
			keep = value == truthTrue
		case labelNot:
			keep = value == truthFalse || (mistake == faultNotKeepsNull && value == truthUnknown)
		case labelNull:
			keep = value == truthUnknown
		case labelStaticWhere:
			keep = compare(item.A, new(c.Parameter), greater) == truthTrue
		default:
			keep = true
		}
		if keep {
			rows = append(rows, itemCells(item))
		}
	}
	return rows
}

func simulateAggregate(c *Case, label string, mistake fault) []Cell {
	partition := map[string]string{labelAggregateWhere: labelWhere, labelAggregateNot: labelNot, labelAggregateNull: labelNull, labelAggregateAll: labelAll}[label]
	rows := simulatePartition(c, partition, faultNone)
	var present, total int64
	var minimum, maximum *int64
	label5 := Cell{Text: "", Null: true}
	count := int64(len(rows))
	for _, row := range rows {
		if mistake == faultCountStar && label == labelAggregateNull && row[2].Null {
			count--
		}
		if !row[4].Null && (label5.Null || row[4].Text > label5.Text) {
			label5 = row[4]
		}
		if row[2].Null {
			continue
		}
		value, _ := strconv.ParseInt(row[2].Text, decimalBase, 64)
		present++
		total += value
		if minimum == nil || value < *minimum {
			minimum = new(value)
		}
		if maximum == nil || value > *maximum {
			maximum = new(value)
		}
	}
	sum := integerCell(nil)
	if present > 0 {
		if mistake == faultSumOffByOne && label == labelAggregateNot {
			total++
		}
		sum = integerCell(&total)
	}
	if mistake == faultMinIsMax && label == labelAggregateWhere {
		minimum = maximum
	}
	return []Cell{integerCell(&count), integerCell(&present), sum, integerCell(minimum), integerCell(maximum), label5}
}

func simulateOrdered(c *Case, descending bool, mistake fault) [][]Cell {
	items := slices.Clone(c.Items)
	slices.SortFunc(items, func(left, right Item) int {
		switch {
		case left.A == nil && right.A != nil:
			return -1
		case left.A != nil && right.A == nil:
			return 1
		case left.A != nil && *left.A != *right.A:
			return int(*left.A - *right.A)
		default:
			return int(left.ID - right.ID)
		}
	})
	if descending {
		slices.Reverse(items)
	}
	rows := make([][]Cell, 0, len(items))
	for _, item := range items {
		rows = append(rows, itemCells(item)[:3])
	}
	if mistake == faultDescNullsSplit && descending && len(rows) > 2 {
		last := rows[len(rows)-1]
		if last[2].Null && !rows[0][2].Null {
			rows = slices.Insert(rows[:len(rows)-1], 1, last)
		}
	}
	return rows
}

func page(rows [][]Cell, limit, offset int) [][]Cell {
	if offset >= len(rows) {
		return nil
	}
	return rows[offset:min(len(rows), offset+limit)]
}

func simulateWindow(c *Case, mistake fault) [][]Cell {
	ordered := simulateOrdered(c, false, faultNone)
	rows := make([][]Cell, 0, len(c.Items))
	for _, item := range c.Items {
		var members []Item
		for _, other := range c.Items {
			if other.Group == item.Group {
				members = append(members, other)
			}
		}
		number := int64(0)
		for _, row := range ordered {
			if row[1].Text == strconv.FormatInt(item.Group, decimalBase) {
				number++
				if row[0].Text == strconv.FormatInt(item.ID, decimalBase) {
					break
				}
			}
		}
		if mistake == faultRowNumberZero {
			number--
		}
		var running, total, previous *int64
		for _, other := range members {
			if other.A != nil {
				total = add(coalesce(total, new(int64(0))), other.A)
			}
			if other.ID < item.ID {
				previous = new(other.ID)
			}
			include := other.ID <= item.ID
			if mistake == faultRunningReverse {
				include = other.ID >= item.ID
			}
			if include && other.A != nil {
				running = add(coalesce(running, new(int64(0))), other.A)
			}
		}
		if mistake == faultLagCurrent {
			previous = new(item.ID)
		}
		size := int64(len(members))
		rows = append(rows, []Cell{integerCell(&item.ID), integerCell(&item.Group), integerCell(&number), integerCell(&size), integerCell(running),
			integerCell(total), integerCell(previous)})
	}
	return rows
}

func simulateGroups(c *Case, mistake fault) [][]Cell {
	var rows [][]Cell
	for group := range int64(groupCount) {
		var size int64
		var total *int64
		for _, item := range c.Items {
			if item.Group != group {
				continue
			}
			size++
			if item.A != nil {
				total = add(coalesce(total, new(int64(0))), item.A)
			}
		}
		if size == 0 {
			continue
		}
		if mistake == faultGroupSum && total != nil {
			total = add(total, new(int64(1)))
		}
		rows = append(rows, []Cell{integerCell(&group), integerCell(&size), integerCell(total)})
	}
	return rows
}

func simulateWindowBuilder(c *Case, filtered bool, mistake fault) [][]Cell {
	rows := make([][]Cell, 0, len(c.Items))
	counts := map[int64]int64{}
	for _, item := range c.Items {
		visible := !filtered || item.ID > c.Parameter
		if visible || mistake != faultWindowPrefilter {
			counts[item.Group]++
		}
		if visible {
			number := counts[item.Group]
			rows = append(rows, []Cell{integerCell(&item.ID), integerCell(&item.Group), integerCell(&number)})
		}
	}
	return rows
}

func simulateLimited(c *Case, label string, mistake fault) [][]Cell {
	filtered := label == labelLimitWhere || label == labelLimitByWhere
	matches := func(row []Cell) bool {
		value, err := cellInteger(row[pageKey])
		return err == nil && value > c.Parameter
	}
	rows := simulateOrdered(c, false, faultNone)
	if filtered && mistake == faultLimitPrefilter {
		rows = slices.DeleteFunc(rows, func(row []Cell) bool { return !matches(row) })
	}
	if label == labelLimitFull || label == labelLimitWhere {
		rows = page(rows, 3, 1)
	} else {
		seen := map[string]bool{}
		rows = slices.DeleteFunc(rows, func(row []Cell) bool {
			first := !seen[row[1].Text]
			seen[row[1].Text] = true
			return !first
		})
	}
	if filtered {
		rows = slices.DeleteFunc(slices.Clone(rows), func(row []Cell) bool { return !matches(row) })
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}

func simulateJoinOrCompound(c *Case, label string, mistake fault) [][]Cell {
	matched := map[int64]bool{}
	var left, inner, orphans [][]Cell
	for _, item := range c.Items {
		found := false
		for _, peer := range c.Peers {
			if peer.ItemID != nil && *peer.ItemID == item.ID {
				found, matched[peer.ID] = true, true
				row := []Cell{integerCell(&item.ID), integerCell(&peer.ID), integerCell(peer.C)}
				left, inner = append(left, row), append(inner, row)
			}
		}
		if !found && mistake != faultLeftIsInner {
			left = append(left, []Cell{integerCell(&item.ID), integerCell(nil), integerCell(nil)})
		}
	}
	for _, peer := range c.Peers {
		if !matched[peer.ID] {
			orphans = append(orphans, []Cell{integerCell(nil), integerCell(&peer.ID)})
		}
	}
	switch label {
	case labelLeftJoin:
		return left
	case labelInnerCount:
		return [][]Cell{{count64(len(inner))}}
	case labelAntiCount:
		return [][]Cell{{count64(len(left) - len(inner))}}
	case labelFullJoin:
		var rows [][]Cell
		for _, row := range left {
			rows = append(rows, row[:2])
		}
		if mistake == faultFullIsLeft {
			return rows
		}
		return append(rows, orphans...)
	case labelOrphanCount:
		return [][]Cell{{count64(len(orphans))}}
	case labelSemiIn, labelSemiExists, labelSemiJoin:
		return simulateSemi(c, label, mistake)
	}
	return simulateCompound(c, label, mistake)
}

func simulateSemi(c *Case, label string, mistake fault) [][]Cell {
	var rows [][]Cell
	for _, item := range c.Items {
		matches := 0
		for _, peer := range c.Peers {
			correlated := peer.ItemID != nil && *peer.ItemID == item.ID
			if mistake == faultExistsAll && label == labelSemiExists {
				correlated = true
			}
			if correlated && compare(peer.C, new(c.Parameter), greater) == truthTrue {
				matches++
			}
		}
		if mistake == faultSemiDuplicates && label == labelSemiIn {
			for range matches {
				rows = append(rows, []Cell{integerCell(&item.ID)})
			}
			continue
		}
		if matches > 0 {
			rows = append(rows, []Cell{integerCell(&item.ID)})
		}
	}
	return rows
}

func simulateCompound(c *Case, label string, mistake fault) [][]Cell {
	items, peers := make([][]Cell, 0, len(c.Items)), make([][]Cell, 0, len(c.Peers))
	for _, item := range c.Items {
		items = append(items, []Cell{integerCell(item.A)})
	}
	for _, peer := range c.Peers {
		peers = append(peers, []Cell{integerCell(peer.C)})
	}
	distinct := func(rows [][]Cell) [][]Cell {
		var result [][]Cell
		for _, row := range rows {
			if !slices.ContainsFunc(result, func(other []Cell) bool { return rowKey(other) == rowKey(row) }) {
				result = append(result, row)
			}
		}
		return result
	}
	contains := func(rows [][]Cell, row []Cell) bool {
		return slices.ContainsFunc(rows, func(other []Cell) bool { return rowKey(other) == rowKey(row) })
	}
	switch label {
	case labelBranchItems:
		return items
	case labelBranchPeers:
		return peers
	case labelUnionAll:
		if mistake == faultUnionAllDedup {
			return distinct(slices.Concat(items, peers))
		}
		return slices.Concat(items, peers)
	case labelUnion, labelUnionDistinct:
		if mistake == faultUnionKeepsDups && label == labelUnion {
			return slices.Concat(items, peers)
		}
		return distinct(slices.Concat(items, peers))
	case labelIntersect, labelExcept:
		if c.Engine == engineClickHouse {
			source := items
			if mistake == faultBareSetDistinct {
				source = distinct(items)
			}
			var rows [][]Cell
			for _, row := range source {
				if contains(peers, row) == (label == labelIntersect) {
					rows = append(rows, row)
				}
			}
			return rows
		}
	}
	switch label {
	case labelIntersect, labelIntersectDistinct:
		var rows [][]Cell
		for _, row := range distinct(items) {
			if contains(peers, row) || mistake == faultIntersectAll {
				rows = append(rows, row)
			}
		}
		return rows
	default:
		source := distinct(items)
		if mistake == faultExceptAll {
			source = items
		}
		var rows [][]Cell
		for _, row := range source {
			if !contains(peers, row) {
				rows = append(rows, row)
			}
		}
		return rows
	}
}

func count64(count int) Cell {
	value := int64(count)
	return integerCell(&value)
}
