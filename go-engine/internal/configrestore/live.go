// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const configMutationLockPoll = 50 * time.Millisecond

// Guard is the live config-mutation capability. It retains the process-wide
// store lock from pending recovery through journal completion.
type Guard struct {
	mu               sync.Mutex
	lock             *flock.Flock
	storeRoot        string
	transactions     string
	legacyMembers    string
	legacyReverts    string
	legacyRevertWork string
	runID            string
	restoreRunID     string
	runStartedAt     time.Time
	nextOrdinal      uint64
	registry         RegistryMutator
	closed           bool
}

// BeginLive acquires the global config-mutation lock for stateDir, recovers
// every pending generation transaction, and returns while retaining the lock.
func BeginLive(ctx context.Context, stateDir, runID string, registry RegistryMutator) (*Guard, error) {
	if ctx == nil {
		return nil, fmt.Errorf("live config restore context is nil")
	}
	if stateDir == "" || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir {
		return nil, fmt.Errorf("state directory must be a clean absolute path")
	}
	if runID == "" || runID != strings.TrimSpace(runID) || containsControl(runID) {
		return nil, fmt.Errorf("run ID is invalid")
	}
	configRoot := filepath.Join(stateDir, "config-restore")
	storeRoot := filepath.Join(configRoot, "v1")
	transactions := filepath.Join(storeRoot, "transactions")
	legacyMembers := filepath.Join(storeRoot, "legacy-members")
	legacyReverts := filepath.Join(storeRoot, "legacy-reverts")
	legacyRevertWork := filepath.Join(storeRoot, "legacy-revert-work")
	if err := ensureStoreDirectories(stateDir, configRoot, storeRoot, transactions, legacyMembers, legacyReverts, legacyRevertWork); err != nil {
		return nil, fmt.Errorf("initialize config restore store: %w", err)
	}

	lockPath := filepath.Join(configRoot, "mutation.lock")
	if err := validateMutationLockPath(lockPath, true); err != nil {
		return nil, fmt.Errorf("validate config mutation lock: %w", err)
	}
	processLock := flock.New(lockPath)
	locked, err := processLock.TryLockContext(ctx, configMutationLockPoll)
	if err != nil {
		return nil, fmt.Errorf("acquire config mutation lock: %w", err)
	}
	if !locked {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("acquire config mutation lock: %w", ctxErr)
		}
		return nil, fmt.Errorf("acquire config mutation lock: lock unavailable")
	}
	if err := validateMutationLockPath(lockPath, false); err != nil {
		_ = processLock.Unlock()
		return nil, fmt.Errorf("validate acquired config mutation lock: %w", err)
	}
	releaseOnFailure := true
	defer func() {
		if releaseOnFailure {
			_ = processLock.Unlock()
		}
	}()

	restoreRunID, err := newOpaqueStoreID()
	if err != nil {
		return nil, fmt.Errorf("create restore run identity: %w", err)
	}
	guard := &Guard{
		lock: processLock, storeRoot: storeRoot, transactions: transactions,
		legacyMembers: legacyMembers, legacyReverts: legacyReverts, legacyRevertWork: legacyRevertWork,
		runID: runID, restoreRunID: restoreRunID, runStartedAt: time.Now().UTC(), registry: registry,
	}
	if err := guard.recoverPending(ctx); err != nil {
		return nil, err
	}
	releaseOnFailure = false
	return guard, nil
}

// CreateTransactionRoot creates one immutable store descriptor and returns the
// safe root consumed by snapshot and journal preparation.
func (g *Guard) CreateTransactionRoot(captureID string) (string, error) {
	if g == nil {
		return "", fmt.Errorf("live config restore guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.lock == nil || !g.lock.Locked() {
		return "", fmt.Errorf("live config restore guard is closed")
	}
	if captureID == "" || captureID != strings.TrimSpace(captureID) || containsControl(captureID) {
		return "", fmt.Errorf("capture ID is invalid")
	}
	for attempts := 0; attempts < 8; attempts++ {
		transactionID, err := newOpaqueStoreID()
		if err != nil {
			return "", err
		}
		root := filepath.Join(g.transactions, transactionID)
		if err := os.Mkdir(root, 0o700); os.IsExist(err) {
			continue
		} else if err != nil {
			return "", fmt.Errorf("create transaction root: %w", err)
		}
		if err := syncDurableDirectory(root); err != nil {
			return "", fmt.Errorf("sync transaction root: %w", err)
		}
		if err := syncDurableDirectory(g.transactions); err != nil {
			return "", fmt.Errorf("sync transaction store: %w", err)
		}
		_, encoded, err := newTransactionDescriptor(
			transactionID, g.restoreRunID, g.runID, g.runStartedAt, g.nextOrdinal, captureID,
		)
		if err != nil {
			return "", err
		}
		if err := publishStoreRecord(root, "transaction.json", encoded); err != nil {
			return "", fmt.Errorf("publish transaction descriptor: %w", err)
		}
		g.nextOrdinal++
		return root, nil
	}
	return "", fmt.Errorf("could not allocate a unique transaction identity")
}

// DiscardTransactionRoot removes a preallocated transaction only while it has
// no durable journal intent. Once intent exists, recovery/history owns it.
func (g *Guard) DiscardTransactionRoot(root string) error {
	if g == nil {
		return fmt.Errorf("live config restore guard is nil")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.requireLeaseLocked(); err != nil {
		return err
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) != g.transactions ||
		!isOpaqueStoreID(filepath.Base(root)) {
		return fmt.Errorf("valid transaction root is required")
	}
	if err := rejectExistingTargetLinks(root); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(root, "journal", "intent.json")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, _, err := readStoredTransactionDescriptor(root); err != nil {
		return err
	}
	return removeSafeTransactionPath(context.Background(), root)
}

// Close releases the process-wide mutation lock. It is idempotent.
func (g *Guard) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	if g.lock == nil {
		g.closed = true
		return nil
	}
	if err := g.lock.Unlock(); err != nil {
		return err
	}
	g.closed = true
	return nil
}

func validateMutationLockPath(path string, allowMissing bool) error {
	if err := rejectExistingTargetLinks(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && allowMissing {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || isLinkOrReparse(info) {
		return fmt.Errorf("mutation lock must be a safe regular file")
	}
	return nil
}

func ensureStoreDirectories(paths ...string) error {
	for _, path := range paths {
		if err := rejectExistingTargetLinks(path); err != nil {
			return err
		}
		created := false
		if err := os.Mkdir(path, 0o700); err == nil {
			created = true
		} else if !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || isLinkOrReparse(info) {
			return fmt.Errorf("store path %q is not a safe directory", path)
		}
		if created {
			if err := syncDurableDirectory(path); err != nil {
				return err
			}
			parent := filepath.Dir(path)
			if parent != path {
				if err := syncDurableDirectory(parent); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func newOpaqueStoreID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func publishStoreRecord(directory, name string, data []byte) (resultErr error) {
	if err := rejectExistingTargetLinks(directory); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".store-record-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	publication, err := publishFileNoReplace(temporaryPath, filepath.Join(directory, name))
	if err != nil {
		return err
	}
	if publication != publicationDurable {
		return errors.Join(ErrPublicationAmbiguous, fmt.Errorf("store record publication was not durable"))
	}
	return nil
}
