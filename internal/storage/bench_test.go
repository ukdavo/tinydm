package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"tinydm/internal/storage"
)

var benchCtx = context.Background()

// benchSizes exercises a range of file sizes from small API payloads to
// larger documents typical of office files.
var benchSizes = []struct {
	label string
	bytes int
}{
	{"1KB", 1 << 10},
	{"64KB", 64 << 10},
	{"1MB", 1 << 20},
	{"16MB", 16 << 20},
}

func newBenchStore(b *testing.B) storage.Store {
	b.Helper()
	s, err := storage.NewLocal(b.TempDir())
	if err != nil {
		b.Fatalf("NewLocal: %v", err)
	}
	return s
}

// BenchmarkPut measures write throughput at each file size.
// Each iteration writes a new unique payload to avoid the dedup short-circuit.
func BenchmarkPut(b *testing.B) {
	for _, tc := range benchSizes {
		tc := tc
		b.Run(tc.label, func(b *testing.B) {
			s := newBenchStore(b)
			payload := bytes.Repeat([]byte("x"), tc.bytes)

			b.SetBytes(int64(tc.bytes))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payload[0] = byte(i) // make each payload unique → skip dedup
				if _, _, _, err := s.Put(benchCtx, bytes.NewReader(payload)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkGet measures read throughput — open + close (not full copy to /dev/null).
func BenchmarkGet(b *testing.B) {
	for _, tc := range benchSizes {
		tc := tc
		b.Run(tc.label, func(b *testing.B) {
			s := newBenchStore(b)
			payload := bytes.Repeat([]byte("y"), tc.bytes)
			key, _, _, err := s.Put(benchCtx, bytes.NewReader(payload))
			if err != nil {
				b.Fatal(err)
			}

			b.SetBytes(int64(tc.bytes))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rc, err := s.Get(benchCtx, key)
				if err != nil {
					b.Fatal(err)
				}
				rc.Close()
			}
		})
	}
}

// BenchmarkPut_Dedup measures the cost of writing identical content repeatedly.
// After the first write, the SHA-256 key already exists on disk;
// subsequent writes should be faster (stat + early return).
func BenchmarkPut_Dedup(b *testing.B) {
	s := newBenchStore(b)
	payload := bytes.Repeat([]byte("z"), 1<<20) // 1 MB
	if _, _, _, err := s.Put(benchCtx, bytes.NewReader(payload)); err != nil {
		b.Fatal(err)
	}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := s.Put(benchCtx, bytes.NewReader(payload)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPut_sizes is a synthetic sub-benchmark table formatted for
// easy comparison across sizes when run with -benchmem.
func BenchmarkPut_sizes(b *testing.B) {
	s := newBenchStore(b)
	for _, tc := range benchSizes {
		tc := tc
		payload := bytes.Repeat([]byte("p"), tc.bytes)
		b.Run(fmt.Sprintf("size=%s", tc.label), func(b *testing.B) {
			b.SetBytes(int64(tc.bytes))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				payload[0] = byte(i)
				if _, _, _, err := s.Put(benchCtx, bytes.NewReader(payload)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
