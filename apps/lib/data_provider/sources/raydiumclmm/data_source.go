// Code initially generated by gen.go.

package raydiumclmm

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Stork-Oracle/stork-external/apps/lib/data_provider/sources"
	"github.com/Stork-Oracle/stork-external/apps/lib/data_provider/types"
	"github.com/Stork-Oracle/stork-external/apps/lib/data_provider/utils"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
)

type raydiumCLMMDataSource struct {
	raydiumCLMMConfig RaydiumCLMMConfig
	valueId           types.ValueId
	updateFrequency   time.Duration
	rpcClient         *rpc.Client
	logger            zerolog.Logger
}

// https://github.com/raydium-io/raydium-sdk/blob/master/src/clmm/layout.ts#L34 RewardInfo
// Represents the reward information for a pool.
type RewardInfo struct {
	RewardState           uint8
	OpenTime              uint64
	EndTime               uint64
	LastUpdateTime        uint64
	EmissionsPerSecondX64 bin.Uint128
	RewardTotalEmissioned uint64
	RewardClaimed         uint64
	TokenMint             solana.PublicKey
	TokenVault            solana.PublicKey
	Creator               solana.PublicKey
	RewardGrowthGlobalX64 bin.Uint128
}

// https://github.com/raydium-io/raydium-sdk/blob/master/src/clmm/layout.ts#L10 PoolInfoLayout
// Represents the Raydium CLMM pool state layout.
type PoolState struct {
	Padding                   [8]uint8
	Bump                      uint8
	AmmConfig                 solana.PublicKey
	Creator                   solana.PublicKey
	MintA                     solana.PublicKey
	MintB                     solana.PublicKey
	VaultA                    solana.PublicKey
	VaultB                    solana.PublicKey
	ObservationId             solana.PublicKey
	MintDecimalsA             uint8
	MintDecimalsB             uint8
	TickSpacing               uint16
	Liquidity                 bin.Uint128
	SqrtPriceX64              bin.Uint128
	TickCurrent               int32
	ObservationIndex          uint16
	ObservationUpdateDuration uint16
	FeeGrowthGlobalX64A       bin.Uint128
	FeeGrowthGlobalX64B       bin.Uint128
	ProtocolFeesTokenA        uint64
	ProtocolFeesTokenB        uint64
	SwapInAmountTokenA        bin.Uint128
	SwapOutAmountTokenB       bin.Uint128
	SwapInAmountTokenB        bin.Uint128
	SwapOutAmountTokenA       bin.Uint128
	Status                    uint8
	Padding2                  [7]uint8
	RewardInfos               [3]RewardInfo
	TickArrayBitmap           [16]uint64
	TotalFeesTokenA           uint64
	TotalFeesClaimedTokenA    uint64
	TotalFeesTokenB           uint64
	TotalFeesClaimedTokenB    uint64
	FundFeesTokenA            uint64
	FundFeesTokenB            uint64
	StartTime                 uint64
	Padding3                  [57]uint64
}

func newRaydiumCLMMDataSource(sourceConfig types.DataProviderSourceConfig) *raydiumCLMMDataSource {
	raydiumCLMMConfig, err := GetSourceSpecificConfig(sourceConfig)
	if err != nil {
		panic("unable to decode config: " + err.Error())
	}

	updateFrequency, err := time.ParseDuration(raydiumCLMMConfig.UpdateFrequency)
	if err != nil {
		panic("unable to parse update frequency: " + raydiumCLMMConfig.UpdateFrequency)
	}

	rpcClient := rpc.New(raydiumCLMMConfig.HttpProviderUrl)

	return &raydiumCLMMDataSource{
		raydiumCLMMConfig: raydiumCLMMConfig,
		valueId:           sourceConfig.Id,
		updateFrequency:   updateFrequency,
		rpcClient:         rpcClient,
		logger:            utils.DataSourceLogger(RaydiumCLMMDataSourceId),
	}
}

func (r raydiumCLMMDataSource) RunDataSource(ctx context.Context, updatesCh chan types.DataSourceUpdateMap) {
	updater := func() (types.DataSourceUpdateMap, error) { return r.getUpdate() }
	scheduler := sources.NewScheduler(
		r.updateFrequency,
		updater,
		sources.GetErrorLogHandler(r.logger, zerolog.WarnLevel),
	)
	scheduler.RunScheduler(ctx, updatesCh)
}

func (r raydiumCLMMDataSource) getUpdate() (types.DataSourceUpdateMap, error) {
	pubKey, err := solana.PublicKeyFromBase58(r.raydiumCLMMConfig.ContractAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create public key: %w", err)
	}

	res, err := r.rpcClient.GetAccountInfoWithOpts(
		context.Background(),
		pubKey,
		&rpc.GetAccountInfoOpts{
			Encoding: solana.EncodingBase64,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get account info: %w", err)
	}

	var poolState PoolState
	decoder := bin.NewBorshDecoder(res.Value.Data.GetBinary())
	if err := decoder.Decode(&poolState); err != nil {
		return nil, fmt.Errorf("failed to deserialize pool state: %w", err)
	}

	updates := make(types.DataSourceUpdateMap)

	updateTime := time.Now().UTC().UnixMilli()
	updates[r.valueId] = types.DataSourceValueUpdate{
		ValueId:      r.valueId,
		DataSourceId: RaydiumCLMMDataSourceId,
		Time:         time.UnixMilli(updateTime),
		Value:        calculatePrice(poolState),
	}

	return updates, nil
}

// https://github.com/raydium-io/raydium-sdk/blob/master/src/clmm/utils/math.ts#L86 sqrtPriceX64ToPrice
// Converts sqrtPriceX64 representation to float64 price.
func calculatePrice(poolState PoolState) float64 {
	// Convert uint128 to float64
	hi := float64(poolState.SqrtPriceX64.Hi) * math.Pow(2, 64)
	lo := float64(poolState.SqrtPriceX64.Lo)
	sqrtPrice := (hi + lo) / math.Pow(2, 64)

	// Square the price and adjust decimals
	price := sqrtPrice * sqrtPrice * math.Pow(10, float64(poolState.MintDecimalsA)-float64(poolState.MintDecimalsB))

	return price
}
