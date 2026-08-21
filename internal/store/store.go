package store

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
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
	}
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
