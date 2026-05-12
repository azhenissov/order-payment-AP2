package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func RateLimiter(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		
		ip := c.ClientIP()
		key := fmt.Sprintf("rate_limit:%s", ip)
		ctx := context.Background()

		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			log.Printf("Rate limit warning (Redis unreachable): %v", err)
			c.Next()
			return
		}

		if count == 1 {
			rdb.Expire(ctx, key, window)
		}
		if count > int64(limit) {
			log.Printf("Rate limit exceeded for IP: %s (Requests: %d)", ip, count)
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "429 Too Many Requests: Rate limit exceeded",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}