package cache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MemoryTestSuite struct {
	suite.Suite
	memory *Memory
}

func TestMemoryTestSuite(t *testing.T) {
	suite.Run(t, new(MemoryTestSuite))
}

func (s *MemoryTestSuite) SetupTest() {
	s.memory = &Memory{}
}

func (s *MemoryTestSuite) TestAdd() {
	s.NoError(s.memory.Put("name", "Rat", 50*time.Millisecond))
	s.False(s.memory.Add("name", "World", 50*time.Millisecond))
	s.True(s.memory.Add("name1", "World", 50*time.Millisecond))
	s.True(s.memory.Has("name1"))
	time.Sleep(100 * time.Millisecond)
	s.False(s.memory.Has("name1"))
	s.True(s.memory.Flush())
}

func (s *MemoryTestSuite) TestAddDoesNotEvictExistingEntry() {
	s.True(s.memory.Forever("keep", "v"))
	s.False(s.memory.Add("keep", "x", 30*time.Millisecond))
	time.Sleep(80 * time.Millisecond)
	s.True(s.memory.Has("keep"))
	s.Equal("v", s.memory.GetString("keep"))
}

func (s *MemoryTestSuite) TestAddReplacesExpiredEntry() {
	s.NoError(s.memory.Put("exp-add", "old", 20*time.Millisecond))
	time.Sleep(50 * time.Millisecond)
	s.True(s.memory.Add("exp-add", "new", NoExpiration))
	s.Equal("new", s.memory.GetString("exp-add"))
}

func (s *MemoryTestSuite) TestDecrement() {
	res, err := s.memory.Decrement("decrement")
	s.Equal(int64(-1), res)
	s.NoError(err)

	s.Equal(int64(-1), s.memory.GetInt64("decrement"))

	res, err = s.memory.Decrement("decrement", 2)
	s.Equal(int64(-3), res)
	s.NoError(err)

	res, err = s.memory.Decrement("decrement1", 2)
	s.Equal(int64(-2), res)
	s.NoError(err)

	s.Equal(int64(-2), s.memory.GetInt64("decrement1"))

	s.True(s.memory.Add("decrement2", int64(4), 200*time.Millisecond))
	res, err = s.memory.Decrement("decrement2")
	s.Equal(int64(3), res)
	s.NoError(err)

	res, err = s.memory.Decrement("decrement2", 2)
	s.Equal(int64(1), res)
	s.NoError(err)
}

func (s *MemoryTestSuite) TestDecrementWithConcurrent() {
	res, err := s.memory.Decrement("decrement_concurrent")
	s.Equal(int64(-1), res)
	s.NoError(err)

	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			_, err := s.memory.Decrement("decrement_concurrent", 1)
			s.NoError(err)
		})
	}

	wg.Wait()

	s.Equal(int64(-1001), s.memory.GetInt64("decrement_concurrent"))
}

func (s *MemoryTestSuite) TestForever() {
	s.True(s.memory.Forever("name", "Rat"))
	s.Equal("Rat", s.memory.Get("name", "").(string))
	s.True(s.memory.Flush())
}

func (s *MemoryTestSuite) TestForget() {
	val := s.memory.Forget("test-forget")
	s.True(val)

	err := s.memory.Put("test-forget", "goravel", 200*time.Millisecond)
	s.NoError(err)
	s.True(s.memory.Forget("test-forget"))
}

func (s *MemoryTestSuite) TestFlush() {
	s.NoError(s.memory.Put("test-flush", "goravel", 200*time.Millisecond))
	s.Equal("goravel", s.memory.Get("test-flush", nil).(string))

	s.True(s.memory.Flush())
	s.False(s.memory.Has("test-flush"))
}

func (s *MemoryTestSuite) TestFlushConcurrentAccess() {
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			key := strconv.Itoa(i)
			s.NoError(s.memory.Put(key, i, NoExpiration))
			s.memory.Get(key)
		}
	})

	for range 100 {
		s.True(s.memory.Flush())
	}
	wg.Wait()
}

