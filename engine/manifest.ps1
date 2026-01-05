# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Provisioning manifest parsing and generation.

.DESCRIPTION
    Reads and writes manifest files in JSONC (JSON with comments), JSON, or YAML format.
    JSONC is the preferred format for human authoring.
    Supports includes for modular manifest composition.
#>

# Track included files to detect circular includes
$script:IncludeStack = @()

# Flag to control config module expansion (can be disabled for raw loading)
$script:ExpandConfigModules = $true

function Resolve-RestoreEntriesFromCatalogs {
    <#
    .SYNOPSIS
        Expand bundles and recipes into restore entries.
    .DESCRIPTION
        Resolves manifest.bundles and manifest.recipes from repo-root catalogs.
        Returns expanded restore[] in the correct order:
        1. Bundle recipes (in bundle order, recipe order within each bundle)
        2. Manifest recipes (in order)
        3. Manifest inline restore[] (appended last)
    .PARAMETER Manifest
        The manifest hashtable to expand.
    .PARAMETER RepoRoot
        The repository root path where /bundles and /recipes directories exist.
    .OUTPUTS
        Array of restore entry objects.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest,
        
        [Parameter(Mandatory = $true)]
        [string]$RepoRoot
    )
    
    $expandedRestore = @()
    
    # 1. Process bundles
    if ($Manifest.bundles -and $Manifest.bundles.Count -gt 0) {
        foreach ($bundleId in $Manifest.bundles) {
            $bundlePath = Join-Path $RepoRoot "bundles\$bundleId.jsonc"
            
            if (-not (Test-Path $bundlePath)) {
                throw "Bundle not found: $bundleId (expected at: $bundlePath)"
            }
            
            try {
                $bundle = Read-JsoncFile -Path $bundlePath
                
                if ($bundle.recipes -and $bundle.recipes.Count -gt 0) {
                    foreach ($recipeId in $bundle.recipes) {
                        $recipePath = Join-Path $RepoRoot "recipes\$recipeId.jsonc"
                        
                        if (-not (Test-Path $recipePath)) {
                            throw "Recipe not found: $recipeId (referenced by bundle $bundleId, expected at: $recipePath)"
                        }
                        
                        try {
                            $recipe = Read-JsoncFile -Path $recipePath
                            
                            if ($recipe.restore -and $recipe.restore.Count -gt 0) {
                                $expandedRestore += @($recipe.restore)
                            }
                        } catch {
                            throw "Failed to load recipe '$recipeId' from bundle '$bundleId': $($_.Exception.Message)"
                        }
                    }
                }
            } catch {
                throw "Failed to load bundle '$bundleId': $($_.Exception.Message)"
            }
        }
    }
    
    # 2. Process manifest recipes
    if ($Manifest.recipes -and $Manifest.recipes.Count -gt 0) {
        foreach ($recipeId in $Manifest.recipes) {
            $recipePath = Join-Path $RepoRoot "recipes\$recipeId.jsonc"
            
            if (-not (Test-Path $recipePath)) {
                throw "Recipe not found: $recipeId (expected at: $recipePath)"
            }
            
            try {
                $recipe = Read-JsoncFile -Path $recipePath
                
                if ($recipe.restore -and $recipe.restore.Count -gt 0) {
                    $expandedRestore += @($recipe.restore)
                }
            } catch {
                throw "Failed to load recipe '$recipeId': $($_.Exception.Message)"
            }
        }
    }
    
    # 3. Append manifest inline restore entries
    if ($Manifest.restore -and $Manifest.restore.Count -gt 0) {
        $expandedRestore += @($Manifest.restore)
    }
    
    return $expandedRestore
}

function Read-Manifest {
    <#
    .SYNOPSIS
        Load a manifest file and resolve all includes.
    .DESCRIPTION
        Supports .jsonc, .json, and .yaml/.yml formats.
        Resolves includes recursively with circular dependency detection.
        Expands configModules into restore/verify items (after includes resolution).
    .PARAMETER Path
        Path to the manifest file.
    .PARAMETER SkipConfigModuleExpansion
        If true, skip config module expansion. Useful for raw manifest inspection.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [switch]$SkipConfigModuleExpansion
    )
    
    # Reset include stack for top-level call
    $script:IncludeStack = @()
    
    $manifest = Read-ManifestInternal -Path $Path
    
    # Expand configModules after includes are resolved (unless skipped)
    if (-not $SkipConfigModuleExpansion -and $script:ExpandConfigModules) {
        if ($manifest.configModules -and $manifest.configModules.Count -gt 0) {
            # Import config-modules if not already loaded
            $configModulesPath = Join-Path $PSScriptRoot "config-modules.ps1"
            if (Test-Path $configModulesPath) {
                . $configModulesPath
                $manifest = Expand-ManifestConfigModules -Manifest $manifest
            }
        }
    }
    
    # Expand bundles and recipes from catalogs (if present)
    if (($manifest.bundles -and $manifest.bundles.Count -gt 0) -or 
        ($manifest.recipes -and $manifest.recipes.Count -gt 0)) {
        
        # Resolve manifest path to find repo root
        $resolvedPath = (Resolve-Path $Path -ErrorAction SilentlyContinue).Path
        if (-not $resolvedPath) {
            $resolvedPath = [System.IO.Path]::GetFullPath($Path)
        }
        
        # Find repo root (walk up to find /bundles and /recipes)
        $manifestDir = Split-Path -Parent $resolvedPath
        $repoRoot = $manifestDir
        
        $current = $manifestDir
        while ($current) {
            $bundlesDir = Join-Path $current "bundles"
            $recipesDir = Join-Path $current "recipes"
            
            if ((Test-Path $bundlesDir) -and (Test-Path $recipesDir)) {
                $repoRoot = $current
                break
            }
            
            $parent = Split-Path -Parent $current
            if ($parent -eq $current) { break }
            $current = $parent
        }
        
        # Expand catalogs into restore entries
        $expandedRestore = Resolve-RestoreEntriesFromCatalogs -Manifest $manifest -RepoRoot $repoRoot
        $manifest.restore = $expandedRestore
    }
    
    return $manifest
}

