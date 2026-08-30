package transactionanalysis

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nickcandy/eth-fund-trace/internal/etherscan"
	"github.com/Nickcandy/eth-fund-trace/internal/store"
)

const (
	testHash   = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testPool   = "0x0000000000000000000000000000000000000010"
	testToken0 = "0x0000000000000000000000000000000000000020"
	testToken1 = "0x0000000000000000000000000000000000000030"
	testUser   = "0x0000000000000000000000000000000000000040"
)

type sourceStub struct {
	tx        etherscan.RPCTransaction
	receipt   etherscan.RPCReceipt
	code      string
	internals []etherscan.InternalTransaction
	calls     map[string]string
	poolCalls map[string]string
	txCalls   int
}

func (s *sourceStub) CodeAt(context.Context, string) (string, error) { return s.code, nil }
func (s *sourceStub) InternalTransactionsByHash(context.Context, string) ([]etherscan.InternalTransaction, error) {
	return s.internals, nil
}

func (s *sourceStub) TransactionByHash(context.Context, string) (etherscan.RPCTransaction, error) {
	s.txCalls++
	return s.tx, nil
}
func (s *sourceStub) TransactionReceipt(context.Context, string) (etherscan.RPCReceipt, error) {
	return s.receipt, nil
}
func (s *sourceStub) Call(_ context.Context, pool string, data string) (string, error) {
	if value, ok := s.poolCalls[pool+data]; ok {
		return value, nil
	}
	value, ok := s.calls[data]
	if !ok {
		return "", fmt.Errorf("unexpected selector %s", data)
	}
	return value, nil
}

type repoStub struct {
	analyses map[string]store.TransactionAnalysis
	pools    map[string]store.PoolMetadata
}

func newRepoStub() *repoStub {
	return &repoStub{analyses: map[string]store.TransactionAnalysis{}, pools: map[string]store.PoolMetadata{}}
}
func (r *repoStub) FindTransactionAnalysis(_ context.Context, chain, hash string) (store.TransactionAnalysis, bool, error) {
	value, ok := r.analyses[chain+hash]
	return value, ok, nil
}
func (r *repoStub) SaveTransactionAnalysis(_ context.Context, value store.TransactionAnalysis) error {
	r.analyses[value.Chain+value.TxHash] = value
	return nil
}
func (r *repoStub) FindPoolMetadata(_ context.Context, chain, pool string) (store.PoolMetadata, bool, error) {
	value, ok := r.pools[chain+pool]
	return value, ok, nil
}
func (r *repoStub) SavePoolMetadata(_ context.Context, value store.PoolMetadata) error {
	r.pools[value.Chain+value.Pool] = value
	return nil
}

func TestAnalyzeVerifiedV3SwapPreservesSignedLargeAmountsAndCaches(t *testing.T) {
	huge := new(big.Int).Lsh(big.NewInt(1), 200)
	negativeFive := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(5))
	source := &sourceStub{
		tx: etherscan.RPCTransaction{Hash: testHash, From: testUser, To: "0x68b3465833fb72a70ecdf485e0e4c7bd8665fc45", Value: "0x0", Input: "0x1234", BlockNumber: "0x10"},
		receipt: etherscan.RPCReceipt{TransactionHash: testHash, BlockNumber: "0x10", Status: "0x1", Logs: []etherscan.RPCLog{
			{Address: testPool, Topics: []string{swapTopic, topic(testUser), topic(testUser)}, Data: "0x" + word(huge) + word(negativeFive) + word(big.NewInt(1)) + word(big.NewInt(1)) + word(big.NewInt(0)), LogIndex: "0x1"},
			{Address: testToken1, Topics: []string{transferTopic, topic(testPool), topic(testUser)}, Data: "0x" + word(big.NewInt(5)), LogIndex: "0x2"},
		}},
		calls: map[string]string{
			"0x0dfe1681": returnWord(testToken0), "0xd21220a7": returnWord(testToken1),
			"0xddca3f43": "0x" + word(big.NewInt(3000)), "0xc45a0155": returnWord(EthereumV3Factory),
		},
	}
	repo := newRepoStub()
	service := New(source, repo, func() time.Time { return time.Unix(1, 0) })
	analysis, err := service.Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Swaps) != 1 || !analysis.Swaps[0].Verified || analysis.Swaps[0].AmountIn != huge.String() || analysis.Swaps[0].AmountOut != "5" {
		t.Fatalf("swap = %+v", analysis.Swaps)
	}
	if analysis.FinalOutputAddress != testUser || analysis.EntryContractName != "Uniswap SwapRouter02" || analysis.Quality.Status != "complete" {
		t.Fatalf("analysis = %+v", analysis)
	}
	if _, err := service.Analyze(context.Background(), "ethereum", testHash); err != nil || source.txCalls != 1 {
		t.Fatalf("cache txCalls=%d err=%v", source.txCalls, err)
	}
}

