$baseTreeSha = "cf50269fb257a2a4fa70fa7fd194d607970cf94e"
$parentSha = "07ab491d6f8867493ac8455969e5d17a051c2453"
$authorName = "1098542053"
$authorEmail = "1098542053@users.noreply.github.com"
$commitMsg = "feat: rebuild frontend - login remember password, version display fix"

# All changed files in the commit
$files = @(
    @{path="build/_app/immutable/assets/_page.BW2TVcl-.css"; mode="100644"},
    @{path="build/_app/immutable/chunks/CrcNBXFM.js"; mode="100644"},
    @{path="build/_app/immutable/chunks/Cxz3plwH.js"; mode="100644"},
    @{path="build/_app/immutable/chunks/DnQUvh6S.js"; mode="100644"},
    @{path="build/_app/immutable/chunks/Dz50Ik7a.js"; mode="100644"},
    @{path="build/_app/immutable/chunks/qYUdjbMy.js"; mode="100644"},
    @{path="build/_app/immutable/entry/app.DqEwRuOf.js"; mode="100644"},
    @{path="build/_app/immutable/entry/start.dnkDI02p.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/0.CYysG2WO.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/1.BRrKnY0A.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/2.D_tZ2eAp.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/3.B4d-D3zq.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/4.DpOZroDK.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/5.DoQFUBFE.js"; mode="100644"},
    @{path="build/_app/immutable/nodes/6.Y0rJpw2g.js"; mode="100644"},
    @{path="build/_app/version.json"; mode="100644"},
    @{path="build/index.html"; mode="100644"}
)

# Step 1: Create blobs for all files
$treeEntries = @()
foreach ($f in $files) {
    $path = $f.path
    $mode = $f.mode
    Write-Host "Creating blob for $path ..."
    
    $content = git show a71fa30:"$path"
    $content | Set-Content -Path "$env:TEMP\blob_temp" -Encoding UTF8 -NoNewline
    $bytes = [System.IO.File]::ReadAllBytes("$env:TEMP\blob_temp")
    $b64 = [Convert]::ToBase64String($bytes)
    
    $result = gh api repos/1098542053/spive2d-web/git/blobs --method POST -f "content=$b64" -f "encoding=base64" --jq ".sha" 2>&1 | Select-Object -Last 1
    $sha = $result.Trim()
    Write-Host "  -> blob SHA: $sha"
    
    $treeEntries += @{path=$path; mode=$mode; type="blob"; sha=$sha}
}

# Step 2: Create tree
Write-Host "Creating tree with $($treeEntries.Count) entries..."
$treePayload = @{
    base_tree = $baseTreeSha
    tree = $treeEntries
} | ConvertTo-Json -Depth 10 -Compress

$treePayload | Set-Content -Path "$env:TEMP\tree_payload.json" -Encoding UTF8 -NoNewline
$treeSha = gh api repos/1098542053/spive2d-web/git/trees --method POST --input "$env:TEMP\tree_payload.json" --jq ".sha" 2>&1 | Select-Object -Last 1
$treeSha = $treeSha.Trim()
Write-Host "Tree SHA: $treeSha"

# Step 3: Create commit
$commitPayload = @{
    message = $commitMsg
    tree = $treeSha
    parents = @($parentSha)
    author = @{
        name = $authorName
        email = $authorEmail
        date = (Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ")
    }
} | ConvertTo-Json -Depth 10 -Compress

$commitPayload | Set-Content -Path "$env:TEMP\commit_payload.json" -Encoding UTF8 -NoNewline
$commitSha = gh api repos/1098542053/spive2d-web/git/commits --method POST --input "$env:TEMP\commit_payload.json" --jq ".sha" 2>&1 | Select-Object -Last 1
$commitSha = $commitSha.Trim()
Write-Host "Commit SHA: $commitSha"

# Step 4: Update ref
Write-Host "Updating ref heads/master to $commitSha ..."
$refPayload = @{
    sha = $commitSha
    force = $true
} | ConvertTo-Json -Compress

$refPayload | Set-Content -Path "$env:TEMP\ref_payload.json" -Encoding UTF8 -NoNewline
$refResult = gh api repos/1098542053/spive2d-web/git/refs/heads/master --method PATCH --input "$env:TEMP\ref_payload.json" --jq ".object.sha" 2>&1 | Select-Object -Last 1
$refResult = $refResult.Trim()
Write-Host "Done! Ref updated to: $refResult"

# Cleanup
Remove-Item "$env:TEMP\blob_temp" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\tree_payload.json" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\commit_payload.json" -Force -ErrorAction SilentlyContinue
Remove-Item "$env:TEMP\ref_payload.json" -Force -ErrorAction SilentlyContinue