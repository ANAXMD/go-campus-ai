package main

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
)

var Cache *redis.Client
var Ctx = context.Background()

func initCache() {
	// In Docker, the host is the exact name of the service in docker-compose.yml
	Cache = redis.NewClient(&redis.Options{
		Addr:     "redis-cache:6379",
		Password: "",
		DB:       0,
	})

	_, err := Cache.Ping(Ctx).Result()
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Redis Cache connection established securely.")
}
