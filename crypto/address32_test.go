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

package crypto

import (
	"encoding/hex"
	"testing"
)

// anvil account 0. Legacy address is the well-known EIP-55 value.
const anvil0Key = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func TestPubkeyToAddressUnchanged(t *testing.T) {
	key, err := HexToECDSA(anvil0Key)

	if err != nil {
		t.Fatal(err)
	}

	got := PubkeyToAddress(key.PublicKey)
	const want = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"

	if got.Hex() != want {
		t.Fatalf("PubkeyToAddress() = %s, want %s", got.Hex(), want)
	}
}

func TestPubkeyToAddress32(t *testing.T) {
	key, err := HexToECDSA(anvil0Key)

	if err != nil {
		t.Fatal(err)
	}

	got, err := PubkeyToAddress32(key.PublicKey)

	if err != nil {
		t.Fatal(err)
	}
	// keccak256(0x01 ‖ pubkey[1:]) for anvil account 0.
	const want = "66412934e73bd0f4e5ee7e9729291f30c6ad084b6de4a76eb74a4968314b32cd"

	if hex.EncodeToString(got.Bytes()) != want {
		t.Fatalf("PubkeyToAddress32() = %x, want %s", got.Bytes(), want)
	}
}
