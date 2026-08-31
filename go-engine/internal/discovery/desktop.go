// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxDesktopEntries   = 4096
	maxDesktopEntrySize = 256 << 10
)

type desktopFilesystem interface {
	ReadDir(string) ([]os.DirEntry, error)
	Lstat(string) (os.FileInfo, error)
	Open(string) (io.ReadCloser, error)
}

type hostDesktopFilesystem struct{}

func (hostDesktopFilesystem) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }
func (hostDesktopFilesystem) Lstat(path string) (os.FileInfo, error)     { return os.Lstat(path) }
func (hostDesktopFilesystem) Open(path string) (io.ReadCloser, error)    { return os.Open(path) }

// DesktopAdapter reads XDG desktop entries as user-facing application
// evidence. Higher-precedence user entries mask system entries with the same
// desktop ID, including Hidden tombstones.
type DesktopAdapter struct {
	Roots []string
	FS    desktopFilesystem
}

func (DesktopAdapter) ID() string { return "xdg-desktop-applications" }

func (adapter DesktopAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	roots := adapter.Roots
	if roots == nil {
		roots = defaultDesktopRoots()
	}
	filesystem := adapter.FS
	if filesystem == nil {
		filesystem = hostDesktopFilesystem{}
	}

	seen := make(map[string]bool)
	items := make([]InventoryItem, 0)
	ignored := 0
	availableRoots := 0
	visited := 0
	for _, root := range roots {
		root = filepath.Clean(root)
		entries, err := filesystem.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return AdapterInventory{}, fmt.Errorf("read XDG desktop application directory: %w", err)
		}
		availableRoots++
		rootItems, rootIgnored, err := walkDesktopEntries(ctx, filesystem, root, root, entries, seen, &visited)
		if err != nil {
			return AdapterInventory{}, err
		}
		items = append(items, rootItems...)
		ignored += rootIgnored
	}
	if availableRoots == 0 {
		return AdapterInventory{}, ErrNotAvailable
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ref < items[j].Ref })
	return AdapterInventory{Items: items, Ignored: ignored}, nil
}

func walkDesktopEntries(ctx context.Context, filesystem desktopFilesystem, root, directory string, entries []os.DirEntry, seen map[string]bool, visited *int) ([]InventoryItem, int, error) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	items := make([]InventoryItem, 0)
	ignored := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		*visited = *visited + 1
		if *visited > maxDesktopEntries {
			return nil, 0, fmt.Errorf("XDG desktop application inventory exceeds %d entries", maxDesktopEntries)
		}
		path := filepath.Join(directory, entry.Name())
		info, err := filesystem.Lstat(path)
		if err != nil {
			return nil, 0, fmt.Errorf("inspect XDG desktop application entry: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			ignored++
			continue
		}
		if info.IsDir() {
			children, readErr := filesystem.ReadDir(path)
			if readErr != nil {
				return nil, 0, fmt.Errorf("read nested XDG desktop application directory: %w", readErr)
			}
			childItems, childIgnored, walkErr := walkDesktopEntries(ctx, filesystem, root, path, children, seen, visited)
			if walkErr != nil {
				return nil, 0, walkErr
			}
			items = append(items, childItems...)
			ignored += childIgnored
			continue
		}
		if !info.Mode().IsRegular() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".desktop") {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, 0, fmt.Errorf("XDG desktop application entry escaped its data root")
		}
		id := strings.ReplaceAll(filepath.ToSlash(relative), "/", "-")
		if seen[id] {
			ignored++
			continue
		}
		seen[id] = true
		entryData, err := readDesktopEntry(filesystem, path, info.Size())
		if err != nil {
			return nil, 0, err
		}
		parsed, err := parseDesktopEntry(entryData)
		if err != nil {
			return nil, 0, fmt.Errorf("parse XDG desktop application %s: %w", id, err)
		}
		if parsed.Type != "Application" || parsed.Hidden || parsed.NoDisplay || parsed.Name == "" {
			ignored++
			continue
		}
		items = append(items, InventoryItem{
			Source: "desktop", Kind: EvidenceDesktop, Ref: id,
			DisplayName: parsed.Name, UserFacing: true,
		})
	}
	return items, ignored, nil
}

func readDesktopEntry(filesystem desktopFilesystem, path string, declaredSize int64) ([]byte, error) {
	if declaredSize < 0 || declaredSize > maxDesktopEntrySize {
		return nil, fmt.Errorf("XDG desktop application entry exceeds %d bytes", maxDesktopEntrySize)
	}
	file, err := filesystem.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open XDG desktop application entry: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDesktopEntrySize+1))
	if err != nil {
		return nil, fmt.Errorf("read XDG desktop application entry: %w", err)
	}
	if len(data) > maxDesktopEntrySize {
		return nil, fmt.Errorf("XDG desktop application entry exceeds %d bytes", maxDesktopEntrySize)
	}
	return data, nil
}

type parsedDesktopEntry struct {
	Type      string
	Name      string
	Hidden    bool
	NoDisplay bool
}

func parseDesktopEntry(data []byte) (parsedDesktopEntry, error) {
	entry := parsedDesktopEntry{}
	scanner := bufio.NewScanner(strings.NewReader(strings.ReplaceAll(string(data), "\r\n", "\n")))
	scanner.Buffer(make([]byte, 4096), maxDesktopEntrySize)
	inDesktopSection := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopSection = line == "[Desktop Entry]"
			continue
		}
		if !inDesktopSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return parsedDesktopEntry{}, fmt.Errorf("malformed key/value line")
		}
		switch key {
		case "Type":
			entry.Type = strings.TrimSpace(value)
		case "Name":
			entry.Name = strings.TrimSpace(value)
		case "Hidden":
			entry.Hidden = strings.EqualFold(strings.TrimSpace(value), "true")
		case "NoDisplay":
			entry.NoDisplay = strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	if err := scanner.Err(); err != nil {
		return parsedDesktopEntry{}, err
	}
	return entry, nil
}

func defaultDesktopRoots() []string {
	home, _ := os.UserHomeDir()
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" && home != "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := strings.TrimSpace(os.Getenv("XDG_DATA_DIRS"))
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	values := make([]string, 0)
	if filepath.IsAbs(dataHome) {
		values = append(values, filepath.Join(dataHome, "applications"))
	}
	for _, value := range filepath.SplitList(dataDirs) {
		value = strings.TrimSpace(value)
		if filepath.IsAbs(value) {
			values = append(values, filepath.Join(value, "applications"))
		}
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = filepath.Clean(value)
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
