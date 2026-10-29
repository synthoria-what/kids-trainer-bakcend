package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kids-trainer/backend/internal/models"
)

type UserRepo struct{ DB *sql.DB }

func (r UserRepo) Palette(ctx context.Context, userID int) (int, error) {
	var index int
	err := r.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT palette_index FROM user_preferences WHERE user_id=?),0)`, userID).Scan(&index)
	return index, err
}

func (r UserRepo) SavePalette(ctx context.Context, userID, index int) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO user_preferences(user_id,palette_index) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET palette_index=excluded.palette_index`, userID, index)
	return err
}

func (r UserRepo) Create(ctx context.Context, u models.User) (int, error) {
	res, err := r.DB.ExecContext(ctx, `INSERT INTO users(login,password_hash,role,name) VALUES(?,?,?,?)`, strings.ToLower(strings.TrimSpace(u.Login)), u.PasswordHash, u.Role, u.Name)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return int(id), nil
}
func (r UserRepo) ByLogin(ctx context.Context, login string) (models.User, error) {
	var u models.User
	err := r.DB.QueryRowContext(ctx, `SELECT id,login,password_hash,role,name,created_at FROM users WHERE login=?`, strings.ToLower(strings.TrimSpace(login))).Scan(&u.ID, &u.Login, &u.PasswordHash, &u.Role, &u.Name, &u.CreatedAt)
	return u, err
}
func (r UserRepo) ByID(ctx context.Context, id int) (models.User, error) {
	var u models.User
	err := r.DB.QueryRowContext(ctx, `SELECT id,login,password_hash,role,name,created_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.Login, &u.PasswordHash, &u.Role, &u.Name, &u.CreatedAt)
	return u, err
}
func (r UserRepo) CreateProfile(ctx context.Context, p models.ChildProfile) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO child_profiles(user_id,name,age,parent_id) VALUES(?,?,?,NULL)`, p.UserID, p.Name, p.Age)
	return err
}
func (r UserRepo) UpdateProfile(ctx context.Context, userID int, name string, age int) error {
	_, err := r.DB.ExecContext(ctx, `INSERT INTO child_profiles(user_id,name,age) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET name=excluded.name,age=excluded.age`, userID, name, age)
	return err
}
func (r UserRepo) LinkParent(ctx context.Context, childID int, codeHash string) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inviteID, parentID int
	if err = tx.QueryRowContext(ctx, `SELECT id,parent_id FROM invite_codes WHERE code_hash=? AND used_by IS NULL AND expires_at>?`, codeHash, time.Now()).Scan(&inviteID, &parentID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE child_profiles SET parent_id=? WHERE user_id=?`, parentID, childID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	res, err = tx.ExecContext(ctx, `UPDATE invite_codes SET used_by=? WHERE id=? AND used_by IS NULL`, childID, inviteID)
	if err != nil {
		return err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
func (r UserRepo) Profile(ctx context.Context, userID int) (models.ChildProfile, error) {
	var p models.ChildProfile
	var parent sql.NullInt64
	err := r.DB.QueryRowContext(ctx, `SELECT user_id,name,age,parent_id FROM child_profiles WHERE user_id=?`, userID).Scan(&p.UserID, &p.Name, &p.Age, &parent)
	if parent.Valid {
		p.ParentID = int(parent.Int64)
	}
	return p, err
}
func (r UserRepo) Children(ctx context.Context, parentID int) ([]int, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT user_id FROM child_profiles WHERE parent_id=?`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r UserRepo) ChildParent(ctx context.Context, childID int) (int, error) {
	var id sql.NullInt64
	err := r.DB.QueryRowContext(ctx, `SELECT parent_id FROM child_profiles WHERE user_id=?`, childID).Scan(&id)
	if err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, sql.ErrNoRows
	}
	return int(id.Int64), nil
}

type TaskRepo struct{ DB *sql.DB }

