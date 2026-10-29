package handlers

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/kids-trainer/backend/internal/models"
)

func (h *Handler) RequireDevMode(c *gin.Context) {
	if !h.DevMode || h.AdminLogin == "" || len(h.AdminPassword) < 12 {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return
	}
	c.Next()
}
func (h *Handler) AdminLoginHandler(c *gin.Context) {
	if !h.DevMode || h.AdminLogin == "" || len(h.AdminPassword) < 12 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var in models.LoginRequest
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid login data"})
		return
	}
	expected := sha256.Sum256([]byte(h.AdminPassword))
	actual := sha256.Sum256([]byte(in.Password))
	if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 || in.Login != h.AdminLogin {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	token, ttl, err := h.Auth.Token(0, "admin")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	c.JSON(http.StatusOK, models.LoginResponse{Token: token, Role: models.UserRole("admin"), Name: "Разработчик", ExpiresIn: ttl})
}

type adminTask struct {
	models.SafetyTask
	CorrectChoice *int `json:"correct_choice"`
}

func (h *Handler) AdminTasks(c *gin.Context) {
	tasks, err := h.Tasks.List(c, "", "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load tasks"})
		return
	}
	out := []adminTask{}
	for _, t := range tasks {
		out = append(out, adminTask{t, t.CorrectChoice})
	}
	c.JSON(http.StatusOK, gin.H{"tasks": out})
}
func (h *Handler) SaveTask(c *gin.Context) {
	// Bound editable content and preserve existing attempts by updating in place.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128*1024)
	var in adminTask
	if c.ShouldBindJSON(&in) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task"})
		return
	}
	t := in.SafetyTask
	t.CorrectChoice = in.CorrectChoice
	t.ID = 0
	if c.Request.Method == http.MethodPut {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
			return
		}
		t.ID = id
	}
	if !validTask(t) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task"})
		return
	}
	t, err := h.Tasks.Save(c, t)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save task"})
		return
	}
	status := http.StatusOK
	if c.Request.Method == http.MethodPost {
		status = http.StatusCreated
	}
	c.JSON(status, adminTask{t, t.CorrectChoice})
}
func validTask(t models.SafetyTask) bool {
	if t.Category != models.CategoryCity && t.Category != models.CategoryCountryside && t.Category != models.CategoryHome {
		return false
	}
	if t.Difficulty != models.DiffEasy && t.Difficulty != models.DiffMedium && t.Difficulty != models.DiffHard {
		return false
	}
	if t.AnswerMode != models.ModeChoices && t.AnswerMode != models.ModeFree {
		return false
	}
	for _, value := range []string{t.Title, t.Scene, t.Description, t.Prompt, t.SafetyTip} {
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 6000 {
			return false
		}
	}
	if len(t.Choices) > 8 {
		return false
	}
	if len(t.Choices) == 0 {
		return t.AnswerMode == models.ModeFree && t.CorrectChoice == nil
	}
	if len(t.Choices) < 2 || t.CorrectChoice == nil || *t.CorrectChoice < 0 || *t.CorrectChoice >= len(t.Choices) {
		return false
	}
	seen := map[string]bool{}
	for _, choice := range t.Choices {
		value := strings.TrimSpace(choice)
		if value == "" || utf8.RuneCountInString(value) > 6000 || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

// All editing routes use JWT role middleware; public registration accepts only child/parent.
