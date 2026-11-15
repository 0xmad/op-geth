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

func TestParseGenesisAllocAcceptsAddress32Key(t *testing.T) {
	const raw = `{
		"1111111111111111111111111111111111111111111111111111111111111111": {"balance": "0x2a"},
		"0x2222222222222222222222222222222222222222": {"balance": "0x1"}
	}`

	accounts, err := ParseGenesisAlloc([]byte(raw))

	if err != nil {
		t.Fatal(err)
	}

	address20, address32 := accounts.Address20, accounts.Address32

	wantWide, err := common.NewAddress32(common.FromHex("0x1111111111111111111111111111111111111111111111111111111111111111"))

	if err != nil {
		t.Fatal(err)
	}

	account, ok := address32[wantWide]

	if !ok {
		t.Fatal("address32 missing")
	}

	if account.Balance.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("address32 balance = %s, want 42", account.Balance)
	}

	address20Addr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	account, ok = address20[address20Addr]

	if !ok {
		t.Fatal("address20 missing")
	}

	if account.Balance.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("address20 balance = %s, want 1", account.Balance)
	}

	if len(address32) != 1 || len(address20) != 1 {
		t.Fatalf("address20=%d address32=%d, want 1 and 1", len(address20), len(address32))
	}
}

func TestParseGenesisAllocRejectsAddress20Subspace(t *testing.T) {
	const raw = `{
		"0000000000000000000000002222222222222222222222222222222222222222": {"balance": "0x1"}
	}`

	_, err := ParseGenesisAlloc([]byte(raw))

	if !errors.Is(err, common.ErrAddress32InAddress20Subspace) {
		t.Fatalf("ParseGenesisAlloc() error = %v, want ErrAddress32InAddress20Subspace", err)
	}
}