func (r TaskRepo) List(ctx context.Context, category, difficulty, mode string) ([]models.SafetyTask, error) {
	q := `SELECT id,category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip,correct_choice FROM tasks WHERE 1=1`
	args := []any{}
	if category != "" {
		q += " AND category=?"
		args = append(args, category)
	}
	if difficulty != "" {
		q += " AND difficulty=?"
		args = append(args, difficulty)
	}
	if mode != "" {
		q += " AND answer_mode=?"
		args = append(args, mode)
	}
	q += " ORDER BY id"
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.SafetyTask{}
	for rows.Next() {
		var t models.SafetyTask
		var choices string
		if err := rows.Scan(&t.ID, &t.Category, &t.Scene, &t.Title, &t.Description, &t.Prompt, &choices, &t.Difficulty, &t.AnswerMode, &t.SafetyTip, &t.CorrectChoice); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(choices), &t.Choices); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (r TaskRepo) ByID(ctx context.Context, id int) (models.SafetyTask, error) {
	var t models.SafetyTask
	var choices string
	err := r.DB.QueryRowContext(ctx, `SELECT id,category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip,correct_choice FROM tasks WHERE id=?`, id).Scan(&t.ID, &t.Category, &t.Scene, &t.Title, &t.Description, &t.Prompt, &choices, &t.Difficulty, &t.AnswerMode, &t.SafetyTip, &t.CorrectChoice)
	if err != nil {
		return t, err
	}
	err = json.Unmarshal([]byte(choices), &t.Choices)
	return t, err
}

func (r TaskRepo) NextForChild(ctx context.Context, childID int, category, difficulty, mode string) (models.SafetyTask, error) {
	q := `SELECT id,category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip,correct_choice FROM tasks t WHERE NOT EXISTS (SELECT 1 FROM task_attempts a WHERE a.task_id=t.id AND a.child_id=?)`
	args := []any{childID}
	if category != "" {
		q += " AND category=?"
		args = append(args, category)
	}
	if difficulty != "" {
		q += " AND difficulty=?"
		args = append(args, difficulty)
	}
	if mode != "" {
		q += " AND answer_mode=?"
		args = append(args, mode)
	}
	q += " ORDER BY RANDOM() LIMIT 1"
	return r.scanRandomTask(ctx, q, args...)
}

func (r TaskRepo) RandomTask(ctx context.Context, category, difficulty, mode string) (models.SafetyTask, error) {
	q := `SELECT id,category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip,correct_choice FROM tasks WHERE 1=1`
	args := []any{}
	if category != "" {
		q += " AND category=?"
		args = append(args, category)
	}
	if difficulty != "" {
		q += " AND difficulty=?"
		args = append(args, difficulty)
	}
	if mode != "" {
		q += " AND answer_mode=?"
		args = append(args, mode)
	}
	q += " ORDER BY RANDOM() LIMIT 1"
	return r.scanRandomTask(ctx, q, args...)
}

func (r TaskRepo) scanRandomTask(ctx context.Context, q string, args ...any) (models.SafetyTask, error) {
	var t models.SafetyTask
	var choices string
	err := r.DB.QueryRowContext(ctx, q, args...).Scan(&t.ID, &t.Category, &t.Scene, &t.Title, &t.Description, &t.Prompt, &choices, &t.Difficulty, &t.AnswerMode, &t.SafetyTip, &t.CorrectChoice)
	if err != nil {
		return t, err
	}
	return t, json.Unmarshal([]byte(choices), &t.Choices)
}

type ActivityRepo struct{ DB *sql.DB }

func (r ActivityRepo) Record(ctx context.Context, childID int, completed bool) error {
	if !completed {
		_, err := r.DB.ExecContext(ctx, `INSERT INTO daily_activity(child_id,activity_date,completed_count) VALUES(?,?,0) ON CONFLICT(child_id,activity_date) DO NOTHING`, childID, time.Now().Format("2006-01-02"))
		return err
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO daily_activity(child_id,activity_date,completed_count) VALUES(?,?,1) ON CONFLICT(child_id,activity_date) DO UPDATE SET completed_count=completed_count+1`, childID, time.Now().Format("2006-01-02"))
	return err
}
func (r ActivityRepo) Attempt(ctx context.Context, a models.TaskAttempt) error {
	var userExists, taskExists int
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=?`, a.ChildID).Scan(&userExists); err != nil {
		return err
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE id=?`, a.TaskID).Scan(&taskExists); err != nil {
		return err
	}
	if userExists == 0 {
		return fmt.Errorf("child user %d does not exist", a.ChildID)
	}
	if taskExists == 0 {
		return fmt.Errorf("task %d does not exist", a.TaskID)
	}
	var aiIsSafe any
	if a.AiIsSafe != nil {
		aiIsSafe = *a.AiIsSafe
	}
	_, err := r.DB.ExecContext(ctx, `INSERT INTO task_attempts(child_id,task_id,answer,mode,ai_is_safe,ai_summary,ai_tip,ai_is_demo) VALUES(?,?,?,?,?,?,?,?)`, a.ChildID, a.TaskID, a.Answer, a.Mode, aiIsSafe, a.AiSummary, a.AiTip, a.AiIsDemo)
	return err
}
func (r ActivityRepo) Progress(ctx context.Context, childID int) (models.ProgressResponse, error) {
	p := models.ProgressResponse{ByCategory: map[string]int{}, CompletedDays: []int{}}
	today := time.Now()
	date := today.Format("2006-01-02")
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT task_id) FROM task_attempts WHERE child_id=? AND ai_is_safe=1 AND date(created_at,'localtime')=?`, childID, date).Scan(&p.CompletedToday); err != nil {
		return p, err
	}
	if err := r.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT task_id) FROM task_attempts WHERE child_id=? AND ai_is_safe=1`, childID).Scan(&p.TotalCompleted); err != nil {
		return p, err
	}
	ids, err := r.DB.QueryContext(ctx, `SELECT DISTINCT task_id FROM task_attempts WHERE child_id=? AND ai_is_safe=1`, childID)
	if err != nil {
		return p, err
	}
	p.CompletedTaskIDs = []int{}
	for ids.Next() {
		var id int
		if err := ids.Scan(&id); err != nil {
			ids.Close()
			return p, err
		}
		p.CompletedTaskIDs = append(p.CompletedTaskIDs, id)
	}
	if err := ids.Err(); err != nil {
		ids.Close()
		return p, err
	}
	ids.Close()
	rows, err := r.DB.QueryContext(ctx, `SELECT t.category,COUNT(DISTINCT a.task_id) FROM task_attempts a JOIN tasks t ON t.id=a.task_id WHERE a.child_id=? AND a.ai_is_safe=1 GROUP BY t.category`, childID)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			rows.Close()
			return p, err
		}
		p.ByCategory[c] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return p, err
	}
	rows.Close()
	rows, err = r.DB.QueryContext(ctx, `SELECT activity_date FROM daily_activity WHERE child_id=?`, childID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	dates := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return p, err
		}
		dates[d] = true
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	cursor := today
	if !dates[date] {
		cursor = cursor.AddDate(0, 0, -1)
	}
	for dates[cursor.Format("2006-01-02")] {
		p.Streak++
		cursor = cursor.AddDate(0, 0, -1)
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

type InviteRepo struct{ DB *sql.DB }

func (r InviteRepo) Create(ctx context.Context, parentID int, hash string, expires time.Time) (models.InviteCode, error) {
	res, err := r.DB.ExecContext(ctx, `INSERT INTO invite_codes(parent_id,code_hash,expires_at) VALUES(?,?,?)`, parentID, hash, expires)
	if err != nil {
		return models.InviteCode{}, err
	}
	id, _ := res.LastInsertId()
	return models.InviteCode{ID: int(id), ParentID: parentID, ExpiresAt: expires}, nil
}
func (r InviteRepo) FindValid(ctx context.Context, hash string) (models.InviteCode, error) {
	var i models.InviteCode
	err := r.DB.QueryRowContext(ctx, `SELECT id,parent_id,code_hash,expires_at,used_by,created_at FROM invite_codes WHERE code_hash=? AND used_by IS NULL AND expires_at>?`, hash, time.Now()).Scan(&i.ID, &i.ParentID, &i.CodeHash, &i.ExpiresAt, &i.UsedBy, &i.CreatedAt)
	return i, err
}

func IsUniqueError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
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

func (r TaskRepo) Save(ctx context.Context, t models.SafetyTask) (models.SafetyTask, error) {
	choices, err := json.Marshal(t.Choices)
	if err != nil {
		return t, err
	}
	if t.ID == 0 {
		res, err := r.DB.ExecContext(ctx, `INSERT INTO tasks(category,scene,title,description,prompt,choices_json,difficulty,answer_mode,safety_tip,correct_choice) VALUES(?,?,?,?,?,?,?,?,?,?)`, t.Category, t.Scene, t.Title, t.Description, t.Prompt, string(choices), t.Difficulty, t.AnswerMode, t.SafetyTip, t.CorrectChoice)
		if err != nil {
			return t, err
		}
		id, err := res.LastInsertId()
		t.ID = int(id)
		return t, err
	}
	res, err := r.DB.ExecContext(ctx, `UPDATE tasks SET category=?,scene=?,title=?,description=?,prompt=?,choices_json=?,difficulty=?,answer_mode=?,safety_tip=?,correct_choice=? WHERE id=?`, t.Category, t.Scene, t.Title, t.Description, t.Prompt, string(choices), t.Difficulty, t.AnswerMode, t.SafetyTip, t.CorrectChoice, t.ID)
	if err != nil {
		return t, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return t, err
	}
	if n == 0 {
		return t, sql.ErrNoRows
	}
	return t, nil
}

func (r ActivityRepo) Submit(ctx context.Context, a models.TaskAttempt) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_attempts(child_id,task_id,answer,mode,ai_is_safe,ai_summary,ai_tip,ai_is_demo) VALUES(?,?,?,?,?,?,?,?)`, a.ChildID, a.TaskID, a.Answer, a.Mode, a.AiIsSafe, a.AiSummary, a.AiTip, a.AiIsDemo); err != nil {
		return err
	}
	increment := 0
	if a.AiIsSafe != nil && *a.AiIsSafe {
		increment = 1
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO daily_activity(child_id,activity_date,completed_count) VALUES(?,?,?) ON CONFLICT(child_id,activity_date) DO UPDATE SET completed_count=completed_count+excluded.completed_count`, a.ChildID, time.Now().Format("2006-01-02"), increment); err != nil {
		return err
	}
	return tx.Commit()
}
