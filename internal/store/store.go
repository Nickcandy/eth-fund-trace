package store

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	AddressesCollection           = "addresses"
	TransfersCollection           = "transfers"
	LabelsCollection              = "labels"
	SyncJobsCollection            = "sync_jobs"
	ProfilesCollection            = "address_profiles"
	TraceJobsCollection           = "trace_jobs"
	CrossChainLinksCollection     = "cross_chain_links"
	TransactionAnalysesCollection = "transaction_analyses"
	PoolMetadataCollection        = "pool_metadata"
)

type Store struct {
	db *mongo.Database
}

func New(db *mongo.Database) *Store {
	return &Store{db: db}
}

func (s *Store) Initialize(ctx context.Context) error {
	for _, name := range []string{AddressesCollection, TransfersCollection, LabelsCollection, SyncJobsCollection, ProfilesCollection, TraceJobsCollection, CrossChainLinksCollection, TransactionAnalysesCollection, PoolMetadataCollection} {
		if err := s.ensureCollection(ctx, name); err != nil {
			return err
		}
	}

	for name, models := range indexModels() {
		if _, err := s.db.Collection(name).Indexes().CreateMany(ctx, models); err != nil {
			return err
		}
	}
	return nil
}

func indexModels() map[string][]mongo.IndexModel {
	return map[string][]mongo.IndexModel{
		AddressesCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_addresses_chain_address")},
			{Keys: bson.D{{Key: "syncStatus", Value: 1}, {Key: "lastSyncedAt", Value: 1}}, Options: options.Index().SetName("idx_addresses_status_synced")},
		},
		TransfersCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "txHash", Value: 1}, {Key: "source", Value: 1}, {Key: "traceId", Value: 1}, {Key: "logIndex", Value: 1}, {Key: "asset", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_transfers_identity")},
			{Keys: edgeIndex("from", ""), Options: options.Index().SetName("idx_transfers_from_cursor")},
			{Keys: edgeIndex("to", ""), Options: options.Index().SetName("idx_transfers_to_cursor")},
			{Keys: edgeIndex("from", "assetType"), Options: options.Index().SetName("idx_transfers_from_type_cursor")},
			{Keys: edgeIndex("to", "assetType"), Options: options.Index().SetName("idx_transfers_to_type_cursor")},
			{Keys: edgeIndex("from", "asset"), Options: options.Index().SetName("idx_transfers_from_asset_cursor")},
			{Keys: edgeIndex("to", "asset"), Options: options.Index().SetName("idx_transfers_to_asset_cursor")},
		},
		LabelsCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "type", Value: 1}, {Key: "source", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_labels_identity")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "observedAt", Value: -1}}, Options: options.Index().SetName("idx_labels_address_observed")},
		},
		SyncJobsCollection: {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("idx_sync_jobs_status_created")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("idx_sync_jobs_address_created")},
		},
		ProfilesCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "ruleVersion", Value: 1}, {Key: "dataThroughBlock", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_profiles_version_block")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "computedAt", Value: -1}}, Options: options.Index().SetName("idx_profiles_address_computed")},
		},
		TraceJobsCollection: {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("idx_trace_jobs_status_created")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "seedAddress", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("idx_trace_jobs_seed_created")},
		},
		CrossChainLinksCollection: {
			{Keys: bson.D{{Key: "sourceChain", Value: 1}, {Key: "sourceTxHash", Value: 1}, {Key: "sourceLogIndex", Value: 1}, {Key: "targetChain", Value: 1}, {Key: "targetTxHash", Value: 1}, {Key: "targetLogIndex", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_cross_chain_link_evidence")},
			{Keys: bson.D{{Key: "sourceChain", Value: 1}, {Key: "sourceAddress", Value: 1}, {Key: "observedAt", Value: -1}}, Options: options.Index().SetName("idx_cross_chain_source_address")},
			{Keys: bson.D{{Key: "targetChain", Value: 1}, {Key: "targetAddress", Value: 1}, {Key: "observedAt", Value: -1}}, Options: options.Index().SetName("idx_cross_chain_target_address")},
			{Keys: bson.D{{Key: "identityKey", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true).SetName("uq_cross_chain_auto_identity")},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "nextCheckAt", Value: 1}}, Options: options.Index().SetName("idx_cross_chain_status_check")},
			{Keys: bson.D{{Key: "sourceChain", Value: 1}, {Key: "targetChain", Value: 1}, {Key: "protocol", Value: 1}, {Key: "status", Value: 1}}, Options: options.Index().SetName("idx_cross_chain_protocol_status")},
		},
		TransactionAnalysesCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "txHash", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_transaction_analyses_chain_hash")},
		},
		PoolMetadataCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "pool", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_pool_metadata_chain_pool")},
		},
	}
}

