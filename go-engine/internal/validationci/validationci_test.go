// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import "testing"

func TestRunSyntheticShardRejectsInvalidShardBounds(t *testing.T) {
	_, err := RunSyntheticShard(ShardRequest{ShardCount: 8, Shard: 8})
	if err == nil {
		t.Fatal("RunSyntheticShard accepted an out-of-range shard")
	}
}
