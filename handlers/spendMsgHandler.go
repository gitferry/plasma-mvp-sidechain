package handlers

import (
	"crypto/sha256"
	"fmt"
	"github.com/FourthState/plasma-mvp-sidechain/msgs"
	"github.com/FourthState/plasma-mvp-sidechain/plasma"
	"github.com/FourthState/plasma-mvp-sidechain/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"math/big"
)

// returns the next tx index in the current block
type NextTxIndex func() uint16

// FeeUpdater updates the aggregate fee amount in a block
type FeeUpdater func(amt *big.Int) sdk.Error

func NewSpendHandler(txStore store.TxStore, depositStore store.DepositStore, blockStore store.BlockStore, nextTxIndex NextTxIndex, feeUpdater FeeUpdater) sdk.Handler {
	return func(ctx sdk.Context, msg sdk.Msg) sdk.Result {
		spendMsg, ok := msg.(msgs.SpendMsg)
		if !ok {
			panic("Msg does not implement SpendMsg")
		}

		txIndex := nextTxIndex()
		blockHeight := blockStore.NextPlasmaBlockNum(ctx)

		// construct the confirmation hash
		merkleHash := spendMsg.MerkleHash()
		header := ctx.BlockHeader().DataHash
		confirmationHash := sha256.Sum256(append(merkleHash, header...))

		/* Store Transaction */
		tx := store.Transaction{
			Transaction:      spendMsg.Transaction,
			Spent:            make([]bool, len(spendMsg.Outputs)),
			Spenders:         make([][]byte, len(spendMsg.Outputs)),
			ConfirmationHash: confirmationHash[:],
			Position:         plasma.NewPosition(blockHeight, txIndex, 0, big.NewInt(0)),
		}
		txStore.StoreTx(ctx, tx)

		/* Spend Inputs */
		for _, input := range spendMsg.Inputs {
			var res sdk.Result
			if input.Position.IsDeposit() {
				// Spend deposit and update account
				nonce := input.Position.DepositNonce
				deposit, ok := depositStore.GetDeposit(ctx, nonce)
				if !ok {
					panic(fmt.Sprintf("deposit store corrupted"))
				}

				res = depositStore.SpendDeposit(ctx, nonce, spendMsg.TxHash())
				txStore.SpendDepositWithAccount(ctx, nonce, deposit.Deposit)
			} else {
				res = txStore.SpendUTXO(ctx, input.Position, spendMsg.TxHash())
			}
			if !res.IsOK() {
				return res
			}
		}

		// update the aggregate fee amount for the block
		if err := feeUpdater(spendMsg.Fee); err != nil {
			return sdk.ErrInternal("error updating the aggregate fee").Result()
		}

		/* Create Outputs */
		for i, _ := range spendMsg.Outputs {
			pos := plasma.NewPosition(blockHeight, txIndex, uint8(i), big.NewInt(0))
			txStore.StoreUTXO(ctx, pos, spendMsg.TxHash())
		}

		return sdk.Result{}
	}
}