func (s *MemoryTestSuite) TestGet() {
	s.NoError(s.memory.Put("name", "Rat", 200*time.Millisecond))
	s.Equal("Rat", s.memory.Get("name", "").(string))
	s.Equal("World", s.memory.Get("name1", "World").(string))
	s.Equal("World1", s.memory.Get("name2", func() any {
		return "World1"
	}).(string))
	s.True(s.memory.Forget("name"))
	s.True(s.memory.Flush())
}

func (s *MemoryTestSuite) TestGetLazyDefault() {
	called := false
	s.Equal("lazy", s.memory.Get("missing", func() any {
		called = true
		return "lazy"
	}))
	s.True(called)

	// Only the exact signature func() any is treated as a lazy default;
	// any other function value is returned as-is.
	fn := func() string { return "x" }
	_, isFunc := s.memory.Get("missing", fn).(func() string)
	s.True(isFunc)
}

func (s *MemoryTestSuite) TestGetBool() {
	s.Equal(true, s.memory.GetBool("test-get-bool", true))
	s.NoError(s.memory.Put("test-get-bool", true, 200*time.Millisecond))
	s.Equal(true, s.memory.GetBool("test-get-bool", false))
}

func (s *MemoryTestSuite) TestGetInt() {
	s.Equal(2, s.memory.GetInt("test-get-int", 2))
	s.NoError(s.memory.Put("test-get-int", 3, 200*time.Millisecond))
	s.Equal(3, s.memory.GetInt("test-get-int", 2))
}

func (s *MemoryTestSuite) TestGetString() {
	s.Equal("2", s.memory.GetString("test-get-string", "2"))
	s.NoError(s.memory.Put("test-get-string", "3", 200*time.Millisecond))
	s.Equal("3", s.memory.GetString("test-get-string", "2"))
}

func (s *MemoryTestSuite) TestHas() {
	s.False(s.memory.Has("test-has"))
	s.NoError(s.memory.Put("test-has", "goravel", 200*time.Millisecond))
	s.True(s.memory.Has("test-has"))
}

func (s *MemoryTestSuite) TestIncrement() {
	res, err := s.memory.Increment("Increment")
	s.Equal(int64(1), res)
	s.NoError(err)

	s.Equal(int64(1), s.memory.GetInt64("Increment"))

	res, err = s.memory.Increment("Increment", 2)
	s.Equal(int64(3), res)
	s.NoError(err)

	res, err = s.memory.Increment("Increment1", 2)
	s.Equal(int64(2), res)
	s.NoError(err)

	s.Equal(int64(2), s.memory.GetInt64("Increment1"))

	s.True(s.memory.Add("Increment2", int64(1), 200*time.Millisecond))
	res, err = s.memory.Increment("Increment2")
	s.Equal(int64(2), res)
	s.NoError(err)

	res, err = s.memory.Increment("Increment2", 2)
	s.Equal(int64(4), res)
	s.NoError(err)
}

func (s *MemoryTestSuite) TestIncrementOnPutInteger() {
	s.NoError(s.memory.Put("counter", 5, NoExpiration))
	res, err := s.memory.Increment("counter")
	s.NoError(err)
	s.Equal(int64(6), res)

	s.NoError(s.memory.Put("counter32", int32(7), NoExpiration))
	res, err = s.memory.Decrement("counter32", 3)
	s.NoError(err)
	s.Equal(int64(4), res)

	s.NoError(s.memory.Put("not-int", "x", NoExpiration))
	_, err = s.memory.Increment("not-int")
	s.ErrorIs(err, ErrNotInteger)
}

func (s *MemoryTestSuite) TestIncrementKeepsExpiration() {
	s.NoError(s.memory.Put("ttl-counter", 1, 80*time.Millisecond))
	res, err := s.memory.Increment("ttl-counter")
	s.NoError(err)
	s.Equal(int64(2), res)

	time.Sleep(120 * time.Millisecond)
	s.False(s.memory.Has("ttl-counter"))
}

