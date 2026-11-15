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
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
)

func TestNewAddress32RejectsLegacySubspace(t *testing.T) {
	raw := make([]byte, Address32Length)
	raw[Address32Length-1] = 0x01

	if _, err := NewAddress32(raw); !errors.Is(err, ErrAddress32LegacySubspace) {
		t.Fatalf("NewAddress32() error = %v, want ErrAddress32LegacySubspace", err)
	}
}

func TestNewAddress32RejectsWrongWidth(t *testing.T) {
	if _, err := NewAddress32(make([]byte, AddressLength)); err == nil {
		t.Fatal("NewAddress32 accepted a 20-byte address")
	}

	wide := make([]byte, Address32Length+1)
	wide[0] = 0x01

	if _, err := NewAddress32(wide); err == nil {
		t.Fatal("NewAddress32 cropped a value longer than 32 bytes")
	}
}

func TestNewAddress32KeepsLeadingZero(t *testing.T) {
	raw, err := hex.DecodeString("001102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")

	if err != nil {
		t.Fatal(err)
	}

	got, err := NewAddress32(raw)

	if err != nil {
		t.Fatal(err)
	}

	if len(got.Bytes()) != Address32Length {
		t.Fatalf("len(Bytes()) = %d, want %d", len(got.Bytes()), Address32Length)
	}

	if !bytes.Equal(got.Bytes(), raw) {
		t.Fatalf("Bytes() = %x, want %x", got.Bytes(), raw)
	}
}

func TestAddress32RLPRoundTripKeepsLeadingZeros(t *testing.T) {
	addr := mustAddress32(t, "001102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	enc, err := rlp.EncodeToBytes(addr)

	if err != nil {
		t.Fatal(err)
	}

	want := append([]byte{0xa0}, addr.Bytes()...)

	if !bytes.Equal(enc, want) {
		t.Fatalf("rlp = %x, want %x", enc, want)
	}

	var got Address32

	if err := rlp.DecodeBytes(enc, &got); err != nil {
		t.Fatal(err)
	}

	if got != addr {
		t.Fatalf("decoded %x, want %x", got, addr)
	}
}

func TestAddress32HexEIP55(t *testing.T) {
	addr := mustAddress32(t, "001102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	const want = "0x001102030405060708090A0b0C0D0E0F101112131415161718191a1b1c1D1e1f"

	if addr.Hex() != want {
		t.Fatalf("Hex() = %s, want %s", addr.Hex(), want)
	}

	text, err := addr.MarshalText()

	if err != nil {
		t.Fatal(err)
	}

	if string(text) != want {
		t.Fatalf("MarshalText() = %s, want %s", text, want)
	}
}

func TestAddress32HexRoundTrip(t *testing.T) {
	addr := mustAddress32(t, "66412934e73bd0f4e5ee7e9729291f30c6ad084b6de4a76eb74a4968314b32cd")
	const want = "0x66412934e73BD0F4e5eE7E9729291F30C6AD084b6de4A76eB74a4968314B32Cd"

	if addr.Hex() != want {
		t.Fatalf("Hex() = %s, want %s", addr.Hex(), want)
	}

	var got Address32

	if err := got.UnmarshalText([]byte(addr.Hex())); err != nil {
		t.Fatal(err)
	}

	if got != addr {
		t.Fatalf("round trip %x, want %x", got, addr)
	}
}

func TestAddress32HexRejectsShortAndLegacy(t *testing.T) {
	var got Address32

	if err := got.UnmarshalText([]byte("0x01")); err == nil {
		t.Fatal("short hex accepted")
	}

	if err := got.UnmarshalText([]byte("0x00000000000000000000000000000000000000000000000000000000000000ab")); !errors.Is(err, ErrAddress32LegacySubspace) {
		t.Fatalf("UnmarshalText() error = %v, want ErrAddress32LegacySubspace", err)
	}
}

func TestAddress32RLPRejectsShortString(t *testing.T) {
	var got Address32

	if err := rlp.DecodeBytes([]byte{0x81, 0x01}, &got); err == nil {
		t.Fatal("decoded a 1-byte RLP string into Address32")
	}
}

func TestAddress32RLPRejectsLegacySubspace(t *testing.T) {
	raw := make([]byte, Address32Length)
	raw[Address32Length-1] = 0xab
	enc := append([]byte{0xa0}, raw...)
	var got Address32

	if err := rlp.DecodeBytes(enc, &got); !errors.Is(err, ErrAddress32LegacySubspace) {
		t.Fatalf("DecodeBytes() error = %v, want ErrAddress32LegacySubspace", err)
	}
}

func mustAddress32(t *testing.T, rawHex string) Address32 {
	t.Helper()
	raw, err := hex.DecodeString(rawHex)

	if err != nil {
		t.Fatal(err)
	}

	addr, err := NewAddress32(raw)

	if err != nil {
		t.Fatal(err)
	}

	return addr
}