func TestInspectAddressClassifiesKyberExecutorAndEOA(t *testing.T) {
	service := New(&sourceStub{code: "0x60016000"}, newRepoStub(), time.Now)
	identity, err := service.InspectAddress(context.Background(), "ethereum", KyberSwapExecutor)
	if err != nil {
		t.Fatal(err)
	}
	if identity.AddressType != "contract" || identity.Protocol != "kyberswap" || !slices.Equal(identity.Roles, []string{"kyberswap_executor"}) {
		t.Fatalf("identity=%+v", identity)
	}

	service = New(&sourceStub{code: "0x"}, newRepoStub(), time.Now)
	identity, err = service.InspectAddress(context.Background(), "ethereum", testUser)
	if err != nil || identity.AddressType != "eoa" || identity.Protocol != "" || len(identity.Roles) != 0 {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
}

func TestInspectAddressClassifiesKnownBridgeAsTerminalIdentity(t *testing.T) {
	service := New(&sourceStub{code: "0x6001"}, &repoStub{}, time.Now)
	identity, err := service.InspectAddress(context.Background(), "ethereum", EthereumL1StandardBridge)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Protocol != "bridge" || !slices.Contains(identity.Roles, "cross_chain_bridge") {
		t.Fatalf("identity=%+v", identity)
	}
}

func TestInspectAddressClassifiesWOOXWalletsBeforeBytecodeLookup(t *testing.T) {
	service := New(&sourceStub{code: "0x60016000"}, newRepoStub(), time.Now)
	identity, err := service.InspectAddress(context.Background(), "ethereum", "0x03DD167D62E1dfC223FfD7b37fC8bF45Fb973478")
	if err != nil {
		t.Fatal(err)
	}
	if identity.AddressType != "eoa" || identity.Protocol != "woo_x" || !slices.Contains(identity.Roles, "woo_x_wallet") {
		t.Fatalf("identity=%+v", identity)
	}

	identity, err = service.InspectAddress(context.Background(), "ethereum", "0x1326a1f39746726fdcfe88d83effe5451606ae85")
	if err != nil {
		t.Fatal(err)
	}
	if identity.AddressType != "contract" || identity.Protocol != "woo_x" || !slices.Contains(identity.Roles, "woo_x_vault") {
		t.Fatalf("vault identity=%+v", identity)
	}

	identity, err = service.InspectAddress(context.Background(), "ethereum", testUser)
	if err != nil || identity.Protocol != "" || identity.AddressType != "contract" {
		t.Fatalf("unknown identity=%+v err=%v", identity, err)
	}
}

func TestInspectAddressClassifiesVerifiedPool(t *testing.T) {
	repo := newRepoStub()
	repo.pools["ethereum"+testPool] = store.PoolMetadata{Chain: "ethereum", Pool: testPool, Verified: true}
	identity, err := New(&sourceStub{code: "0x6001"}, repo, time.Now).InspectAddress(context.Background(), "ethereum", testPool)
	if err != nil || identity.AddressType != "contract" || identity.Protocol != "uniswap" || !slices.Equal(identity.Roles, []string{"pool"}) {
		t.Fatalf("identity=%+v err=%v", identity, err)
	}
}

func TestAnalyzeKyberRFQBuildsVerifiedConversion(t *testing.T) {
	const (
		initiator = "0x00000000000000000000000000000000000000a1"
		provider  = "0x67336cec42645f55059eff241cb02ea5cc52ff86"
		usdt      = "0xdac17f958d2ee523a2206206994597c13d831ec7"
		amountIn  = "1000000000000"
		amountOut = "274823886000000000000"
	)
	source := &sourceStub{
		tx: etherscan.RPCTransaction{Hash: testHash, From: initiator, To: KyberSwapRouter, Value: "0x0", Input: "0x1234", BlockNumber: "0x10"},
		receipt: etherscan.RPCReceipt{TransactionHash: testHash, BlockNumber: "0x10", Status: "0x1", Logs: []etherscan.RPCLog{
			{Address: usdt, Topics: []string{transferTopic, topic(initiator), topic(KyberSwapExecutor)}, Data: "0x" + word(decimal(amountIn)), LogIndex: "0x1"},
			{Address: EthereumWETH, Topics: []string{transferTopic, topic(provider), topic(KyberSwapExecutor)}, Data: "0x" + word(decimal(amountOut)), LogIndex: "0x2"},
			{Address: usdt, Topics: []string{transferTopic, topic(KyberSwapExecutor), topic(provider)}, Data: "0x" + word(decimal(amountIn)), LogIndex: "0x3"},
			{Address: EthereumWETH, Topics: []string{withdrawalTopic, topic(KyberSwapExecutor)}, Data: "0x" + word(decimal(amountOut)), LogIndex: "0x4"},
		}},
		internals: []etherscan.InternalTransaction{
			{From: KyberSwapRouter, To: KyberSwapExecutor, Value: "0", Type: "call", TraceID: "0"},
			{From: EthereumWETH, To: KyberSwapExecutor, Value: amountOut, Type: "call", TraceID: "0_1"},
			{From: KyberSwapExecutor, To: initiator, Value: amountOut, Type: "call", TraceID: "0_2"},
		},
	}

	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.InternalCalls) != 3 || len(analysis.Conversions) != 1 {
		t.Fatalf("analysis=%+v", analysis)
	}
	conversion := analysis.Conversions[0]
	if conversion.Status != "complete" || conversion.Protocol != "kyberswap" || conversion.Version != "rfq" || conversion.Initiator != initiator || conversion.Router != KyberSwapRouter || conversion.Executor != KyberSwapExecutor || conversion.LiquidityProvider != provider || conversion.Recipient != initiator || conversion.TokenIn != usdt || conversion.AmountIn != amountIn || conversion.TokenOut != "ETH" || conversion.AmountOut != amountOut {
		t.Fatalf("conversion=%+v", conversion)
	}
	if analysis.Quality.Status != "complete" || analysis.FinalOutputAddress != initiator {
		t.Fatalf("quality=%+v output=%s", analysis.Quality, analysis.FinalOutputAddress)
	}
}

