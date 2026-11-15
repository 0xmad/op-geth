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

package core

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

func address32TransactionToMessage(tx *types.Transaction, signer types.Signer, baseFee *big.Int) (*Message, error) {
	inner := tx.Address32Tx()

	if inner == nil {
		return nil, fmt.Errorf("%w: type %d", ErrTxTypeNotSupported, tx.Type())
	}

	from, err := types.SenderAccount(signer, tx)

	if err != nil {
		return nil, err
	}

	var to *common.StateAddress

	if word, _ := inner.ToWord(); word != nil {
		recipient, err := common.StateAddressFromWord(word)

		if err != nil {
			return nil, err
		}

		to = &recipient
	}

	msg := &Message{
		Nonce:          tx.Nonce(),
		GasLimit:       tx.Gas(),
		GasPrice:       new(big.Int).Set(tx.GasPrice()),
		GasFeeCap:      new(big.Int).Set(tx.GasFeeCap()),
		GasTipCap:      new(big.Int).Set(tx.GasTipCap()),
		Value:          tx.Value(),
		Data:           tx.Data(),
		RollupCostData: tx.RollupCostData(),
		Address32: &Address32Call{
			SenderKind: inner.SenderKind,
			From:       from,
			To:         to,
			AccessList: inner.AccessList,
		},
	}

	if legacy, ok := from.Legacy(); ok {
		msg.From = legacy
	}

	if legacyTo := tx.To(); legacyTo != nil {
		msg.To = legacyTo
	}

	if baseFee != nil {
		msg.GasPrice = new(big.Int).Add(msg.GasTipCap, baseFee)

		if msg.GasPrice.Cmp(msg.GasFeeCap) > 0 {
			msg.GasPrice = new(big.Int).Set(msg.GasFeeCap)
		}
	}

	return msg, nil
}

