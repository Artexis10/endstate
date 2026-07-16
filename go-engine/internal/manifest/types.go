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
// Version field is declared as interface{} so callers can distinguish a
// missing field, a wrong type, and the supported numeric versions 1 and 2.
type Manifest struct {
	Version        interface{}     `json:"version"`
	Name           string          `json:"name,omitempty"`
	Captured       string          `json:"captured,omitempty"`
	Apps           []App           `json:"apps"`
	Includes       []string        `json:"includes,omitempty"`
	Restore        []RestoreEntry  `json:"restore,omitempty"`
	Verify         []VerifyEntry   `json:"verify,omitempty"`
	ConfigModules  []string        `json:"configModules,omitempty"`
	ExcludeConfigs []string        `json:"excludeConfigs,omitempty"`
	ConfigCaptures []ConfigCapture `json:"configCaptures,omitempty"`
	// LegacyConfigLanes explicitly associates every flat schema-v1 payload
	// retained in a mixed manifest-v2 bundle with its module and isolated root.
	LegacyConfigLanes []LegacyConfigLane `json:"legacyConfigLanes,omitempty"`

	// HomeManager declares a home-manager configuration the Nix realizer activates
	// as a config stage of apply (opt-in via --enable-restore). Absent ⇒ no config
	// stage (default apply unchanged). Realizer-only; the winget path ignores it.
	HomeManager *HomeManagerConfig `json:"homeManager,omitempty"`
}

// LegacyConfigLane identifies one explicitly isolated schema-v1 payload in a
// manifest-v2 bundle. It carries no generation claim and remains unverified.
type LegacyConfigLane struct {
	CaptureID           string `json:"captureId"`
	ModuleID            string `json:"moduleId"`
	ModuleSchemaVersion int    `json:"moduleSchemaVersion"`
	PayloadRoot         string `json:"payloadRoot"`
}

// ConfigCapture is the immutable manifest-v2 provenance and payload index for
// one captured application config set.
type ConfigCapture struct {
	CaptureID                   string                  `json:"captureId"`
	ModuleID                    string                  `json:"moduleId"`
	ConfigSetID                 string                  `json:"configSetId"`
	SourceInstance              ConfigSourceInstance    `json:"sourceInstance"`
	SourceGeneration            string                  `json:"sourceGeneration"`
	SourceGenerationFingerprint string                  `json:"sourceGenerationFingerprint"`
	CaptureModule               CaptureModuleProvenance `json:"captureModule"`
	PayloadRoot                 string                  `json:"payloadRoot"`
	PayloadManifest             []PayloadManifestEntry  `json:"payloadManifest"`
}

// ConfigSourceInstance records the source instance identity and its preserved
// raw/normalized version evidence.
type ConfigSourceInstance struct {
	ID                string                        `json:"id"`
	DetectorID        string                        `json:"detectorId"`
	RawVersion        string                        `json:"rawVersion"`
	NormalizedVersion string                        `json:"normalizedVersion"`
	Evidence          *ConfigSourceInstanceEvidence `json:"evidence"`
}

// ConfigSourceInstanceEvidence is portable discovery evidence. Machine-local
// roots are intentionally absent from the persisted shape.
type ConfigSourceInstanceEvidence struct {
	Type     string `json:"type"`
	AppID    string `json:"appId,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Platform string `json:"platform,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Driver   string `json:"driver,omitempty"`
}

// CaptureModuleProvenance identifies the exact declarative source module used
// at capture time and its inspectable bundle snapshot.
type CaptureModuleProvenance struct {
	SchemaVersion int    `json:"schemaVersion"`
	ContentHash   string `json:"contentHash"`
	SnapshotPath  string `json:"snapshotPath"`
}