func TestAnalyzeKyberRFQAcceptsCompleteReceiptWhenZeroValueRouterCallIsOmitted(t *testing.T) {
	source := kyberFixtureSource()
	source.internals = source.internals[1:]
	amountOut, _ := new(big.Int).SetString(source.internals[1].Value, 10)
	unwrapAmount := new(big.Int).Add(amountOut, big.NewInt(1)).String()
	source.receipt.Logs[3].Data = "0x" + word(decimal(unwrapAmount))
	source.internals[0].Value = unwrapAmount

	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Conversions) != 1 || analysis.Conversions[0].Status != "complete" || analysis.Quality.Status != "complete" {
		t.Fatalf("analysis=%+v", analysis)
	}
}

func TestAnalyzeKyberRFQRejectsIncompleteOrAmbiguousEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sourceStub)
	}{
		{name: "missing internal payout", mutate: func(source *sourceStub) { source.internals = source.internals[:2] }},
		{name: "failed internal call", mutate: func(source *sourceStub) { source.internals[2].IsError = true }},
		{name: "amount mismatch", mutate: func(source *sourceStub) { source.internals[2].Value = "1" }},
		{name: "multiple recipients", mutate: func(source *sourceStub) {
			source.internals = append(source.internals, etherscan.InternalTransaction{From: KyberSwapExecutor, To: "0x00000000000000000000000000000000000000b2", Value: source.internals[2].Value, Type: "call", TraceID: "0_3"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := kyberFixtureSource()
			test.mutate(source)
			analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
			if err != nil {
				t.Fatal(err)
			}
			if len(analysis.Conversions) != 1 || analysis.Conversions[0].Status != "partial" || analysis.Quality.Status != "partial" || len(analysis.Conversions[0].Issues) == 0 {
				t.Fatalf("analysis=%+v", analysis)
			}
		})
	}
}

func kyberFixtureSource() *sourceStub {
	const (
		initiator = "0x00000000000000000000000000000000000000a1"
		provider  = "0x67336cec42645f55059eff241cb02ea5cc52ff86"
		usdt      = "0xdac17f958d2ee523a2206206994597c13d831ec7"
		amountIn  = "1000000000000"
		amountOut = "274823886000000000000"
	)
	return &sourceStub{
		tx: etherscan.RPCTransaction{Hash: testHash, From: initiator, To: KyberSwapRouter, Value: "0x0", Input: "0x1234", BlockNumber: "0x10"},
		receipt: etherscan.RPCReceipt{TransactionHash: testHash, BlockNumber: "0x10", Status: "0x1", Logs: []etherscan.RPCLog{
			{Address: usdt, Topics: []string{transferTopic, topic(initiator), topic(KyberSwapExecutor)}, Data: "0x" + word(decimal(amountIn)), LogIndex: "0x1"},
			{Address: EthereumWETH, Topics: []string{transferTopic, topic(provider), topic(KyberSwapExecutor)}, Data: "0x" + word(decimal(amountOut)), LogIndex: "0x2"},
			{Address: usdt, Topics: []string{transferTopic, topic(KyberSwapExecutor), topic(provider)}, Data: "0x" + word(decimal(amountIn)), LogIndex: "0x3"},
			{Address: EthereumWETH, Topics: []string{withdrawalTopic, topic(KyberSwapExecutor)}, Data: "0x" + word(decimal(amountOut)), LogIndex: "0x4"},
		}},
		internals: []etherscan.InternalTransaction{
			{From: KyberSwapRouter, To: KyberSwapExecutor, Value: "0", Type: "call", TraceID: "0"},
			{From: EthereumWETH, To: KyberSwapExecutor, Value: amountOut, Type: "call", TraceID: "0_1"},
			{From: KyberSwapExecutor, To: initiator, Value: amountOut, Type: "call", TraceID: "0_2"},
		},
	}
}

func TestAnalyzeRejectsFakePoolAndMarksAmbiguousRoutes(t *testing.T) {
	source := fixtureSource()
	source.calls["0xc45a0155"] = returnWord("0x0000000000000000000000000000000000000099")
	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Swaps) != 1 || analysis.Swaps[0].Verified || analysis.Swaps[0].Protocol != "" || analysis.Quality.Status != "partial" {
		t.Fatalf("analysis = %+v", analysis)
	}
}

