package store

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
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
	PropagationJobsCollection     = "propagation_jobs"
	RiskAssociationsCollection    = "inferred_risk_associations"
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
	for _, name := range []string{AddressesCollection, TransfersCollection, LabelsCollection, SyncJobsCollection, ProfilesCollection, TraceJobsCollection, PropagationJobsCollection, RiskAssociationsCollection, CrossChainLinksCollection, TransactionAnalysesCollection, PoolMetadataCollection} {
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
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "to", Value: 1}, {Key: "assetType", Value: 1}, {Key: "from", Value: 1}}, Options: options.Index().SetName("idx_transfers_to_type_from")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "from", Value: 1}, {Key: "assetType", Value: 1}, {Key: "to", Value: 1}}, Options: options.Index().SetName("idx_transfers_from_type_to")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "to", Value: 1}, {Key: "asset", Value: 1}, {Key: "from", Value: 1}}, Options: options.Index().SetName("idx_transfers_to_asset_from")},
			{Keys: bson.D{{Key: "chain", Value: 1}, {Key: "from", Value: 1}, {Key: "asset", Value: 1}, {Key: "to", Value: 1}}, Options: options.Index().SetName("idx_transfers_from_asset_to")},
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
		PropagationJobsCollection: {
			{Keys: bson.D{{Key: "idempotencyKey", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_propagation_jobs_idempotency")},
			{Keys: bson.D{{Key: "propagationVersion", Value: 1}, {Key: "status", Value: 1}, {Key: "leaseUntil", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("idx_propagation_jobs_claim")},
		},
		RiskAssociationsCollection: {
			{Keys: bson.D{{Key: "sourceLabelId", Value: 1}, {Key: "targetChain", Value: 1}, {Key: "targetAddress", Value: 1}, {Key: "direction", Value: 1}, {Key: "asset", Value: 1}, {Key: "propagationVersion", Value: 1}, {Key: "dataThroughBlock", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_risk_associations_version")},
			{Keys: bson.D{{Key: "targetChain", Value: 1}, {Key: "targetAddress", Value: 1}, {Key: "stale", Value: 1}, {Key: "score", Value: -1}}, Options: options.Index().SetName("idx_risk_associations_target")},
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

func (s *Store) CompleteAddressSync(ctx context.Context, chain, address string, earliest, latest, internalFrom, internalTo int64, syncedAt time.Time) error {
	_, err := s.db.Collection(AddressesCollection).UpdateOne(ctx,
		bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "earliestSyncedBlock", Value: earliest},
			{Key: "historySyncedToBlock", Value: latest},
			{Key: "latestSyncedBlock", Value: latest},
			{Key: "internalSyncedFrom", Value: internalFrom},
			{Key: "internalSyncedTo", Value: internalTo},
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

func (s *Store) FindLatestSyncJob(ctx context.Context, chain, address string) (SyncJob, error) {
	var job SyncJob
	err := s.db.Collection(SyncJobsCollection).FindOne(
		ctx,
		bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}},
		options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}}),
	).Decode(&job)
	return job, err
}

func (s *Store) FindSyncCheckpoints(ctx context.Context, chain, address string, startBlock, internalLookbackBlocks int64) (map[string]int64, error) {
	var job SyncJob
	err := s.db.Collection(SyncJobsCollection).FindOne(ctx, bson.D{{Key: "chain", Value: chain}, {Key: "address", Value: address}, {Key: "startBlock", Value: startBlock}}, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})).Decode(&job)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return map[string]int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	if job.Status != "failed" {
		return map[string]int64{}, nil
	}
	if len(job.Progress.ActionCheckpoints) > 0 {
		result := make(map[string]int64, len(job.Progress.ActionCheckpoints))
		for action, checkpoint := range job.Progress.ActionCheckpoints {
			if action != "txlistinternal" || job.InternalLookbackBlocks == internalLookbackBlocks {
				result[action] = checkpoint
			}
		}
		return result, nil
	}
	return legacySyncCheckpoints(job), nil
}

func legacySyncCheckpoints(job SyncJob) map[string]int64 {
	result := map[string]int64{}
	actions := []string{"txlist", "txlistinternal", "tokentx"}
	currentIndex := -1
	for index, action := range actions {
		if action == job.Progress.CurrentAction {
			currentIndex = index
			break
		}
	}
	if currentIndex < 0 {
		return result
	}
	for _, action := range actions {
		if action == job.Progress.CurrentAction {
			if job.Progress.RangeStart > job.StartBlock {
				result[action] = job.Progress.RangeStart - 1
			}
			break
		}
		if job.Progress.RangeEnd >= job.StartBlock {
			result[action] = job.Progress.RangeEnd
		}
	}
	return result
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

// ListRiskLabels returns bounded deterministic high-risk labels for forced candidates.
func (s *Store) ListRiskLabels(ctx context.Context, chain string, limit int64) ([]Label, error) {
	filter := bson.D{{Key: "chain", Value: chain}, {Key: "source", Value: bson.D{{Key: "$in", Value: bson.A{"manual", "public-list"}}}}, {Key: "riskLevel", Value: bson.D{{Key: "$in", Value: bson.A{"medium", "high"}}}}}
	cursor, err := s.db.Collection(LabelsCollection).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "observedAt", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var labels []Label
	return labels, cursor.All(ctx, &labels)
}

// CreatePropagationJob inserts a new idempotent propagation request.
func (s *Store) CreatePropagationJob(ctx context.Context, job *PropagationJob) error {
	result, err := s.db.Collection(PropagationJobsCollection).InsertOne(ctx, job)
	if err != nil {
		return err
	}
	job.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

// FindPropagationJobByKey returns an existing idempotent request.
func (s *Store) FindPropagationJobByKey(ctx context.Context, key string) (PropagationJob, error) {
	var job PropagationJob
	err := s.db.Collection(PropagationJobsCollection).FindOne(ctx, bson.D{{Key: "idempotencyKey", Value: key}}).Decode(&job)
	job.Result = normalizeBSON(job.Result)
	return job, err
}

// GetPropagationJob returns one propagation job.
func (s *Store) GetPropagationJob(ctx context.Context, id primitive.ObjectID) (PropagationJob, error) {
	var job PropagationJob
	err := s.db.Collection(PropagationJobsCollection).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&job)
	job.Result = normalizeBSON(job.Result)
	return job, err
}

// ClaimPropagationJob atomically leases the oldest runnable job.
func (s *Store) ClaimPropagationJob(ctx context.Context, now, leaseUntil time.Time, maxRetries int) (PropagationJob, error) {
	filter := bson.D{{Key: "propagationVersion", Value: "propagation-v3"}, {Key: "retryCount", Value: bson.D{{Key: "$lt", Value: maxRetries}}}, {Key: "$or", Value: bson.A{
		bson.D{{Key: "status", Value: "queued"}},
		bson.D{{Key: "status", Value: "running"}, {Key: "leaseUntil", Value: bson.D{{Key: "$lte", Value: now}}}},
		bson.D{{Key: "status", Value: "failed"}, {Key: "retryable", Value: true}},
	}}}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: "running"}, {Key: "leaseUntil", Value: leaseUntil}, {Key: "startedAt", Value: now}}}, {Key: "$inc", Value: bson.D{{Key: "retryCount", Value: 1}}}}
	var job PropagationJob
	err := s.db.Collection(PropagationJobsCollection).FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetSort(bson.D{{Key: "createdAt", Value: 1}}).SetReturnDocument(options.After)).Decode(&job)
	return job, err
}

