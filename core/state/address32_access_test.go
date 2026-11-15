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

package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
)

func TestPaddedWordWarmsSameLegacyAccount(t *testing.T) {
	legacy := common.HexToAddress("0x2222222222222222222222222222222222222222")
	var padded [32]byte
	copy(padded[12:], legacy[:])

	fromEntry := newAccessTestState(t)
	fromEntry.AddAddressToAccessList(legacy)

	if !fromEntry.AddressInAccessList(legacy) {
		t.Fatal("20-byte access-list entry did not warm the legacy account")
	}

	fromWord := newAccessTestState(t)

	if err := fromWord.AddLegacyAccessPreimage(padded[:]); err != nil {
		t.Fatal(err)
	}

	if !fromWord.AddressInAccessList(legacy) {
		t.Fatal("zero-padded 32-byte word did not warm the legacy account")
	}

	wide := bytes.Repeat([]byte{0x11}, 32)

	if err := fromWord.AddLegacyAccessPreimage(wide); err == nil {
		t.Fatal("wide word warmed a legacy account")
	}
}

func TestPaddedAccessListEntryWarmsSameLegacyAccount(t *testing.T) {
	const legacyHex = "0x2222222222222222222222222222222222222222"
	const paddedHex = "0x0000000000000000000000002222222222222222222222222222222222222222"
	raw := `[{"address":"` + legacyHex + `","storageKeys":[]},{"address":"` + paddedHex + `","storageKeys":[]}]`

	var list types.AccessList

	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		t.Fatal(err)
	}

	legacy := common.HexToAddress(legacyHex)

	if list[0].Address != legacy || list[1].Address != legacy {
		t.Fatalf("access list addresses = %x and %x, want %x", list[0].Address, list[1].Address, legacy)
	}

	statedb := newAccessTestState(t)
	statedb.Prepare(params.Rules{IsEIP2929: true}, common.Address{}, common.Address{}, nil, nil, list)

	if !statedb.AddressInAccessList(legacy) {
		t.Fatal("access list did not warm the legacy account")
	}

	wide := `[{"address":"0x1111111111111111111111111111111111111111111111111111111111111111","storageKeys":[]}]`

	if err := json.Unmarshal([]byte(wide), &list); !errors.Is(err, types.ErrAccessListWideAddress) {
		t.Fatalf("wide access-list entry error = %v, want ErrAccessListWideAddress", err)
	}
}

func newAccessTestState(t *testing.T) *StateDB {
	t.Helper()
	db := rawdb.NewMemoryDatabase()
	statedb, err := New(types.EmptyRootHash, NewDatabase(triedb.NewDatabase(db, nil), nil))

	if err != nil {
		t.Fatal(err)
	}

	return statedb
}
