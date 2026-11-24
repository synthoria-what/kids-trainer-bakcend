package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kids-trainer/backend/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrNoDocuments = mongo.ErrNoDocuments

type UserRepo struct {
	Client *mongo.Client
	DBName string
}

func (r UserRepo) col() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("users")
}
func (r UserRepo) profiles() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("child_profiles")
}
func (r UserRepo) prefs() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("user_preferences")
}

type userDoc struct {
	ID           int64             `bson:"id"`
	Login        string            `bson:"login"`
	PasswordHash string            `bson:"password_hash"`
	Role         models.UserRole   `bson:"role"`
	Name         string            `bson:"name"`
	CreatedAt    time.Time         `bson:"created_at"`
	Preferences  *userPrefDoc      `bson:"preferences,omitempty"`
}

type userPrefDoc struct {
	PaletteIndex int `bson:"palette_index"`
}

func (r UserRepo) Palette(ctx context.Context, userID int) (int, error) {
	var doc userDoc
	err := r.col().FindOne(ctx, bson.M{"id": userID}).Decode(&doc)
	if err != nil {
		return 0, err
	}
	if doc.Preferences == nil {
		return 0, nil
	}
	return doc.Preferences.PaletteIndex, nil
}

func (r UserRepo) SavePalette(ctx context.Context, userID, index int) error {
	_, err := r.col().UpdateOne(ctx, bson.M{"id": userID},
		bson.M{"$set": bson.M{"preferences.palette_index": index}})
	return err
}

func (r UserRepo) nextID(ctx context.Context) (int64, error) {
	// Find highest existing id and increment.
	opts := options.FindOne().SetSort(bson.D{{Key: "id", Value: -1}})
	var last userDoc
	err := r.col().FindOne(ctx, bson.M{}, opts).Decode(&last)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return last.ID + 1, nil
}

func (r UserRepo) Create(ctx context.Context, u models.User) (int, error) {
	id, err := r.nextID(ctx)
	if err != nil {
		return 0, err
	}
	doc := userDoc{
		ID:           id,
		Login:        strings.ToLower(strings.TrimSpace(u.Login)),
		PasswordHash: u.PasswordHash,
		Role:         u.Role,
		Name:         u.Name,
		CreatedAt:    time.Now(),
	}
	_, err = r.col().InsertOne(ctx, doc)
	return int(id), err
}

func (r UserRepo) ByLogin(ctx context.Context, login string) (models.User, error) {
	var doc userDoc
	err := r.col().FindOne(ctx, bson.M{"login": strings.ToLower(strings.TrimSpace(login))}).Decode(&doc)
	return userDocToModel(doc), err
}

func (r UserRepo) ByID(ctx context.Context, id int) (models.User, error) {
	var doc userDoc
	err := r.col().FindOne(ctx, bson.M{"id": int64(id)}).Decode(&doc)
	return userDocToModel(doc), err
}

func userDocToModel(d userDoc) models.User {
	return models.User{
		ID:           int(d.ID),
		Login:        d.Login,
		PasswordHash: d.PasswordHash,
		Role:         d.Role,
		Name:         d.Name,
		CreatedAt:    d.CreatedAt,
	}
}

type profileDoc struct {
	UserID    int64     `bson:"user_id"`
	Name      string    `bson:"name"`
	Age       int       `bson:"age"`
	ParentID  *int64    `bson:"parent_id"`
	CreatedAt time.Time `bson:"created_at"`
}

func (r UserRepo) CreateProfile(ctx context.Context, p models.ChildProfile) error {
	doc := profileDoc{
		UserID:    int64(p.UserID),
		Name:      p.Name,
		Age:       p.Age,
		CreatedAt: time.Now(),
	}
	_, err := r.profiles().InsertOne(ctx, doc)
	return err
}

