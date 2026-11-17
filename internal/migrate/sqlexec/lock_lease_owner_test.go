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

package sqlexec

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLeaseOwnerNamesItsProcessAndScope(t *testing.T) {
	t.Parallel()

	owner, err := leaseOwner()
	require.NoError(t, err)
	require.LessOrEqual(t, len(owner), 64, "the lease table's owner column holds 64 characters")
	scope := processScope()
	if scope == "" {
		require.NotContains(t, owner, leaseOwnerSeparator, "an owner without a scope names no process")
		return
	}
	parts := strings.Split(owner, leaseOwnerSeparator)
	require.Len(t, parts, leaseOwnerParts)
	require.Equal(t, strconv.Itoa(os.Getpid()), parts[1])
	require.Equal(t, scope, parts[2])
	other, err := leaseOwner()
	require.NoError(t, err)
	require.NotEqual(t, owner, other, "each acquisition has a token of its own")
}

func TestLeaseHolderGoneOnlyForAnEndedProcessOfThisScope(t *testing.T) {
	t.Parallel()

	scope := processScope()
	if scope == "" {
		t.Skip("this system exposes no machine or PID namespace identity, so no holder is ever known to have ended")
	}
	ended := exec.Command(os.Args[0], "-test.run=^$")
	require.NoError(t, ended.Run())
	endedPID := ended.ProcessState.Pid()
	owner := func(pid int, scope string) string {
		return "00ff" + leaseOwnerSeparator + strconv.Itoa(pid) + leaseOwnerSeparator + scope
	}

	cases := []struct {
		name   string
		holder string
		gone   bool
	}{
		{name: "an ended process of this scope", holder: owner(endedPID, scope), gone: true},
		{name: "this process", holder: owner(os.Getpid(), scope), gone: false},
		{name: "a running process of this scope", holder: owner(os.Getppid(), scope), gone: false},
		{name: "an ended process of another scope", holder: owner(endedPID, "0123456789abcdef"), gone: false},
		{name: "an owner from before owners named their process", holder: strings.Repeat("ab", leaseOwnerBytes), gone: false},
		{name: "a malformed process ID", holder: owner(0, scope), gone: false},
		{name: "an empty scope", holder: owner(endedPID, ""), gone: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.gone, leaseHolderGone(testCase.holder))
		})
	}
}
