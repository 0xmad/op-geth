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
	"encoding/hex"
	"errors"
	"io"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

// Address32Length is the expected length of an Address32.
const Address32Length = 32

// ErrAddress32InAddress20Subspace is returned when an address32 has its top 12
// bytes clear. That subspace is the 20-byte address, and an Address32 is not
// one of those.
var ErrAddress32InAddress20Subspace = errors.New("address32 falls in the address20 subspace")

// Address32 is a 32-byte account identifier. It is not a hash and not a
// 20-byte address.
type Address32 [Address32Length]byte

// NewAddress32 copies b into an Address32. b must be exactly 32 bytes, and
// its top 12 bytes must not be zero.
func NewAddress32(b []byte) (Address32, error) {
	if len(b) != Address32Length {
		return Address32{}, errors.New("address32 must be 32 bytes")
	}

	if address32InAddress20Subspace(b) {
		return Address32{}, ErrAddress32InAddress20Subspace
	}

	var out Address32
	copy(out[:], b)

	return out, nil
}

// Bytes returns the 32-byte identifier.
func (a Address32) Bytes() []byte { return a[:] }

// Hex returns the EIP-55 checksummed hex of all 32 bytes.
func (a Address32) Hex() string { return eip55Hex(a[:]) }

// String implements fmt.Stringer.
func (a Address32) String() string { return a.Hex() }

// MarshalText returns the EIP-55 hex encoding.
func (a Address32) MarshalText() ([]byte, error) {
	return []byte(a.Hex()), nil
}

// UnmarshalText parses a fixed 32-byte hex string. The 0x prefix is required.
func (a *Address32) UnmarshalText(input []byte) error {
	var buf Address32

	if err := hexutil.UnmarshalFixedText("Address32", input, buf[:]); err != nil {
		return err
	}

	decoded, err := NewAddress32(buf[:])

	if err != nil {
		return err
	}

	*a = decoded

	return nil
}

// EncodeRLP writes Address32 as a fixed 32-byte RLP string. Leading zeros
// stay in the payload.
func (a Address32) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, a[:])
}

// DecodeRLP reads a fixed 32-byte RLP string. A shorter string is rejected,
// so leading zeros cannot be reconstructed by padding. A value in the address20
// subspace is rejected.
func (a *Address32) DecodeRLP(s *rlp.Stream) error {
	b, err := s.Bytes()

	if err != nil {
		return err
	}

	decoded, err := NewAddress32(b)

	if err != nil {
		return err
	}

	*a = decoded

	return nil
}

func address32InAddress20Subspace(b []byte) bool {
	for _, c := range b[:12] {
		if c != 0 {
			return false
		}
	}

	return true
}

// Address20Of returns the address20 named by preimage.
// A 20-byte preimage is that account. A 32-byte preimage names it only when
// the top 12 bytes are zero; the account is the low 20 bytes. Any other
// width, including an Address32, returns false.
func Address20Of(preimage []byte) (Address, bool) {
	switch len(preimage) {
	case AddressLength:
		var addr Address
		copy(addr[:], preimage)
		return addr, true
	case Address32Length:
		if !address32InAddress20Subspace(preimage) {
			return Address{}, false
		}

		var addr Address
		copy(addr[:], preimage[12:])
		return addr, true
	default:
		return Address{}, false
	}
}

// eip55Hex applies EIP-55 checksum casing to raw. The hash covers every hex
// character, so a 32-byte value is checksummed over all 64 digits.
func eip55Hex(raw []byte) string {
	buf := make([]byte, 2+len(raw)*2)
	copy(buf, "0x")
	hex.Encode(buf[2:], raw)

	sha := sha3.NewLegacyKeccak256()
	sha.Write(buf[2:])
	sum := sha.Sum(nil)

	for i := 2; i < len(buf); i++ {
		hashByte := sum[(i-2)/2]

		if i%2 == 0 {
			hashByte >>= 4
		} else {
			hashByte &= 0xf
		}

		if buf[i] > '9' && hashByte > 7 {
			buf[i] -= 32
		}
	}

	return string(buf)
}
