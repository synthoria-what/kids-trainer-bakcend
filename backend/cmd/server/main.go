package main

import (
	"context"
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
	client, err := database.Open(cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			log.Printf("mongo disconnect: %v", err)
		}
	}()
	if err := database.Migrate(client, cfg.MongoDB); err != nil {
		log.Fatal(err)
	}
	if err := database.SeedExtraTasks(client, cfg.MongoDB); err != nil {
		log.Printf("seed extra tasks: %v", err)
	}
	authService := auth.New(cfg.JWTSecret, cfg.JWTTL)
	h := handlers.New(handlers.Handler{
		AdminLogin: cfg.AdminLogin, AdminPassword: cfg.AdminPassword, DevMode: cfg.DevMode,
		Users:    repositories.UserRepo{Client: client, DBName: cfg.MongoDB},
		Tasks:    repositories.TaskRepo{Client: client, DBName: cfg.MongoDB},
		Activity: repositories.ActivityRepo{Client: client, DBName: cfg.MongoDB},
		Invites:  repositories.InviteRepo{Client: client, DBName: cfg.MongoDB},
		Auth:     authService,
		AI:       services.NewAIService(cfg),
	})
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), middleware.CORS(cfg.CORSOrigins))
	handlers.RegisterRoutes(r, h)
	log.Printf("safe-route backend listening on %s (mongo: %s/%s)", cfg.HTTPAddr, cfg.MongoURI, cfg.MongoDB)
	if err := r.Run(cfg.HTTPAddr); err != nil {
		log.Fatal(err)
	}
}