func (r UserRepo) UpdateProfile(ctx context.Context, userID int, name string, age int) error {
	filter := bson.M{"user_id": int64(userID)}
	update := bson.M{"$set": bson.M{"name": name, "age": age}}
	_, err := r.profiles().UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

var ErrParentAlreadyLinked = errors.New("child is linked to another parent")

type inviteDoc struct {
	ID        int64     `bson:"id"`
	ParentID  int64     `bson:"parent_id"`
	CodeHash  string    `bson:"code_hash"`
	ExpiresAt time.Time `bson:"expires_at"`
	UsedBy    *int64    `bson:"used_by"`
	CreatedAt time.Time `bson:"created_at"`
}

func (r UserRepo) LinkParent(ctx context.Context, childID int, codeHash string, replace bool) (string, error) {
	// Find valid invite
	var invite inviteDoc
	err := r.Client.Database(r.DBName).Collection("invite_codes").FindOne(ctx, bson.M{
		"code_hash": codeHash,
		"used_by":   bson.M{"$exists": false},
		"expires_at": bson.M{"$gt": time.Now()},
	}).Decode(&invite)
	if err != nil {
		return "", err
	}
	// Parent name
	var parent userDoc
	err = r.col().FindOne(ctx, bson.M{"id": invite.ParentID}).Decode(&parent)
	if err != nil {
		return "", err
	}
	// Current profile
	var profile profileDoc
	err = r.profiles().FindOne(ctx, bson.M{"user_id": int64(childID)}).Decode(&profile)
	if err != nil {
		return "", err
	}
	if profile.ParentID != nil && *profile.ParentID != invite.ParentID && !replace {
		return "", ErrParentAlreadyLinked
	}
	// Update profile
	_, err = r.profiles().UpdateOne(ctx, bson.M{"user_id": int64(childID)},
		bson.M{"$set": bson.M{"parent_id": invite.ParentID}})
	if err != nil {
		return "", err
	}
	// Mark invite used
	res, err := r.Client.Database(r.DBName).Collection("invite_codes").UpdateOne(ctx,
		bson.M{"id": invite.ID, "used_by": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"used_by": int64(childID)}})
	if err != nil {
		return "", err
	}
	if res.ModifiedCount != 1 {
		return "", mongo.ErrNoDocuments
	}
	return parent.Name, nil
}

func (r UserRepo) Profile(ctx context.Context, userID int) (models.ChildProfile, error) {
	var p profileDoc
	err := r.profiles().FindOne(ctx, bson.M{"user_id": int64(userID)}).Decode(&p)
	out := models.ChildProfile{
		UserID:   int(p.UserID),
		Name:     p.Name,
		Age:      p.Age,
	}
	if p.ParentID != nil {
		out.ParentID = int(*p.ParentID)
	}
	return out, err
}

