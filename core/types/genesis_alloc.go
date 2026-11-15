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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// GenesisAccounts is one genesis alloc: the legacy accounts and the Address32
// accounts. Callers pass this instead of the two maps.
type GenesisAccounts struct {
	Legacy GenesisAlloc
	Wide   map[common.Address32]Account
}

// ParseGenesisAlloc decodes a genesis alloc object. A key of 20 bytes is a
// legacy account. A key of 32 bytes is an Address32, and one whose top 12
// bytes are zero is rejected. The 0x prefix is optional.
func ParseGenesisAlloc(data []byte) (GenesisAccounts, error) {
	var raw map[string]Account

	if err := json.Unmarshal(data, &raw); err != nil {
		return GenesisAccounts{}, err
	}

	legacy := make(GenesisAlloc, len(raw))
	var wide map[common.Address32]Account

	for key, account := range raw {
		decoded, err := decodeAllocKey(key)

		if err != nil {
			return GenesisAccounts{}, err
		}

		switch len(decoded) {
		case common.AddressLength:
			var addr common.Address
			copy(addr[:], decoded)
			legacy[addr] = account
		case common.Address32Length:
			addr, err := common.NewAddress32(decoded)

			if err != nil {
				return GenesisAccounts{}, fmt.Errorf("genesis alloc key %s: %w", key, err)
			}

			if wide == nil {
				wide = make(map[common.Address32]Account)
			}

			wide[addr] = account
		default:
			return GenesisAccounts{}, fmt.Errorf("genesis alloc key %s: want 20 or 32 bytes, have %d", key, len(decoded))
		}
	}

	return GenesisAccounts{Legacy: legacy, Wide: wide}, nil
}

// MarshalGenesisAlloc encodes legacy and wide genesis accounts as one alloc
// object. Keys are unprefixed hex, 40 digits for a legacy account and 64
// digits for an Address32. A nil legacy map and no wide accounts encode as
// JSON null.
func MarshalGenesisAlloc(accounts GenesisAccounts) ([]byte, error) {
	if accounts.Legacy == nil && len(accounts.Wide) == 0 {
		return []byte("null"), nil
	}

	out := make(map[string]Account, len(accounts.Legacy)+len(accounts.Wide))

	for addr, account := range accounts.Legacy {
		out[hex.EncodeToString(addr[:])] = account
	}

	for addr, account := range accounts.Wide {
		out[hex.EncodeToString(addr[:])] = account
	}

	return json.Marshal(out)
}

func decodeAllocKey(key string) ([]byte, error) {
	key = strings.TrimPrefix(strings.TrimPrefix(key, "0x"), "0X")

	if len(key) == 0 || len(key)%2 != 0 {
		return nil, fmt.Errorf("genesis alloc key %q is not even-length hex", key)
	}

	decoded, err := hex.DecodeString(key)

	if err != nil {
		return nil, fmt.Errorf("genesis alloc key %q: %w", key, err)
	}

	return decoded, nil
}
