package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

func TestBlockFetcher_GetBlock_and_SubscribeNewBlockEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	t.Cleanup(cancel)

	client := StartTestNode(t).Client
	fetcher := NewBlockFetcher(client)

	// generate some blocks
	newBlockChan, err := fetcher.SubscribeNewBlockEvent(ctx)
	require.NoError(t, err)

	for i := 1; i < 3; i++ {
		select {
		case newBlockFromChan := <-newBlockChan:
			h := newBlockFromChan.Height
			block, err := fetcher.GetBlock(ctx, &h)
			require.NoError(t, err)
			assert.Equal(t, newBlockFromChan, block)
			require.GreaterOrEqual(t, block.Height, int64(i))
		case <-ctx.Done():
			require.NoError(t, ctx.Err())
		}
	}
	require.NoError(t, fetcher.UnsubscribeNewBlockEvent(ctx))
}

// TestBlockFetcherHeaderValues tests that both the Commit and ValidatorSet
// endpoints are working as intended.
func TestBlockFetcherHeaderValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	t.Cleanup(cancel)

	client := StartTestNode(t).Client
	fetcher := NewBlockFetcher(client)

	// generate some blocks
	newBlockChan, err := fetcher.SubscribeNewBlockEvent(ctx)
	require.NoError(t, err)
	// read once from channel to generate next block
	var h int64
	select {
	case evt := <-newBlockChan:
		h = evt.Header.Height
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}
	// get Commit from current height
	commit, err := fetcher.Commit(ctx, &h)
	require.NoError(t, err)
	// get ValidatorSet from current height
	valSet, err := fetcher.ValidatorSet(ctx, &h)
	require.NoError(t, err)
	// get next block
	var nextBlock *types.Block
	select {
	case nextBlock = <-newBlockChan:
	case <-ctx.Done():
		require.NoError(t, ctx.Err())
	}
	// compare LastCommit from next block to Commit from first block height
	assert.Equal(t, nextBlock.LastCommit.Hash(), commit.Hash())
	assert.Equal(t, nextBlock.LastCommit.Height, commit.Height)
	assert.Equal(t, nextBlock.LastCommit.Signatures, commit.Signatures)
	// compare ValidatorSet hash to the ValidatorsHash from first block height
	hexBytes := valSet.Hash()
	assert.Equal(t, nextBlock.ValidatorsHash.Bytes(), hexBytes)
	require.NoError(t, fetcher.UnsubscribeNewBlockEvent(ctx))
}
