// Copyright 2026 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

func TestAddress32OverrideSwitch(t *testing.T) {
	cfg := *params.OptimismTestConfig

	if cfg.Address32Time != nil {
		t.Fatalf("Address32Time = %d, want unset", *cfg.Address32Time)
	}

	ts := uint64(42)

	if err := (&ChainOverrides{OverrideOptimismAddress32: &ts}).apply(&cfg); err != nil {
		t.Fatal(err)
	}

	if cfg.Address32Time == nil || *cfg.Address32Time != 42 {
		t.Fatalf("Address32Time = %v, want 42", cfg.Address32Time)
	}
}