func (s *MemoryTestSuite) TestIncrementRestartsAfterExpiration() {
	s.NoError(s.memory.Put("expired-counter", 10, -time.Second))
	res, err := s.memory.Increment("expired-counter")
	s.NoError(err)
	s.Equal(int64(1), res)
}

func (s *MemoryTestSuite) TestIncrementWithConcurrent() {
	res, err := s.memory.Increment("increment_concurrent")
	s.Equal(int64(1), res)
	s.NoError(err)

	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			_, err := s.memory.Increment("increment_concurrent", 1)
			s.NoError(err)
		})
	}

	wg.Wait()

	s.Equal(int64(1001), s.memory.GetInt64("increment_concurrent"))
}

func (s *MemoryTestSuite) TestLock() {
	tests := []struct {
		name  string
		setup func()
	}{
		{
			name: "once got lock, lock can't be got again",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				lock1 := s.memory.Lock("lock")
				s.False(lock1.Get())

				lock.Release()
			},
		},
		{
			name: "lock can be got again when had been released",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				s.True(lock.Release())

				lock1 := s.memory.Lock("lock")
				s.True(lock1.Get())

				s.True(lock1.Release())
			},
		},
		{
			name: "lock cannot be released when had been got",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				lock1 := s.memory.Lock("lock")
				s.False(lock1.Get())
				s.False(lock1.Release())

				s.True(lock.Release())
			},
		},
		{
			name: "lock can be force released",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				lock1 := s.memory.Lock("lock")
				s.False(lock1.Get())
				s.False(lock1.Release())
				s.True(lock1.ForceRelease())

				// The original holder no longer owns the lock, so a
				// regular release must fail.
				s.False(lock.Release())
			},
		},
		{
			name: "lock can be got again when timeout",
			setup: func() {
				lock := s.memory.Lock("lock", 50*time.Millisecond)
				s.True(lock.Get())

				time.Sleep(100 * time.Millisecond)

				lock1 := s.memory.Lock("lock")
				s.True(lock1.Get())
				s.True(lock1.Release())

				// The expired lock cannot be released anymore.
				s.False(lock.Release())
			},
		},
		{
			name: "expired lock cannot release a competitor's lock",
			setup: func() {
				lock := s.memory.Lock("lock", 50*time.Millisecond)
				s.True(lock.Get())

				time.Sleep(100 * time.Millisecond)

				lock1 := s.memory.Lock("lock", 10*time.Second)
				s.True(lock1.Get())

				s.False(lock.Release())

				lock2 := s.memory.Lock("lock")
				s.False(lock2.Get())

				s.True(lock1.Release())
			},
		},
		{
			name: "lock cannot be released twice",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())
				s.True(lock.Release())
				s.False(lock.Release())
			},
		},
		{
			name: "callback outliving the ttl still reports success and runs once",
			setup: func() {
				calls := 0
				lock := s.memory.Lock("lock", 30*time.Millisecond)
				s.True(lock.Get(func() {
					calls++
					time.Sleep(80 * time.Millisecond)
				}))
				s.Equal(1, calls)
			},
		},
		{
			name: "block does not rerun the callback when it outlives the ttl",
			setup: func() {
				calls := 0
				lock := s.memory.Lock("lock", 30*time.Millisecond)
				s.True(lock.Block(300*time.Millisecond, func() {
					calls++
					time.Sleep(80 * time.Millisecond)
				}))
				s.Equal(1, calls)
			},
		},
		{
			name: "lock can be got again when had been released by callback",
			setup: func() {
				lock := s.memory.Lock("lock")
				called := false
				s.True(lock.Get(func() {
					called = true
				}))
				s.True(called)

				lock1 := s.memory.Lock("lock")
				s.True(lock1.Get())
				s.True(lock1.Release())
			},
		},
		{
			name: "block wait out",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				var wg sync.WaitGroup
				wg.Go(func() {
					lock1 := s.memory.Lock("lock")
					s.False(lock1.Block(300 * time.Millisecond))
				})

				wg.Wait()
				s.True(lock.Release())
			},
		},
		{
			name: "get lock by block when just timeout",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				var wg sync.WaitGroup
				wg.Go(func() {
					lock1 := s.memory.Lock("lock")
					s.True(lock1.Block(600 * time.Millisecond))
					s.True(lock1.Release())
				})

				time.Sleep(400 * time.Millisecond)
				s.True(lock.Release())
				wg.Wait()
			},
		},
		{
			name: "get lock by block",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				var wg sync.WaitGroup
				wg.Go(func() {
					lock1 := s.memory.Lock("lock")
					s.True(lock1.Block(1 * time.Second))
					s.True(lock1.Release())
				})

				time.Sleep(300 * time.Millisecond)
				s.True(lock.Release())
				wg.Wait()
			},
		},
		{
			name: "get lock by block with callback",
			setup: func() {
				lock := s.memory.Lock("lock")
				s.True(lock.Get())

				var wg sync.WaitGroup
				wg.Go(func() {
					lock1 := s.memory.Lock("lock")
					called := false
					s.True(lock1.Block(600*time.Millisecond, func() {
						called = true
					}))
					s.True(called)
				})

				time.Sleep(300 * time.Millisecond)
				s.True(lock.Release())
				wg.Wait()
			},
		},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.memory.Flush()
			test.setup()
		})
	}
}

