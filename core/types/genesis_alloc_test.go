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

package types

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestParseGenesisAllocAcceptsWideKey(t *testing.T) {
	const raw = `{
		"1111111111111111111111111111111111111111111111111111111111111111": {"balance": "0x2a"},
		"0x2222222222222222222222222222222222222222": {"balance": "0x1"}
	}`

	accounts, err := ParseGenesisAlloc([]byte(raw))

	if err != nil {
		t.Fatal(err)
	}

	legacy, wide := accounts.Legacy, accounts.Wide

	wantWide, err := common.NewAddress32(common.FromHex("0x1111111111111111111111111111111111111111111111111111111111111111"))

	if err != nil {
		t.Fatal(err)
	}

	account, ok := wide[wantWide]

	if !ok {
		t.Fatal("wide account missing")
	}

	if account.Balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("wide balance = %s, want 42", account.Balance)
	}

	legacyAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	account, ok = legacy[legacyAddr]

	if !ok {
		t.Fatal("legacy account missing")
	}

	if account.Balance.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("legacy balance = %s, want 1", account.Balance)
	}

	if len(wide) != 1 || len(legacy) != 1 {
		t.Fatalf("legacy=%d wide=%d, want 1 and 1", len(legacy), len(wide))
	}
}

func TestParseGenesisAllocRejectsLegacySubspace(t *testing.T) {
	const raw = `{
		"0000000000000000000000002222222222222222222222222222222222222222": {"balance": "0x1"}
	}`

	_, err := ParseGenesisAlloc([]byte(raw))

	if !errors.Is(err, common.ErrAddress32LegacySubspace) {
		t.Fatalf("ParseGenesisAlloc() error = %v, want ErrAddress32LegacySubspace", err)
	}
}