func applyAddress32Message(evm *vm.EVM, msg *Message, gp *GasPool) (*ExecutionResult, error) {
	if !evm.ChainConfig().IsAddress32(evm.Context.Time) {
		return nil, fmt.Errorf("%w: type %d rejected, Address32 is not active", ErrTxTypeNotSupported, types.Address32TxType)
	}

	statedb := state.AsStateDB(evm.StateDB)

	if statedb == nil {
		return nil, fmt.Errorf("address32 transfer requires a state database")
	}

	call := msg.Address32

	if call.To == nil {
		return nil, fmt.Errorf("address32 contract creation is not supported")
	}

	sender := accountBalance{statedb, call.From}
	recipient := accountBalance{statedb, *call.To}

	chargeFees := true

	if !msg.SkipNonceChecks {
		if err := sender.checkNonce(msg.Nonce); err != nil {
			return nil, err
		}
	}

	if !msg.SkipTransactionChecks {
		if len(sender.code()) > 0 {
			return nil, fmt.Errorf("%w: address %s", ErrSenderNoEOA, sender.label())
		}
	} else if msg.SkipNonceChecks {
		// eth_call and eth_estimateGas do not buy rollup fees.
		chargeFees = false
	}

	if evm.ChainConfig().IsLondon(evm.Context.BlockNumber) {
		skip := evm.Config.NoBaseFee && msg.GasFeeCap.BitLen() == 0 && msg.GasTipCap.BitLen() == 0

		if !skip {
			if msg.GasFeeCap.Cmp(msg.GasTipCap) < 0 {
				return nil, fmt.Errorf("%w: address %s", ErrTipAboveFeeCap, sender.label())
			}

			if msg.GasFeeCap.Cmp(evm.Context.BaseFee) < 0 {
				return nil, fmt.Errorf("%w: address %s, maxFeePerGas: %s, baseFee: %s", ErrFeeCapTooLow, sender.label(), msg.GasFeeCap, evm.Context.BaseFee)
			}
		}
	}

	rules := evm.ChainConfig().Rules(evm.Context.BlockNumber, evm.Context.Random != nil, evm.Context.Time)
	intrinsic, err := IntrinsicGasAddress32(msg.Data, call.AccessList, false, rules.IsShanghai)

	if err != nil {
		return nil, err
	}

	if msg.GasLimit < intrinsic {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrIntrinsicGas, msg.GasLimit, intrinsic)
	}

	floor := intrinsic

	if rules.IsPrague {
		floorData, err := FloorDataGas(msg.Data)

		if err != nil {
			return nil, err
		}

		if msg.GasLimit < floorData {
			return nil, fmt.Errorf("%w: have %d, want %d", ErrFloorDataGas, msg.GasLimit, floorData)
		}

		if floor < floorData {
			floor = floorData
		}
	}

	if len(recipient.code()) > 0 {
		return nil, fmt.Errorf("address32 target %s has code", recipient.label())
	}

	var l1Cost *big.Int
	var operatorLimit *uint256.Int

	if chargeFees {
		if evm.Context.L1CostFunc != nil {
			l1Cost = evm.Context.L1CostFunc(msg.RollupCostData, evm.Context.Time)
		}

		if evm.Context.OperatorCostFunc != nil {
			operatorLimit = evm.Context.OperatorCostFunc(msg.GasLimit, evm.Context.Time)
		}
	}

	if err := sender.canPay(msg, l1Cost, operatorLimit); err != nil {
		return nil, err
	}

	if err := gp.SubGas(msg.GasLimit); err != nil {
		return nil, err
	}

	bought := new(big.Int).SetUint64(msg.GasLimit)
	bought.Mul(bought, msg.GasPrice)

	if l1Cost != nil {
		bought.Add(bought, l1Cost)
	}

	if operatorLimit != nil {
		bought.Add(bought, operatorLimit.ToBig())
	}

	if err := sender.sub(bought, tracing.BalanceDecreaseGasBuy); err != nil {
		return nil, err
	}

	sender.setNonce(sender.nonce() + 1)

	value, overflow := uint256.FromBig(msg.Value)

	if overflow {
		return nil, fmt.Errorf("%w: address %s", ErrInsufficientFundsForTransfer, sender.label())
	}

	if err := sender.sub(value.ToBig(), tracing.BalanceChangeTransfer); err != nil {
		return nil, err
	}

	recipient.add(value, tracing.BalanceChangeTransfer)

	gasUsed := floor

	if gasUsed > msg.GasLimit {
		gasUsed = msg.GasLimit
	}

	leftover := msg.GasLimit - gasUsed

	if leftover > 0 {
		refund := new(big.Int).SetUint64(leftover)
		refund.Mul(refund, msg.GasPrice)
		sender.add(uint256.MustFromBig(refund), tracing.BalanceIncreaseGasReturn)
		gp.AddGas(leftover)
	}

	if chargeFees && evm.Context.OperatorCostFunc != nil && rules.IsOptimismIsthmus {
		usedCost := evm.Context.OperatorCostFunc(gasUsed, evm.Context.Time)

		if operatorLimit != nil && operatorLimit.Cmp(usedCost) > 0 {
			sender.add(new(uint256.Int).Sub(operatorLimit, usedCost), tracing.BalanceIncreaseGasReturn)
		}
	}

	payFees := !(evm.Config.NoBaseFee && msg.GasFeeCap.Sign() == 0 && msg.GasTipCap.Sign() == 0)

	if payFees && chargeFees {
		if err := payAddress32Fees(evm, statedb, msg, rules, gasUsed, l1Cost); err != nil {
			return nil, err
		}
	}

	if err := statedb.Error(); err != nil {
		return nil, err
	}

	return &ExecutionResult{UsedGas: gasUsed, MaxUsedGas: gasUsed}, nil
}

func payAddress32Fees(evm *vm.EVM, statedb *state.StateDB, msg *Message, rules params.Rules, gasUsed uint64, l1Cost *big.Int) error {
	effectiveTip := new(big.Int).Set(msg.GasPrice)

	if rules.IsLondon {
		effectiveTip.Sub(effectiveTip, evm.Context.BaseFee)
	}

	if effectiveTip.Sign() > 0 {
		tip := new(uint256.Int).SetUint64(gasUsed)
		tip.Mul(tip, uint256.MustFromBig(effectiveTip))
		statedb.AddBalance(evm.Context.Coinbase, tip, tracing.BalanceIncreaseRewardTransactionFee)
	}

	if evm.ChainConfig().Optimism == nil || !rules.IsOptimismBedrock {
		return nil
	}

	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), evm.Context.BaseFee)
	amt, overflow := uint256.FromBig(gasCost)

	if overflow {
		return fmt.Errorf("optimism gas cost overflows U256: %d", gasCost)
	}

	statedb.AddBalance(params.OptimismBaseFeeRecipient, amt, tracing.BalanceIncreaseRewardTransactionFee)

	if l1Cost != nil {
		amt, overflow = uint256.FromBig(l1Cost)

		if overflow {
			return fmt.Errorf("optimism l1 cost overflows U256: %d", l1Cost)
		}

		statedb.AddBalance(params.OptimismL1FeeRecipient, amt, tracing.BalanceIncreaseRewardTransactionFee)
	}

	if rules.IsOptimismIsthmus && evm.Context.OperatorCostFunc != nil {
		operatorFee := evm.Context.OperatorCostFunc(gasUsed, evm.Context.Time)
		statedb.AddBalance(params.OptimismOperatorFeeRecipient, operatorFee, tracing.BalanceIncreaseRewardTransactionFee)
	}

	return nil
}