function Read-ManifestInternal {
    <#
    .SYNOPSIS
        Internal manifest loader with include tracking.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    # Resolve to absolute path
    $absolutePath = (Resolve-Path $Path -ErrorAction SilentlyContinue).Path
    if (-not $absolutePath) {
        if (-not (Test-Path $Path)) {
            throw "Manifest not found: $Path"
        }
        $absolutePath = [System.IO.Path]::GetFullPath($Path)
    }
    
    # Check for circular includes
    if ($script:IncludeStack -contains $absolutePath) {
        $cycle = ($script:IncludeStack + $absolutePath) -join " -> "
        throw "Circular include detected: $cycle"
    }
    
    # Push to include stack
    $script:IncludeStack += $absolutePath
    
    try {
        $content = Get-Content -Path $absolutePath -Raw -Encoding UTF8
        $extension = [System.IO.Path]::GetExtension($absolutePath).ToLower()
        
        # Parse based on file extension
        $manifest = switch ($extension) {
            ".jsonc" { ConvertFrom-Jsonc -Content $content -Depth 100 }
            ".json"  { ConvertFrom-Jsonc -Content $content -Depth 100 }
            ".yaml"  { ConvertFrom-SimpleYaml -Content $content }
            ".yml"   { ConvertFrom-SimpleYaml -Content $content }
            default  {
                # Try JSONC first, fall back to YAML
                try {
                    ConvertFrom-Jsonc -Content $content -Depth 100
                } catch {
                    ConvertFrom-SimpleYaml -Content $content
                }
            }
        }
        
        # Normalize to hashtable if needed
        if ($manifest -is [PSCustomObject]) {
            $manifest = Convert-PsObjectToHashtable -InputObject $manifest
        }
        
        # Process includes
        if ($manifest.includes -and $manifest.includes.Count -gt 0) {
            $baseDir = Split-Path -Parent $absolutePath
            $manifest = Resolve-ManifestIncludes -Manifest $manifest -BaseDir $baseDir
        }
        
        # Ensure required fields exist
        $manifest = Normalize-Manifest -Manifest $manifest
        
        return $manifest
        
    } finally {
        # Pop from include stack
        $script:IncludeStack = $script:IncludeStack | Where-Object { $_ -ne $absolutePath }
    }
}

function Remove-JsoncComments {
    <#
    .SYNOPSIS
        Strip JSONC comments from JSON content (PS5.1-safe state machine).
    .DESCRIPTION
        Removes single-line (//) and multi-line (/* */) comments while preserving:
        - Strings containing // or /* (e.g., "http://example.com")
        - Escaped quotes inside strings
        - Line endings (CRLF/LF)
    .PARAMETER Content
        Raw JSONC content string.
    .OUTPUTS
        String with comments removed, ready for ConvertFrom-Json.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Content
    )
    
    # PS5.1-safe StringBuilder construction
    $result = New-Object System.Text.StringBuilder
    $inString = $false
    $escaped = $false
    $i = 0
    
    while ($i -lt $Content.Length) {
        $char = $Content[$i]
        $nextChar = if ($i + 1 -lt $Content.Length) { $Content[$i + 1] } else { $null }
        
        # Handle escape sequences inside strings
        if ($escaped) {
            [void]$result.Append($char)
            $escaped = $false
            $i++
            continue
        }
        
        # Detect escape character inside string
        if ($char -eq '\' -and $inString) {
            [void]$result.Append($char)
            $escaped = $true
            $i++
            continue
        }
        
        # Toggle string state on unescaped quote
        if ($char -eq '"' -and -not $escaped) {
            $inString = -not $inString
            [void]$result.Append($char)
            $i++
            continue
        }
        
        # Only strip comments outside of strings
        if (-not $inString) {
            # Single-line comment: // ...
            if ($char -eq '/' -and $nextChar -eq '/') {
                # Skip until end of line (preserve the newline)
                while ($i -lt $Content.Length -and $Content[$i] -ne "`n" -and $Content[$i] -ne "`r") {
                    $i++
                }
                # Keep the line ending
                if ($i -lt $Content.Length -and ($Content[$i] -eq "`n" -or $Content[$i] -eq "`r")) {
                    [void]$result.Append($Content[$i])
                    $i++
                    # Handle CRLF
                    if ($i -lt $Content.Length -and $Content[$i-1] -eq "`r" -and $Content[$i] -eq "`n") {
                        [void]$result.Append($Content[$i])
                        $i++
                    }
                }
                continue
            }
            
            # Multi-line comment: /* ... */
            if ($char -eq '/' -and $nextChar -eq '*') {
                $i += 2
                # Skip until */
                while ($i -lt $Content.Length - 1) {
                    if ($Content[$i] -eq '*' -and $Content[$i + 1] -eq '/') {
                        $i += 2
                        break
                    }
                    $i++
                }
                continue
            }
        }
        
        # Append character to result
        [void]$result.Append($char)
        $i++
    }
    
    return $result.ToString()
}

