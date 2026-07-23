$repo = "1098542053/spive2d-web"
$authorName = "1098542053"
$authorEmail = "1098542053@users.noreply.github.com"
$commitMsg = "feat: keep logged in - replace remember password with stay logged in"
$remoteRef = "1a5796df4310c86cab5e9eb2751c9d1e6b86391e"
$baseTreeSha = "cd6256749b1608d7469280dd093d93fddc6f7f56"
$repoRoot = "I:\AICode\spive2d\spive2d-web"

Write-Host "=== Step 1: Get all tree entries from local HEAD ==="
$treeLines = git ls-tree -r HEAD --full-name
$allEntries = @()
foreach ($line in $treeLines) {
    $parts = $line -split '\s+', 4
    $mode = $parts[0]
    $type = $parts[1]
    $sha = $parts[2]
    $path = $parts[3]
    $allEntries += @{mode=$mode; type=$type; sha=$sha; path=$path}
}
Write-Host "Total files in tree: $($allEntries.Count)"

# Get the diff to know which files are new or modified
$diffLines = git diff-tree -r --no-commit-id $remoteRef HEAD
$changedPaths = @{}
foreach ($line in $diffLines) {
    $parts = $line -split '\s+', 6
    $status = $parts[4]
    $path = $parts[5]
    if ($status -eq 'A' -or $status -eq 'M') {
        $changedPaths[$path] = $true
    }
}
Write-Host "Files to create blobs for: $($changedPaths.Count)"

# Step 2: Create blobs for all changed files
Write-Host "=== Step 2: Create blobs ==="
$blobShas = @{}
$i = 0
foreach ($entry in $allEntries) {
    $path = $entry.path
    if (-not $changedPaths.ContainsKey($path)) {
        continue
    }
    $i++
    Write-Host "[$i/$($changedPaths.Count)] Creating blob for $path (mode=$($entry.mode)) ..."
    
    # Read the file directly from disk
    $diskPath = Join-Path $repoRoot $path
    $bytes = [System.IO.File]::ReadAllBytes($diskPath)
    $b64 = [Convert]::ToBase64String($bytes)
    
    $jsonPayload = @{
        content = $b64
        encoding = "base64"
    } | ConvertTo-Json -Compress
    
    $jsonFile = "$env:TEMP\blob_payload_$i.json"
    [System.IO.File]::WriteAllText($jsonFile, $jsonPayload, [System.Text.UTF8Encoding]::new($false))
    
    $result = gh api repos/$repo/git/blobs --method POST --input $jsonFile --jq ".sha" 2>&1 | Select-Object -Last 1
    $blobSha = $result.Trim()
    Write-Host "  -> blob SHA: $blobSha"
    $blobShas[$path] = $blobSha
}

# Step 3: Create tree
Write-Host "=== Step 3: Create tree ==="
$treeEntries = @()
foreach ($entry in $allEntries) {
    $path = $entry.path
    $mode = $entry.mode
    $sha = if ($blobShas.ContainsKey($path)) { $blobShas[$path] } else { $entry.sha }
    $treeEntries += @{path=$path; mode=$mode; type="blob"; sha=$sha}
}

$treePayload = @{
    base_tree = $baseTreeSha
    tree = $treeEntries
} | ConvertTo-Json -Depth 10 -Compress

$treeFile = "$env:TEMP\tree_payload.json"
[System.IO.File]::WriteAllText($treeFile, $treePayload, [System.Text.UTF8Encoding]::new($false))
Write-Host "Creating tree with $($treeEntries.Count) entries..."
$treeSha = gh api repos/$repo/git/trees --method POST --input $treeFile --jq ".sha" 2>&1 | Select-Object -Last 1
$treeSha = $treeSha.Trim()
Write-Host "Tree SHA: $treeSha"

# Step 4: Create commit
Write-Host "=== Step 4: Create commit ==="
$commitPayload = @{
    message = $commitMsg
    tree = $treeSha
    parents = @($remoteRef)
    author = @{
        name = $authorName
        email = $authorEmail
        date = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
    }
} | ConvertTo-Json -Depth 10 -Compress

$commitFile = "$env:TEMP\commit_payload.json"
[System.IO.File]::WriteAllText($commitFile, $commitPayload, [System.Text.UTF8Encoding]::new($false))
$commitSha = gh api repos/$repo/git/commits --method POST --input $commitFile --jq ".sha" 2>&1 | Select-Object -Last 1
$commitSha = $commitSha.Trim()
Write-Host "Commit SHA: $commitSha"

# Step 5: Update ref
Write-Host "=== Step 5: Update ref ==="
$refPayload = @{
    sha = $commitSha
    force = $true
} | ConvertTo-Json -Compress

$refFile = "$env:TEMP\ref_payload.json"
[System.IO.File]::WriteAllText($refFile, $refPayload, [System.Text.UTF8Encoding]::new($false))
$refResult = gh api repos/$repo/git/refs/heads/master --method PATCH --input $refFile --jq ".object.sha" 2>&1 | Select-Object -Last 1
$refResult = $refResult.Trim()
Write-Host "Done! Ref updated to: $refResult"

# Cleanup
Remove-Item "$env:TEMP\blob_payload_*.json" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\tree_payload.json" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\commit_payload.json" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\ref_payload.json" -Force -ErrorAction SilentlyContinue