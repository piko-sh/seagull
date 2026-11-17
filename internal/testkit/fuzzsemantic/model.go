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
	"fmt"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
)

// SemanticVersion is the decoder version byte of semantic relation inputs.
const SemanticVersion = 19

// MaxBytes bounds one structured input.
const MaxBytes = 1024

const (
	// modeByte selects the generated emitter.
	modeByte = 1

	// predicateByte selects the predicate the partition relations use.
	predicateByte = 2

	// parameterByte selects the predicate's parameter.
	parameterByte = 3

	// pivotByte selects the pivot row.
	pivotByte = 4

	// pageByte selects the page size and the builder's direction.
	pageByte = 5

	// countsByte selects the item count in its low half and the peer count in its high half.
	countsByte = 6

	// headerWidth is the number of bytes before the row data.
	headerWidth = 7

	// itemWidth is the number of bytes describing one item.
	itemWidth = 3

	// peerWidth is the number of bytes describing one peer.
	peerWidth = 2

	// maxItems bounds the item table.
	maxItems = 8

	// maxPeers bounds the peer table.
	maxPeers = 8

	// nibble splits the counts byte.
	nibble = 4

	// lowNibble masks the item count.
	lowNibble = 0x0F

	// valueChoices is the number of integer choices, the last of which is NULL.
	valueChoices = 8

	// valueOffset centres the integer choices on zero.
	valueOffset = 3

	// parameterChoices is the number of parameter values.
	parameterChoices = 9

	// parameterOffset centres the parameter on zero.
	parameterOffset = 4

	// groupCount is the number of distinct item groups.
	groupCount = 3

	// pageChoices is the number of page sizes.
	pageChoices = 3

	// peerTargets is the number of item reference choices, the first NULL and the last an
	// identifier no item has.
	peerTargets = 10

	// orphanID is the item identifier no item has.
	orphanID = 99

	// decimalBase formats integers.
	decimalBase = 10

	// integerBits is the width of the modelled integers.
	integerBits = 64
)

// labels are the non-NULL text values items hold, chosen so every engine orders and
// matches them alike.
var labels = []string{"", "a", "ab", "b", "ba", "abc"}

// Case is one decoded semantic relation input.
type Case struct {
	// Engine names the dialect.
	Engine string

	// Data retains the original input.
	Data []byte

	// Items holds the item rows in identifier order.
	Items []Item

	// Peers holds the peer rows in identifier order.
	Peers []Peer

	// Predicate selects the partitioning predicate.
	Predicate int

	// Parameter is the predicate's and the other parameterised queries' value.
	Parameter int64

	// Pivot indexes the pivot item, meaningful when there are items.
	Pivot int

	// PageSize is the number of rows each page holds.
	PageSize int

	// Descending orders the builder's pages from the largest key.
	Descending bool

	// Mode selects the generated emitter.
	Mode fuzzbuilder.Mode
}

// Item is one modelled item row.
type Item struct {
	// A is the first nullable integer, nil for NULL.
	A *int64

	// B is the second nullable integer, nil for NULL.
	B *int64

	// Label is the nullable text, nil for NULL.
	Label *string

	// ID is the unique identifier.
	ID int64

	// Group is the repeated category.
	Group int64
}

// Peer is one modelled peer row.
type Peer struct {
	// ItemID references an item, nil for NULL.
	ItemID *int64

	// C is the nullable integer, nil for NULL.
	C *int64

	// ID is the unique identifier.
	ID int64
}

// Inserts writes the modelled rows as literal INSERT statements.
//
// Returns []string which holds one statement per row.
func (c Case) Inserts() []string {
	statements := make([]string, 0, len(c.Items)+len(c.Peers))
	for _, item := range c.Items {
		label := "NULL"
		if item.Label != nil {
			label = "'" + *item.Label + "'"
		}
		statements = append(statements, fmt.Sprintf("INSERT INTO %s (id, grp, a, b, label) VALUES (%d, %d, %s, %s, %s)", itemTable, item.ID,
			item.Group, integerLiteral(item.A), integerLiteral(item.B), label))
	}
	for _, peer := range c.Peers {
		statements = append(statements, fmt.Sprintf("INSERT INTO %s (id, item_id, c) VALUES (%d, %s, %s)", peerTable, peer.ID,
			integerLiteral(peer.ItemID), integerLiteral(peer.C)))
	}
	return statements
}

