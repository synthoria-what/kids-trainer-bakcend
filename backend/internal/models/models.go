package models

import "time"

type UserRole string

const (
	RoleChild  UserRole = "child"
	RoleParent UserRole = "parent"
)

type SafetyCategory string

const (
	CategoryCity        SafetyCategory = "city"
	CategoryCountryside SafetyCategory = "countryside"
	CategoryHome        SafetyCategory = "home"
)

type AnswerMode string

const (
	ModeChoices AnswerMode = "choices"
	ModeFree    AnswerMode = "free"
)

type TaskDifficulty string

const (
	DiffEasy   TaskDifficulty = "easy"
	DiffMedium TaskDifficulty = "medium"
	DiffHard   TaskDifficulty = "hard"
)

type User struct {
	ID           int       `json:"id"`
	Login        string    `json:"login"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
}

type ChildProfile struct {
	ID         int       `json:"id"`
	UserID     int       `json:"user_id"`
	Name       string    `json:"name"`
	Age        int       `json:"age"`
	ParentID   int       `json:"parent_id"`
	InviteCode string    `json:"invite_code"`
	CreatedAt  time.Time `json:"created_at"`
}

type SafetyTask struct {
	CorrectChoice *int           `json:"-"`
	ID            int            `json:"id"`
	Category      SafetyCategory `json:"category"`
	Scene         string         `json:"scene"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Prompt        string         `json:"prompt"`
	Choices       []string       `json:"choices"`
	Difficulty    TaskDifficulty `json:"difficulty"`
	AnswerMode    AnswerMode     `json:"answer_mode"`
	SafetyTip     string         `json:"safety_tip"`
}

type TaskAttempt struct {
	ID        int        `json:"id"`
	ChildID   int        `json:"child_id"`
	TaskID    int        `json:"task_id"`
	Answer    string     `json:"answer"`
	Mode      AnswerMode `json:"mode"`
	AiIsSafe  *bool      `json:"ai_is_safe"`
	AiSummary string     `json:"ai_summary"`
	AiTip     string     `json:"ai_tip"`
	AiIsDemo  bool       `json:"ai_is_demo"`
	CreatedAt time.Time  `json:"created_at"`
}

type DailyActivity struct {
	ID             int       `json:"id"`
	ChildID        int       `json:"child_id"`
	ActivityDate   time.Time `json:"activity_date"`
	CompletedCount int       `json:"completed_count"`
}

type InviteCode struct {
	ID        int       `json:"id"`
	ParentID  int       `json:"parent_id"`
	CodeHash  string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	UsedBy    *int      `json:"used_by"`
	CreatedAt time.Time `json:"created_at"`
}

type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Login    string   `json:"login"`
	Password string   `json:"password"`
	Role     UserRole `json:"role"`
	Name     string   `json:"name"`
}

type LoginResponse struct {
	Token     string   `json:"token"`
	Role      UserRole `json:"role"`
	Name      string   `json:"name"`
	ExpiresIn int64    `json:"expires_in"`
}

type ChildJoinRequest struct {
	Code string `json:"code"`
}

type ChildOnboardingRequest struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type CreateInviteRequest struct {
	ExpiresInMinutes int `json:"expires_in_minutes"`
}

type CreateInviteResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type AnalyzeRequest struct {
	TaskID int        `json:"task_id"`
	Answer string     `json:"answer"`
	Mode   AnswerMode `json:"mode"`
}

type AnalyzeResponse struct {
	IsSafe    bool   `json:"is_safe"`
	Score     int    `json:"score"`
	Summary   string `json:"summary"`
	Tip       string `json:"tip"`
	IsDemo    bool   `json:"is_demo"`
	Evaluated bool   `json:"evaluated"`
}

type SafetyDatasetEntry struct {
	ID        int    `json:"id"`
	Topic     string `json:"topic"`
	Situation string `json:"situation"`
	Solution  string `json:"solution"`
}

type TaskListResponse struct {
	Tasks []SafetyTask `json:"tasks"`
}

type GenerateTasksRequest struct {
	Count      int            `json:"count"`
	Category   SafetyCategory `json:"category"`
	Difficulty TaskDifficulty `json:"difficulty"`
	AnswerMode AnswerMode     `json:"answer_mode"`
}

type GenerateTasksResponse struct {
	Tasks []SafetyTask `json:"tasks"`
}

type ProgressResponse struct {
	CompletedTaskIDs []int          `json:"completed_task_ids,omitempty"`
	CompletedToday   int            `json:"completed_today"`
	TotalCompleted   int            `json:"total_completed"`
	Streak           int            `json:"streak"`
	ByCategory       map[string]int `json:"by_category"`
	CompletedDays    []int          `json:"completed_days"`
}