function Read-JsoncFile {
    <#
    .SYNOPSIS
        Canonical JSONC file loader for all manifest and plan parsing (PS5.1+ compatible).
    .DESCRIPTION
        Reads a file and parses it as JSONC (JSON with comments).
        Strips single-line (//) and multi-line (/* */) comments before parsing.
        This is the single source of truth for JSONC parsing in the provisioning engine.
        
        Compatible with Windows PowerShell 5.1 and PowerShell 7+.
    .PARAMETER Path
        Path to the JSONC file to read.
    .PARAMETER Depth
        Maximum depth for JSON parsing. Default: 100.
    .OUTPUTS
        Hashtable representation of the parsed JSONC content.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [int]$Depth = 100
    )
    
    if (-not (Test-Path $Path)) {
        throw "JSONC parsing failed: File not found: $Path"
    }
    
    try {
        $content = Get-Content -Path $Path -Raw -Encoding UTF8
        $cleanJson = Remove-JsoncComments -Content $content
        
        # PS5.1 vs PS7+ compatibility: -AsHashtable only exists in PS6+
        if ($PSVersionTable.PSVersion.Major -ge 6) {
            $parsed = $cleanJson | ConvertFrom-Json -AsHashtable -Depth $Depth
        } else {
            # PS5.1: ConvertFrom-Json returns PSCustomObject, convert to hashtable
            $parsed = $cleanJson | ConvertFrom-Json
            $parsed = Convert-PsObjectToHashtable -InputObject $parsed
        }
        
        return $parsed
    } catch {
        throw "JSONC parsing failed for '$Path': $($_.Exception.Message)"
    }
}

function ConvertFrom-Jsonc {
    <#
    .SYNOPSIS
        Parse JSONC (JSON with comments) content (PS5.1+ compatible).
    .DESCRIPTION
        Strips single-line (//) and multi-line (/* */) comments before parsing.
        Wrapper around Remove-JsoncComments for backward compatibility.
    .PARAMETER Content
        Raw JSONC content string.
    .PARAMETER Depth
        Maximum depth for JSON parsing. Default: 100.
    .OUTPUTS
        Hashtable representation of the parsed JSONC content.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Content,
        
        [Parameter(Mandatory = $false)]
        [int]$Depth = 100
    )
    
    try {
        $cleanJson = Remove-JsoncComments -Content $Content
        
        # PS5.1 vs PS7+ compatibility
        if ($PSVersionTable.PSVersion.Major -ge 6) {
            $parsed = $cleanJson | ConvertFrom-Json -AsHashtable -Depth $Depth
        } else {
            # PS5.1: ConvertFrom-Json returns PSCustomObject, convert to hashtable
            $parsed = $cleanJson | ConvertFrom-Json
            $parsed = Convert-PsObjectToHashtable -InputObject $parsed
        }
        
        return $parsed
    } catch {
        throw "Failed to parse JSONC: $($_.Exception.Message)"
    }
}

function Convert-PsObjectToHashtable {
    <#
    .SYNOPSIS
        Recursively convert PSCustomObject to hashtable.
    #>
    param(
        [Parameter(Mandatory = $true)]
        $InputObject
    )
    
    if ($null -eq $InputObject) {
        return $null
    }
    
    # Already a hashtable (including OrderedHashtable) - process values recursively
    if ($InputObject -is [hashtable]) {
        $hash = @{}
        foreach ($key in $InputObject.Keys) {
            $hash[$key] = Convert-PsObjectToHashtable -InputObject $InputObject[$key]
        }
        return $hash
    }
    
    # Array or collection (but not string)
    if ($InputObject -is [System.Collections.IEnumerable] -and $InputObject -isnot [string]) {
        $collection = @()
        foreach ($item in $InputObject) {
            $collection += Convert-PsObjectToHashtable -InputObject $item
        }
        return $collection
    }
    
    # PSCustomObject (but not hashtable - checked above)
    if ($InputObject -is [PSCustomObject]) {
        $hash = @{}
        foreach ($property in $InputObject.PSObject.Properties) {
            $hash[$property.Name] = Convert-PsObjectToHashtable -InputObject $property.Value
        }
        return $hash
    }
    
    return $InputObject
}