func TestAnalyzeWETHAndFailedTransaction(t *testing.T) {
	source := fixtureSource()
	source.receipt.Logs = []etherscan.RPCLog{{Address: EthereumWETH, Topics: []string{depositTopic, topic(testUser)}, Data: "0x" + word(big.NewInt(42)), LogIndex: "0x5"}}
	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil || len(analysis.Wraps) != 1 || analysis.Wraps[0].Amount != "42" || analysis.Wraps[0].Type != "deposit" {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}

	source = fixtureSource()
	source.receipt.Status = "0x0"
	analysis, err = New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil || analysis.Succeeded || len(analysis.Swaps) != 0 || analysis.Quality.Status != "partial" {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
}

func TestAnalyzeSortsConnectedTwoHopUniversalRouterRoute(t *testing.T) {
	poolB := "0x0000000000000000000000000000000000000011"
	token2 := "0x0000000000000000000000000000000000000050"
	router := "0xef1c6e67703c7bd7107eed8303fbe6ec2554bf6b"
	source := fixtureSource()
	source.tx.To = router
	source.receipt.Logs = []etherscan.RPCLog{
		{Address: poolB, Topics: []string{swapTopic, topic(router), topic(testUser)}, Data: swapData(9, -8), LogIndex: "0x4"},
		{Address: testToken1, Topics: []string{transferTopic, topic(testPool), topic(router)}, Data: "0x" + word(big.NewInt(9)), LogIndex: "0x2"},
		{Address: testPool, Topics: []string{swapTopic, topic(testUser), topic(router)}, Data: swapData(10, -9), LogIndex: "0x1"},
		{Address: token2, Topics: []string{transferTopic, topic(poolB), topic(testUser)}, Data: "0x" + word(big.NewInt(8)), LogIndex: "0x5"},
	}
	source.poolCalls = map[string]string{
		testPool + "0x0dfe1681": returnWord(testToken0), testPool + "0xd21220a7": returnWord(testToken1),
		testPool + "0xddca3f43": "0x" + word(big.NewInt(3000)), testPool + "0xc45a0155": returnWord(EthereumV3Factory),
		poolB + "0x0dfe1681": returnWord(testToken1), poolB + "0xd21220a7": returnWord(token2),
		poolB + "0xddca3f43": "0x" + word(big.NewInt(500)), poolB + "0xc45a0155": returnWord(EthereumV3Factory),
	}
	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.EntryContractName != "Uniswap Universal Router" || len(analysis.Swaps) != 2 || analysis.Swaps[0].LogIndex != 1 || analysis.Swaps[1].LogIndex != 4 {
		t.Fatalf("analysis=%+v", analysis)
	}
	if analysis.Quality.AmbiguousRoute || analysis.FinalOutputAddress != testUser {
		t.Fatalf("quality=%+v final=%s", analysis.Quality, analysis.FinalOutputAddress)
	}
}

func TestAnalyzeMarksMalformedSwapLogPartial(t *testing.T) {
	source := fixtureSource()
	source.receipt.Logs = []etherscan.RPCLog{{Address: testPool, Topics: []string{swapTopic}, Data: "0x01", LogIndex: "0x1"}}
	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil || analysis.Quality.Status != "partial" || len(analysis.Swaps) != 0 {
		t.Fatalf("analysis=%+v err=%v", analysis, err)
	}
}

func TestAnalyzeValidatesInput(t *testing.T) {
	service := New(fixtureSource(), newRepoStub(), time.Now)
	if _, err := service.Analyze(context.Background(), "base", testHash); !errors.Is(err, ErrUnsupportedChain) {
		t.Fatalf("error=%v", err)
	}
	if _, err := service.Analyze(context.Background(), "ethereum", "0xbad"); !errors.Is(err, ErrInvalidHash) {
		t.Fatalf("error=%v", err)
	}
	source := fixtureSource()
	source.tx.Value = "not-hex"
	if _, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash); !errors.Is(err, etherscan.ErrMalformedResponse) {
		t.Fatalf("error=%v", err)
	}
}

