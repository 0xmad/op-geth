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
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

const anvil0Key = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func TestAddress32TxEnvelope(t *testing.T) {
	var to Address32Word
	to[0] = 0x11
	to[31] = 0x22
	var key common.Hash
	key[31] = 0x7
	tx := &Address32Tx{
		ChainID:    big.NewInt(1),
		Nonce:      4,
		GasTipCap:  big.NewInt(2),
		GasFeeCap:  big.NewInt(3),
		Gas:        21000,
		To:         &to,
		Value:      big.NewInt(1000),
		Data:       []byte{0x01},
		AccessList: Address32AccessList{{Address: to, StorageKeys: []common.Hash{key}}},
		SenderKind: SenderKindWide,
		V:          big.NewInt(1),
		R:          big.NewInt(2),
		S:          big.NewInt(3),
	}
	got, err := NewTx(tx).MarshalBinary()

	if err != nil {
		t.Fatal(err)
	}

	payload, err := rlp.EncodeToBytes([]any{
		tx.ChainID,
		tx.Nonce,
		tx.GasTipCap,
		tx.GasFeeCap,
		tx.Gas,
		tx.To,
		tx.Value,
		tx.Data,
		tx.AccessList,
		tx.SenderKind,
		tx.V,
		tx.R,
		tx.S,
	})

	if err != nil {
		t.Fatal(err)
	}

	want := append([]byte{Address32TxType}, payload...)

	if hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("envelope = %x, want %x", got, want)
	}

	var decoded Transaction

	if err := decoded.UnmarshalBinary(got); err != nil {
		t.Fatal(err)
	}

	if decoded.Type() != Address32TxType || decoded.Nonce() != 4 || decoded.Address32Tx().SenderKind != SenderKindWide {
		t.Fatalf("decoded type %d nonce %d kind %d", decoded.Type(), decoded.Nonce(), decoded.Address32Tx().SenderKind)
	}

	word, ok := decoded.Address32Tx().ToWord()

	if !ok || hex.EncodeToString(word) != hex.EncodeToString(to[:]) {
		t.Fatalf("to = %x, ok %v", word, ok)
	}

	creation := *tx
	creation.To = nil
	raw, err := NewTx(&creation).MarshalBinary()

	if err != nil {
		t.Fatal(err)
	}

	var round Transaction

	if err := round.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}

	if round.Address32Tx().To != nil || !round.CreatesContract() {
		t.Fatal("empty to did not round-trip as creation")
	}
}

func TestAddress32TxSigHash(t *testing.T) {
	var to Address32Word
	to[0] = 0xab
	inner := &Address32Tx{
		ChainID:    big.NewInt(1),
		Nonce:      9,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        21000,
		To:         &to,
		Value:      big.NewInt(5),
		Data:       []byte{0xff},
		SenderKind: SenderKindLegacy,
		V:          big.NewInt(0),
		R:          big.NewInt(1),
		S:          big.NewInt(1),
	}
	payload, err := rlp.EncodeToBytes([]any{
		inner.ChainID,
		inner.Nonce,
		inner.GasTipCap,
		inner.GasFeeCap,
		inner.Gas,
		inner.To,
		inner.Value,
		inner.Data,
		inner.AccessList,
		inner.SenderKind,
	})

	if err != nil {
		t.Fatal(err)
	}

	want := crypto.Keccak256Hash(append([]byte{Address32TxType}, payload...))
	cfg := *params.OptimismTestConfig
	ts := uint64(0)
	cfg.Address32Time = &ts
	got := LatestSigner(&cfg).Hash(NewTx(inner))

	if got != want {
		t.Fatalf("sig hash = %s, want %s", got, want)
	}
}