// accountBalance reads and writes one legacy account or one Address32.
type accountBalance struct {
	db   *state.StateDB
	addr common.StateAddress
}

func (a accountBalance) balance() *uint256.Int {
	return a.db.AccountBalance(a.addr)
}

func (a accountBalance) nonce() uint64 {
	return a.db.AccountNonce(a.addr)
}

func (a accountBalance) code() []byte {
	return a.db.AccountCode(a.addr)
}

func (a accountBalance) label() string {
	if legacy, ok := a.addr.Legacy(); ok {
		return legacy.Hex()
	}

	wide, _ := a.addr.Address32()
	return wide.Hex()
}

func (a accountBalance) checkNonce(nonce uint64) error {
	have := a.nonce()

	if have < nonce {
		return fmt.Errorf("%w: address %s, tx: %d state: %d", ErrNonceTooHigh, a.label(), nonce, have)
	}

	if have > nonce {
		return fmt.Errorf("%w: address %s, tx: %d state: %d", ErrNonceTooLow, a.label(), nonce, have)
	}

	return nil
}

func (a accountBalance) use(legacy func(common.Address), wide func(common.Address32)) {
	if addr, ok := a.addr.Legacy(); ok {
		legacy(addr)
		return
	}

	addr, _ := a.addr.Address32()
	wide(addr)
}

func (a accountBalance) setNonce(nonce uint64) {
	a.use(func(addr common.Address) {
		a.db.SetNonce(addr, nonce, tracing.NonceChangeEoACall)
	}, func(addr common.Address32) {
		a.db.SetAddress32Nonce(addr, nonce)
	})
}

func (a accountBalance) add(amount *uint256.Int, reason tracing.BalanceChangeReason) {
	if amount.IsZero() {
		return
	}

	a.use(func(addr common.Address) {
		a.db.AddBalance(addr, amount, reason)
	}, func(addr common.Address32) {
		a.db.AddAddress32Balance(addr, amount)
	})
}

func (a accountBalance) sub(amount *big.Int, reason tracing.BalanceChangeReason) error {
	if amount.Sign() == 0 {
		return nil
	}

	cost, overflow := uint256.FromBig(amount)

	if overflow {
		return fmt.Errorf("%w: address %s", ErrInsufficientFunds, a.label())
	}

	if a.balance().Cmp(cost) < 0 {
		return fmt.Errorf("%w: address %s have %v want %v", ErrInsufficientFunds, a.label(), a.balance(), cost)
	}

	a.use(func(addr common.Address) {
		a.db.SubBalance(addr, cost, reason)
	}, func(addr common.Address32) {
		a.db.SubAddress32Balance(addr, cost)
	})

	return nil
}

func (a accountBalance) canPay(msg *Message, l1Cost *big.Int, operator *uint256.Int) error {
	need := new(big.Int).SetUint64(msg.GasLimit)
	price := msg.GasFeeCap

	if price == nil || price.BitLen() == 0 {
		price = msg.GasPrice
	}

	need.Mul(need, price)

	if l1Cost != nil {
		need.Add(need, l1Cost)
	}

	if operator != nil {
		need.Add(need, operator.ToBig())
	}

	if msg.Value != nil {
		need.Add(need, msg.Value)
	}

	have := a.balance().ToBig()

	if have.Cmp(need) < 0 {
		return fmt.Errorf("%w: address %s have %v want %v", ErrInsufficientFunds, a.label(), have, need)
	}

	return nil
}