func (r UserRepo) Children(ctx context.Context, parentID int) ([]models.ChildProfile, error) {
	cursor, err := r.profiles().Find(ctx, bson.M{"parent_id": int64(parentID)}, options.Find().SetSort(bson.D{{Key: "user_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := []models.ChildProfile{}
	for cursor.Next(ctx) {
		var p profileDoc
		if err := cursor.Decode(&p); err != nil {
			return nil, err
		}
		out = append(out, models.ChildProfile{UserID: int(p.UserID), Name: p.Name})
	}
	return out, cursor.Err()
}

func (r UserRepo) ChildParent(ctx context.Context, childID int) (int, error) {
	var p profileDoc
	err := r.profiles().FindOne(ctx, bson.M{"user_id": int64(childID)}).Decode(&p)
	if err != nil {
		return 0, err
	}
	if p.ParentID == nil {
		return 0, mongo.ErrNoDocuments
	}
	return int(*p.ParentID), nil
}

type TaskRepo struct {
	Client *mongo.Client
	DBName string
}

func (r TaskRepo) col() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("tasks")
}

type taskDoc struct {
	ID            int64            `bson:"id"`
	Category      string           `bson:"category"`
	Scene         string           `bson:"scene"`
	Title         string           `bson:"title"`
	Description   string           `bson:"description"`
	Prompt        string           `bson:"prompt"`
	Choices       json.RawMessage  `bson:"choices"`
	Difficulty    string           `bson:"difficulty"`
	AnswerMode    string           `bson:"answer_mode"`
	SafetyTip     string           `bson:"safety_tip"`
	CorrectChoice *int             `bson:"correct_choice"`
	Steps         json.RawMessage  `bson:"steps"`
}

func taskDocToModel(d taskDoc) (models.SafetyTask, error) {
	t := models.SafetyTask{
		ID:            int(d.ID),
		Category:      models.SafetyCategory(d.Category),
		Scene:         d.Scene,
		Title:         d.Title,
		Description:   d.Description,
		Prompt:        d.Prompt,
		Difficulty:    models.TaskDifficulty(d.Difficulty),
		AnswerMode:    models.AnswerMode(d.AnswerMode),
		SafetyTip:     d.SafetyTip,
		CorrectChoice: d.CorrectChoice,
	}
	if len(d.Choices) > 0 {
		if err := json.Unmarshal(d.Choices, &t.Choices); err != nil {
			return t, err
		}
	}
	if len(d.Steps) > 0 {
		_ = json.Unmarshal(d.Steps, &t.Steps)
	}
	return t, nil
}

func taskFilter(category, difficulty, mode string) bson.M {
	f := bson.M{}
	if category != "" {
		f["category"] = category
	}
	if difficulty != "" {
		f["difficulty"] = difficulty
	}
	if mode != "" {
		f["answer_mode"] = mode
	}
	return f
}

func (r TaskRepo) List(ctx context.Context, category, difficulty, mode string) ([]models.SafetyTask, error) {
	cursor, err := r.col().Find(ctx, taskFilter(category, difficulty, mode), options.Find().SetSort(bson.D{{Key: "id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := []models.SafetyTask{}
	for cursor.Next(ctx) {
		var d taskDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		t, err := taskDocToModel(d)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, cursor.Err()
}

func (r TaskRepo) ByID(ctx context.Context, id int) (models.SafetyTask, error) {
	var d taskDoc
	err := r.col().FindOne(ctx, bson.M{"id": int64(id)}).Decode(&d)
	if err != nil {
		return models.SafetyTask{}, err
	}
	return taskDocToModel(d)
}

// RandomTask picks one random task matching the filters using $sample.
func (r TaskRepo) RandomTask(ctx context.Context, category, difficulty, mode string) (models.SafetyTask, error) {
	return r.sampleTask(ctx, taskFilter(category, difficulty, mode))
}

func (r TaskRepo) sampleTask(ctx context.Context, filter bson.M) (models.SafetyTask, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$sample", Value: bson.M{"size": 1}}},
	}
	cursor, err := r.col().Aggregate(ctx, pipeline)
	if err != nil {
		return models.SafetyTask{}, err
	}
	defer cursor.Close(ctx)
	if !cursor.Next(ctx) {
		return models.SafetyTask{}, cursor.Err()
	}
	var d taskDoc
	if err := cursor.Decode(&d); err != nil {
		return models.SafetyTask{}, err
	}
	return taskDocToModel(d)
}

// solvedTaskIDs returns the set of task ids this child already attempted.
func (r TaskRepo) solvedTaskIDs(ctx context.Context, childID int) (map[int64]bool, error) {
	cursor, err := r.Client.Database(r.DBName).Collection("task_attempts").
		Find(ctx, bson.M{"child_id": int64(childID)}, options.Find().SetProjection(bson.M{"task_id": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := map[int64]bool{}
	for cursor.Next(ctx) {
		var doc struct {
			TaskID int64 `bson:"task_id"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		out[doc.TaskID] = true
	}
	return out, cursor.Err()
}

func (r TaskRepo) NextForChild(ctx context.Context, childID int, category, difficulty, mode string) (models.SafetyTask, error) {
	solved, err := r.solvedTaskIDs(ctx, childID)
	if err != nil {
		return models.SafetyTask{}, err
	}
	filter := taskFilter(category, difficulty, mode)
	if len(solved) > 0 {
		ids := make([]int64, 0, len(solved))
		for id := range solved {
			ids = append(ids, id)
		}
		filter["id"] = bson.M{"$nin": ids}
	}
	return r.sampleTask(ctx, filter)
}

// NextForChildBatch returns several untried tasks at once for the carousel.
func (r TaskRepo) NextForChildBatch(ctx context.Context, childID int, category, difficulty, mode string, limit int) ([]models.SafetyTask, error) {
	if limit < 1 {
		limit = 5
	}
	solved, err := r.solvedTaskIDs(ctx, childID)
	if err != nil {
		return nil, err
	}
	filter := taskFilter(category, difficulty, mode)
	if len(solved) > 0 {
		ids := make([]int64, 0, len(solved))
		for id := range solved {
			ids = append(ids, id)
		}
		filter["id"] = bson.M{"$nin": ids}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$sample", Value: bson.M{"size": limit}}},
	}
	cursor, err := r.col().Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := []models.SafetyTask{}
	for cursor.Next(ctx) {
		var d taskDoc
		if err := cursor.Decode(&d); err != nil {
			return nil, err
		}
		t, err := taskDocToModel(d)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, cursor.Err()
}

func (r TaskRepo) nextID(ctx context.Context) (int64, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "id", Value: -1}})
	var last taskDoc
	err := r.col().FindOne(ctx, bson.M{}, opts).Decode(&last)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return last.ID + 1, nil
}

func (r TaskRepo) Save(ctx context.Context, t models.SafetyTask) (models.SafetyTask, error) {
	choices, err := json.Marshal(t.Choices)
	if err != nil {
		return t, err
	}
	steps, err := json.Marshal(t.Steps)
	if err != nil {
		return t, err
	}
	doc := bson.M{
		"category":       string(t.Category),
		"scene":          t.Scene,
		"title":          t.Title,
		"description":    t.Description,
		"prompt":         t.Prompt,
		"choices":        json.RawMessage(choices),
		"difficulty":     string(t.Difficulty),
		"answer_mode":    string(t.AnswerMode),
		"safety_tip":     t.SafetyTip,
		"correct_choice": t.CorrectChoice,
		"steps":          json.RawMessage(steps),
	}
	if t.ID == 0 {
		id, err := r.nextID(ctx)
		if err != nil {
			return t, err
		}
		doc["id"] = id
		_, err = r.col().InsertOne(ctx, doc)
		t.ID = int(id)
		return t, err
	}
	res, err := r.col().UpdateOne(ctx, bson.M{"id": int64(t.ID)}, bson.M{"$set": doc})
	if err != nil {
		return t, err
	}
	if res.MatchedCount == 0 {
		return t, mongo.ErrNoDocuments
	}
	return t, nil
}

type ActivityRepo struct {
	Client *mongo.Client
	DBName string
}

func (r ActivityRepo) attempts() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("task_attempts")
}
func (r ActivityRepo) daily() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("daily_activity")
}
func (r ActivityRepo) tasks() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("tasks")
}
func (r ActivityRepo) users() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("users")
}
func (r ActivityRepo) platinum() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("platinum_awards")
}

func todayStr() string { return time.Now().Format("2006-01-02") }

func (r ActivityRepo) Record(ctx context.Context, childID int, completed bool) error {
	filter := bson.M{"child_id": int64(childID), "activity_date": todayStr()}
	inc := 0
	if completed {
		inc = 1
	}
	update := bson.M{"$inc": bson.M{"completed_count": inc}, "$setOnInsert": bson.M{"created_at": time.Now()}}
	_, err := r.daily().UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

type attemptDoc struct {
	ID        int64             `bson:"id"`
	ChildID   int64             `bson:"child_id"`
	TaskID    int64             `bson:"task_id"`
	Answer    string            `bson:"answer"`
	Mode      string            `bson:"mode"`
	AiIsSafe  *bool             `bson:"ai_is_safe"`
	AiScore   *int              `bson:"ai_score"`
	AiSummary string            `bson:"ai_summary"`
	AiTip     string            `bson:"ai_tip"`
	AiIsDemo  bool              `bson:"ai_is_demo"`
	CreatedAt time.Time         `bson:"created_at"`
}

func (r ActivityRepo) nextAttemptID(ctx context.Context) (int64, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "id", Value: -1}})
	var last attemptDoc
	err := r.attempts().FindOne(ctx, bson.M{}, opts).Decode(&last)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return last.ID + 1, nil
}

func (r ActivityRepo) Attempt(ctx context.Context, a models.TaskAttempt) error {
	if err := r.users().FindOne(ctx, bson.M{"id": int64(a.ChildID)}).Err(); err != nil {
		return fmt.Errorf("child user %d does not exist", a.ChildID)
	}
	if err := r.tasks().FindOne(ctx, bson.M{"id": int64(a.TaskID)}).Err(); err != nil {
		return fmt.Errorf("task %d does not exist", a.TaskID)
	}
	id, err := r.nextAttemptID(ctx)
	if err != nil {
		return err
	}
	doc := attemptDoc{
		ID: id, ChildID: int64(a.ChildID), TaskID: int64(a.TaskID),
		Answer: a.Answer, Mode: string(a.Mode), AiIsSafe: a.AiIsSafe,
		AiScore: a.AiScore, AiSummary: a.AiSummary, AiTip: a.AiTip,
		AiIsDemo: a.AiIsDemo, CreatedAt: time.Now(),
	}
	_, err = r.attempts().InsertOne(ctx, doc)
	return err
}

func (r ActivityRepo) Progress(ctx context.Context, childID int) (models.ProgressResponse, error) {
	p := models.ProgressResponse{ByCategory: map[string]int{}, CompletedDays: []int{}}
	today := time.Now()
	date := todayStr()
	startOfDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	endOfDay := startOfDay.AddDate(0, 0, 1)

	countToday, err := r.attempts().Distinct(ctx, "task_id", bson.M{
		"child_id": int64(childID), "ai_is_safe": true,
		"created_at": bson.M{"$gte": startOfDay, "$lt": endOfDay},
	})
	if err != nil {
		return p, err
	}
	p.CompletedToday = len(countToday)

	allIDs, err := r.attempts().Distinct(ctx, "task_id", bson.M{"child_id": int64(childID), "ai_is_safe": true})
	if err != nil {
		return p, err
	}
	p.TotalCompleted = len(allIDs)
	p.CompletedTaskIDs = []int{}
	for _, v := range allIDs {
		if id, ok := v.(int32); ok {
			p.CompletedTaskIDs = append(p.CompletedTaskIDs, int(id))
		} else if id, ok := v.(int64); ok {
			p.CompletedTaskIDs = append(p.CompletedTaskIDs, int(id))
		}
	}

	// by_category: join via tasks collection
	catCursor, err := r.tasks().Find(ctx, bson.M{})
	if err != nil {
		return p, err
	}
	var taskCats map[int64]string
	if err := catCursor.All(ctx, &taskCats); err == nil {
		// Fallback: build map manually
	}
	catCursor.Close(ctx)
	taskCats, err = r.taskCategoryMap(ctx)
	if err != nil {
		return p, err
	}
	for _, v := range allIDs {
		var taskID int64
		switch id := v.(type) {
		case int32:
			taskID = int64(id)
		case int64:
			taskID = id
		}
		if cat, ok := taskCats[taskID]; ok {
			p.ByCategory[cat]++
		}
	}

	// Streak and week days from daily_activity
	cursor, err := r.daily().Find(ctx, bson.M{"child_id": int64(childID)})
	if err != nil {
		return p, err
	}
	defer cursor.Close(ctx)
	dates := map[string]bool{}
	for cursor.Next(ctx) {
		var doc struct {
			ActivityDate string `bson:"activity_date"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return p, err
		}
		dates[doc.ActivityDate] = true
	}
	if err := cursor.Err(); err != nil {
		return p, err
	}
	cursor2 := today
	if !dates[date] {
		cursor2 = cursor2.AddDate(0, 0, -1)
	}
	for dates[cursor2.Format("2006-01-02")] {
		p.Streak++
		cursor2 = cursor2.AddDate(0, 0, -1)
	}
	weekday := int(today.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	monday := today.AddDate(0, 0, 1-weekday)
	for i := 0; i < weekday; i++ {
		if dates[monday.AddDate(0, 0, i).Format("2006-01-02")] {
			p.CompletedDays = append(p.CompletedDays, i+1)
		}
	}
	return p, nil
}

func (r ActivityRepo) taskCategoryMap(ctx context.Context) (map[int64]string, error) {
	cursor, err := r.tasks().Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"id": 1, "category": 1}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	out := map[int64]string{}
	for cursor.Next(ctx) {
		var doc struct {
			ID       int64  `bson:"id"`
			Category string `bson:"category"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		out[doc.ID] = doc.Category
	}
	return out, cursor.Err()
}

func (r ActivityRepo) Scores(ctx context.Context, childID int) (count int, average *int, err error) {
	// Get the latest attempt per task with a score.
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"child_id": int64(childID), "ai_score": bson.M{"$ne": nil}}}},
		{{Key: "$sort", Value: bson.D{{Key: "id", Value: -1}}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$task_id",
			"score": bson.M{"$first": "$ai_score"},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id": nil,
			"count": bson.M{"$sum": 1},
			"avg":   bson.M{"$avg": "$score"},
		}}},
	}
	cursor, err := r.attempts().Aggregate(ctx, pipeline)
	if err != nil {
		return 0, nil, err
	}
	defer cursor.Close(ctx)
	if !cursor.Next(ctx) {
		return 0, nil, cursor.Err()
	}
	var doc struct {
		Count int       `bson:"count"`
		Avg   *float64  `bson:"avg"`
	}
	if err := cursor.Decode(&doc); err != nil {
		return 0, nil, err
	}
	if doc.Avg != nil {
		value := int(*doc.Avg + 0.5)
		average = &value
	}
	return doc.Count, average, nil
}

func (r ActivityRepo) GlobalStats(ctx context.Context, childID int) (models.GlobalStats, error) {
	var stats models.GlobalStats
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"ai_is_safe": true}}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$child_id",
			"total": bson.M{"$addToSet": "$task_id"},
		}}},
		{{Key: "$project", Value: bson.M{"total": bson.M{"$size": "$total"}}}},
		{{Key: "$sort", Value: bson.D{{Key: "total", Value: -1}}}},
	}
	cursor, err := r.attempts().Aggregate(ctx, pipeline)
	if err != nil {
		return stats, err
	}
	defer cursor.Close(ctx)
	rank := 0
	for cursor.Next(ctx) {
		var doc struct {
			ChildID int64 `bson:"_id"`
			Total   int   `bson:"total"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return stats, err
		}
		stats.Players++
		stats.Completed += doc.Total
		if doc.ChildID == int64(childID) {
			rank = stats.Players
		}
	}
	if rank > 0 {
		stats.Rank = &rank
	}
	if err := cursor.Err(); err != nil {
		return stats, err
	}
	return stats, nil
}

func (r ActivityRepo) Platinum(ctx context.Context, childID int, completed int) (bool, error) {
	var doc struct {
		ChildID int64 `bson:"child_id"`
	}
	err := r.platinum().FindOne(ctx, bson.M{"child_id": int64(childID)}).Decode(&doc)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return false, err
	}
	total, err := r.tasks().CountDocuments(ctx, bson.M{})
	if err != nil || total == 0 || completed < int(total) {
		return false, err
	}
	_, err = r.platinum().InsertOne(ctx, bson.M{"child_id": int64(childID), "awarded_at": time.Now()})
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r ActivityRepo) Submit(ctx context.Context, a models.TaskAttempt) error {
	if err := r.Attempt(ctx, a); err != nil {
		return err
	}
	increment := 0
	if a.AiIsSafe != nil && *a.AiIsSafe {
		increment = 1
		// Grant platinum if all tasks are solved.
		total, err := r.tasks().CountDocuments(ctx, bson.M{})
		if err == nil && total > 0 {
			solved, err := r.attempts().Distinct(ctx, "task_id", bson.M{
				"child_id": int64(a.ChildID), "ai_is_safe": true,
			})
			if err == nil && len(solved) >= int(total) {
				_, _ = r.platinum().UpdateOne(ctx,
					bson.M{"child_id": int64(a.ChildID)},
					bson.M{"$setOnInsert": bson.M{"awarded_at": time.Now()}},
					options.Update().SetUpsert(true))
			}
		}
	}
	filter := bson.M{"child_id": int64(a.ChildID), "activity_date": todayStr()}
	update := bson.M{"$inc": bson.M{"completed_count": increment}, "$setOnInsert": bson.M{"created_at": time.Now()}}
	_, err := r.daily().UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

type InviteRepo struct {
	Client *mongo.Client
	DBName string
}

func (r InviteRepo) col() *mongo.Collection {
	return r.Client.Database(r.DBName).Collection("invite_codes")
}

func (r InviteRepo) nextID(ctx context.Context) (int64, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "id", Value: -1}})
	var last inviteDoc
	err := r.col().FindOne(ctx, bson.M{}, opts).Decode(&last)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return last.ID + 1, nil
}

func (r InviteRepo) Create(ctx context.Context, parentID int, hash string, expires time.Time) (models.InviteCode, error) {
	id, err := r.nextID(ctx)
	if err != nil {
		return models.InviteCode{}, err
	}
	doc := inviteDoc{
		ID: int64(id), ParentID: int64(parentID), CodeHash: hash,
		ExpiresAt: expires, CreatedAt: time.Now(),
	}
	_, err = r.col().InsertOne(ctx, doc)
	return models.InviteCode{ID: int(id), ParentID: parentID, ExpiresAt: expires}, err
}

func (r InviteRepo) FindValid(ctx context.Context, hash string) (models.InviteCode, error) {
	var d inviteDoc
	err := r.col().FindOne(ctx, bson.M{
		"code_hash": hash,
		"used_by":   bson.M{"$exists": false},
		"expires_at": bson.M{"$gt": time.Now()},
	}).Decode(&d)
	out := models.InviteCode{ID: int(d.ID), ParentID: int(d.ParentID), ExpiresAt: d.ExpiresAt, CreatedAt: d.CreatedAt}
	if d.UsedBy != nil {
		usedBy := int(*d.UsedBy)
		out.UsedBy = &usedBy
	}
	return out, err
}

func IsUniqueError(err error) bool {
	return err != nil && (mongo.IsDuplicateKeyError(err) ||
		strings.Contains(strings.ToLower(err.Error()), "unique") ||
		strings.Contains(strings.ToLower(err.Error()), "duplicate"))
}

func ValidateRole(role models.UserRole) bool {
	return role == models.RoleChild || role == models.RoleParent
}

func ValidateCredentials(login, password string) error {
	if len(strings.TrimSpace(login)) < 3 {
		return fmt.Errorf("login must be at least 3 characters")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}
