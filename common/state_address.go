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

package common

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// StateAddressKind identifies which account form a StateAddress holds.
// A new account form is a new kind, not another boolean.
type StateAddressKind uint8

const (
	// StateAddressLegacy is a 20-byte account. The zero StateAddress has this kind.
	StateAddressLegacy StateAddressKind = iota
	// StateAddress32 is an Address32. A 32-byte word whose top 12 bytes are zero
	// is StateAddressLegacy, the account in the low 20 bytes.
	StateAddress32
)

// StateAddress is an account named in an RPC argument.
type StateAddress struct {
	kind StateAddressKind
	data [Address32Length]byte
}

// LegacyStateAddress names a 20-byte account.
func LegacyStateAddress(addr Address) StateAddress {
	var out StateAddress
	out.kind = StateAddressLegacy
	copy(out.data[:], addr[:])
	return out
}

// WideStateAddress names an Address32.
func WideStateAddress(addr Address32) StateAddress {
	var out StateAddress
	out.kind = StateAddress32
	copy(out.data[:], addr[:])
	return out
}

// StateAddressFromWord names the account in a 32-byte word.
// Twelve leading zero bytes select the legacy account in the low 20 bytes.
func StateAddressFromWord(word []byte) (StateAddress, error) {
	if len(word) != Address32Length {
		return StateAddress{}, fmt.Errorf("state address word must be 32 bytes, have %d", len(word))
	}

	if legacy, ok := LegacyAddressOf(word); ok {
		return LegacyStateAddress(legacy), nil
	}

	wide, err := NewAddress32(word)

	if err != nil {
		return StateAddress{}, err
	}

	return WideStateAddress(wide), nil
}

// Kind reports which account form this holds.
func (a StateAddress) Kind() StateAddressKind { return a.kind }

// Word32 returns the 32-byte transaction word for this account.
// A legacy account is the 20-byte address with 12 leading zero bytes.
func (a StateAddress) Word32() ([]byte, error) {
	switch a.kind {
	case StateAddressLegacy:
		out := make([]byte, Address32Length)
		copy(out[Address32Length-AddressLength:], a.data[:AddressLength])
		return out, nil
	case StateAddress32:
		out := make([]byte, Address32Length)
		copy(out, a.data[:])
		return out, nil
	default:
		return nil, fmt.Errorf("unknown state address kind %d", a.kind)
	}
}

// Legacy returns the 20-byte account. ok is false for any other kind.
func (a StateAddress) Legacy() (Address, bool) {
	if a.kind != StateAddressLegacy {
		return Address{}, false
	}

	var addr Address
	copy(addr[:], a.data[:AddressLength])
	return addr, true
}

// Address32 returns the wide account. ok is false for any other kind.
func (a StateAddress) Address32() (Address32, bool) {
	if a.kind != StateAddress32 {
		return Address32{}, false
	}

	var addr Address32
	copy(addr[:], a.data[:])
	return addr, true
}

// Preimage returns the bytes hashed into the account trie key.
func (a StateAddress) Preimage() ([]byte, error) {
	switch a.kind {
	case StateAddressLegacy:
		out := make([]byte, AddressLength)
		copy(out, a.data[:AddressLength])
		return out, nil
	case StateAddress32:
		out := make([]byte, Address32Length)
		copy(out, a.data[:])
		return out, nil
	default:
		return nil, fmt.Errorf("unknown state address kind %d", a.kind)
	}
}

// MarshalText encodes a legacy account as 20-byte hex and an Address32 as
// 32-byte hex. The encoding matches the existing RPC address encoding:
// 0x-prefixed, lowercase, no checksum.
func (a StateAddress) MarshalText() ([]byte, error) {
	raw, err := a.Preimage()

	if err != nil {
		return nil, err
	}

	return []byte(hexutil.Encode(raw)), nil
}

// UnmarshalText parses a 0x-prefixed hex address of 20 or 32 bytes.
func (a *StateAddress) UnmarshalText(input []byte) error {
	b, err := hexutil.Decode(string(input))

	if err != nil {
		return err
	}

	switch len(b) {
	case AddressLength:
		*a = StateAddress{kind: StateAddressLegacy}
		copy(a.data[:], b)
		return nil
	case Address32Length:
		if legacy, ok := LegacyAddressOf(b); ok {
			*a = StateAddress{kind: StateAddressLegacy}
			copy(a.data[:], legacy[:])
			return nil
		}

		wide, err := NewAddress32(b)

		if err != nil {
			return err
		}

		*a = StateAddress{kind: StateAddress32}
		copy(a.data[:], wide[:])
		return nil
	default:
		return fmt.Errorf("state address must be 20 or 32 bytes, have %d", len(b))
	}
}

// UnmarshalJSON parses a quoted hex string.
func (a *StateAddress) UnmarshalJSON(input []byte) error {
	if len(input) < 2 || input[0] != '"' || input[len(input)-1] != '"' {
		return errors.New("state address must be a hex string")
	}

	return a.UnmarshalText(input[1 : len(input)-1])
}