// PayloadManifestEntry is one hierarchy-preserving config payload record.
type PayloadManifestEntry struct {
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
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
//
// Secrets is a SIBLING of those three, not part of their mutual exclusivity: it
// composes with the engine-generated modes (settings/config) by appending
// reference-only sinks to the generated home configuration. The framing invariant
// is "referenced, never embedded" — the engine never holds secret material (see
// HomeManagerSecret). Secrets combined with a pure flake input is rejected at load
// (the user's external flake owns its own secrets; the engine generates nothing to
// inject into).
type HomeManagerConfig struct {
	Flake    string               `json:"flake,omitempty"`
	Config   string               `json:"config,omitempty"`
	Settings *HomeManagerSettings `json:"settings,omitempty"`
	Secrets  []HomeManagerSecret  `json:"secrets,omitempty"`
}

// HomeManagerSecret is a single documented-boundary secret reference. The engine
// NEVER reads, embeds, or stores the secret material: it emits only a Nix REFERENCE
// to where the secret is expected to land at activation time (a file path the user
// provisions out-of-band). The user owns provisioning the actual material; the
// engine documents the boundary.
//
// A secret ALWAYS references a file via Path; it MAY additionally expose that file's
// PATH through an environment variable via Env (the *_FILE path-reference
// convention — the variable holds the FILE PATH, never the secret value). The two
// shapes (LoadManifest rejects env-without-path and an invalid env name):
//   - Path only   → home.file.<homeRelTarget(Name)>.source references the path (the
//     secret file the user places there out-of-band).
//   - Path + Env  → home.sessionVariables.<Env> = "<Path>"; — references the file
//     PATH, never the value (no-embed by construction). Env must be a valid
//     identifier (^[A-Za-z_][A-Za-z0-9_]*$); the load-time check blocks Nix-attr
//     injection because Env is emitted as a bare attribute.
//
// Backend selects the secret-management strategy and MUST be named explicitly; it
// defaults to "boundary" (the only supported backend). An unsupported backend is
// rejected at load rather than silently degrading to embedding.
type HomeManagerSecret struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Env     string `json:"env,omitempty"`
	Backend string `json:"backend,omitempty"`
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
	Eza      *EzaSettings      `json:"eza,omitempty"`
	Gh       *GhSettings       `json:"gh,omitempty"`
	Lazygit  *LazygitSettings  `json:"lazygit,omitempty"`
	Neovim   *NeovimSettings   `json:"neovim,omitempty"`

	// Dotfiles/CLI tier — each maps to a stable home-manager programs.<name> surface
	// (see home_catalog.go curatedTable). Same DisallowUnknownFields typo-rejection.
	Ripgrep   *RipgrepSettings   `json:"ripgrep,omitempty"`
	Fd        *FdSettings        `json:"fd,omitempty"`
	Zsh       *ZshSettings       `json:"zsh,omitempty"`
	Bash      *BashSettings      `json:"bash,omitempty"`
	Helix     *HelixSettings     `json:"helix,omitempty"`
	Kitty     *KittySettings     `json:"kitty,omitempty"`
	Alacritty *AlacrittySettings `json:"alacritty,omitempty"`
	Wezterm   *WeztermSettings   `json:"wezterm,omitempty"`
	Jujutsu   *JujutsuSettings   `json:"jujutsu,omitempty"`
	Atuin     *AtuinSettings     `json:"atuin,omitempty"`
	Yazi      *YaziSettings      `json:"yazi,omitempty"`

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

// EzaSettings are the curated eza concepts mapped to programs.eza.enable plus
// programs.eza.extraOptions — a slice of raw eza CLI flags (e.g. ["--git","--icons"]),
// the stable surface that insulates the user from home-manager eza option renames.
type EzaSettings struct {
	Enable       bool     `json:"enable,omitempty"`
	ExtraOptions []string `json:"extraOptions,omitempty"`
}

// GhSettings are the curated gh (GitHub CLI) concepts mapped to programs.gh.enable plus
// programs.gh.settings — a raw attrset forwarded verbatim to the gh config, the stable
// surface (gh's own config key namespace) that insulates the user from option renames.
type GhSettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// LazygitSettings are the curated lazygit concepts mapped to programs.lazygit.enable
// plus programs.lazygit.settings — a raw attrset forwarded verbatim to the lazygit
// config, the stable surface (lazygit's own config structure) that insulates the user
// from home-manager option renames.
type LazygitSettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// NeovimSettings are the curated neovim concepts mapped to programs.neovim.enable plus
// programs.neovim.extraConfig — the raw vimscript/lua string, the stable surface that
// insulates the user from home-manager neovim option renames.
type NeovimSettings struct {
	Enable      bool   `json:"enable,omitempty"`
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// RipgrepSettings → programs.ripgrep.enable + programs.ripgrep.arguments, a []string of
// raw ripgrep CLI flags (e.g. ["--smart-case","--hidden"]) home-manager writes to the
// ripgreprc. The flag namespace is ripgrep's own — stable across home-manager renames.
type RipgrepSettings struct {
	Enable    bool     `json:"enable,omitempty"`
	Arguments []string `json:"arguments,omitempty"`
}

// FdSettings → programs.fd.enable + programs.fd.extraOptions, a []string of raw fd CLI
// flags (e.g. ["--hidden","--no-ignore"]). fd's own flag namespace — stable surface.
type FdSettings struct {
	Enable       bool     `json:"enable,omitempty"`
	ExtraOptions []string `json:"extraOptions,omitempty"`
}

// ZshSettings → programs.zsh.enable + programs.zsh.initContent, the raw .zshrc body.
// initContent is the consolidated, stable init surface (the older initExtra/initExtraFirst
// options fold into it), insulating the user from home-manager zsh option renames.
type ZshSettings struct {
	Enable      bool   `json:"enable,omitempty"`
	InitContent string `json:"initContent,omitempty"`
}

// BashSettings → programs.bash.enable + programs.bash.initExtra, the raw .bashrc body —
// the long-stable bash init surface that insulates the user from home-manager renames.
type BashSettings struct {
	Enable    bool   `json:"enable,omitempty"`
	InitExtra string `json:"initExtra,omitempty"`
}

// HelixSettings → programs.helix.enable + programs.helix.settings, a raw attrset forwarded
// verbatim to helix's config.toml — helix's own config namespace, the stable surface.
type HelixSettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// KittySettings → programs.kitty.enable + programs.kitty.settings, a raw attrset forwarded
// verbatim to kitty.conf — kitty's own config-key namespace, the stable surface.
type KittySettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// AlacrittySettings → programs.alacritty.enable + programs.alacritty.settings, a raw attrset
// forwarded verbatim to alacritty.toml — alacritty's own config structure, the stable surface.
type AlacrittySettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// WeztermSettings → programs.wezterm.enable + programs.wezterm.extraConfig, the raw Lua
// config string — the stable surface that insulates the user from home-manager renames.
type WeztermSettings struct {
	Enable      bool   `json:"enable,omitempty"`
	ExtraConfig string `json:"extraConfig,omitempty"`
}

// JujutsuSettings → programs.jujutsu.enable + programs.jujutsu.settings, a raw attrset
// forwarded verbatim to jj's config.toml — jujutsu's own config namespace, the stable surface.
type JujutsuSettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// AtuinSettings → programs.atuin.enable + programs.atuin.settings, a raw attrset forwarded
// verbatim to atuin's config.toml — atuin's own config namespace, the stable surface.
type AtuinSettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// YaziSettings → programs.yazi.enable + programs.yazi.settings, a raw attrset forwarded
// verbatim to yazi's yazi.toml — yazi's own config namespace, the stable surface.
type YaziSettings struct {
	Enable   bool           `json:"enable,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
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

	// Installed, InstalledVersion, and Backend are runtime detection evidence.
	// They must never turn an unpinned manifest into a pinned declaration.
	Installed        bool       `json:"-"`
	InstalledVersion string     `json:"-"`
	Backend          string     `json:"-"`
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
//
// For the value-level registry-set restore type, Source/Target are unused; the
// operation is fully described by Key, ValueName, ValueType, and Data.
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
	// LegacyCaptureID binds a manifest-v2 flat restore action to exactly one
	// LegacyConfigLane. It is intentionally absent from manifest v1.
	LegacyCaptureID string `json:"legacyCaptureId,omitempty"`

	// registry-set fields (value-level Windows OS-settings ops). Key is an HKCU
	// key path; ValueName/ValueType/Data describe the single named value to set.
	Key       string `json:"key,omitempty"`
	ValueName string `json:"valueName,omitempty"`
	ValueType string `json:"valueType,omitempty"`
	Data      string `json:"data,omitempty"`
}

// VerifyEntry describes a single state assertion.
//
// For the value-level registry-value-equals verify type, ValueType/Data carry
// the expected named-value type and data to compare against.
type VerifyEntry struct {
	Type      string `json:"type"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	ValueName string `json:"valueName,omitempty"`
	ValueType string `json:"valueType,omitempty"`
	Data      string `json:"data,omitempty"`
}