func edgeIndex(endpoint, assetField string) bson.D {
	keys := bson.D{{Key: "chain", Value: 1}, {Key: endpoint, Value: 1}}
	if assetField != "" {
		keys = append(keys, bson.E{Key: assetField, Value: 1})
	}
	keys = append(keys,
		bson.E{Key: "blockNumber", Value: -1}, bson.E{Key: "txHash", Value: -1}, bson.E{Key: "source", Value: -1},
		bson.E{Key: "traceId", Value: -1}, bson.E{Key: "logIndex", Value: -1},
	)
	if assetField != "asset" {
		keys = append(keys, bson.E{Key: "asset", Value: -1})
	}
	return keys
}

func (s *Store) FindAddress(ctx context.Context, chain, address string) (Address, bool, error) {
	var result Address
	err := s.db.Collection(AddressesCollection).FindOne(ctx, bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}}).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Address{}, false, nil
	}
	return result, err == nil, err
}

func (s *Store) SetAddressSyncing(ctx context.Context, chain string, chainID int64, address string) error {
	_, err := s.db.Collection(AddressesCollection).UpdateOne(ctx,
		bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}},
		bson.D{
			{Key: "$set", Value: bson.D{{Key: "syncStatus", Value: "running"}, {Key: "syncError", Value: ""}}},
			{Key: "$setOnInsert", Value: bson.D{{Key: "chainId", Value: chainID}}},
		}, options.Update().SetUpsert(true))
	return err
}

func (s *Store) CompleteAddressSync(ctx context.Context, chain, address string, earliest, latest int64, syncedAt time.Time) error {
	_, err := s.db.Collection(AddressesCollection).UpdateOne(ctx,
		bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "earliestSyncedBlock", Value: earliest},
			{Key: "historySyncedToBlock", Value: latest},
			{Key: "latestSyncedBlock", Value: latest},
			{Key: "lastSyncedAt", Value: syncedAt},
			{Key: "syncStatus", Value: "synced"},
			{Key: "syncError", Value: ""},
		}}})
	return err
}

func (s *Store) FailAddressSync(ctx context.Context, chain, address, message string) error {
	_, err := s.db.Collection(AddressesCollection).UpdateOne(ctx,
		bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "syncStatus", Value: "failed"}, {Key: "syncError", Value: message}}}})
	return err
}

func (s *Store) BulkUpsertTransfers(ctx context.Context, transfers []Transfer) (int64, error) {
	if len(transfers) == 0 {
		return 0, nil
	}
	models := make([]mongo.WriteModel, 0, len(transfers))
	for _, transfer := range transfers {
		filter := bson.D{{Key: "chain", Value: transfer.Chain}, {Key: "txHash", Value: transfer.TxHash}, {Key: "source", Value: transfer.Source}, {Key: "traceId", Value: transfer.TraceID}, {Key: "logIndex", Value: transfer.LogIndex}, {Key: "asset", Value: transfer.Asset}}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(bson.D{{Key: "$set", Value: transfer}}).SetUpsert(true))
	}
	result, err := s.db.Collection(TransfersCollection).BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		return 0, err
	}
	return result.UpsertedCount + result.ModifiedCount, nil
}

func (s *Store) UpsertDiscoveredAddresses(ctx context.Context, chain string, chainID int64, addresses []string, observedAt time.Time) error {
	if len(addresses) == 0 {
		return nil
	}
	models := make([]mongo.WriteModel, 0, len(addresses))
	for _, address := range addresses {
		filter := bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}}
		insert := bson.D{{Key: "chain", Value: chain}, {Key: "chainId", Value: chainID}, {Key: "address", Value: address}, {Key: "syncStatus", Value: "discovered"}, {Key: "lastSyncedAt", Value: time.Time{}}, {Key: "observedAt", Value: observedAt}}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(bson.D{{Key: "$setOnInsert", Value: insert}}).SetUpsert(true))
	}
	_, err := s.db.Collection(AddressesCollection).BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	return err
}

