package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kids-trainer/backend/internal/auth"
	"github.com/kids-trainer/backend/internal/middleware"
	"github.com/kids-trainer/backend/internal/models"
	"github.com/kids-trainer/backend/internal/repositories"
	"github.com/kids-trainer/backend/internal/services"
)

type Handler struct {
	AdminLogin    string
	AdminPassword string
	DevMode       bool
	Users         repositories.UserRepo
	Tasks         repositories.TaskRepo
	Activity      repositories.ActivityRepo
	Invites       repositories.InviteRepo
	Auth          *auth.Service
	AI            *services.AIService
}

func New(h Handler) *Handler { return &h }

func (h *Handler) Register(c *gin.Context) {
	var in models.RegisterRequest
	if c.ShouldBindJSON(&in) != nil || !repositories.ValidateRole(in.Role) || strings.TrimSpace(in.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid registration data"})
		return
	}
	if err := repositories.ValidateCredentials(in.Login, in.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := h.Users.Create(c, models.User{Login: in.Login, PasswordHash: hash, Role: in.Role, Name: in.Name})
	if repositories.IsUniqueError(err) {
		c.JSON(http.StatusConflict, gin.H{"error": "login is already registered"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create user"})
		return
	}

	token, ttl, _ := h.Auth.Token(id, string(in.Role))
	c.JSON(http.StatusCreated, models.LoginResponse{Token: token, Role: in.Role, Name: in.Name, ExpiresIn: ttl})
}

func (h *Handler) Login(c *gin.Context) {
	var in models.LoginRequest
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login data"})
		return
	}
	u, err := h.Users.ByLogin(c, in.Login)
	if err != nil || !auth.CheckPassword(u.PasswordHash, in.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, ttl, _ := h.Auth.Token(u.ID, string(u.Role))
	c.JSON(http.StatusOK, models.LoginResponse{Token: token, Role: u.Role, Name: u.Name, ExpiresIn: ttl})
}

func (h *Handler) Me(c *gin.Context) {
	role, _ := c.Get("role")
	if role == "admin" {
		if !h.DevMode {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": 0, "name": "Разработчик", "role": "admin"})
		return
	}
	u, err := h.Users.ByID(c, middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	out := gin.H{"id": u.ID, "name": u.Name, "role": u.Role, "login": u.Login}
	palette, err := h.Users.Palette(c, u.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load preferences"})
		return
	}
	out["palette_index"] = palette
	if u.Role == models.RoleChild {
		p, err := h.Users.Profile(c, u.ID)
		if err == nil {
			out["child_profile"] = p
		} else if !errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load profile"})
			return
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) SavePreferences(c *gin.Context) {
	role, _ := c.Get("role")
	if role != "child" && role != "parent" {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}
	var in struct {
		PaletteIndex *int `json:"palette_index"`
	}
	if c.ShouldBindJSON(&in) != nil || in.PaletteIndex == nil || *in.PaletteIndex < 0 || *in.PaletteIndex > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "palette_index must be 0..5"})
		return
	}
	if err := h.Users.SavePalette(c, middleware.UserID(c), *in.PaletteIndex); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save preferences"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"palette_index": *in.PaletteIndex})
}

func (h *Handler) Onboard(c *gin.Context) {
	var in models.ChildOnboardingRequest
	if c.ShouldBindJSON(&in) != nil || in.Age < 6 || in.Age > 12 || strings.TrimSpace(in.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and age 6..12 are required"})
		return
	}
	if err := h.Users.UpdateProfile(c, middleware.UserID(c), strings.TrimSpace(in.Name), in.Age); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save profile"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) CreateInvite(c *gin.Context) {
	minutes := 10
	var in models.CreateInviteRequest
	if c.ShouldBindJSON(&in) == nil && in.ExpiresInMinutes > 0 && in.ExpiresInMinutes <= 60 {
		minutes = in.ExpiresInMinutes
	}
	code, err := auth.NewInviteCode()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create invite"})
		return
	}
	expires := time.Now().Add(time.Duration(minutes) * time.Minute)
	if _, err = h.Invites.Create(c, middleware.UserID(c), auth.HashCode(code), expires); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save invite"})
		return
	}
	c.JSON(http.StatusCreated, models.CreateInviteResponse{Code: code, ExpiresAt: expires})
}

func (h *Handler) Join(c *gin.Context) {
	var in models.ChildJoinRequest
	if c.ShouldBindJSON(&in) != nil || len(strings.TrimSpace(in.Code)) < 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code is required"})
		return
	}
	err := h.Users.LinkParent(c, middleware.UserID(c), auth.HashCode(strings.TrimSpace(in.Code)))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "code is invalid or expired"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not link accounts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) TasksList(c *gin.Context) {
	tasks, err := h.Tasks.List(c, c.Query("category"), c.Query("difficulty"), c.Query("mode"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load tasks"})
		return
	}
	c.JSON(http.StatusOK, models.TaskListResponse{Tasks: tasks})
}

func (h *Handler) GenerateTasks(c *gin.Context) {
	if h.AI == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "AI service is not configured"})
		return
	}
	var in models.GenerateTasksRequest
	if c.ShouldBindJSON(&in) != nil {
		in = models.GenerateTasksRequest{}
	}
	if in.Count < 1 || in.Count > 10 {
		in.Count = 3
	}
	examples, err := h.Tasks.List(c, string(in.Category), string(in.Difficulty), string(in.AnswerMode))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load task examples"})
		return
	}
	generated, err := h.AI.GenerateTasks(c, examples, in)
	if err != nil {
		log.Printf("generate tasks: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "task generation failed"})
		return
	}
	saved := make([]models.SafetyTask, 0, len(generated))
	for _, task := range generated {
		if task.Category == "" {
			task.Category = in.Category
		}
		if task.Difficulty == "" {
			task.Difficulty = in.Difficulty
		}
		if task.AnswerMode == "" {
			task.AnswerMode = in.AnswerMode
		}
		if !validTask(task) {
			continue
		}
		task.ID = 0
		created, saveErr := h.Tasks.Save(c, task)
		if saveErr != nil {
			log.Printf("save generated task: %v", saveErr)
			continue
		}
		saved = append(saved, created)
	}
	if len(saved) == 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI returned invalid tasks"})
		return
	}
	c.JSON(http.StatusCreated, models.GenerateTasksResponse{Tasks: saved})
}

