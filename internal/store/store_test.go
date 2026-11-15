package store_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/schmenniooo/proxie/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMiniredis returns a Store backed by an in-process Redis.
func newMiniredis(t *testing.T) (store.Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	s, err := store.NewRedis(store.RedisOptions{
		URL:     &url.URL{Scheme: "redis", Host: mr.Addr()},
		Timeout: time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, mr
}

func TestStoreContract(t *testing.T) {
	s, _ := newMiniredis(t)
	ctx := t.Context()

	t.Run("miss is not an error", func(t *testing.T) {
		val, found, err := s.Get(ctx, "missing")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, val)
	})

	t.Run("round trip keeps bytes exactly", func(t *testing.T) {
		want := []byte("data: {\"a\":1}\n\n\x00\xff")
		require.NoError(t, s.Set(ctx, "k", want, time.Minute))
		got, found, err := s.Get(ctx, "k")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, want, got)
	})

	t.Run("set overwrites", func(t *testing.T) {
		require.NoError(t, s.Set(ctx, "k2", []byte("old"), time.Minute))
		require.NoError(t, s.Set(ctx, "k2", []byte("new"), time.Minute))
		got, found, err := s.Get(ctx, "k2")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []byte("new"), got)
	})

	t.Run("empty value is a hit", func(t *testing.T) {
		require.NoError(t, s.Set(ctx, "empty", []byte{}, time.Minute))
		got, found, err := s.Get(ctx, "empty")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Empty(t, got)
	})

	t.Run("ping", func(t *testing.T) {
		require.NoError(t, s.Ping(ctx))
	})
}