func (s *MemoryTestSuite) TestPull() {
	s.NoError(s.memory.Put("name", "Rat", 200*time.Millisecond))
	s.True(s.memory.Has("name"))
	s.Equal("Rat", s.memory.Pull("name", "").(string))
	s.False(s.memory.Has("name"))

	s.Equal("default", s.memory.Pull("missing", "default"))

	s.NoError(s.memory.Put("expired", "v", -time.Second))
	s.Nil(s.memory.Pull("expired"))
}

func (s *MemoryTestSuite) TestPullIsAtomic() {
	s.NoError(s.memory.Put("pull-once", "v", NoExpiration))

	var got atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if s.memory.Pull("pull-once") != nil {
				got.Add(1)
			}
		})
	}
	wg.Wait()

	s.Equal(int32(1), got.Load())
}

func (s *MemoryTestSuite) TestPut() {
	s.NoError(s.memory.Put("name", "Rat", 50*time.Millisecond))
	s.True(s.memory.Has("name"))
	s.Equal("Rat", s.memory.Get("name", "").(string))
	time.Sleep(100 * time.Millisecond)
	s.False(s.memory.Has("name"))
}

func (s *MemoryTestSuite) TestPutOverwriteReplacesTTL() {
	// A short-lived entry overwritten by a permanent one must survive the
	// original TTL.
	s.NoError(s.memory.Put("k", "v1", 30*time.Millisecond))
	s.True(s.memory.Forever("k", "v2"))
	time.Sleep(80 * time.Millisecond)
	s.Equal("v2", s.memory.GetString("k"))

	// And the other way around: overwriting with a TTL must expire.
	s.NoError(s.memory.Put("k", "v3", 30*time.Millisecond))
	time.Sleep(80 * time.Millisecond)
	s.False(s.memory.Has("k"))
}

func (s *MemoryTestSuite) TestNegativeTTLExpiresImmediately() {
	s.NoError(s.memory.Put("negative", "v", -time.Second))
	s.False(s.memory.Has("negative"))
	s.Nil(s.memory.Get("negative"))
}

func (s *MemoryTestSuite) TestRemember() {
	s.NoError(s.memory.Put("name", "Rat", 100*time.Millisecond))
	value, err := s.memory.Remember("name", 100*time.Millisecond, func() (any, error) {
		return "World", nil
	})
	s.NoError(err)
	s.Equal("Rat", value)

	value, err = s.memory.Remember("name1", 50*time.Millisecond, func() (any, error) {
		return "World1", nil
	})
	s.NoError(err)
	s.Equal("World1", value)
	time.Sleep(100 * time.Millisecond)
	s.False(s.memory.Has("name1"))
	s.True(s.memory.Flush())

	value, err = s.memory.Remember("name2", 100*time.Millisecond, func() (any, error) {
		return nil, errors.New("error")
	})
	s.EqualError(err, "error")
	s.Nil(value)
}

