package cache

import (
	"strconv"
	"testing"
	"time"
)

func BenchmarkPut(b *testing.B) {
	m := &Memory{}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = m.Put(strconv.Itoa(i%1024), i, time.Minute)
	}
}

func BenchmarkGet(b *testing.B) {
	m := &Memory{}
	for i := range 1024 {
		_ = m.Put(strconv.Itoa(i), i, time.Minute)
	}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		m.Get(strconv.Itoa(i % 1024))
	}
}

func BenchmarkGetParallel(b *testing.B) {
	m := &Memory{}
	for i := range 1024 {
		_ = m.Put(strconv.Itoa(i), i, time.Minute)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			m.Get(strconv.Itoa(i % 1024))
			i++
		}
	})
}

func BenchmarkIncrementParallel(b *testing.B) {
	m := &Memory{}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = m.Increment("counter")
		}
	})
}

func BenchmarkRememberHit(b *testing.B) {
	m := &Memory{}
	callback := func() (any, error) { return "value", nil }
	if _, err := m.Remember("key", time.Minute, callback); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.Remember("key", time.Minute, callback)
	}
}