func TestAddress32TxRejectsBadFields(t *testing.T) {
	var to Address32Word
	to[0] = 1
	base := []any{
		big.NewInt(1),
		uint64(0),
		big.NewInt(1),
		big.NewInt(1),
		uint64(21000),
		&to,
		big.NewInt(0),
		[]byte{},
		Address32AccessList{},
		uint8(SenderKindLegacy),
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(1),
	}
	envelope := func(fields []any) []byte {
		t.Helper()
		payload, err := rlp.EncodeToBytes(fields)
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte{Address32TxType}, payload...)
	}
	short := common.Address{0x01}
	fields := append([]any{}, base...)
	fields[5] = &short
	var tx Transaction

	if err := tx.UnmarshalBinary(envelope(fields)); err == nil {
		t.Fatal("20-byte to was accepted")
	}

	fields = append([]any{}, base...)
	fields[8] = []any{[]any{&short, []common.Hash{}}}

	if err := tx.UnmarshalBinary(envelope(fields)); err == nil {
		t.Fatal("20-byte access-list address was accepted")
	}

	fields = append([]any{}, base...)
	fields[9] = uint8(2)

	if err := tx.UnmarshalBinary(envelope(fields)); !errors.Is(err, ErrInvalidSenderKind) {
		t.Fatalf("sender kind 2 error = %v", err)
	}

	fields = append([]any{}, base...)
	fields[10] = big.NewInt(2)

	if err := tx.UnmarshalBinary(envelope(fields)); !errors.Is(err, errInvalidYParity) {
		t.Fatalf("y parity 2 error = %v", err)
	}
}

func TestAddress32TxRecovery(t *testing.T) {
	key := anvilKey(t)
	cfg := *params.OptimismTestConfig
	ts := uint64(0)
	cfg.Address32Time = &ts
	signer := LatestSigner(&cfg)
	var to Address32Word
	to[0] = 0x44

	legacyTx := signAddress32(t, signer, key, &Address32Tx{
		ChainID:    cfg.ChainID,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        21000,
		To:         &to,
		Value:      big.NewInt(1),
		SenderKind: SenderKindLegacy,
	})
	account, err := SenderAccount(signer, legacyTx)

	if err != nil {
		t.Fatal(err)
	}

	legacy, ok := account.Legacy()

	if !ok || legacy.Hex() != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" {
		t.Fatalf("legacy sender = %s, ok %v", legacy.Hex(), ok)
	}

	wideTx := signAddress32(t, signer, key, &Address32Tx{
		ChainID:    cfg.ChainID,
		GasTipCap:  big.NewInt(1),
		GasFeeCap:  big.NewInt(2),
		Gas:        21000,
		To:         &to,
		Value:      big.NewInt(1),
		SenderKind: SenderKindWide,
	})
	account, err = SenderAccount(signer, wideTx)

	if err != nil {
		t.Fatal(err)
	}

	wide, ok := account.Address32()

	if !ok || hex.EncodeToString(wide.Bytes()) != "66412934e73bd0f4e5ee7e9729291f30c6ad084b6de4a76eb74a4968314b32cd" {
		t.Fatalf("wide sender = %x, ok %v", wide.Bytes(), ok)
	}

	high := wideTx.Address32Tx()
	high.S = new(big.Int).Sub(crypto.S256().Params().N, high.S)

	if _, err := SenderAccount(signer, wideTx); !errors.Is(err, ErrInvalidSig) {
		t.Fatalf("high s error = %v", err)
	}
}

func TestAddress32SenderRejectsLegacySubspace(t *testing.T) {
	var word [32]byte
	word[31] = 1

	if _, err := address32Sender(word[:]); !errors.Is(err, common.ErrAddress32LegacySubspace) {
		t.Fatalf("address32Sender() error = %v", err)
	}
}

func anvilKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(anvil0Key)

	if err != nil {
		t.Fatal(err)
	}

	return key
}

func signAddress32(t *testing.T, signer Signer, key *ecdsa.PrivateKey, inner *Address32Tx) *Transaction {
	t.Helper()
	signed, err := SignTx(NewTx(inner), signer, key)

	if err != nil {
		t.Fatal(err)
	}

	return signed
}
