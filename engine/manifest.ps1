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

function Read-Manifest {
    <#
    .SYNOPSIS
        Load a manifest file and resolve all includes.
    .DESCRIPTION
        Supports .jsonc, .json, and .yaml/.yml formats.
        Resolves includes recursively with circular dependency detection.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    # Reset include stack for top-level call
    $script:IncludeStack = @()
    
    $manifest = Read-ManifestInternal -Path $Path
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
            ".jsonc" { ConvertFrom-Jsonc -Content $content }
            ".json"  { $content | ConvertFrom-Json -AsHashtable }
            ".yaml"  { ConvertFrom-SimpleYaml -Content $content }
            ".yml"   { ConvertFrom-SimpleYaml -Content $content }
            default  {
                # Try JSONC first, fall back to YAML
                try {
                    ConvertFrom-Jsonc -Content $content
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

function ConvertFrom-Jsonc {
    <#
    .SYNOPSIS
        Parse JSONC (JSON with comments) content.
    .DESCRIPTION
        Strips single-line (//) and multi-line (/* */) comments before parsing.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Content
    )
    
    # Remove single-line comments (// ...)
    # Be careful not to remove // inside strings
    $inString = $false
    $escaped = $false
    $result = [System.Text.StringBuilder]::new()
    $i = 0
    
    while ($i -lt $Content.Length) {
        $char = $Content[$i]
        $nextChar = if ($i + 1 -lt $Content.Length) { $Content[$i + 1] } else { $null }
        
        if ($escaped) {
            [void]$result.Append($char)
            $escaped = $false
            $i++
            continue
        }
        
        if ($char -eq '\' -and $inString) {
            [void]$result.Append($char)
            $escaped = $true
            $i++
            continue
        }
        
        if ($char -eq '"' -and -not $escaped) {
            $inString = -not $inString
            [void]$result.Append($char)
            $i++
            continue
        }
        
        if (-not $inString) {
            # Check for single-line comment
            if ($char -eq '/' -and $nextChar -eq '/') {
                # Skip until end of line
                while ($i -lt $Content.Length -and $Content[$i] -ne "`n") {
                    $i++
                }
                continue
            }
            
            # Check for multi-line comment
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
        
        [void]$result.Append($char)
        $i++
    }
    
    $cleanJson = $result.ToString()
    
    # Parse the cleaned JSON
    try {
        $parsed = $cleanJson | ConvertFrom-Json -AsHashtable
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
    foreach ($arrayKey in @('apps', 'restore', 'verify', 'includes')) {
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

# Functions exported: Read-Manifest, Write-Manifest, ConvertFrom-Jsonc, ConvertTo-Jsonc, ConvertFrom-SimpleYaml, ConvertTo-SimpleYaml