func (h *Handler) NextTask(c *gin.Context) {
	task, err := h.Tasks.NextForChild(c, middleware.UserID(c), c.Query("category"), c.Query("difficulty"), c.Query("mode"))
	if err != nil {
		// After the child completes the current pool, refresh should still offer
		// another random practice task instead of leaving the home screen empty.
		task, err = h.Tasks.RandomTask(c, c.Query("category"), c.Query("difficulty"), c.Query("mode"))
	}
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no tasks available"})
		return
	}
	c.JSON(http.StatusOK, task)
}

func (h *Handler) Analyze(c *gin.Context) {
	var in models.AnalyzeRequest
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Answer) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id and answer are required"})
		return
	}
	user, err := h.Users.ByID(c, middleware.UserID(c))
	if err != nil || user.Role != models.RoleChild {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "child account not found; login again"})
		return
	}
	task, err := h.Tasks.ByID(c, in.TaskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if (in.Mode != models.ModeFree && in.Mode != models.ModeChoices) || len(in.Answer) > 12000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid answer mode"})
		return
	}
	if in.Mode == models.ModeChoices {
		found := false
		for _, choice := range task.Choices {
			if choice == in.Answer {
				found = true
			}
		}
		if !found {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid choice"})
			return
		}
	}
	result, err := h.AI.Analyze(c, task, in.Answer)
	if err != nil {
		log.Printf("AI analysis: task_id=%d: %v", in.TaskID, err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI analysis failed"})
		return
	}
	safe := result.Evaluated && result.IsSafe
	if err = h.Activity.Submit(c, models.TaskAttempt{ChildID: middleware.UserID(c), TaskID: in.TaskID, Answer: in.Answer, Mode: in.Mode, AiIsSafe: &safe, AiSummary: result.Summary, AiTip: result.Tip, AiIsDemo: result.IsDemo}); err != nil {
		log.Printf("save attempt: child_id=%d task_id=%d: %v", middleware.UserID(c), in.TaskID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save attempt"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) AnalyzeStream(c *gin.Context) {
	var in models.AnalyzeRequest
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Answer) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id and answer are required"})
		return
	}
	user, err := h.Users.ByID(c, middleware.UserID(c))
	if err != nil || user.Role != models.RoleChild {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "child account not found; login again"})
		return
	}
	task, err := h.Tasks.ByID(c, in.TaskID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if (in.Mode != models.ModeFree && in.Mode != models.ModeChoices) || len(in.Answer) > 12000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid answer mode"})
		return
	}
	if in.Mode == models.ModeChoices && !containsChoice(task.Choices, in.Answer) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid choice"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming is not supported"})
		return
	}
	emit := func(piece string) error {
		payload, _ := json.Marshal(gin.H{"text": piece})
		_, err := c.Writer.Write([]byte("event: delta\ndata: " + string(payload) + "\n\n"))
		flusher.Flush()
		return err
	}
	result, err := h.AI.AnalyzeStream(c, task, in.Answer, emit)
	if err != nil {
		log.Printf("AI streaming analysis: task_id=%d: %v", in.TaskID, err)
		payload := gin.H{"message": "AI analysis failed"}
		encoded, _ := json.Marshal(payload)
		_, _ = c.Writer.Write([]byte("event: error\ndata: " + string(encoded) + "\n\n"))
		flusher.Flush()
		return
	}
	safe := result.Evaluated && result.IsSafe
	if err = h.Activity.Submit(c, models.TaskAttempt{ChildID: middleware.UserID(c), TaskID: in.TaskID, Answer: in.Answer, Mode: in.Mode, AiIsSafe: &safe, AiSummary: result.Summary, AiTip: result.Tip, AiIsDemo: result.IsDemo}); err != nil {
		log.Printf("save streamed attempt: %v", err)
		return
	}
	encoded, _ := json.Marshal(result)
	_, _ = c.Writer.Write([]byte("event: result\ndata: " + string(encoded) + "\n\n"))
	flusher.Flush()
}

