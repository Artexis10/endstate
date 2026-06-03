// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package manifest provides JSONC manifest loading, include resolution, and
// profile validation for the Endstate engine.
package manifest

import (
	"bytes"
	"encoding/json"
)

// Manifest represents a fully-loaded Endstate provisioning manifest. The
// Version field is declared as interface{} so the validator can distinguish
// between a missing field, a wrong type, and the correct numeric 1.
type Manifest struct {
	Version        interface{}    `json:"version"`
	Name           string         `json:"name,omitempty"`
	Captured       string         `json:"captured,omitempty"`
	Apps           []App          `json:"apps"`
	Includes       []string       `json:"includes,omitempty"`
	Restore        []RestoreEntry `json:"restore,omitempty"`
	Verify         []VerifyEntry  `json:"verify,omitempty"`
	ConfigModules  []string       `json:"configModules,omitempty"`
	ExcludeConfigs []string       `json:"excludeConfigs,omitempty"`

	// HomeManager declares a home-manager configuration the Nix realizer activates
	// as a config stage of apply (opt-in via --enable-restore). Absent ⇒ no config
	// stage (default apply unchanged). Realizer-only; the winget path ignores it.
	HomeManager *HomeManagerConfig `json:"homeManager,omitempty"`
}

// HomeManagerConfig is the manifest input to the home-manager config stage.
//
// Flake is a home-manager flakeref (e.g. "/home/me/dotfiles#hugo" or
// "github:me/dotfiles#hugo") that the engine activates with an engine-owned,
// pinned home-manager — a permanent power-user escape hatch.
//
// Config is a path (resolved relative to the manifest) to a home.nix the engine
// wraps in a generated, pinned, identity-injected flake before activating it via
// the same stage — so the user supplies only their configuration, not the flake
// plumbing. The orchestrator is input-agnostic: Config is the first engine-
// generated input that produces a flakeref this stage consumes; a programs.*
// catalog layers on later the same way.
//
// Settings is the declarative, Endstate-native catalog input (see
// HomeManagerSettings): the user declares config in Endstate's own format and the
// engine compiles the home.nix Config would otherwise be, flowing through the same
// generated-flake activation.
//
// Flake, Config, and Settings are mutually exclusive (exactly one home-manager
// input); LoadManifest rejects a manifest that sets more than one.
type HomeManagerConfig struct {
	Flake    string               `json:"flake,omitempty"`
	Config   string               `json:"config,omitempty"`
	Settings *HomeManagerSettings `json:"settings,omitempty"`
}

// HomeManagerSettings is the declarative home-manager catalog input. It is a
// HYBRID — curated, engine-mapped concepts (Git, Shell, Direnv, Starship) that the
// engine translates to the correct home-manager options, plus a raw Programs
// passthrough forwarded to home-manager verbatim — together with a Files map for
// placing arbitrary files (text or binary) via home-manager.
//
// Unknown keys WITHIN a curated concept are rejected at load (see UnmarshalJSON):
// a typo like git.usrName must fail loudly, not silently drop. Programs and Files
// stay permissive (any home-manager program / any file target).
type HomeManagerSettings struct {
	Git      *GitSettings      `json:"git,omitempty"`
	Shell    *ShellSettings    `json:"shell,omitempty"`
	Direnv   *ProgramToggle    `json:"direnv,omitempty"`
	Starship *ProgramToggle    `json:"starship,omitempty"`
	Fzf      *ProgramToggle    `json:"fzf,omitempty"`
	Zoxide   *ProgramToggle    `json:"zoxide,omitempty"`
	Bat      *BatSettings      `json:"bat,omitempty"`
	Tmux     *TmuxSettings     `json:"tmux,omitempty"`
	SSH      *SSHSettings      `json:"ssh,omitempty"`
	Programs map[string]any    `json:"programs,omitempty"` // raw home-manager passthrough
	Files    map[string]string `json:"files,omitempty"`    // target path -> source path (relative to manifest)
}

// UnmarshalJSON decodes the settings block with unknown-field rejection so a
// mistyped curated key fails to load. DisallowUnknownFields applies recursively to
// the typed curated structs (Git/Shell/ProgramToggle); the Programs/Files maps are
// unaffected (maps have no "unknown fields"), keeping the raw passthrough open.
func (s *HomeManagerSettings) UnmarshalJSON(data []byte) error {
	type alias HomeManagerSettings // shed the custom method to avoid recursion
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var a alias
	if err := dec.Decode(&a); err != nil {
		return err
	}
	*s = HomeManagerSettings(a)
	return nil
}

// GitSettings are the curated git concepts the engine maps to home-manager's
// stable programs.git.extraConfig (which insulates the user from option renames).
type GitSettings struct {
	UserName      string `json:"userName,omitempty"`
	UserEmail     string `json:"userEmail,omitempty"`
	DefaultBranch string `json:"defaultBranch,omitempty"`
}

// ShellSettings are shell-agnostic curated concepts mapped to home.shellAliases /
// home.sessionVariables.
type ShellSettings struct {
	Aliases          map[string]string `json:"aliases,omitempty"`
	SessionVariables map[string]string `json:"sessionVariables,omitempty"`
}

// ProgramToggle is a curated enable flag for a single home-manager program
// (mapped to programs.<name>.enable — e.g. direnv, starship, fzf, zoxide).
type ProgramToggle struct {
	Enable bool `json:"enable,omitempty"`
}

// BatSettings are the curated bat concepts mapped to home-manager's
// programs.bat.enable plus programs.bat.config (a stable key→value attrset of
// bat config entries forwarded verbatim).
type BatSettings struct {
	Enable bool              `json:"enable,omitempty"`
	Config map[string]string `json:"config,omitempty"`
}

// TmuxSettings are the curated tmux concepts mapped to programs.tmux.enable plus
// programs.tmux.extraConfig — the raw tmux.conf string, which is the stable surface
// that insulates the user from home-manager tmux option renames.
type TmuxSettings struct {
	Enable      bool   `json:"enable,omitempty"`
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// SSHSettings are the curated ssh concepts mapped to programs.ssh.enable plus
// programs.ssh.extraConfig — the raw ssh config string, the stable surface that
// insulates the user from home-manager ssh option renames.
type SSHSettings struct {
	Enable      bool   `json:"enable,omitempty"`
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// App represents a single application entry in the manifest. The Refs map
// holds platform-specific package identifiers (e.g. "windows": "Vendor.App").
type App struct {
	ID          string            `json:"id"`
	Refs        map[string]string `json:"refs"`
	Driver      string            `json:"driver,omitempty"`
	Version     string            `json:"version,omitempty"`
	Manual      *ManualApp        `json:"manual,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
}

// ManualApp describes an app that cannot be installed automatically but can be
// verified as present via a filesystem path check.
type ManualApp struct {
	VerifyPath   string `json:"verifyPath"`
	Launch       string `json:"launch,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Fallback     string `json:"fallback,omitempty"`
}

// RestoreEntry describes a single configuration restore operation.
type RestoreEntry struct {
	Type       string   `json:"type"`
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	Pattern    string   `json:"pattern,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Backup     bool     `json:"backup,omitempty"`
	Optional   bool     `json:"optional,omitempty"`
	Exclude    []string `json:"exclude,omitempty"`
	FromModule string   `json:"fromModule,omitempty"`
}

// VerifyEntry describes a single state assertion.
type VerifyEntry struct {
	Type      string `json:"type"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	ValueName string `json:"valueName,omitempty"`
}
