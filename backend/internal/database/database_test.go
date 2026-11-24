package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const testURI = "mongodb://127.0.0.1:27017"

func testClient(t *testing.T) *mongo.Client {
	t.Helper()
	if os.Getenv("SKIP_MONGO_TESTS") != "" {
		t.Skip("MongoDB not available")
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(testURI))
	if err != nil {
		t.Skipf("mongo unavailable: %v", err)
	}
	if err := client.Ping(context.Background(), nil); err != nil {
		t.Skipf("mongo unavailable: %v", err)
	}
	dbName := "t_" + strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())
	_ = client.Database(dbName).Drop(context.Background())
	t.Cleanup(func() {
		_ = client.Database(dbName).Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	t.Setenv("TEST_DB_NAME", dbName)
	return client
}

func countTasks(t *testing.T, client *mongo.Client, dbName, filter string) int64 {
	t.Helper()
	q := bson.M{}
	if filter != "" {
		q["title"] = bson.M{"$regex": filter}
	}
	n, err := client.Database(dbName).Collection("tasks").CountDocuments(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDatasetTasksAreSeededAndNotDuplicated(t *testing.T) {
	client := testClient(t)
	dbName := os.Getenv("TEST_DB_NAME")
	if err := Migrate(client, dbName); err != nil {
		t.Fatal(err)
	}
	before := countTasks(t, client, dbName, "ситуация")
	if before == 0 {
		t.Fatal("dataset tasks not seeded")
	}
	// Running migrate again must not duplicate.
	if err := Migrate(client, dbName); err != nil {
		t.Fatal(err)
	}
	after := countTasks(t, client, dbName, "ситуация")
	if after != before {
		t.Fatalf("dataset tasks were duplicated: %d -> %d", before, after)
	}
}

func TestTaskEditsSurviveRestartAndMigrationIsIdempotent(t *testing.T) {
	client := testClient(t)
	dbName := os.Getenv("TEST_DB_NAME")
	if err := Migrate(client, dbName); err != nil {
		t.Fatal(err)
	}
	before := countTasks(t, client, dbName, "")
	// Simulate an admin edit.
	if _, err := client.Database(dbName).Collection("tasks").UpdateOne(
		context.Background(), bson.M{"title": bson.M{"$exists": true}},
		bson.M{"$set": bson.M{"safety_tip": "Изменённый совет"}}); err != nil {
		t.Fatal(err)
	}
	// Migrate again — no reseeding of the edited row.
	if err := Migrate(client, dbName); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		SafetyTip string `bson:"safety_tip"`
	}
	err := client.Database(dbName).Collection("tasks").FindOne(
		context.Background(), bson.M{"safety_tip": "Изменённый совет"}).Decode(&doc)
	if err != nil {
		t.Fatal(err)
	}
	after := countTasks(t, client, dbName, "")
	if after != before {
		t.Fatalf("tasks reseeded: %d -> %d", before, after)
	}
}
