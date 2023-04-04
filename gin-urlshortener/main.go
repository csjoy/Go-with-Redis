package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file:", err)
	}

	router := gin.Default()
	router.POST("/api/v1", shortenURL)
	router.Run("localhost:3000")
}

type request struct {
	URL         string        `json:"url"`
	CustomShort string        `json:"short"`
	Expiry      time.Duration `json:"expiry"`
}

type response struct {
	URL         string        `json:"url"`
	CustomShort string        `json:"short"`
	Expiry      time.Duration `json:"expiry"`
	RateLimit   int           `json:"rate_limit"`
	ResetLimit  time.Duration `json:"reset_limit"`
}

func shortenURL(c *gin.Context) {
	var body request
	err := c.BindJSON(&body)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, gin.H{"error": "cannot parse JSON"})
		return
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("DB_ADDR"),
		Password: os.Getenv("DB_PASS"),
		DB:       0,
	})
	defer rdb.Close()

	clientIP := c.ClientIP()

	val, _ := rdb.Get(ctx, clientIP).Result()
	if val == "" {
		rdb.Set(ctx, clientIP, os.Getenv("API_QUOTA"), 30*time.Minute).Err()
	} else {
		quota, _ := strconv.Atoi(val)

		if quota <= 0 {
			limit, _ := rdb.TTL(ctx, clientIP).Result()
			c.IndentedJSON(http.StatusServiceUnavailable, gin.H{"error": "Rate limit exceeded", "reset_limit": limit / time.Nanosecond / time.Minute})
			return
		}
	}

	var uid string
	if body.CustomShort == "" {
		uid = uuid.New().String()[:6]
	} else {
		uid = body.CustomShort
	}

	val, _ = rdb.Get(ctx, uid).Result()
	if val != "" {
		c.IndentedJSON(http.StatusForbidden, gin.H{"error": "URL custom short is already in use"})
		return
	}

	if body.Expiry == 0 {
		body.Expiry = 24
	}

	err = rdb.Set(ctx, uid, body.URL, body.Expiry*time.Hour).Err()
	if err != nil {
		c.IndentedJSON(http.StatusInternalServerError, gin.H{"error": "Unable to connect to server"})
		return
	}

	res := response{
		URL:         body.URL,
		CustomShort: "",
		Expiry:      body.Expiry,
		RateLimit:   10,
		ResetLimit:  30,
	}

	rdb.Decr(ctx, clientIP)
	val, _ = rdb.Get(ctx, clientIP).Result()
	res.RateLimit, _ = strconv.Atoi(val)
	ttl, _ := rdb.TTL(ctx, clientIP).Result()
	res.ResetLimit = ttl / time.Nanosecond / time.Minute
	res.CustomShort = os.Getenv("DOMAIN") + "/" + uid

	c.IndentedJSON(http.StatusOK, res)
}
