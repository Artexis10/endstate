// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package configrestore

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestUnixPublicationContractAroundDestinationDurability(t *testing.T) {
	temporary := filepath.Join("root", "journal", ".record.tmp")
	destination := filepath.Join("root", "journal", "record.json")
	syncFailure := errors.New("sync failed")
	removeFailure := errors.New("remove failed")

	t.Run("failed destination sync is cleanly undone", func(t *testing.T) {
		syncCalls := 0
		removed := ""
		state, err := publishFileNoReplaceUnix(temporary, destination, unixPublicationOps{
			link: func(from, to string) error { return nil },
			remove: func(path string) error {
				removed = path
				return nil
			},
			syncDirectory: func(path string) error {
				syncCalls++
				if syncCalls == 1 {
					return syncFailure
				}
				return nil
			},
		})
		if state != publicationNotDurable || !errors.Is(err, syncFailure) || removed != destination || syncCalls != 2 {
			t.Fatalf("state=%v err=%v removed=%q syncCalls=%d", state, err, removed, syncCalls)
		}
	})

	t.Run("failed cleanup after failed sync is ambiguous", func(t *testing.T) {
		state, err := publishFileNoReplaceUnix(temporary, destination, unixPublicationOps{
			link:   func(from, to string) error { return nil },
			remove: func(path string) error { return removeFailure },
			syncDirectory: func(path string) error {
				return syncFailure
			},
		})
		if state != publicationAmbiguous || !errors.Is(err, ErrPublicationAmbiguous) ||
			!errors.Is(err, syncFailure) || !errors.Is(err, removeFailure) {
			t.Fatalf("state=%v err=%v", state, err)
		}
	})

	t.Run("temporary cleanup cannot reverse durable success", func(t *testing.T) {
		syncCalls := 0
		state, err := publishFileNoReplaceUnix(temporary, destination, unixPublicationOps{
			link:   func(from, to string) error { return nil },
			remove: func(path string) error { return removeFailure },
			syncDirectory: func(path string) error {
				syncCalls++
				return nil
			},
		})
		if state != publicationDurable || err != nil || syncCalls != 1 {
			t.Fatalf("state=%v err=%v syncCalls=%d", state, err, syncCalls)
		}
	})
}
