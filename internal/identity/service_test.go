package identity

import (
	"context"
	"errors"
	"testing"

	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
)

func TestLoginFailsClosedWithoutRedis(t *testing.T) {
	for _, service := range []Service{{}, {Redis: &redisstore.Client{}}} {
		if err := service.checkLoginLimit(context.Background(), "enterprise", "user", "127.0.0.1"); !errors.Is(err, ErrLoginDependency) {
			t.Fatalf("checkLoginLimit() error = %v, want ErrLoginDependency", err)
		}
	}
}