// String describes the case for failure messages.
//
// Returns string which lists the choices and rows.
func (c Case) String() string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s mode %d predicate %d (%s) parameter %d pivot %d page %d descending %t", c.Engine, c.Mode, c.Predicate,
		predicates[c.Predicate].sql("K"), c.Parameter, c.Pivot, c.PageSize, c.Descending)
	for _, item := range c.Items {
		label := "NULL"
		if item.Label != nil {
			label = strconv.Quote(*item.Label)
		}
		fmt.Fprintf(&text, "; item(%d, grp %d, a %s, b %s, label %s)", item.ID, item.Group, integerLiteral(item.A), integerLiteral(item.B), label)
	}
	for _, peer := range c.Peers {
		fmt.Fprintf(&text, "; peer(%d, item %s, c %s)", peer.ID, integerLiteral(peer.ItemID), integerLiteral(peer.C))
	}
	return text.String()
}

// Decode reads a version 19 input without consulting Seagull.
//
// Takes engine (string) which names the dialect.
// Takes data ([]byte) which holds the version and choices.
//
// Returns Case which owns a copy of the input.
// Returns bool which is false for another version, an unmodelled engine or an oversized
// input.
func Decode(engine string, data []byte) (Case, bool) {
	if len(data) == 0 || len(data) > MaxBytes || data[0] != SemanticVersion || !Supported(engine) {
		return Case{}, false
	}
	original := data
	data = []byte(strings.TrimSuffix(string(data), fuzzbuilder.RegressionMarker))
	at := func(index int) byte {
		if index < len(data) {
			return data[index]
		}
		return 0
	}
	result := Case{Engine: engine, Data: append([]byte(nil), original...), Items: nil, Peers: nil,
		Predicate: int(at(predicateByte)) % len(predicates), Parameter: int64(at(parameterByte)%parameterChoices) - parameterOffset,
		Pivot: int(at(pivotByte)), PageSize: 1 + int(at(pageByte)%pageChoices), Descending: at(pageByte)/pageChoices%2 == 1,
		Mode: decodeMode(engine, at(modeByte))}
	items := int(at(countsByte)&lowNibble) % (maxItems + 1)
	peers := int(at(countsByte)>>nibble) % (maxPeers + 1)
	for index := range items {
		start := headerWidth + index*itemWidth
		result.Items = append(result.Items, decodeItem(int64(index+1), at(start), at(start+1), at(start+2)))
	}
	for index := range peers {
		start := headerWidth + items*itemWidth + index*peerWidth
		result.Peers = append(result.Peers, decodePeer(int64(index+1), at(start), at(start+1)))
	}
	if len(result.Items) > 0 {
		result.Pivot %= len(result.Items)
	}
	return result, true
}

// decodeMode selects an emitter the engine supports.
//
// Takes engine (string) which names the dialect.
// Takes choice (byte) which holds the requested mode.
//
// Returns fuzzbuilder.Mode which falls back to database/sql where the engine lacks the
// mode.
func decodeMode(engine string, choice byte) fuzzbuilder.Mode {
	mode := fuzzbuilder.Mode(choice % (byte(fuzzbuilder.ModeNative) + 1))
	if !fuzzbuilder.ModeSupported(engine, mode) {
		return fuzzbuilder.ModeDatabaseSQL
	}
	return mode
}

// decodeItem builds one item from three choice bytes.
//
// Takes id (int64) which is the item's identifier.
// Takes first (byte) which selects the group and the label.
// Takes second (byte) which selects A.
// Takes third (byte) which selects B.
//
// Returns Item which is the modelled row.
func decodeItem(id int64, first, second, third byte) Item {
	item := Item{A: decodeInteger(second), B: decodeInteger(third), Label: nil, ID: id, Group: int64(first % groupCount)}
	if choice := int(first/groupCount) % (len(labels) + 1); choice > 0 {
		label := labels[choice-1]
		item.Label = &label
	}
	return item
}

// decodePeer builds one peer from two choice bytes.
//
// Takes id (int64) which is the peer's identifier.
// Takes first (byte) which selects the referenced item.
// Takes second (byte) which selects C.
//
// Returns Peer which is the modelled row.
func decodePeer(id int64, first, second byte) Peer {
	peer := Peer{ItemID: nil, C: decodeInteger(second), ID: id}
	switch target := int64(first % peerTargets); target {
	case 0:
	case peerTargets - 1:
		orphan := int64(orphanID)
		peer.ItemID = &orphan
	default:
		peer.ItemID = &target
	}
	return peer
}

// decodeInteger reads one small nullable integer.
//
// Takes choice (byte) which selects the value.
//
// Returns *int64 which is nil for NULL.
func decodeInteger(choice byte) *int64 {
	value := int64(choice % valueChoices)
	if value == valueChoices-1 {
		return nil
	}
	value -= valueOffset
	return &value
}

// integerLiteral writes a nullable integer.
//
// Takes value (*int64) which is nil for NULL.
//
// Returns string which is the SQL literal.
func integerLiteral(value *int64) string {
	if value == nil {
		return "NULL"
	}
	return strconv.FormatInt(*value, decimalBase)
}
