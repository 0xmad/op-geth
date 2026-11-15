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
	"bytes"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	// SenderKindAddress20 spends the 20-byte account derived from the signature.
	SenderKindAddress20 = 0
	// SenderKindAddress32 spends the Address32 derived from the signature.
	SenderKindAddress32 = 1
)

// ErrInvalidSenderKind is returned when a type 0x05 sender kind is not 0 or 1.
var ErrInvalidSenderKind = errors.New("sender kind must be 0 or 1")

// Address32Word is one fixed 32-byte field in a type 0x05 transaction.
// RLP keeps every byte, including leading zeros.
type Address32Word [32]byte

// Address32WordFromBytes copies a 32-byte word.
// An empty slice returns a nil word. Any other width is an error.
func Address32WordFromBytes(b []byte) (*Address32Word, error) {
	if len(b) == 0 {
		return nil, nil
	}

	if len(b) != 32 {
		return nil, errors.New("address32 field must be empty or 32 bytes")
	}

	var word Address32Word
	copy(word[:], b)
	return &word, nil
}

// Address32AccessList is the access list of a type 0x05 transaction.
// Every address is exactly 32 bytes.
type Address32AccessList []Address32AccessTuple

// Address32AccessTuple is one access-list entry. Address is a 32-byte word.
type Address32AccessTuple struct {
	Address     Address32Word
	StorageKeys []common.Hash
}

// StorageKeys returns the number of storage keys in the list.
func (al Address32AccessList) StorageKeys() int {
	sum := 0

	for _, tuple := range al {
		sum += len(tuple.StorageKeys)
	}

	return sum
}

// Address32Tx is a type 0x05 transaction.
// The envelope is 0x05 ‖ rlp([chainId, nonce, maxPriorityFeePerGas,
// maxFeePerGas, gas, to, value, data, accessList, senderKind, yParity, r, s]).
type Address32Tx struct {
	// ChainID is the chain the transaction is valid on.
	ChainID *big.Int
	// Nonce is the sender account nonce.
	Nonce uint64
	// GasTipCap is the maximum priority fee per gas.
	GasTipCap *big.Int
	// GasFeeCap is the maximum fee per gas.
	GasFeeCap *big.Int
	// Gas is the gas limit.
	Gas uint64
	// To is the 32-byte recipient. A nil To means contract creation.
	To *Address32Word `rlp:"nil"`
	// Value is the amount of wei to transfer.
	Value *big.Int
	// Data is the calldata.
	Data []byte
	// AccessList is the list of 32-byte addresses and storage keys to warm.
	AccessList Address32AccessList
	// SenderKind selects the account the signature spends. Kind 0 is the address20 and kind 1 is the address32.
	SenderKind uint8
	// V is the signature y-parity, 0 or 1.
	V *big.Int
	// R is the signature R value.
	R *big.Int
	// S is the signature S value.
	S *big.Int
}

// ToWord returns the 32-byte recipient. A creation transaction returns nil, false.
func (tx *Address32Tx) ToWord() ([]byte, bool) {
	if tx.To == nil {
		return nil, false
	}

	out := make([]byte, 32)
	copy(out, tx.To[:])

	return out, true
}

func (tx *Address32Tx) copy() TxData {
	cpy := &Address32Tx{
		Nonce:      tx.Nonce,
		SenderKind: tx.SenderKind,
		To:         copyAddress32Word(tx.To),
		Data:       common.CopyBytes(tx.Data),
		Gas:        tx.Gas,
		AccessList: make(Address32AccessList, len(tx.AccessList)),
		Value:      new(big.Int),
		ChainID:    new(big.Int),
		GasTipCap:  new(big.Int),
		GasFeeCap:  new(big.Int),
		V:          new(big.Int),
		R:          new(big.Int),
		S:          new(big.Int),
	}
	copy(cpy.AccessList, tx.AccessList)

	if tx.Value != nil {
		cpy.Value.Set(tx.Value)
	}

	if tx.ChainID != nil {
		cpy.ChainID.Set(tx.ChainID)
	}

	if tx.GasTipCap != nil {
		cpy.GasTipCap.Set(tx.GasTipCap)
	}

	if tx.GasFeeCap != nil {
		cpy.GasFeeCap.Set(tx.GasFeeCap)
	}

	if tx.V != nil {
		cpy.V.Set(tx.V)
	}

	if tx.R != nil {
		cpy.R.Set(tx.R)
	}

	if tx.S != nil {
		cpy.S.Set(tx.S)
	}

	return cpy
}

func copyAddress32Word(w *Address32Word) *Address32Word {
	if w == nil {
		return nil
	}

	cpy := *w
	return &cpy
}

func (tx *Address32Tx) txType() byte           { return Address32TxType }
func (tx *Address32Tx) chainID() *big.Int      { return tx.ChainID }
func (tx *Address32Tx) accessList() AccessList { return nil }
func (tx *Address32Tx) data() []byte           { return tx.Data }
func (tx *Address32Tx) gas() uint64            { return tx.Gas }
func (tx *Address32Tx) gasFeeCap() *big.Int    { return tx.GasFeeCap }
func (tx *Address32Tx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *Address32Tx) gasPrice() *big.Int     { return tx.GasFeeCap }
func (tx *Address32Tx) value() *big.Int        { return tx.Value }
func (tx *Address32Tx) nonce() uint64          { return tx.Nonce }
func (tx *Address32Tx) isSystemTx() bool       { return false }

func (tx *Address32Tx) to() *common.Address {
	if tx.To == nil {
		return nil
	}

	address20, ok := common.Address20Of(tx.To[:])

	if !ok {
		return nil
	}

	return &address20
}

func (tx *Address32Tx) effectiveGasPrice(dst *big.Int, baseFee *big.Int) *big.Int {
	if baseFee == nil {
		return dst.Set(tx.GasFeeCap)
	}

	tip := dst.Sub(tx.GasFeeCap, baseFee)

	if tip.Cmp(tx.GasTipCap) > 0 {
		tip.Set(tx.GasTipCap)
	}

	return tip.Add(tip, baseFee)
}

func (tx *Address32Tx) rawSignatureValues() (v, r, s *big.Int) {
	return tx.V, tx.R, tx.S
}

func (tx *Address32Tx) setSignatureValues(chainID, v, r, s *big.Int) {
	tx.ChainID, tx.V, tx.R, tx.S = chainID, v, r, s
}

func (tx *Address32Tx) encode(b *bytes.Buffer) error {
	return rlp.Encode(b, tx)
}

func (tx *Address32Tx) decode(input []byte) error {
	if err := rlp.DecodeBytes(input, tx); err != nil {
		return err
	}

	return tx.validate()
}

func (tx *Address32Tx) validate() error {
	if tx.SenderKind != SenderKindAddress20 && tx.SenderKind != SenderKindAddress32 {
		return ErrInvalidSenderKind
	}

	if tx.V == nil || tx.R == nil || tx.S == nil {
		return ErrInvalidSig
	}

	if tx.V.BitLen() > 8 || (tx.V.Uint64() != 0 && tx.V.Uint64() != 1) {
		return errInvalidYParity
	}

	return nil
}

func (tx *Address32Tx) sigHash(chainID *big.Int) common.Hash {
	return prefixedRlpHash(
		Address32TxType,
		[]any{
			chainID,
			tx.Nonce,
			tx.GasTipCap,
			tx.GasFeeCap,
			tx.Gas,
			tx.To,
			tx.Value,
			tx.Data,
			tx.AccessList,
			tx.SenderKind,
		})
}