func (s *MemoryTestSuite) TestRememberSharesConcurrentCalls() {
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			value, err := s.memory.Remember("shared", time.Minute, func() (any, error) {
				calls.Add(1)
				time.Sleep(50 * time.Millisecond)
				return "x", nil
			})
			s.NoError(err)
			s.Equal("x", value)
		})
	}
	wg.Wait()

	s.Equal(int32(1), calls.Load())
}

func (s *MemoryTestSuite) TestRememberCachesNilValue() {
	calls := 0
	value, err := s.memory.Remember("nil-key", time.Minute, func() (any, error) {
		calls++
		return nil, nil
	})
	s.NoError(err)
	s.Nil(value)

	value, err = s.memory.Remember("nil-key", time.Minute, func() (any, error) {
		calls++
		return "not-nil", nil
	})
	s.NoError(err)
	s.Nil(value)
	s.Equal(1, calls)
}

func (s *MemoryTestSuite) TestRememberForever() {
	s.NoError(s.memory.Put("name", "Rat", 100*time.Millisecond))
	value, err := s.memory.RememberForever("name", func() (any, error) {
		return "World", nil
	})
	s.NoError(err)
	s.Equal("Rat", value)

	value, err = s.memory.RememberForever("name1", func() (any, error) {
		return "World1", nil
	})
	s.NoError(err)
	s.Equal("World1", value)
	s.True(s.memory.Flush())

	value, err = s.memory.RememberForever("name2", func() (any, error) {
		return nil, errors.New("error")
	})
	s.EqualError(err, "error")
	s.Nil(value)
}

func (s *MemoryTestSuite) TestDeleteExpired() {
	s.NoError(s.memory.Put("expired", 1, -time.Second))
	s.True(s.memory.Forever("alive", 2))

	s.memory.DeleteExpired()

	_, exist := s.memory.items.Load("expired")
	s.False(exist)
	_, exist = s.memory.items.Load("alive")
	s.True(exist)
}

func TestGetAs(t *testing.T) {
	m := &Memory{}
	m.Forever("s", "hello")

	require.Equal(t, "hello", GetAs[string](m, "s"))
	require.Equal(t, 0, GetAs[int](m, "s"))
	require.Equal(t, 42, GetAs(m, "s", 42))
	require.Equal(t, "d", GetAs(m, "missing", "d"))

	type user struct{ Name string }
	m.Forever("u", user{Name: "n"})
	require.Equal(t, user{Name: "n"}, GetAs[user](m, "u"))
	require.Nil(t, GetAs[*user](m, "u"))
}

func TestNewCacheJanitorSweepsExpiredEntries(t *testing.T) {
	c := NewCache(WithCleanupInterval(10 * time.Millisecond))
	w, ok := c.(*janitorCache)
	require.True(t, ok)

	require.NoError(t, c.Put("k", "v", 5*time.Millisecond))

	// The entry must disappear from the underlying map without being
	// accessed, proving the janitor (not lazy expiration) removed it.
	require.Eventually(t, func() bool {
		_, exist := w.items.Load("k")
		return !exist
	}, time.Second, 10*time.Millisecond)
}

func TestNewCacheWithoutJanitor(t *testing.T) {
	c := NewCache(WithCleanupInterval(0))
	_, ok := c.(*Memory)
	require.True(t, ok)

	require.NoError(t, c.Put("k", "v", 5*time.Millisecond))
	time.Sleep(20 * time.Millisecond)
	require.False(t, c.Has("k"))
}

func TestWithContext(t *testing.T) {
	c := NewCache(WithCleanupInterval(0))
	c2 := c.WithContext(context.Background())
	require.NoError(t, c.Put("k", "v", NoExpiration))
	require.Equal(t, "v", c2.GetString("k"))

	// The janitor-backed cache keeps returning the wrapper so the janitor's
	// lifetime stays tied to the instance the caller holds.
	cw := NewCache(WithCleanupInterval(time.Minute))
	_, ok := cw.WithContext(context.Background()).(*janitorCache)
	require.True(t, ok)
}
