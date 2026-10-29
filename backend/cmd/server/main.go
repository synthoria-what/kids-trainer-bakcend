package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/kids-trainer/backend/internal/auth"
	"github.com/kids-trainer/backend/internal/config"
	"github.com/kids-trainer/backend/internal/database"
	"github.com/kids-trainer/backend/internal/handlers"
	"github.com/kids-trainer/backend/internal/middleware"
	"github.com/kids-trainer/backend/internal/repositories"
	"github.com/kids-trainer/backend/internal/services"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	authService := auth.New(cfg.JWTSecret, cfg.JWTTL)
	h := handlers.New(handlers.Handler{AdminLogin: cfg.AdminLogin, AdminPassword: cfg.AdminPassword, DevMode: cfg.DevMode, Users: repositories.UserRepo{DB: db}, Tasks: repositories.TaskRepo{DB: db}, Activity: repositories.ActivityRepo{DB: db}, Invites: repositories.InviteRepo{DB: db}, Auth: authService, AI: services.NewAIService(cfg)})
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS(cfg.CORSOrigins))
	handlers.RegisterRoutes(r, h)
	log.Printf("safe-route backend listening on %s", cfg.HTTPAddr)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