func TestAnalyzeBitTorrentBridgeETHDeposit(t *testing.T) {
	user := "0xd2811518b89f30400c4633747189ba1216f539f7"
	amount := "101296840289918400000"
	source := &sourceStub{
		tx: etherscan.RPCTransaction{From: user, To: BitTorrentRootChainManager, Value: "0x57dc6ab8cce103600", Input: "0x4faa8a26000000000000000000000000d2811518b89f30400c4633747189ba1216f539f7"},
		receipt: etherscan.RPCReceipt{Status: "0x1", BlockNumber: "0x1", Logs: []etherscan.RPCLog{
			{Address: BitTorrentEtherPredicate, Topics: []string{lockedEtherTopic, "0x000000000000000000000000" + user[2:], "0x000000000000000000000000" + user[2:]}, Data: "0x0000000000000000000000000000000000000000000000057dc6ab8cce103600", LogIndex: "0x1"},
			{Address: "0xedf53026aea60f8f75fca25f8830b7e2d6200662", Topics: []string{stateSyncedTopic, "0x0000000000000000000000000000000000000000000000000000000000001732"}, LogIndex: "0x2"},
		}},
	}
	analysis, err := New(source, newRepoStub(), time.Now).Analyze(context.Background(), "ethereum", testHash)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ProtocolAction != "bittorrent_bridge_inbound" || analysis.ProtocolDestination != user || analysis.ProtocolAsset != "ETH" || analysis.ProtocolAmount != amount || analysis.ProtocolMemo != "StateSynced #5938" {
		t.Fatalf("analysis=%+v", analysis)
	}
}

func fixtureSource() *sourceStub {
	return &sourceStub{
		tx:      etherscan.RPCTransaction{Hash: testHash, From: testUser, To: testPool, Value: "0x0", BlockNumber: "0x10"},
		receipt: etherscan.RPCReceipt{TransactionHash: testHash, BlockNumber: "0x10", Status: "0x1", Logs: []etherscan.RPCLog{{Address: testPool, Topics: []string{swapTopic, topic(testUser), topic(testUser)}, Data: "0x" + word(big.NewInt(10)) + word(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(5))) + strings.Repeat(word(big.NewInt(0)), 3), LogIndex: "0x1"}}},
		calls:   map[string]string{"0x0dfe1681": returnWord(testToken0), "0xd21220a7": returnWord(testToken1), "0xddca3f43": "0x" + word(big.NewInt(3000)), "0xc45a0155": returnWord(EthereumV3Factory)},
	}
}

func word(number *big.Int) string { return fmt.Sprintf("%064x", number) }
func swapData(amount0, amount1 int64) string {
	values := []*big.Int{big.NewInt(amount0), big.NewInt(amount1), big.NewInt(0), big.NewInt(0), big.NewInt(0)}
	for i := 0; i < 2; i++ {
		if values[i].Sign() < 0 {
			values[i].Add(values[i], new(big.Int).Lsh(big.NewInt(1), 256))
		}
	}
	var result strings.Builder
	result.WriteString("0x")
	for _, value := range values {
		result.WriteString(word(value))
	}
	return result.String()
}
func topic(value string) string {
	return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(value, "0x")
}
func returnWord(value string) string { return topic(value) }
func decimal(value string) *big.Int {
	result, _ := new(big.Int).SetString(value, 10)
	return result
}
