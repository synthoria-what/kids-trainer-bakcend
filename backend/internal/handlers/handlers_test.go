package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kids-trainer/backend/internal/auth"
	"github.com/kids-trainer/backend/internal/config"
	"github.com/kids-trainer/backend/internal/database"
	"github.com/kids-trainer/backend/internal/repositories"
	"github.com/kids-trainer/backend/internal/services"
)

type fixture struct {
	r *gin.Engine
	h *Handler
	t *testing.T
}

func setup(t *testing.T) fixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := New(Handler{Users: repositories.UserRepo{DB: db}, Tasks: repositories.TaskRepo{DB: db}, Activity: repositories.ActivityRepo{DB: db}, Invites: repositories.InviteRepo{DB: db}, Auth: auth.New("test-secret", time.Hour), AI: services.NewAIService(&config.Config{}), DevMode: true, AdminLogin: "developer", AdminPassword: "test-password-12345"})
	r := gin.New()
	RegisterRoutes(r, h)
	return fixture{r, h, t}
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
	_, err := f.h.Tasks.DB.Exec(`INSERT INTO task_attempts(child_id,task_id,answer,mode) SELECT id,?,'answer','free' FROM users WHERE login='batch-child'`, int(attempted))
	if err != nil {
		t.Fatal(err)
	}
	batch = f.call("GET", path, child, nil, 200)["tasks"].([]any)
	for _, value := range batch {
		if value.(map[string]any)["id"] == attempted {
			t.Fatal("completed task returned in batch")
		}
	}
}