function Resolve-ManifestIncludes {
    <#
    .SYNOPSIS
        Resolve and merge included manifests.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest,
        
        [Parameter(Mandatory = $true)]
        [string]$BaseDir
    )
    
    $includes = $Manifest.includes
    $Manifest.Remove('includes')
    
    foreach ($includePath in $includes) {
        # Resolve relative path
        $fullPath = if ([System.IO.Path]::IsPathRooted($includePath)) {
            $includePath
        } else {
            Join-Path $BaseDir $includePath
        }
        
        if (-not (Test-Path $fullPath)) {
            throw "Include not found: $fullPath (referenced from $BaseDir)"
        }
        
        # Load included manifest
        $included = Read-ManifestInternal -Path $fullPath
        
        # Merge arrays (apps, restore, verify)
        foreach ($arrayKey in @('apps', 'restore', 'verify')) {
            if ($included[$arrayKey] -and $included[$arrayKey].Count -gt 0) {
                if (-not $Manifest[$arrayKey]) {
                    $Manifest[$arrayKey] = @()
                }
                $Manifest[$arrayKey] = @($Manifest[$arrayKey]) + @($included[$arrayKey])
            }
        }
        
        # Scalar fields: root manifest takes precedence (don't overwrite)
        foreach ($scalarKey in @('version', 'name', 'captured')) {
            if ($included[$scalarKey] -and -not $Manifest[$scalarKey]) {
                $Manifest[$scalarKey] = $included[$scalarKey]
            }
        }
    }
    
    return $Manifest
}

function Normalize-Manifest {
    <#
    .SYNOPSIS
        Ensure manifest has all required fields with defaults.
    .DESCRIPTION
        Applies predictable defaults for optional sections.
        Called after parsing for JSONC/JSON/YAML equally.
        Ensures array fields are always arrays (not single objects).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest
    )
    
    # Ensure required scalar fields exist (use ContainsKey to avoid falsy value issues)
    if (-not $Manifest.ContainsKey('version') -or $null -eq $Manifest.version) { $Manifest.version = 1 }
    if (-not $Manifest.ContainsKey('name') -or $null -eq $Manifest.name) { $Manifest.name = "" }
    
    # Ensure array fields default to empty arrays and are always arrays (not single objects)
    foreach ($arrayKey in @('apps', 'restore', 'verify', 'includes', 'configModules', 'bundles', 'recipes')) {
        if (-not $Manifest.ContainsKey($arrayKey) -or $null -eq $Manifest[$arrayKey]) {
            $Manifest[$arrayKey] = @()
        } else {
            # Ensure single items are wrapped in array
            $Manifest[$arrayKey] = @($Manifest[$arrayKey])
        }
    }
    
    return $Manifest
}

function Write-Manifest {
    <#
    .SYNOPSIS
        Write a manifest to file in JSONC format.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $true)]
        [hashtable]$Manifest
    )
    
    $parentDir = Split-Path -Parent $Path
    if ($parentDir -and -not (Test-Path $parentDir)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }
    
    $extension = [System.IO.Path]::GetExtension($Path).ToLower()
    
    $content = switch ($extension) {
        ".yaml" { ConvertTo-SimpleYaml -Object $Manifest }
        ".yml"  { ConvertTo-SimpleYaml -Object $Manifest }
        default { ConvertTo-Jsonc -Object $Manifest }
    }
    
    $content | Out-File -FilePath $Path -Encoding UTF8 -NoNewline
    
    return $Path
}