// SupersedeLegacyPropagationJobs prevents workers from executing obsolete rules.
func (s *Store) SupersedeLegacyPropagationJobs(ctx context.Context, now time.Time, version string) error {
	filter := bson.D{{Key: "propagationVersion", Value: bson.D{{Key: "$ne", Value: version}}}, {Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"queued", "running", "failed"}}}}}
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: "failed"}, {Key: "errorCode", Value: "superseded_rule_version"}, {Key: "error", Value: "superseded propagation rule version"}, {Key: "retryable", Value: false}, {Key: "finishedAt", Value: now}, {Key: "leaseUntil", Value: time.Time{}}}}}
	_, err := s.db.Collection(PropagationJobsCollection).UpdateMany(ctx, filter, update)
	return err
}

// ExtendPropagationLease prevents an active job from being reclaimed.
func (s *Store) ExtendPropagationLease(ctx context.Context, id primitive.ObjectID, leaseUntil time.Time) error {
	result, err := s.db.Collection(PropagationJobsCollection).UpdateOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "status", Value: "running"}}, bson.D{{Key: "$set", Value: bson.D{{Key: "leaseUntil", Value: leaseUntil}}}})
	if err == nil && result.MatchedCount == 0 {
		return context.Canceled
	}
	return err
}

// UpdatePropagationProgress persists bounded task progress without replacing its lease.
func (s *Store) UpdatePropagationProgress(ctx context.Context, id primitive.ObjectID, hop, nodes, edges int) error {
	result, err := s.db.Collection(PropagationJobsCollection).UpdateOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "status", Value: "running"}}, bson.D{{Key: "$set", Value: bson.D{{Key: "currentHop", Value: hop}, {Key: "visitedNodes", Value: nodes}, {Key: "edgeCount", Value: edges}}}})
	if err == nil && result.MatchedCount == 0 {
		return context.Canceled
	}
	return err
}

