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
)

type Store struct {
	db *mongo.Database
}

func New(db *mongo.Database) *Store {
	return &Store{db: db}
}

func (s *Store) Initialize(ctx context.Context) error {
	for _, name := range []string{AddressesCollection, TransfersCollection, LabelsCollection, SyncJobsCollection} {
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
