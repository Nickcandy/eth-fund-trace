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
	AddressesCollection = "addresses"
	TransfersCollection = "transfers"
	LabelsCollection    = "labels"
	SyncJobsCollection  = "sync_jobs"
	ProfilesCollection  = "address_profiles"
)

type Store struct {
	db *mongo.Database
}

func New(db *mongo.Database) *Store {
	return &Store{db: db}
}

func (s *Store) Initialize(ctx context.Context) error {
	for _, name := range []string{AddressesCollection, TransfersCollection, LabelsCollection, SyncJobsCollection, ProfilesCollection} {
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
		},
		TransfersCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "txHash", Value: 1}, {Key: "source", Value: 1}, {Key: "traceId", Value: 1}, {Key: "logIndex", Value: 1}, {Key: "asset", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_transfers_identity")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "from", Value: 1}, {Key: "assetType", Value: 1}, {Key: "blockNumber", Value: 1}}, Options: options.Index().SetName("idx_transfers_from_type_block")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "to", Value: 1}, {Key: "assetType", Value: 1}, {Key: "blockNumber", Value: 1}}, Options: options.Index().SetName("idx_transfers_to_type_block")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "from", Value: 1}, {Key: "asset", Value: 1}}, Options: options.Index().SetName("idx_transfers_from_asset")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "to", Value: 1}, {Key: "asset", Value: 1}}, Options: options.Index().SetName("idx_transfers_to_asset")},
		},
		LabelsCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "type", Value: 1}, {Key: "source", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_labels_identity")},
		},
		SyncJobsCollection: {
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("idx_sync_jobs_status_created")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("idx_sync_jobs_address_created")},
		},
		ProfilesCollection: {
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "ruleVersion", Value: 1}, {Key: "dataThroughBlock", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_profiles_version_block")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "address", Value: 1}, {Key: "computedAt", Value: -1}}, Options: options.Index().SetName("idx_profiles_address_computed")},
		},
	}
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