// SavePropagationJob replaces a job after checking it was not stopped.
func (s *Store) SavePropagationJob(ctx context.Context, job PropagationJob) error {
	result, err := s.db.Collection(PropagationJobsCollection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: job.ID}, {Key: "status", Value: bson.D{{Key: "$ne", Value: "stopped"}}}}, job)
	if err == nil && result.MatchedCount == 0 {
		return context.Canceled
	}
	return err
}

// StopPropagationJob marks a runnable job as stopped.
func (s *Store) StopPropagationJob(ctx context.Context, id primitive.ObjectID, now time.Time) (PropagationJob, error) {
	update := bson.D{{Key: "$set", Value: bson.D{{Key: "status", Value: "stopped"}, {Key: "errorCode", Value: "stopped_by_user"}, {Key: "error", Value: "stopped by user"}, {Key: "retryable", Value: false}, {Key: "finishedAt", Value: now}}}}
	var job PropagationJob
	filter := bson.D{{Key: "_id", Value: id}, {Key: "status", Value: bson.D{{Key: "$in", Value: bson.A{"queued", "running", "failed"}}}}}
	err := s.db.Collection(PropagationJobsCollection).FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&job)
	return job, err
}

// UpsertRiskAssociation saves a versioned inferred result without touching labels.
func (s *Store) UpsertRiskAssociation(ctx context.Context, association InferredRiskAssociation) error {
	filter := bson.D{{Key: "sourceLabelId", Value: association.SourceLabelID}, {Key: "targetChain", Value: association.TargetChain}, {Key: "targetAddress", Value: association.TargetAddress}, {Key: "direction", Value: association.Direction}, {Key: "asset", Value: association.Asset}, {Key: "propagationVersion", Value: association.PropagationVersion}, {Key: "dataThroughBlock", Value: association.DataThroughBlock}}
	association.ID = primitive.NilObjectID
	_, err := s.db.Collection(RiskAssociationsCollection).ReplaceOne(ctx, filter, association, options.Replace().SetUpsert(true))
	return err
}