func (s *Store) TopNeighbors(ctx context.Context, chain, address string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	positive := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "amount", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}}},
		bson.D{{Key: "tokenValue", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}}},
	}}}
	connected := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "from", Value: address}},
		bson.D{{Key: "to", Value: address}},
	}}}
	matchStage := bson.D{{Key: "$match", Value: bson.D{{Key: "chain", Value: chain}, {Key: "$and", Value: bson.A{connected, positive}}}}}
	condition := bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$from", address}}}, "$to", "$from"}}}
	projectStage := bson.D{{Key: "$project", Value: bson.D{{Key: "neighbor", Value: condition}}}}
	neighborStage := bson.D{{Key: "$match", Value: bson.D{{Key: "neighbor", Value: bson.D{{Key: "$nin", Value: bson.A{"", address, "0x0000000000000000000000000000000000000000"}}}}}}}
	groupStage := bson.D{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$neighbor"}, {Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}}}}}
	sortStage := bson.D{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}}}}
	limitStage := bson.D{{Key: "$limit", Value: int64(limit)}}
	pipeline := mongo.Pipeline{matchStage, projectStage, neighborStage, groupStage, sortStage, limitStage}
	cursor, err := s.db.Collection(TransfersCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var rows []struct {
		Address string `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.Address)
	}
	return result, nil
}

func (s *Store) CreateSyncJob(ctx context.Context, job *SyncJob) error {
	if job.ID.IsZero() {
		job.ID = primitive.NewObjectID()
	}
	_, err := s.db.Collection(SyncJobsCollection).InsertOne(ctx, job)
	return err
}

func (s *Store) GetSyncJob(ctx context.Context, id primitive.ObjectID) (SyncJob, error) {
	var job SyncJob
	err := s.db.Collection(SyncJobsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&job)
	return job, err
}

func (s *Store) SaveSyncJob(ctx context.Context, job SyncJob) error {
	_, err := s.db.Collection(SyncJobsCollection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: job.ID}}, job)
	return err
}

func (s *Store) FailInterruptedJobs(ctx context.Context, now time.Time) error {
	_, err := s.db.Collection(SyncJobsCollection).UpdateMany(ctx,
		bson.D{{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"queued", "running"}}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: "failed"}, {Key: "finishedAt", Value: now}, {Key: "errorCode", Value: "interrupted"}, {Key: "error", Value: "service restarted before job completed"}, {Key: "retryable", Value: true}}}})
	return err
}

func (s *Store) FindAddressProfile(ctx context.Context, chain, address, ruleVersion string, dataThroughBlock int64) (AddressProfile, bool, error) {
	var result AddressProfile
	err := s.db.Collection(ProfilesCollection).FindOne(ctx, bson.D{
		{Key: "chain", Value: chain}, {Key: "address", Value: address},
		{Key: "ruleVersion", Value: ruleVersion}, {Key: "dataThroughBlock", Value: dataThroughBlock},
	}).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AddressProfile{}, false, nil
	}
	return result, err == nil, err
}

func (s *Store) SaveAddressProfile(ctx context.Context, profile AddressProfile) error {
	filter := bson.D{
		{Key: "chain", Value: profile.Chain}, {Key: "address", Value: profile.Address},
		{Key: "ruleVersion", Value: profile.RuleVersion}, {Key: "dataThroughBlock", Value: profile.DataThroughBlock},
	}
	_, err := s.db.Collection(ProfilesCollection).ReplaceOne(ctx, filter, profile, options.Replace().SetUpsert(true))
	return err
}

func (s *Store) UpsertLabel(ctx context.Context, label Label) error {
	filter := bson.D{{Key: "chain", Value: label.Chain}, {Key: "address", Value: label.Address}, {Key: "type", Value: label.Type}, {Key: "source", Value: label.Source}}
	_, err := s.db.Collection(LabelsCollection).ReplaceOne(ctx, filter, label, options.Replace().SetUpsert(true))
	return err
}

func (s *Store) ListLabels(ctx context.Context, chain, address string) ([]Label, error) {
	cursor, err := s.db.Collection(LabelsCollection).Find(ctx, bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}}, options.Find().SetSort(bson.D{{Key: "observedAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var result []Label
	return result, cursor.All(ctx, &result)
}

func (s *Store) CreateTraceJob(ctx context.Context, job *TraceJob) error {
	if job.ID.IsZero() {
		job.ID = primitive.NewObjectID()
	}
	_, err := s.db.Collection(TraceJobsCollection).InsertOne(ctx, job)
	return err
}

func (s *Store) GetTraceJob(ctx context.Context, id primitive.ObjectID) (TraceJob, error) {
	var job TraceJob
	err := s.db.Collection(TraceJobsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&job)
	return job, err
}

func (s *Store) SaveTraceJob(ctx context.Context, job TraceJob) error {
	_, err := s.db.Collection(TraceJobsCollection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: job.ID}}, job)
	return err
}

func (s *Store) FailInterruptedTraceJobs(ctx context.Context, now time.Time) error {
	_, err := s.db.Collection(TraceJobsCollection).UpdateMany(ctx, bson.D{{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"queued", "waiting_sync", "running"}}}}}, bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: "failed"}, {Key: "finishedAt", Value: now}, {Key: "errorCode", Value: "interrupted"}, {Key: "error", Value: "service restarted before trace completed"}, {Key: "retryable", Value: true}}}})
	return err
}

func (s *Store) AddressActivity(ctx context.Context, chain, address string) (AddressActivity, error) {
	match := profileMatch(chain, address)
	lifetimePipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "incoming", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$to", address}}}, 1, 0}}}}}},
			{Key: "outgoing", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$from", address}}}, 1, 0}}}}}},
			{Key: "latest", Value: bson.D{{Key: "$max", Value: "$blockTime"}}},
		}}},
	}
	var lifetimeRows []struct {
		Total    int64     `bson:"total"`
		Incoming int64     `bson:"incoming"`
		Outgoing int64     `bson:"outgoing"`
		Latest   time.Time `bson:"latest"`
	}
	if err := s.aggregateAll(ctx, lifetimePipeline, &lifetimeRows); err != nil {
		return AddressActivity{}, err
	}
	if len(lifetimeRows) == 0 {
		return AddressActivity{}, nil
	}
	lifetime := lifetimeRows[0]
	windowMatch := append(bson.D{}, match...)
	windowMatch = append(windowMatch, bson.E{Key: "blockTime", Value: bson.D{{Key: "$gte", Value: lifetime.Latest.Add(-30 * 24 * time.Hour)}, {Key: "$lte", Value: lifetime.Latest}}})
	isIncoming := bson.D{{Key: "$eq", Value: bson.A{"$to", address}}}
	isOutgoing := bson.D{{Key: "$eq", Value: bson.A{"$from", address}}}
	project := bson.D{
		{Key: "incoming", Value: isIncoming},
		{Key: "outgoing", Value: isOutgoing},
		{Key: "counterparty", Value: bson.D{{Key: "$cond", Value: bson.A{isOutgoing, "$to", "$from"}}}},
		{Key: "sender", Value: bson.D{{Key: "$cond", Value: bson.A{isIncoming, "$from", nil}}}},
		{Key: "recipient", Value: bson.D{{Key: "$cond", Value: bson.A{isOutgoing, "$to", nil}}}},
		{Key: "day", Value: bson.D{{Key: "$dateToString", Value: bson.D{{Key: "format", Value: "%Y-%m-%d"}, {Key: "date", Value: "$blockTime"}, {Key: "timezone", Value: "UTC"}}}}},
		{Key: "assetType", Value: 1},
	}
	group := bson.D{
		{Key: "_id", Value: nil}, {Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
		{Key: "incoming", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{"$incoming", 1, 0}}}}}},
		{Key: "outgoing", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{"$outgoing", 1, 0}}}}}},
		{Key: "counterparties", Value: bson.D{{Key: "$addToSet", Value: "$counterparty"}}},
		{Key: "senders", Value: bson.D{{Key: "$addToSet", Value: "$sender"}}},
		{Key: "recipients", Value: bson.D{{Key: "$addToSet", Value: "$recipient"}}},
		{Key: "days", Value: bson.D{{Key: "$addToSet", Value: "$day"}}},
		{Key: "eth", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$assetType", "eth"}}}, 1, 0}}}}}},
		{Key: "erc20", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$assetType", "erc20"}}}, 1, 0}}}}}},
	}
	windowPipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: windowMatch}},
		bson.D{{Key: "$project", Value: project}},
		bson.D{{Key: "$group", Value: group}},
	}
	var windowRows []struct {
		Total          int64 `bson:"total"`
		Incoming       int64 `bson:"incoming"`
		Outgoing       int64 `bson:"outgoing"`
		Counterparties []any `bson:"counterparties"`
		Senders        []any `bson:"senders"`
		Recipients     []any `bson:"recipients"`
		Days           []any `bson:"days"`
		ETH            int64 `bson:"eth"`
		ERC20          int64 `bson:"erc20"`
	}
	if err := s.aggregateAll(ctx, windowPipeline, &windowRows); err != nil {
		return AddressActivity{}, err
	}
	features := ProfileFeatures{LifetimeTransfers: lifetime.Total, LifetimeIncoming: lifetime.Incoming, LifetimeOutgoing: lifetime.Outgoing}
	if len(windowRows) > 0 {
		window := windowRows[0]
		features.WindowTransfers, features.Incoming, features.Outgoing = window.Total, window.Incoming, window.Outgoing
		features.UniqueCounterparts = nonEmptyCount(window.Counterparties)
		features.UniqueSenders, features.UniqueRecipients = nonEmptyCount(window.Senders), nonEmptyCount(window.Recipients)
		features.ActiveDays, features.ETHTransfers, features.ERC20Transfers = nonEmptyCount(window.Days), window.ETH, window.ERC20
	}
	return AddressActivity{LatestTransferAt: lifetime.Latest, Features: features}, nil
}

func profileMatch(chain, address string) bson.D {
	connected := bson.D{{Key: "$or", Value: bson.A{bson.D{{Key: "from", Value: address}}, bson.D{{Key: "to", Value: address}}}}}
	positive := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "amount", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}}},
		bson.D{{Key: "tokenValue", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}}},
	}}}
	return bson.D{{Key: "chain", Value: chain}, {Key: "$and", Value: bson.A{connected, positive}}}
}

func (s *Store) aggregateAll(ctx context.Context, pipeline mongo.Pipeline, results any) error {
	cursor, err := s.db.Collection(TransfersCollection).Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer func() { _ = cursor.Close(ctx) }()
	return cursor.All(ctx, results)
}

func nonEmptyCount(values []any) int {
	count := 0
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			count++
		}
	}
	return count
}

func (s *Store) QueryTransfers(ctx context.Context, query TransferQuery) ([]Transfer, error) {
	conditions := bson.A{positiveTransferCondition()}
	switch query.Direction {
	case "in":
		conditions = append(conditions, bson.D{{Key: "to", Value: bson.D{{Key: "$in", Value: query.Addresses}}}})
	case "out":
		conditions = append(conditions, bson.D{{Key: "from", Value: bson.D{{Key: "$in", Value: query.Addresses}}}})
	default:
		conditions = append(conditions, bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "from", Value: bson.D{{Key: "$in", Value: query.Addresses}}}},
			bson.D{{Key: "to", Value: bson.D{{Key: "$in", Value: query.Addresses}}}},
		}}})
	}
	switch query.AssetMode {
	case "eth":
		conditions = append(conditions, bson.D{{Key: "assetType", Value: "eth"}})
	case "erc20":
		conditions = append(conditions, bson.D{{Key: "assetType", Value: "erc20"}})
	case "contract":
		conditions = append(conditions, bson.D{{Key: "asset", Value: query.Asset}})
	}
	if query.FromBlock > 0 || query.ToBlock > 0 {
		rangeFilter := bson.D{}
		if query.FromBlock > 0 {
			rangeFilter = append(rangeFilter, bson.E{Key: "$gte", Value: query.FromBlock})
		}
		if query.ToBlock > 0 {
			rangeFilter = append(rangeFilter, bson.E{Key: "$lte", Value: query.ToBlock})
		}
		conditions = append(conditions, bson.D{{Key: "blockNumber", Value: rangeFilter}})
	}
	if query.After != nil {
		conditions = append(conditions, transferCursorCondition(*query.After))
	}
	filter := bson.D{{Key: "chain", Value: query.Chain}, {Key: "$and", Value: conditions}}
	sort := bson.D{{Key: "blockNumber", Value: -1}, {Key: "txHash", Value: -1}, {Key: "source", Value: -1}, {Key: "traceId", Value: -1}, {Key: "logIndex", Value: -1}, {Key: "asset", Value: -1}}
	cursor, err := s.db.Collection(TransfersCollection).Find(ctx, filter, options.Find().SetSort(sort).SetLimit(query.Limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var results []Transfer
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func positiveTransferCondition() bson.D {
	return bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "amount", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}}},
		bson.D{{Key: "tokenValue", Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}}},
	}}}
}

func transferCursorCondition(cursor TransferCursor) bson.D {
	fields := []struct {
		name  string
		value any
	}{
		{name: "blockNumber", value: cursor.BlockNumber},
		{name: "txHash", value: cursor.TxHash},
		{name: "source", value: cursor.Source},
		{name: "traceId", value: cursor.TraceID},
		{name: "logIndex", value: cursor.LogIndex},
		{name: "asset", value: cursor.Asset},
	}
	clauses := make(bson.A, 0, len(fields))
	for index, field := range fields {
		clause := bson.D{}
		for prefix := 0; prefix < index; prefix++ {
			clause = append(clause, bson.E{Key: fields[prefix].name, Value: fields[prefix].value})
		}
		clause = append(clause, bson.E{Key: field.name, Value: bson.D{{Key: "$lt", Value: field.value}}})
		clauses = append(clauses, clause)
	}
	return bson.D{{Key: "$or", Value: clauses}}
}

func (s *Store) ensureCollection(ctx context.Context, name string) error {
	err := s.db.CreateCollection(ctx, name)
	if err != nil && !isNamespaceExists(err) {
		return err
	}
	return nil
}

func isNamespaceExists(err error) bool {
	var commandErr mongo.CommandError
	return errors.As(err, &commandErr) && commandErr.Code == 48
}

func (s *Store) UpsertTransfer(ctx context.Context, transfer Transfer) error {
	filter := bson.D{{Key: "chain", Value: transfer.Chain}, {Key: "txHash", Value: transfer.TxHash}, {Key: "source", Value: transfer.Source}, {Key: "traceId", Value: transfer.TraceID}, {Key: "logIndex", Value: transfer.LogIndex}, {Key: "asset", Value: transfer.Asset}}
	_, err := s.db.Collection(TransfersCollection).UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: transfer}}, options.Update().SetUpsert(true))
	return err
}

func (s *Store) UpsertCrossChainLink(ctx context.Context, link CrossChainLink) (CrossChainLink, error) {
	filter := bson.D{{Key: "sourceChain", Value: link.SourceChain}, {Key: "sourceTxHash", Value: link.SourceTxHash}, {Key: "sourceLogIndex", Value: link.SourceLogIndex}, {Key: "targetChain", Value: link.TargetChain}, {Key: "targetTxHash", Value: link.TargetTxHash}, {Key: "targetLogIndex", Value: link.TargetLogIndex}}
	if link.IdentityKey != "" {
		filter = bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "identityKey", Value: link.IdentityKey}},
			bson.D{{Key: "protocol", Value: link.Protocol}, {Key: "sourceChain", Value: link.SourceChain}, {Key: "sourceTxHash", Value: link.SourceTxHash}, {Key: "sourceLogIndex", Value: link.SourceLogIndex}, {Key: "targetChain", Value: link.TargetChain}},
		}}}
	}
	collection := s.db.Collection(CrossChainLinksCollection)
	for attempt := 0; attempt < 5; attempt++ {
		var existing CrossChainLink
		err := collection.FindOne(ctx, filter).Decode(&existing)
		if errors.Is(err, mongo.ErrNoDocuments) {
			link.ID = primitive.NilObjectID
			if _, err = collection.InsertOne(ctx, link); mongo.IsDuplicateKeyError(err) {
				continue
			} else if err != nil {
				return CrossChainLink{}, err
			}
			err = collection.FindOne(ctx, filter).Decode(&link)
			return link, err
		}
		if err != nil {
			return CrossChainLink{}, err
		}
		merged := mergeCrossChainLink(existing, link)
		merged.ID = primitive.NilObjectID
		result, err := collection.UpdateOne(ctx, bson.D{{Key: "_id", Value: existing.ID}, {Key: "status", Value: existing.Status}, {Key: "evidence", Value: existing.Evidence}}, bson.D{{Key: "$set", Value: merged}})
		if err != nil {
			return CrossChainLink{}, err
		}
		if result.ModifiedCount == 0 {
			continue
		}
		err = collection.FindOne(ctx, bson.D{{Key: "_id", Value: existing.ID}}).Decode(&merged)
		return merged, err
	}
	return CrossChainLink{}, errors.New("cross-chain link changed concurrently")
}

func mergeCrossChainLink(existing, incoming CrossChainLink) CrossChainLink {
	result := incoming
	result.ID = existing.ID
	if statusRank(existing.Status) > statusRank(incoming.Status) {
		result.Status = existing.Status
	}
	if result.TargetTxHash == "" {
		result.TargetTxHash = existing.TargetTxHash
	}
	if result.TargetLogIndex == 0 && existing.TargetTxHash != "" {
		result.TargetLogIndex = existing.TargetLogIndex
	}
	if result.TargetBlock == 0 {
		result.TargetBlock = existing.TargetBlock
	}
	if result.MessageHash == "" {
		result.MessageHash = existing.MessageHash
	}
	if result.Nonce == "" {
		result.Nonce = existing.Nonce
	}
	if result.ObservedAt.IsZero() {
		result.ObservedAt = existing.ObservedAt
	}
	if result.SourceAddress == "" {
		result.SourceAddress = existing.SourceAddress
	}
	if result.TargetAddress == "" {
		result.TargetAddress = existing.TargetAddress
	}
	seen := make(map[string]struct{}, len(existing.Evidence)+len(incoming.Evidence))
	result.Evidence = make([]string, 0, len(existing.Evidence)+len(incoming.Evidence))
	for _, values := range [][]string{existing.Evidence, incoming.Evidence} {
		for _, value := range values {
			if _, ok := seen[value]; !ok && value != "" {
				seen[value] = struct{}{}
				result.Evidence = append(result.Evidence, value)
			}
		}
	}
	return result
}

func statusRank(status string) int {
	return map[string]int{"ambiguous": 1, "initiated": 1, "proven": 2, "finalized": 3, "confirmed": 4, "completed": 4, "failed": 5}[status]
}

func (s *Store) FindCrossChainLink(ctx context.Context, id primitive.ObjectID) (CrossChainLink, error) {
	var link CrossChainLink
	err := s.db.Collection(CrossChainLinksCollection).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&link)
	return link, err
}

// QueryCrossChainLinks returns filtered links without implying graph traversability.
func (s *Store) QueryCrossChainLinks(ctx context.Context, query BridgeLinkQuery) ([]CrossChainLink, error) {
	filter := bson.D{}
	if query.Chain != "" && query.Address != "" {
		filter = append(filter, bson.E{Key: "$or", Value: bson.A{
			bson.D{{Key: "sourceChain", Value: query.Chain}, {Key: "sourceAddress", Value: query.Address}},
			bson.D{{Key: "targetChain", Value: query.Chain}, {Key: "targetAddress", Value: query.Address}},
		}})
	}
	if query.Status != "" {
		filter = append(filter, bson.E{Key: "status", Value: query.Status})
	}
	if query.Protocol != "" {
		filter = append(filter, bson.E{Key: "protocol", Value: query.Protocol})
	}
	if query.Direction != "" {
		filter = append(filter, bson.E{Key: "direction", Value: query.Direction})
	}
	if !query.DueBefore.IsZero() {
		filter = append(filter, bson.E{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"initiated", "proven", "finalized"}}}}, bson.E{Key: "nextCheckAt", Value: bson.D{{Key: "$lte", Value: query.DueBefore}}})
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	cursor, err := s.db.Collection(CrossChainLinksCollection).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "observedAt", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []CrossChainLink
	return result, cursor.All(ctx, &result)
}

func (s *Store) ListCrossChainLinks(ctx context.Context, chain, address string, limit int64) ([]CrossChainLink, error) {
	filter := bson.D{{Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"completed", "confirmed"}}}}, {Key: "$or", Value: bson.A{
		bson.D{{Key: "sourceChain", Value: chain}, {Key: "sourceAddress", Value: address}},
		bson.D{{Key: "targetChain", Value: chain}, {Key: "targetAddress", Value: address}},
	}}}
	cursor, err := s.db.Collection(CrossChainLinksCollection).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "observedAt", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var result []CrossChainLink
	return result, cursor.All(ctx, &result)
}

func (s *Store) HasTransferEvidence(ctx context.Context, chain, txHash string, logIndex int64, address, asset, amount string) (bool, error) {
	amountField := "tokenValue"
	if asset == "ETH" {
		amountField = "amount"
	}
	filter := bson.D{{Key: "chain", Value: chain}, {Key: "txHash", Value: txHash}, {Key: "logIndex", Value: logIndex}, {Key: "asset", Value: asset}, {Key: amountField, Value: amount}, {Key: "$or", Value: bson.A{bson.D{{Key: "from", Value: address}}, bson.D{{Key: "to", Value: address}}}}}
	count, err := s.db.Collection(TransfersCollection).CountDocuments(ctx, filter, options.Count().SetLimit(1))
	return count > 0, err
}

func (s *Store) HasTargetTransferEvidence(ctx context.Context, chain, txHash, address, asset, amount string) (bool, error) {
	amountField := "tokenValue"
	if asset == "ETH" {
		amountField = "amount"
	}
	filter := bson.D{{Key: "chain", Value: chain}, {Key: "txHash", Value: txHash}, {Key: "to", Value: address}, {Key: "asset", Value: asset}, {Key: amountField, Value: amount}}
	count, err := s.db.Collection(TransfersCollection).CountDocuments(ctx, filter, options.Count().SetLimit(1))
	return count > 0, err
}

func (s *Store) HasSourceTransferEvidence(ctx context.Context, chain, txHash, address, asset, amount string) (bool, error) {
	filter := bson.D{{Key: "chain", Value: chain}, {Key: "txHash", Value: txHash}, {Key: "from", Value: address}, {Key: "asset", Value: asset}, {Key: "tokenValue", Value: amount}}
	count, err := s.db.Collection(TransfersCollection).CountDocuments(ctx, filter, options.Count().SetLimit(1))
	return count > 0, err
}

// FindTransactionAnalysis returns a cached confirmed transaction analysis.
func (s *Store) FindTransactionAnalysis(ctx context.Context, chain, txHash string) (TransactionAnalysis, bool, error) {
	var result TransactionAnalysis
	err := s.db.Collection(TransactionAnalysesCollection).FindOne(ctx, bson.D{{Key: "chain", Value: chain}, {Key: "txHash", Value: txHash}}).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return TransactionAnalysis{}, false, nil
	}
	return result, err == nil, err
}

// SaveTransactionAnalysis stores an analysis idempotently.
func (s *Store) SaveTransactionAnalysis(ctx context.Context, analysis TransactionAnalysis) error {
	filter := bson.D{{Key: "chain", Value: analysis.Chain}, {Key: "txHash", Value: analysis.TxHash}}
	_, err := s.db.Collection(TransactionAnalysesCollection).ReplaceOne(ctx, filter, analysis, options.Replace().SetUpsert(true))
	return err
}

// FindPoolMetadata returns cached metadata for a verified or rejected pool.
func (s *Store) FindPoolMetadata(ctx context.Context, chain, pool string) (PoolMetadata, bool, error) {
	var result PoolMetadata
	err := s.db.Collection(PoolMetadataCollection).FindOne(ctx, bson.D{{Key: "chain", Value: chain}, {Key: "pool", Value: pool}}).Decode(&result)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return PoolMetadata{}, false, nil
	}
	return result, err == nil, err
}

// SavePoolMetadata stores pool metadata idempotently.
func (s *Store) SavePoolMetadata(ctx context.Context, metadata PoolMetadata) error {
	filter := bson.D{{Key: "chain", Value: metadata.Chain}, {Key: "pool", Value: metadata.Pool}}
	_, err := s.db.Collection(PoolMetadataCollection).ReplaceOne(ctx, filter, metadata, options.Replace().SetUpsert(true))
	return err
}
