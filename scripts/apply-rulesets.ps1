# Applies the rulesets in .github/rulesets/*.json to the GitHub repository:
# a ruleset whose name exists on GitHub is replaced, a new one is created.
# The JSON files are the definition; change a rule there, then run this
# script (gh must be logged in with admin rights on the repository).
#
#   powershell -ExecutionPolicy Bypass -File scripts\apply-rulesets.ps1

$ErrorActionPreference = 'Stop'
$repo = 'phsc84/MountSafe'
$root = Split-Path -Parent $PSScriptRoot

$existing = gh api "repos/$repo/rulesets" | ConvertFrom-Json
if ($LASTEXITCODE -ne 0) { throw 'Could not read the rulesets (is gh logged in?)' }

foreach ($file in Get-ChildItem (Join-Path $root '.github\rulesets') -Filter *.json) {
    $name = (Get-Content $file.FullName -Raw | ConvertFrom-Json).name
    $match = $existing | Where-Object { $_.name -eq $name }
    if ($match) {
        gh api --method PUT "repos/$repo/rulesets/$($match.id)" --input $file.FullName --jq '.name' | Out-Null
        $action = 'updated'
    } else {
        gh api --method POST "repos/$repo/rulesets" --input $file.FullName --jq '.name' | Out-Null
        $action = 'created'
    }
    if ($LASTEXITCODE -ne 0) { throw "Applying $($file.Name) failed" }
    Write-Output "$name ($($file.Name)): $action"
}
