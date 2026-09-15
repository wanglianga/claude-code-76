package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client

func initRedis(addr string) {
	rdb = redis.NewClient(&redis.Options{Addr: addr})
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := rdb.Ping(ctx).Err()
		cancel()
		if err == nil {
			log.Println("redis connected")
			return
		}
		log.Printf("waiting for redis (%d/30): %v", i+1, err)
		time.Sleep(2 * time.Second)
	}
	log.Fatal("cannot connect to redis")
}

// ---- 登录会话（Redis 存储，24h 过期）----

func sessSet(token string, u *SessionUser) {
	b, _ := json.Marshal(u)
	rdb.Set(context.Background(), "sess:"+token, b, 24*time.Hour)
}

func sessGet(ctx context.Context, token string) (*SessionUser, error) {
	b, err := rdb.Get(ctx, "sess:"+token).Bytes()
	if err != nil {
		return nil, err
	}
	var u SessionUser
	if err := json.Unmarshal(b, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func sessDel(token string) {
	rdb.Del(context.Background(), "sess:"+token)
}

// ---- 通用缓存 ----

func cacheGet(key string, dst any) bool {
	b, err := rdb.Get(context.Background(), key).Bytes()
	if err != nil {
		return false
	}
	return json.Unmarshal(b, dst) == nil
}

func cacheSet(key string, v any, ttl time.Duration) {
	b, _ := json.Marshal(v)
	rdb.Set(context.Background(), key, b, ttl)
}

func cacheDel(keys ...string) {
	rdb.Del(context.Background(), keys...)
}
