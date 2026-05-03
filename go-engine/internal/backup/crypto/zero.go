// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package crypto

// zeroBytes overwrites b with zeroes. This is defense-in-depth and a
// code-review signal that the developer thought about post-use hygiene —
// not a hard guarantee. Go's escape analysis, garbage collection, and
// stack/heap behaviour mean a sensitive value may have been copied
// elsewhere before zeroBytes runs. Use it where it costs nothing, not as
// a stand-in for a stronger memory model.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
