package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kids-trainer/backend/internal/auth"
	"github.com/kids-trainer/backend/internal/config"
	"github.com/kids-trainer/backend/internal/database"
	"github.com/kids-trainer/backend/internal/repositories"
	"github.com/kids-trainer/backend/internal/services"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type fixture struct {
	r    *gin.Engine
	h    *Handler
	t    *testing.T
	// db holds a direct Mongo client so tests can seed rows bypassing HTTP.
	db     *mongo.Client
	dbName string
}

// mongoURI points at the local MongoDB instance. Tests use one throwaway
// database per test function and drop it on cleanup.
const mongoURI = "mongodb://127.0.0.1:27017"

func testDBName(t *testing.T) string {
	t.Helper()
	return "safe_route_test_" + strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())
}

func setup(t *testing.T) fixture {
	t.Helper()
	if os.Getenv("SKIP_MONGO_TESTS") != "" {
		t.Skip("MongoDB not available")
	}
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Skipf("mongo unavailable: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		t.Skipf("mongo unavailable: %v", err)
	}
	dbName := testDBName(t)
	_ = client.Database(dbName).Drop(ctx)
	if err := database.Migrate(client, dbName); err != nil {
		t.Fatal(err)
	}
	if err := database.SeedExtraTasks(client, dbName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Database(dbName).Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	h := New(Handler{
		Users:    repositories.UserRepo{Client: client, DBName: dbName},
		Tasks:    repositories.TaskRepo{Client: client, DBName: dbName},
		Activity: repositories.ActivityRepo{Client: client, DBName: dbName},
		Invites:  repositories.InviteRepo{Client: client, DBName: dbName},
		Auth:     auth.New("test-secret", time.Hour),
		AI:       services.NewAIService(&config.Config{}),
		DevMode:  true, AdminLogin: "developer", AdminPassword: "test-password-12345",
	})
	r := gin.New()
	RegisterRoutes(r, h)
	return fixture{r, h, t, client, dbName}
}

// insertAttempt seeds a task_attempt row directly, with an optional child
// login to resolve its id from the users collection.
func (f fixture) insertAttempt(childID int64, taskID int64, answer, mode string, safe bool) {
	f.t.Helper()
	col := f.db.Database(f.dbName).Collection("task_attempts")
	_, err := col.InsertOne(context.Background(), bson.M{
		"child_id":    childID,
		"task_id":     taskID,
		"answer":      answer,
		"mode":        mode,
		"ai_is_safe":  safe,
		"ai_summary":  "",
		"ai_tip":      "",
		"ai_is_demo":  false,
		"created_at":  time.Now(),
	})
	if err != nil {
		f.t.Fatal(err)
	}
}

// loginID returns the numeric user id for a login.
func (f fixture) loginID(login string) int64 {
	f.t.Helper()
	var doc struct {
		ID int64 `bson:"id"`
	}
	err := f.db.Database(f.dbName).Collection("users").FindOne(context.Background(), bson.M{"login": login}).Decode(&doc)
	if err != nil {
		f.t.Fatal(err)
	}
	return doc.ID
}


func (f fixture) call(method, path, token string, body any, status int) map[string]any {
	f.t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code != status {
		f.t.Fatalf("%s %s: expected %d got %d: %s", method, path, status, w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}
func (f fixture) account(login, role string) string {
	f.t.Helper()
	v := f.call("POST", "/auth/register", "", map[string]any{"login": login, "password": "password-123", "name": "Тест", "role": role}, 201)
	return v["token"].(string)
}
func (f fixture) admin() string {
	return f.call("POST", "/auth/admin", "", map[string]any{"login": "developer", "password": "test-password-12345"}, 200)["token"].(string)
}
func TestPaletteBelongsToAuthenticatedAccount(t *testing.T) {
	f := setup(t)
	child := f.account("theme-child", "child")
	parent := f.account("theme-parent", "parent")
	f.call("PUT", "/me/preferences", "", map[string]any{"palette_index": 2}, 401)
	if f.call("GET", "/me", child, nil, 200)["palette_index"] != float64(0) {
		t.Fatal("missing default palette")
	}
	f.call("PUT", "/me/preferences", child, map[string]any{"palette_index": 2}, 200)
	f.call("PUT", "/me/preferences", parent, map[string]any{"palette_index": 4}, 200)
	for _, body := range []any{map[string]any{}, map[string]any{"palette_index": -1}, map[string]any{"palette_index": 6}, map[string]any{"palette_index": "blue"}} {
		f.call("PUT", "/me/preferences", child, body, 400)
	}
	login := f.call("POST", "/auth/login", "", map[string]any{"login": "theme-child", "password": "password-123"}, 200)
	if f.call("GET", "/me", login["token"].(string), nil, 200)["palette_index"] != float64(2) {
		t.Fatal("palette lost after login")
	}
	if f.call("GET", "/me", parent, nil, 200)["palette_index"] != float64(4) {
		t.Fatal("another account was changed")
	}
	f.call("PUT", "/me/preferences", child, map[string]any{"palette_index": 0}, 200)
	if f.call("GET", "/me", child, nil, 200)["palette_index"] != float64(0) {
		t.Fatal("cannot restore default palette")
	}
}
func taskPayload() map[string]any {
	return map[string]any{"category": "home", "scene": "Дома", "title": "Новая ситуация", "description": "Ты дома и слышишь звонок.", "prompt": "Что сделаешь?", "choices": []string{"Позвоню взрослому", "Открою незнакомцу"}, "correct_choice": 0, "difficulty": "easy", "answer_mode": "choices", "safety_tip": "Не открывай дверь незнакомому."}
}

func TestDeveloperAccessIsEnforcedOnServer(t *testing.T) {
	f := setup(t)
	child := f.account("child", "child")
	parent := f.account("parent", "parent")
	f.call("POST", "/auth/register", "", map[string]any{"login": "hacker", "password": "password-123", "name": "Test", "role": "admin"}, 400)
	f.call("POST", "/admin/tasks", "", taskPayload(), 401)
	f.call("POST", "/admin/tasks", child, taskPayload(), 403)
	f.call("POST", "/admin/tasks", parent, taskPayload(), 403)
	f.call("POST", "/auth/admin", "", map[string]any{"login": "developer", "password": "wrong"}, 401)
	admin := f.admin()
	f.h.DevMode = false
	f.call("POST", "/admin/tasks", admin, taskPayload(), 403)
	f.call("POST", "/auth/admin", "", map[string]any{"login": "developer", "password": "test-password-12345"}, 404)
}
func TestEditorChangesCorrectAnswerAndPreservesProgress(t *testing.T) {
	f := setup(t)
	admin := f.admin()
	child := f.account("child", "child")
	f.call("POST", "/child/onboarding", child, map[string]any{"name": "Маша", "age": 9}, 200)
	created := f.call("POST", "/admin/tasks", admin, taskPayload(), 201)
	id := int(created["id"].(float64))
	list := f.call("GET", "/tasks", child, nil, 200)["tasks"].([]any)
	for _, item := range list {
		if _, ok := item.(map[string]any)["correct_choice"]; ok {
			t.Fatal("child can read answer key")
		}
	}
	for i := 0; i < 2; i++ {
		result := f.call("POST", "/child/analyze", child, map[string]any{"task_id": id, "answer": "Позвоню взрослому", "mode": "choices"}, 200)
		if result["is_safe"] != true || result["evaluated"] != true {
			t.Fatal(result)
		}
	}
	p := f.call("GET", "/child/progress", child, nil, 200)
	if p["total_completed"] != float64(1) || p["completed_today"] != float64(1) || p["streak"] != float64(1) {
		t.Fatal(p)
	}
	edit := taskPayload()
	edit["title"] = "Редактирование"
	edit["correct_choice"] = 1
	f.call("PUT", fmt.Sprintf("/admin/tasks/%d", id), admin, edit, 200)
	result := f.call("POST", "/child/analyze", child, map[string]any{"task_id": id, "answer": "Позвоню взрослому", "mode": "choices"}, 200)
	if result["is_safe"] != false {
		t.Fatal("old answer is still safe", result)
	}
	f.call("POST", "/child/analyze", child, map[string]any{"task_id": id, "answer": "несуществующий вариант", "mode": "choices"}, 400)
	edit["correct_choice"] = 99
	f.call("PUT", fmt.Sprintf("/admin/tasks/%d", id), admin, edit, 400)
	edit["correct_choice"] = 0
	f.call("PUT", "/admin/tasks/99999", admin, edit, 404)
}
func TestInvitationConsumesOnlyItsOwnCodeAndParentSeesCounts(t *testing.T) {
	f := setup(t)
	parent := f.account("parent", "parent")
	child1 := f.account("child1", "child")
	child2 := f.account("child2", "child")
	// Profile must survive a subsequent login and /me refresh.
	for _, child := range []string{child1, child2} {
		f.call("POST", "/child/onboarding", child, map[string]any{"name": "Ребёнок", "age": 8}, 200)
	}
	c1 := f.call("POST", "/parent/invites", parent, map[string]any{"expires_in_minutes": 10}, 201)["code"]
	c2 := f.call("POST", "/parent/invites", parent, map[string]any{"expires_in_minutes": 10}, 201)["code"]
	f.call("POST", "/child/join", child1, map[string]any{"code": c1}, 200)
	f.call("POST", "/child/join", child2, map[string]any{"code": c1}, 404)
	f.call("POST", "/child/join", child2, map[string]any{"code": c2}, 200)
	me := f.call("GET", "/me", child1, nil, 200)
	if me["child_profile"].(map[string]any)["age"] != float64(8) {
		t.Fatal(me)
	}
	tasks := f.call("GET", "/tasks", child1, nil, 200)["tasks"].([]any)
	task := tasks[0].(map[string]any)
	choice := task["choices"].([]any)[1]
	for _, child := range []string{child1, child2} {
		f.call("POST", "/child/analyze", child, map[string]any{"task_id": task["id"], "answer": choice, "mode": "choices"}, 200)
	}
	p := f.call("GET", "/parent/activity", parent, nil, 200)
	if p["total_completed"] != float64(2) || p["completed_today"] != float64(2) {
		t.Fatal(p)
	}
	for _, key := range []string{"answer", "attempts", "ai_summary", "completed_task_ids"} {
		if _, exists := p[key]; exists {
			t.Fatal("parent can read non-count data", key)
		}
	}
	stats := p["children"].([]any)
	if len(stats) != 2 || stats[0].(map[string]any)["average_score"] != float64(100) {
		t.Fatal(p)
	}
	wrong := task["choices"].([]any)[0]
	f.call("POST", "/child/analyze", child1, map[string]any{"task_id": task["id"], "answer": wrong, "mode": "choices"}, 200)
	p = f.call("GET", "/parent/activity", parent, nil, 200)
	first := p["children"].([]any)[0].(map[string]any)
	if first["average_score"] != float64(0) || first["evaluated_tasks"] != float64(1) || first["total_completed"] != float64(1) {
		t.Fatal(first)
	}
	for _, key := range []string{"answer", "attempts", "ai_summary", "completed_task_ids"} {
		if _, exists := first[key]; exists {
			t.Fatal("parent can read private attempt data", key)
		}
	}
	other := f.account("other-parent", "parent")
	code := f.call("POST", "/parent/invites", other, nil, 201)["code"].(string)
	f.call("POST", "/child/join", child1, map[string]any{"code": code}, 409)
	f.call("POST", "/child/join", child1, map[string]any{"code": strings.ToLower(code), "replace": true}, 200)
	if p = f.call("GET", "/parent/activity", parent, nil, 200); p["total_completed"] != float64(1) {
		t.Fatal(p)
	}
}

func TestPlatinumAndGlobalStatsUseUniqueCompletedMissions(t *testing.T) {
	f := setup(t)
	child := f.account("platinum-child", "child")
	other := f.account("ranking-child", "child")
	id := f.call("GET", "/me", child, nil, 200)["id"]
	catalog := f.call("GET", "/tasks", child, nil, 200)["tasks"].([]any)
	task := catalog[0].(map[string]any)
	// Seed solved attempts for every task except the current one.
	allCol := f.db.Database(f.dbName).Collection("tasks")
	cursor, err := allCol.Find(context.Background(), bson.M{"id": bson.M{"$ne": task["id"]}})
	if err != nil {
		t.Fatal(err)
	}
	var docs []struct {
		ID int64 `bson:"id"`
	}
	if err := cursor.All(context.Background(), &docs); err != nil {
		t.Fatal(err)
	}
	childID := int64(id.(float64))
	for _, d := range docs {
		f.insertAttempt(childID, d.ID, "fixture", "free", true)
	}
	if p := f.call("GET", "/child/progress", child, nil, 200); p["platinum"] == true {
		t.Fatal(p)
	}
	answer := map[string]any{"task_id": task["id"], "answer": task["choices"].([]any)[1], "mode": "choices"}
	for _, token := range []string{child, child, other} {
		f.call("POST", "/child/analyze", token, answer, 200)
	}
	f.call("POST", "/admin/tasks", f.admin(), taskPayload(), 201)
	p := f.call("GET", "/child/progress", child, nil, 200)
	g := p["global"].(map[string]any)
	if p["platinum"] != true || g["players"] != float64(2) || g["rank"] != float64(1) || g["completed"] != float64(len(catalog)+1) {
		t.Fatal(p)
	}
	if p = f.call("GET", "/child/progress", other, nil, 200); p["global"].(map[string]any)["rank"] != float64(2) {
		t.Fatal(p)
	}
}
func TestFreeTextWithoutAIIsNotCountedAsCompleted(t *testing.T) {
	f := setup(t)
	child := f.account("child", "child")
	result := f.call("POST", "/child/analyze", child, map[string]any{"task_id": 1, "answer": "Я не буду звонить родителю", "mode": "free"}, 200)
	if result["evaluated"] != false || result["is_demo"] != true || result["is_safe"] != false {
		t.Fatal(result)
	}
	p := f.call("GET", "/child/progress", child, nil, 200)
	if p["total_completed"] != float64(0) || p["completed_today"] != float64(0) || p["streak"] != float64(1) {
		t.Fatal(p)
	}
}

func TestNextTaskBatchSupportsFiltersAndExcludesAttempted(t *testing.T) {
	f := setup(t)
	child := f.account("batch-child", "child")
	parent := f.account("batch-parent", "parent")
	path := "/child/tasks/next?count=5&category=city&difficulty=easy&mode=free"
	f.call("GET", path, parent, nil, 403)
	batch := f.call("GET", path, child, nil, 200)["tasks"].([]any)
	if len(batch) < 2 || len(batch) > 5 {
		t.Fatalf("expected 2 to 5 tasks, got %d", len(batch))
	}
	seen := map[float64]bool{}
	for _, value := range batch {
		task := value.(map[string]any)
		id := task["id"].(float64)
		if seen[id] || task["category"] != "city" || task["difficulty"] != "easy" || task["answer_mode"] != "free" {
			t.Fatalf("duplicate task or ignored filter: %v", task)
		}
		seen[id] = true
	}
	attempted := batch[0].(map[string]any)["id"].(float64)
	f.insertAttempt(f.loginID("batch-child"), int64(attempted), "answer", "free", false)
	batch = f.call("GET", path, child, nil, 200)["tasks"].([]any)
	for _, value := range batch {
		if value.(map[string]any)["id"] == attempted {
			t.Fatal("completed task returned in batch")
		}
	}
}