function ConvertTo-Jsonc {
    <#
    .SYNOPSIS
        Convert manifest to JSONC format with comments.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Object
    )
    
    $sb = [System.Text.StringBuilder]::new()
    
    # Header comment
    [void]$sb.AppendLine("{")
    [void]$sb.AppendLine("  // Provisioning Manifest")
    [void]$sb.AppendLine("  // Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    [void]$sb.AppendLine("  // Machine: $env:COMPUTERNAME")
    [void]$sb.AppendLine("  // Format: JSONC (JSON with comments)")
    [void]$sb.AppendLine("")
    
    # Version and name
    [void]$sb.AppendLine("  `"version`": $($Object.version),")
    [void]$sb.AppendLine("  `"name`": `"$($Object.name)`",")
    if ($Object.captured) {
        [void]$sb.AppendLine("  `"captured`": `"$($Object.captured)`",")
    }
    [void]$sb.AppendLine("")
    
    # Includes section (if present)
    if ($Object.includes -and $Object.includes.Count -gt 0) {
        [void]$sb.AppendLine("  // Included manifest files")
        [void]$sb.AppendLine("  `"includes`": [")
        $includeIndex = 0
        foreach ($inc in $Object.includes) {
            $includeIndex++
            $isLast = $includeIndex -eq $Object.includes.Count
            $comma = if ($isLast) { "" } else { "," }
            [void]$sb.AppendLine("    `"$inc`"$comma")
        }
        [void]$sb.AppendLine("  ],")
        [void]$sb.AppendLine("")
    }
    
    # Apps section
    [void]$sb.AppendLine("  // Applications to install")
    [void]$sb.AppendLine("  `"apps`": [")
    
    if ($Object.apps -and $Object.apps.Count -gt 0) {
        $appIndex = 0
        foreach ($app in $Object.apps) {
            $appIndex++
            $isLast = $appIndex -eq $Object.apps.Count
            
            [void]$sb.AppendLine("    {")
            [void]$sb.AppendLine("      `"id`": `"$($app.id)`",")
            
            if ($app.refs) {
                [void]$sb.AppendLine("      `"refs`": {")
                $refKeys = @($app.refs.Keys)
                $refIndex = 0
                foreach ($platform in $refKeys | Sort-Object) {
                    $refIndex++
                    $refIsLast = $refIndex -eq $refKeys.Count
                    $comma = if ($refIsLast) { "" } else { "," }
                    [void]$sb.AppendLine("        `"$platform`": `"$($app.refs[$platform])`"$comma")
                }
                [void]$sb.AppendLine("      }")
            }
            
            $comma = if ($isLast) { "" } else { "," }
            [void]$sb.AppendLine("    }$comma")
        }
    }
    
    [void]$sb.AppendLine("  ],")
    [void]$sb.AppendLine("")
    
    # Restore section (commented template)
    [void]$sb.AppendLine("  // Configuration restore (opt-in)")
    [void]$sb.AppendLine("  // Uncomment and customize paths to restore configurations.")
    [void]$sb.AppendLine("  // IMPORTANT: Review paths before uncommenting - never auto-export secrets.")
    [void]$sb.AppendLine("  `"restore`": [")
    [void]$sb.AppendLine("    // Example:")
    [void]$sb.AppendLine("    // { `"type`": `"copy`", `"source`": `"./configs/.gitconfig`", `"target`": `"~/.gitconfig`", `"backup`": true }")
    [void]$sb.AppendLine("  ],")
    [void]$sb.AppendLine("")
    
    # Verify section
    [void]$sb.AppendLine("  // Verification steps")
    [void]$sb.AppendLine("  `"verify`": [")
    
    if ($Object.verify -and $Object.verify.Count -gt 0) {
        $verifyIndex = 0
        foreach ($v in $Object.verify) {
            $verifyIndex++
            $isLast = $verifyIndex -eq $Object.verify.Count
            $comma = if ($isLast) { "" } else { "," }
            
            $verifyJson = @{ type = $v.type }
            if ($v.path) { $verifyJson.path = $v.path }
            if ($v.command) { $verifyJson.command = $v.command }
            
            $jsonLine = $verifyJson | ConvertTo-Json -Compress
            [void]$sb.AppendLine("    $jsonLine$comma")
        }
    }
    
    [void]$sb.AppendLine("  ]")
    [void]$sb.AppendLine("}")
    
    return $sb.ToString()
}

function ConvertFrom-SimpleYaml {
    <#
    .SYNOPSIS
        Parse simple YAML content (backward compatibility).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Content
    )
    
    $result = @{
        version = 1
        name = ""
        captured = ""
        apps = @()
        restore = @()
        verify = @()
    }
    
    $lines = $Content -split "`n" | ForEach-Object { $_.TrimEnd("`r") }
    $currentSection = $null
    $currentItem = $null
    $currentSubKey = $null
    
    foreach ($line in $lines) {
        # Skip empty lines and comments
        if ($line -match '^\s*$' -or $line -match '^\s*#') {
            continue
        }
        
        # Top-level key: value
        if ($line -match '^(\w+):\s*(.*)$') {
            $key = $Matches[1]
            $value = $Matches[2].Trim()
            
            if ($value -eq "" -or $null -eq $value) {
                # Section start (apps:, restore:, verify:)
                $currentSection = $key
                $currentItem = $null
                $currentSubKey = $null
            } else {
                # Simple key: value
                $result[$key] = $value
                $currentSection = $null
            }
            continue
        }
        
        # List item start: - key: value or just - value
        if ($line -match '^\s+-\s+(\w+):\s*(.*)$') {
            $key = $Matches[1]
            $value = $Matches[2].Trim()
            
            if ($currentSection) {
                $currentItem = @{ $key = $value }
                $result[$currentSection] += $currentItem
                $currentSubKey = $null
            }
            continue
        }
        
        # Deep nested key (under refs:): platform: id (6+ spaces)
        # Must check BEFORE 4+ spaces to avoid false matches
        if ($line -match '^\s{6,}(\w+):\s*(.+)$') {
            $key = $Matches[1]
            $value = $Matches[2].Trim()
            
            if ($currentItem -and $currentSubKey) {
                $currentItem[$currentSubKey][$key] = $value
            }
            continue
        }
        
        # Nested key under list item: key: value (4-5 spaces)
        if ($line -match '^\s{4,5}(\w+):\s*(.*)$') {
            $key = $Matches[1]
            $value = $Matches[2].Trim()
            
            if ($currentItem) {
                if ($value -eq "" -or $null -eq $value) {
                    # Sub-object start (refs:)
                    $currentItem[$key] = @{}
                    $currentSubKey = $key
                } else {
                    $currentItem[$key] = $value
                }
            }
            continue
        }
    }
    
    return $result
}

function ConvertTo-SimpleYaml {
    <#
    .SYNOPSIS
        Convert manifest to YAML format (backward compatibility).
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Object
    )
    
    $sb = [System.Text.StringBuilder]::new()
    
    # Header
    [void]$sb.AppendLine("# Provisioning Manifest")
    [void]$sb.AppendLine("# Generated: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')")
    [void]$sb.AppendLine("# Machine: $env:COMPUTERNAME")
    [void]$sb.AppendLine("")
    
    # Version and name
    [void]$sb.AppendLine("version: $($Object.version)")
    [void]$sb.AppendLine("name: $($Object.name)")
    if ($Object.captured) {
        [void]$sb.AppendLine("captured: $($Object.captured)")
    }
    [void]$sb.AppendLine("")
    
    # Apps section
    if ($Object.apps -and $Object.apps.Count -gt 0) {
        [void]$sb.AppendLine("apps:")
        foreach ($app in $Object.apps) {
            [void]$sb.AppendLine("  - id: $($app.id)")
            if ($app.refs) {
                [void]$sb.AppendLine("    refs:")
                foreach ($platform in $app.refs.Keys | Sort-Object) {
                    [void]$sb.AppendLine("      $($platform): $($app.refs[$platform])")
                }
            }
            if ($app.verify) {
                [void]$sb.AppendLine("    verify:")
                [void]$sb.AppendLine("      command: $($app.verify.command)")
            }
        }
        [void]$sb.AppendLine("")
    }
    
    # Restore section
    if ($Object.restore -and $Object.restore.Count -gt 0) {
        [void]$sb.AppendLine("restore:")
        foreach ($item in $Object.restore) {
            [void]$sb.AppendLine("  - type: $($item.type)")
            if ($item.source) { [void]$sb.AppendLine("    source: $($item.source)") }
            if ($item.target) { [void]$sb.AppendLine("    target: $($item.target)") }
            if ($item.backup) { [void]$sb.AppendLine("    backup: $($item.backup)") }
        }
        [void]$sb.AppendLine("")
    }
    
    # Verify section
    if ($Object.verify -and $Object.verify.Count -gt 0) {
        [void]$sb.AppendLine("verify:")
        foreach ($v in $Object.verify) {
            [void]$sb.AppendLine("  - type: $($v.type)")
            if ($v.path) { [void]$sb.AppendLine("    path: $($v.path)") }
            if ($v.command) { [void]$sb.AppendLine("    command: $($v.command)") }
        }
        [void]$sb.AppendLine("")
    }
    
    return $sb.ToString()
}

function Read-ManifestRaw {
    <#
    .SYNOPSIS
        Load a manifest file WITHOUT resolving includes.
    .DESCRIPTION
        Returns the raw manifest content as a hashtable.
        Used for update mode to preserve the root manifest structure.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    if (-not (Test-Path $Path)) {
        return $null
    }
    
    $content = Get-Content -Path $Path -Raw -Encoding UTF8
    $extension = [System.IO.Path]::GetExtension($Path).ToLower()
    
    $manifest = switch ($extension) {
        ".jsonc" { ConvertFrom-Jsonc -Content $content -Depth 100 }
        ".json"  { ConvertFrom-Jsonc -Content $content -Depth 100 }
        ".yaml"  { ConvertFrom-SimpleYaml -Content $content }
        ".yml"   { ConvertFrom-SimpleYaml -Content $content }
        default  {
            try {
                ConvertFrom-Jsonc -Content $content -Depth 100
            } catch {
                ConvertFrom-SimpleYaml -Content $content
            }
        }
    }
    
    if ($manifest -is [PSCustomObject]) {
        $manifest = Convert-PsObjectToHashtable -InputObject $manifest
    }
    
    return $manifest
}

function Get-IncludedAppIds {
    <#
    .SYNOPSIS
        Get all app IDs from included manifests (resolved).
    .DESCRIPTION
        Loads each include file and collects app IDs.
        Used to avoid duplicating apps that come from includes.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$IncludePaths,
        
        [Parameter(Mandatory = $true)]
        [string]$BaseDir
    )
    
    $includedIds = @{}
    
    foreach ($includePath in $IncludePaths) {
        $fullPath = if ([System.IO.Path]::IsPathRooted($includePath)) {
            $includePath
        } else {
            Join-Path $BaseDir $includePath
        }
        
        if (-not (Test-Path $fullPath)) {
            continue
        }
        
        try {
            # Reset include stack before loading
            $script:IncludeStack = @()
            $included = Read-ManifestInternal -Path $fullPath
            
            if ($included.apps) {
                foreach ($app in $included.apps) {
                    if ($app.id) {
                        $includedIds[$app.id] = $true
                    }
                }
            }
        } catch {
            # Warn but continue - can't load include
            Write-Warning "Could not load include for deduplication: $fullPath - $($_.Exception.Message)"
        }
    }
    
    return $includedIds
}

function Merge-ManifestsForUpdate {
    <#
    .SYNOPSIS
        Merge new capture data into existing manifest.
    .DESCRIPTION
        Pure function for merging manifests during update mode.
        Preserves existing includes, restore, verify blocks.
        Updates captured timestamp and merges apps.
    .PARAMETER ExistingManifest
        The existing manifest hashtable (raw, not resolved).
    .PARAMETER NewCaptureApps
        Array of app objects from new capture.
    .PARAMETER IncludedAppIds
        Hashtable of app IDs that come from includes (to avoid duplication).
    .PARAMETER PruneMissingApps
        If true, remove apps not present in new capture.
    .PARAMETER NewIncludes
        New includes to add (e.g., manual include from discovery).
    .OUTPUTS
        Merged manifest hashtable.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$ExistingManifest,
        
        [Parameter(Mandatory = $false)]
        [AllowEmptyCollection()]
        [array]$NewCaptureApps = @(),
        
        [Parameter(Mandatory = $false)]
        [hashtable]$IncludedAppIds = @{},
        
        [Parameter(Mandatory = $false)]
        [switch]$PruneMissingApps,
        
        [Parameter(Mandatory = $false)]
        [string[]]$NewIncludes = @()
    )
    
    # Start with a copy of existing manifest structure
    $merged = @{
        version = if ($ExistingManifest.version) { $ExistingManifest.version } else { 1 }
        name = if ($ExistingManifest.name) { $ExistingManifest.name } else { "" }
        captured = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
    }
    
    # Preserve existing includes
    $existingIncludes = @()
    if ($ExistingManifest.includes) {
        $existingIncludes = @($ExistingManifest.includes)
    }
    
    # Add new includes that don't already exist
    foreach ($newInc in $NewIncludes) {
        if ($existingIncludes -notcontains $newInc) {
            $existingIncludes += $newInc
        }
    }
    
    if ($existingIncludes.Count -gt 0) {
        $merged.includes = $existingIncludes
    }
    
    # Preserve restore and verify blocks
    if ($ExistingManifest.restore) {
        $merged.restore = @($ExistingManifest.restore)
    } else {
        $merged.restore = @()
    }
    
    if ($ExistingManifest.verify) {
        $merged.verify = @($ExistingManifest.verify)
    } else {
        $merged.verify = @()
    }
    
    # Build lookup of new capture apps by ID
    $newAppsById = @{}
    foreach ($app in $NewCaptureApps) {
        if ($app.id) {
            $newAppsById[$app.id] = $app
        }
    }
    
    # Build lookup of existing root apps by ID
    $existingAppsById = @{}
    if ($ExistingManifest.apps) {
        foreach ($app in $ExistingManifest.apps) {
            if ($app.id) {
                $existingAppsById[$app.id] = $app
            }
        }
    }
    
    # Merge apps
    $mergedApps = @{}
    
    # First, process existing apps
    foreach ($id in $existingAppsById.Keys) {
        # Skip if this app comes from an include (don't duplicate)
        if ($IncludedAppIds.ContainsKey($id)) {
            continue
        }
        
        if ($newAppsById.ContainsKey($id)) {
            # App exists in both - use new capture data (refs may have changed)
            $mergedApps[$id] = $newAppsById[$id]
        } elseif (-not $PruneMissingApps) {
            # App only in existing - keep it (unless pruning)
            $mergedApps[$id] = $existingAppsById[$id]
        }
        # If PruneMissingApps and app not in new capture, it gets dropped
    }
    
    # Add new apps that don't exist in existing manifest
    foreach ($id in $newAppsById.Keys) {
        # Skip if this app comes from an include (don't duplicate)
        if ($IncludedAppIds.ContainsKey($id)) {
            continue
        }
        
        if (-not $mergedApps.ContainsKey($id)) {
            $mergedApps[$id] = $newAppsById[$id]
        }
    }
    
    # Sort apps by ID for deterministic output
    $merged.apps = @($mergedApps.Values | Sort-Object -Property { $_.id })
    
    return $merged
}

function Test-ProfileManifest {
    <#
    .SYNOPSIS
        Validate a file against the Endstate profile contract.
    .DESCRIPTION
        Canonical validation function for profile manifests.
        Checks file existence, parseability, and profile signature.
        See docs/profile-contract.md for the full contract specification.
    .PARAMETER Path
        Path to the file to validate.
    .OUTPUTS
        Hashtable with:
        - Valid: boolean indicating if the file is a valid profile
        - Errors: array of error objects with Code and Message
        - Summary: hashtable with name, version, appCount, captured (if valid)
    .EXAMPLE
        $result = Test-ProfileManifest -Path ".\manifests\my-profile.jsonc"
        if ($result.Valid) { "Profile is valid" }
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    $result = @{
        Valid = $false
        Errors = @()
        Summary = $null
    }
    
    # Check 1: File exists
    if (-not (Test-Path $Path)) {
        $result.Errors += @{
            Code = "FILE_NOT_FOUND"
            Message = "File does not exist: $Path"
        }
        return $result
    }
    
    # Check 2: Parseable (JSON/JSONC/JSON5)
    $manifest = $null
    try {
        $content = Get-Content -Path $Path -Raw -Encoding UTF8
        $extension = [System.IO.Path]::GetExtension($Path).ToLower()
        
        $manifest = switch ($extension) {
            ".jsonc" { ConvertFrom-Jsonc -Content $content -Depth 100 }
            ".json"  { ConvertFrom-Jsonc -Content $content -Depth 100 }
            ".json5" { ConvertFrom-Jsonc -Content $content -Depth 100 }
            default  { ConvertFrom-Jsonc -Content $content -Depth 100 }
        }
        
        if ($manifest -is [PSCustomObject]) {
            $manifest = Convert-PsObjectToHashtable -InputObject $manifest
        }
    } catch {
        $result.Errors += @{
            Code = "PARSE_ERROR"
            Message = "Invalid JSON/JSONC syntax: $($_.Exception.Message)"
        }
        return $result
    }
    
    # Check 3: Version field exists
    if (-not $manifest.ContainsKey('version')) {
        $result.Errors += @{
            Code = "MISSING_VERSION"
            Message = "No 'version' field present"
        }
        return $result
    }
    
    # Check 4: Version is a number
    $version = $manifest.version
    if ($version -isnot [int] -and $version -isnot [double] -and $version -isnot [long]) {
        $result.Errors += @{
            Code = "INVALID_VERSION_TYPE"
            Message = "Field 'version' must be a number, got: $($version.GetType().Name)"
        }
        return $result
    }
    
    # Check 5: Version is supported (currently only v1)
    if ([int]$version -ne 1) {
        $result.Errors += @{
            Code = "UNSUPPORTED_VERSION"
            Message = "Unsupported profile version: $version (supported: 1)"
        }
        return $result
    }
    
    # Check 6: Apps field exists
    if (-not $manifest.ContainsKey('apps')) {
        $result.Errors += @{
            Code = "MISSING_APPS"
            Message = "No 'apps' field present"
        }
        return $result
    }
    
    # Check 7: Apps is an array
    $apps = $manifest.apps
    if ($null -eq $apps) {
        # null is acceptable, treat as empty array
        $apps = @()
    } elseif ($apps -isnot [array] -and $apps -isnot [System.Collections.IEnumerable]) {
        $result.Errors += @{
            Code = "INVALID_APPS_TYPE"
            Message = "Field 'apps' must be an array"
        }
        return $result
    }
    
    # Ensure apps is an array
    $apps = @($apps)
    
    # Optional: Warn about app entries missing 'id' (not a hard failure for backward compat)
    $warnings = @()
    $appIndex = 0
    foreach ($app in $apps) {
        $appIndex++
        if ($app -is [hashtable] -or $app -is [PSCustomObject]) {
            $appHash = if ($app -is [PSCustomObject]) { 
                @{}; $app.PSObject.Properties | ForEach-Object { $appHash[$_.Name] = $_.Value }
                $appHash
            } else { $app }
            
            if (-not $appHash.ContainsKey('id') -or [string]::IsNullOrWhiteSpace($appHash.id)) {
                $warnings += @{
                    Code = "INVALID_APP_ENTRY"
                    Message = "App entry at index $appIndex is missing 'id' field"
                }
            }
        }
    }
    
    # Profile is valid
    $result.Valid = $true
    $result.Summary = @{
        Name = if ($manifest.name) { $manifest.name } else { "" }
        Version = [int]$version
        AppCount = $apps.Count
        Captured = if ($manifest.captured) { $manifest.captured } else { $null }
    }
    
    # Include warnings in errors array (they don't invalidate the profile)
    if ($warnings.Count -gt 0) {
        $result.Warnings = $warnings
    }
    
    return $result
}

# Functions exported: Read-Manifest, Read-ManifestRaw, Write-Manifest, Read-JsoncFile, ConvertFrom-Jsonc, ConvertTo-Jsonc, ConvertFrom-SimpleYaml, ConvertTo-SimpleYaml, Get-IncludedAppIds, Merge-ManifestsForUpdate, Test-ProfileManifest