func containsChoice(choices []string, answer string) bool {
	for _, choice := range choices {
		if choice == answer {
			return true
		}
	}
	return false
}

func (h *Handler) Progress(c *gin.Context) {
	p, err := h.Activity.Progress(c, middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load progress"})
		return
	}
	c.JSON(http.StatusOK, p)
}
func (h *Handler) ParentActivity(c *gin.Context) {
	children, err := h.Users.Children(c, middleware.UserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load activity"})
		return
	}
	p := models.ProgressResponse{ByCategory: map[string]int{}, CompletedDays: []int{}}
	days := map[int]bool{}
	for _, child := range children {
		v, err := h.Activity.Progress(c, child)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load activity"})
			return
		}
		p.CompletedToday += v.CompletedToday
		p.TotalCompleted += v.TotalCompleted
		if v.Streak > p.Streak {
			p.Streak = v.Streak
		}
		for k, n := range v.ByCategory {
			p.ByCategory[k] += n
		}
		for _, d := range v.CompletedDays {
			days[d] = true
		}
	}
	for d := 1; d <= 7; d++ {
		if days[d] {
			p.CompletedDays = append(p.CompletedDays, d)
		}
	}
	c.JSON(http.StatusOK, p)
}

func RegisterRoutes(r *gin.Engine, h *Handler) {
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	api := r.Group("/api/v1")
	api.POST("/auth/register", h.Register)
	api.POST("/auth/login", h.Login)
	api.POST("/auth/admin", h.AdminLoginHandler)
	secure := api.Group("")
	secure.Use(middleware.Auth(h.Auth))
	secure.GET("/me", h.Me)
	secure.PUT("/me/preferences", h.SavePreferences)
	admin := secure.Group("/admin")
	admin.Use(middleware.RequireRole("admin"), h.RequireDevMode)
	admin.GET("/tasks", h.AdminTasks)
	admin.POST("/tasks", h.SaveTask)
	admin.PUT("/tasks/:id", h.SaveTask)
	admin.POST("/tasks/generate", h.GenerateTasks)
	secure.POST("/child/tasks/generate", middleware.RequireRole("child"), h.GenerateTasks)
	secure.GET("/tasks", h.TasksList)
	secure.GET("/child/tasks/next", middleware.RequireRole("child"), h.NextTask)
	secure.POST("/child/onboarding", middleware.RequireRole("child"), h.Onboard)
	secure.POST("/child/join", middleware.RequireRole("child"), h.Join)
	secure.POST("/child/analyze", middleware.RequireRole("child"), h.Analyze)
	secure.POST("/child/analyze/stream", middleware.RequireRole("child"), h.AnalyzeStream)
	secure.GET("/child/progress", middleware.RequireRole("child"), h.Progress)
	secure.POST("/parent/invites", middleware.RequireRole("parent"), h.CreateInvite)
	secure.GET("/parent/activity", middleware.RequireRole("parent"), h.ParentActivity)
}