// MarkRiskAssociationsStale expires results outside the newly completed version.
func (s *Store) MarkRiskAssociationsStale(ctx context.Context, targetChain, targetAddress, version string, block int64) error {
	filter := bson.D{{Key: "targetChain", Value: targetChain}, {Key: "targetAddress", Value: targetAddress}, {Key: "stale", Value: false}, {Key: "$or", Value: bson.A{
		bson.D{{Key: "propagationVersion", Value: bson.D{{Key: "$ne", Value: version}}}},
		bson.D{{Key: "dataThroughBlock", Value: bson.D{{Key: "$ne", Value: block}}}},
	}}}
	_, err := s.db.Collection(RiskAssociationsCollection).UpdateMany(ctx, filter, bson.D{{Key: "$set", Value: bson.D{{Key: "stale", Value: true}}}})
	return err
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
	job.Result = normalizeBSON(job.Result)
	return job, err
}

func (s *Store) FindLatestTraceJob(ctx context.Context, chain, seedAddress, direction string, depth, topN int, asset, ruleVersion string) (TraceJob, error) {
	var job TraceJob
	filter := bson.D{
		{Key: "chain", Value: chain},
		{Key: "seedAddress", Value: seedAddress},
		{Key: "direction", Value: direction},
		{Key: "depth", Value: depth},
		{Key: "topN", Value: topN},
		{Key: "asset", Value: asset},
		{Key: "ruleVersion", Value: ruleVersion},
	}
	err := s.db.Collection(TraceJobsCollection).FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})).Decode(&job)
	job.Result = normalizeBSON(job.Result)
	return job, err
}

