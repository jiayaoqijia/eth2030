package eiptest_test

import (
	"os"
	"testing"

	"github.com/eth2030/eth2030/core/eftest"
	"github.com/eth2030/eth2030/eiptest"
)

func TestEESTVectorPipeline(t *testing.T) {
	dir := os.Getenv("EIPTEST_EEST_DIR")
	if dir == "" {
		dir = "eest"
	}
	fork := os.Getenv("EIPTEST_EEST_FORK")
	if fork == "" {
		fork = "Amsterdam"
	}
	vectors := eiptest.LoadVectors(t, dir)
	paths := make(map[string]bool)
	for _, vector := range vectors {
		paths[vector.Path] = true
	}
	count := 0
	for path := range paths {
		tests, err := eftest.LoadGethTests(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, test := range tests {
			for _, sub := range test.Subtests() {
				if sub.Fork != fork {
					continue
				}
				result := test.RunWithGeth(sub)
				if !result.Passed {
					t.Fatalf("%s/%s[%d]: %v", test.Name, fork, sub.Index, result.Error)
				}
				count++
			}
		}
	}
	if count == 0 {
		t.Fatalf("no %s state vectors were executed from %s", fork, dir)
	}
	t.Logf("executed %d %s state vectors", count, fork)
}
