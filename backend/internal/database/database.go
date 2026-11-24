package database

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

//go:embed dataset_tasks.json
var datasetTasksJSON []byte

type datasetTask struct {
	Category    string   `json:"category"`
	Scene       string   `json:"scene"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Choices     []string `json:"choices"`
	Steps       []string `json:"steps"`
	Difficulty  string   `json:"difficulty"`
	AnswerMode  string   `json:"answer_mode"`
	SafetyTip   string   `json:"safety_tip"`
}

// Open connects to MongoDB and returns the client.
func Open(uri, dbName string) (*mongo.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping mongo: %w", err)
	}
	return client, nil
}

// DB returns a reference to the configured database.
func DB(client *mongo.Client, name string) *mongo.Database {
	return client.Database(name)
}

// Migrate creates indexes and seeds tasks. Idempotent — safe to call on every start.
func Migrate(client *mongo.Client, dbName string) error {
	db := client.Database(dbName)
	ctx := context.Background()

	// Unique login index on users
	_, _ = db.Collection("users").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "login", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	// Unique user_id on child_profiles
	_, _ = db.Collection("child_profiles").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	// Index on invite codes
	_, _ = db.Collection("invite_codes").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "code_hash", Value: 1}},
	})
	// Index on task attempts (child + task)
	_, _ = db.Collection("task_attempts").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "child_id", Value: 1},
			{Key: "task_id", Value: 1},
		},
	})
	// Index on daily_activity
	_, _ = db.Collection("daily_activity").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "child_id", Value: 1},
			{Key: "activity_date", Value: 1},
		},
		Options: options.Index().SetUnique(true),
	})
	// Seed dataset tasks by title (skip existing)
	var tasks []datasetTask
	if err := json.Unmarshal(datasetTasksJSON, &tasks); err != nil {
		return fmt.Errorf("decode dataset tasks: %w", err)
	}
	for _, t := range tasks {
		filter := bson.M{"title": t.Title}
		choices, _ := json.Marshal(t.Choices)
		steps, _ := json.Marshal(t.Steps)
		update := bson.M{
			"$setOnInsert": bson.M{
				"category":    t.Category,
				"scene":       t.Scene,
				"title":       t.Title,
				"description": t.Description,
				"prompt":      t.Prompt,
				"choices":     choices,
				"difficulty":  t.Difficulty,
				"safety_tip":  t.SafetyTip,
			},
			"$set": bson.M{
				"answer_mode": t.AnswerMode,
				"steps":       steps,
			},
		}
		opts := options.Update().SetUpsert(true)
		_, _ = db.Collection("tasks").UpdateOne(ctx, filter, update, opts)
	}
	return nil
}

//go:embed seed_tasks.json
var seedTasksJSON []byte

// SeedExtraTasks loads additional bundled tasks from seed_tasks.json.
func SeedExtraTasks(client *mongo.Client, dbName string) error {
	var tasks []datasetTask
	if err := json.Unmarshal(seedTasksJSON, &tasks); err != nil {
		return nil // file may not exist, that's fine
	}
	db := client.Database(dbName)
	ctx := context.Background()
	for _, t := range tasks {
		choices, _ := json.Marshal(t.Choices)
		steps, _ := json.Marshal(t.Steps)
		filter := bson.M{"title": t.Title}
		update := bson.M{
			"$setOnInsert": bson.M{
				"category":       t.Category,
				"scene":          t.Scene,
				"title":          t.Title,
				"description":    t.Description,
				"prompt":         t.Prompt,
				"choices":        string(choices),
				"difficulty":     t.Difficulty,
				"answer_mode":    t.AnswerMode,
				"safety_tip":     t.SafetyTip,
				"correct_choice": 1,
				"steps":          string(steps),
			},
			"$set": bson.M{
				"answer_mode": t.AnswerMode,
			},
		}
		opts := options.Update().SetUpsert(true)
		_, _ = db.Collection("tasks").UpdateOne(ctx, filter, update, opts)
	}
	return nil
}