func normalizeBSON(value any) any {
	switch value := value.(type) {
	case primitive.D:
		result := make(map[string]any, len(value))
		for _, entry := range value {
			result[traceResultJSONKey(entry.Key)] = normalizeBSON(entry.Value)
		}
		return result
	case primitive.A:
		result := make([]any, len(value))
		for index, entry := range value {
			result[index] = normalizeBSON(entry)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for index, entry := range value {
			result[index] = normalizeBSON(entry)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, entry := range value {
			result[traceResultJSONKey(key)] = normalizeBSON(entry)
		}
		return result
	default:
		return value
	}
}

func traceResultJSONKey(key string) string {
	legacy := map[string]string{
		"bridgeedges": "bridgeEdges", "crosschainpaths": "crossChainPaths",
		"datathroughblock": "dataThroughBlock", "datathroughblocks": "dataThroughBlocks", "datastatus": "dataStatus",
		"ruleversion": "ruleVersion", "inferredlabels": "inferredLabels", "propagationversion": "propagationVersion",
		"labeltype": "labelType", "basescore": "baseScore", "txhashes": "txHashes",
	}
	if normalized, ok := legacy[key]; ok {
		return normalized
	}
	return key
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

func (s *Store) TopCounterparties(ctx context.Context, query CounterpartyQuery) ([]CounterpartySummary, error) {
	if query.TopN < 1 || query.Address == "" || (query.Direction != "in" && query.Direction != "out") || !validCounterpartyAssetMode(query.AssetMode) {
		return nil, errors.New("invalid counterparty query")
	}
	filter, counterparty := counterpartyFilter(query)
	projection := bson.D{{Key: "chain", Value: 1}, {Key: "chainId", Value: 1}, {Key: "txHash", Value: 1}, {Key: "blockNumber", Value: 1}, {Key: "from", Value: 1}, {Key: "to", Value: 1}, {Key: "assetType", Value: 1}, {Key: "asset", Value: 1}, {Key: "symbol", Value: 1}, {Key: "decimals", Value: 1}, {Key: "tokenMetadataComplete", Value: 1}, {Key: "amount", Value: 1}, {Key: "tokenValue", Value: 1}, {Key: "transferKind", Value: 1}, {Key: "source", Value: 1}, {Key: "traceId", Value: 1}, {Key: "logIndex", Value: 1}}
	cursor, err := s.db.Collection(TransfersCollection).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: counterparty, Value: 1}}).SetProjection(projection).SetHint(counterpartyIndexName(query)).SetBatchSize(1000))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	selected := &counterpartyHeap{}
	heap.Init(selected)
	var current CounterpartySummary
	hasCurrent := false
	currentTotal := new(big.Int)
	flush := func() {
		if !hasCurrent {
			return
		}
		current.TotalAmount = currentTotal.String()
		if selected.Len() < query.TopN {
			heap.Push(selected, current)
			return
		}
		if summaryBetter(current, (*selected)[0]) {
			(*selected)[0] = current
			heap.Fix(selected, 0)
		}
	}
	for cursor.Next(ctx) {
		var transfer Transfer
		if err := cursor.Decode(&transfer); err != nil {
			return nil, err
		}
		other := transfer.To
		if query.Direction == "in" {
			other = transfer.From
		}
		amount, ok := new(big.Int).SetString(transferAmountString(transfer), 10)
		if !ok || amount.Sign() < 0 {
			return nil, fmt.Errorf("invalid transfer amount for %s", transfer.TxHash)
		}
		if !hasCurrent || !strings.EqualFold(summaryCounterparty(current, query.Direction), other) {
			flush()
			current = CounterpartySummary{Chain: transfer.Chain, ChainID: transfer.ChainID, From: transfer.From, To: transfer.To, AssetType: transfer.AssetType, Asset: transfer.Asset, Symbol: transfer.Symbol, Decimals: transfer.Decimals, TokenMetadataComplete: transfer.TokenMetadataComplete, TotalAmount: "0", Representative: transfer}
			hasCurrent = true
			currentTotal.SetInt64(0)
		}
		currentTotal.Add(currentTotal, amount)
		current.TransferCount++
		if transferBetter(transfer, current.Representative) {
			current.Representative = transfer
		}
		if transfer.TokenMetadataComplete {
			current.Symbol, current.Decimals, current.TokenMetadataComplete = transfer.Symbol, transfer.Decimals, true
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	flush()
	result := make([]CounterpartySummary, selected.Len())
	copy(result, *selected)
	sort.Slice(result, func(i, j int) bool { return summaryBetter(result[i], result[j]) })
	return result, nil
}

// PropagationCandidates scans one indexed relationship set and retains bounded
// amount, frequency, recency, and forced-address channels.
func (s *Store) PropagationCandidates(ctx context.Context, query CandidateQuery) (CandidateResult, error) {
	if query.PerChannelLimit < 1 || query.Limit < 1 || query.Address == "" || (query.Direction != "in" && query.Direction != "out") || !validCounterpartyAssetMode(query.AssetMode) {
		return CandidateResult{}, errors.New("invalid propagation candidate query")
	}
	base := CounterpartyQuery{Chain: query.Chain, Address: query.Address, Direction: query.Direction, AssetMode: query.AssetMode, Asset: query.Asset}
	filter, counterparty := counterpartyFilter(base)
	if query.ToBlock > 0 {
		filter = append(filter, bson.E{Key: "blockNumber", Value: bson.D{{Key: "$lte", Value: query.ToBlock}}})
	}
	projection := bson.D{{Key: "chain", Value: 1}, {Key: "chainId", Value: 1}, {Key: "txHash", Value: 1}, {Key: "blockNumber", Value: 1}, {Key: "blockTime", Value: 1}, {Key: "from", Value: 1}, {Key: "to", Value: 1}, {Key: "assetType", Value: 1}, {Key: "asset", Value: 1}, {Key: "symbol", Value: 1}, {Key: "decimals", Value: 1}, {Key: "amount", Value: 1}, {Key: "tokenValue", Value: 1}, {Key: "transferKind", Value: 1}, {Key: "source", Value: 1}, {Key: "traceId", Value: 1}, {Key: "logIndex", Value: 1}}
	cursor, err := s.db.Collection(TransfersCollection).Find(ctx, filter, options.Find().SetSort(bson.D{{Key: counterparty, Value: 1}}).SetProjection(projection).SetHint(counterpartyIndexName(base)).SetBatchSize(1000))
	if err != nil {
		return CandidateResult{}, fmt.Errorf("find propagation candidates: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	forced := make(map[string]struct{}, len(query.ForcedCounterparties))
	for _, address := range query.ForcedCounterparties {
		forced[strings.ToLower(address)] = struct{}{}
	}
	byAmount := &rankedSummaryHeap{better: summaryBetter}
	byCount := &rankedSummaryHeap{better: func(left, right CounterpartySummary) bool {
		if left.TransferCount != right.TransferCount {
			return left.TransferCount > right.TransferCount
		}
		return summaryBetter(left, right)
	}}
	byRecent := &rankedSummaryHeap{better: func(left, right CounterpartySummary) bool {
		if left.LatestBlock != right.LatestBlock {
			return left.LatestBlock > right.LatestBlock
		}
		return summaryBetter(left, right)
	}}
	heap.Init(byAmount)
	heap.Init(byCount)
	heap.Init(byRecent)
	forcedItems := make(map[string]CounterpartySummary)
	totalAmount := new(big.Int)
	totalCounterparties := 0
	var current CounterpartySummary
	currentTotal := new(big.Int)
	hasCurrent := false
	flush := func() {
		if !hasCurrent {
			return
		}
		other := strings.ToLower(summaryCounterparty(current, query.Direction))
		if other == strings.ToLower(query.Address) {
			return
		}
		current.TotalAmount = currentTotal.String()
		totalCounterparties++
		totalAmount.Add(totalAmount, currentTotal)
		keepRankedSummary(byAmount, current, query.PerChannelLimit)
		keepRankedSummary(byCount, current, query.PerChannelLimit)
		keepRankedSummary(byRecent, current, query.PerChannelLimit)
		if _, ok := forced[other]; ok {
			forcedItems[other] = current
		}
	}
	for cursor.Next(ctx) {
		var transfer Transfer
		if err := cursor.Decode(&transfer); err != nil {
			return CandidateResult{}, fmt.Errorf("decode propagation candidate: %w", err)
		}
		amount, ok := new(big.Int).SetString(transferAmountString(transfer), 10)
		if !ok || amount.Sign() < 0 {
			return CandidateResult{}, fmt.Errorf("invalid transfer amount for %s", transfer.TxHash)
		}
		other := transfer.To
		if query.Direction == "in" {
			other = transfer.From
		}
		if !hasCurrent || !strings.EqualFold(summaryCounterparty(current, query.Direction), other) {
			flush()
			current = CounterpartySummary{Chain: transfer.Chain, ChainID: transfer.ChainID, From: transfer.From, To: transfer.To, AssetType: transfer.AssetType, Asset: transfer.Asset, Symbol: transfer.Symbol, Decimals: transfer.Decimals, LatestTime: transfer.BlockTime, LatestTransfer: transfer, Representative: transfer}
			currentTotal.SetInt64(0)
			hasCurrent = true
		}
		currentTotal.Add(currentTotal, amount)
		current.TransferCount++
		if transfer.BlockNumber > current.LatestBlock {
			current.LatestBlock = transfer.BlockNumber
			current.LatestTime, current.LatestTransfer = transfer.BlockTime, transfer
		}
		if transferBetter(transfer, current.Representative) {
			current.Representative = transfer
		}
	}
	if err := cursor.Err(); err != nil {
		return CandidateResult{}, fmt.Errorf("scan propagation candidates: %w", err)
	}
	flush()
	items := mergeCandidateChannels(query.Direction, query.Limit, forcedItems, rankedSummaries(byAmount), rankedSummaries(byCount), rankedSummaries(byRecent))
	selectedAmount := new(big.Int)
	for _, item := range items {
		value, _ := new(big.Int).SetString(item.TotalAmount, 10)
		selectedAmount.Add(selectedAmount, value)
	}
	coverage := CandidateCoverage{SelectedCounterparties: len(items), TotalCounterparties: totalCounterparties, SelectedAmount: selectedAmount.String(), TotalAmount: totalAmount.String(), AmountCoverage: "1.0000"}
	if totalAmount.Sign() > 0 {
		coverage.AmountCoverage = new(big.Rat).SetFrac(selectedAmount, totalAmount).FloatString(4)
	}
	if len(items) < totalCounterparties {
		coverage.Truncated, coverage.TruncationReason = true, "per_node_candidate_cap"
	}
	return CandidateResult{Items: items, Coverage: coverage}, nil
}

type rankedSummaryHeap struct {
	items  []CounterpartySummary
	better func(CounterpartySummary, CounterpartySummary) bool
}

func (h rankedSummaryHeap) Len() int           { return len(h.items) }
func (h rankedSummaryHeap) Less(i, j int) bool { return h.better(h.items[j], h.items[i]) }
func (h rankedSummaryHeap) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *rankedSummaryHeap) Push(value any)    { h.items = append(h.items, value.(CounterpartySummary)) }
func (h *rankedSummaryHeap) Pop() any {
	last := h.items[len(h.items)-1]
	h.items = h.items[:len(h.items)-1]
	return last
}

func keepRankedSummary(selected *rankedSummaryHeap, candidate CounterpartySummary, limit int) {
	if selected.Len() < limit {
		heap.Push(selected, candidate)
		return
	}
	if selected.better(candidate, selected.items[0]) {
		selected.items[0] = candidate
		heap.Fix(selected, 0)
	}
}

func rankedSummaries(selected *rankedSummaryHeap) []CounterpartySummary {
	result := append([]CounterpartySummary(nil), selected.items...)
	sort.Slice(result, func(i, j int) bool { return selected.better(result[i], result[j]) })
	return result
}

func mergeCandidateChannels(direction string, limit int, forced map[string]CounterpartySummary, channels ...[]CounterpartySummary) []CounterpartySummary {
	result := make([]CounterpartySummary, 0, limit)
	seen := make(map[string]struct{}, limit)
	forcedKeys := make([]string, 0, len(forced))
	for address := range forced {
		forcedKeys = append(forcedKeys, address)
	}
	sort.Strings(forcedKeys)
	appendItem := func(item CounterpartySummary) {
		key := strings.ToLower(summaryCounterparty(item, direction))
		if len(result) >= limit {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	for _, key := range forcedKeys {
		appendItem(forced[key])
	}
	for _, channel := range channels {
		for _, item := range channel {
			appendItem(item)
		}
	}
	return result
}

func (s *Store) TopRelationshipTransfers(ctx context.Context, query CounterpartyQuery, limit int) ([]Transfer, error) {
	if limit < 1 || query.Counterparty == "" || !validCounterpartyAssetMode(query.AssetMode) {
		return nil, errors.New("invalid relationship query")
	}
	filter, _ := counterpartyFilter(query)
	projection := bson.D{{Key: "chain", Value: 1}, {Key: "chainId", Value: 1}, {Key: "txHash", Value: 1}, {Key: "from", Value: 1}, {Key: "to", Value: 1}, {Key: "assetType", Value: 1}, {Key: "asset", Value: 1}, {Key: "amount", Value: 1}, {Key: "tokenValue", Value: 1}, {Key: "source", Value: 1}, {Key: "traceId", Value: 1}, {Key: "logIndex", Value: 1}}
	cursor, err := s.db.Collection(TransfersCollection).Find(ctx, filter, options.Find().SetProjection(projection).SetHint(counterpartyIndexName(query)).SetBatchSize(1000))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	selected := &transferHeap{}
	heap.Init(selected)
	for cursor.Next(ctx) {
		var transfer Transfer
		if err := cursor.Decode(&transfer); err != nil {
			return nil, err
		}
		amount, ok := new(big.Int).SetString(transferAmountString(transfer), 10)
		if !ok || amount.Sign() < 0 {
			return nil, fmt.Errorf("invalid transfer amount for %s", transfer.TxHash)
		}
		if selected.Len() < limit {
			heap.Push(selected, transfer)
		} else if transferBetter(transfer, (*selected)[0]) {
			(*selected)[0] = transfer
			heap.Fix(selected, 0)
		}
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	result := make([]Transfer, selected.Len())
	copy(result, *selected)
	sort.Slice(result, func(i, j int) bool { return transferBetter(result[i], result[j]) })
	return result, nil
}

func validCounterpartyAssetMode(mode string) bool {
	return mode == "eth" || mode == "erc20" || mode == "contract"
}

func counterpartyIndexName(query CounterpartyQuery) string {
	prefix := "idx_transfers_from_"
	other := "to"
	if query.Direction == "in" {
		prefix = "idx_transfers_to_"
		other = "from"
	}
	field := "type_"
	if query.AssetMode == "contract" {
		field = "asset_"
	}
	return prefix + field + other
}

func counterpartyFilter(query CounterpartyQuery) (bson.D, string) {
	parent, other := "from", "to"
	if query.Direction == "in" {
		parent, other = "to", "from"
	}
	filter := bson.D{{Key: "chain", Value: query.Chain}, {Key: parent, Value: query.Address}}
	if query.Counterparty != "" {
		filter = append(filter, bson.E{Key: other, Value: query.Counterparty})
	}
	switch query.AssetMode {
	case "eth":
		filter = append(filter, bson.E{Key: "assetType", Value: "eth"})
	case "erc20":
		filter = append(filter, bson.E{Key: "assetType", Value: "erc20"})
	case "contract":
		filter = append(filter, bson.E{Key: "asset", Value: query.Asset})
	}
	amountField := "tokenValue"
	if query.AssetMode == "eth" {
		amountField = "amount"
	}
	filter = append(filter, bson.E{Key: amountField, Value: bson.D{{Key: "$exists", Value: true}, {Key: "$nin", Value: bson.A{"", "0"}}}})
	return filter, other
}

type counterpartyHeap []CounterpartySummary

func (h counterpartyHeap) Len() int           { return len(h) }
func (h counterpartyHeap) Less(i, j int) bool { return summaryBetter(h[j], h[i]) }
func (h counterpartyHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *counterpartyHeap) Push(value any)    { *h = append(*h, value.(CounterpartySummary)) }
func (h *counterpartyHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

type transferHeap []Transfer

func (h transferHeap) Len() int           { return len(h) }
func (h transferHeap) Less(i, j int) bool { return transferBetter(h[j], h[i]) }
func (h transferHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *transferHeap) Push(value any)    { *h = append(*h, value.(Transfer)) }
func (h *transferHeap) Pop() any {
	old := *h
	value := old[len(old)-1]
	*h = old[:len(old)-1]
	return value
}

func summaryBetter(left, right CounterpartySummary) bool {
	if comparison := compareUnsignedDecimal(left.TotalAmount, right.TotalAmount); comparison != 0 {
		return comparison > 0
	}
	if left.From != right.From {
		return left.From < right.From
	}
	return left.To < right.To
}
func summaryCounterparty(summary CounterpartySummary, direction string) string {
	if direction == "in" {
		return summary.From
	}
	return summary.To
}
func transferAmountString(transfer Transfer) string {
	if transfer.AssetType == "eth" || strings.EqualFold(transfer.Asset, "ETH") {
		return transfer.Amount
	}
	return transfer.TokenValue
}
func transferBetter(left, right Transfer) bool {
	if comparison := compareUnsignedDecimal(transferAmountString(left), transferAmountString(right)); comparison != 0 {
		return comparison > 0
	}
	if left.TxHash != right.TxHash {
		return left.TxHash < right.TxHash
	}
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.TraceID != right.TraceID {
		return left.TraceID < right.TraceID
	}
	return left.LogIndex < right.LogIndex
}

func compareUnsignedDecimal(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
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
