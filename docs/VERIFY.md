# Forge Live Human Verification Guide

This document defines the exact, reproducible manual verification steps for all features requiring human live verification in Forge.

Every step references a built binary via an explicit absolute-path variable `$forge` so no assumptions are made about system PATH.

## Setup Environment

Run all commands in PowerShell (Windows 10/11 or PowerShell Core):

```powershell
# Set absolute path to compiled forge binary
$forge = (Get-Item .\forge.exe).FullName
Write-Host "Using Forge binary at: $forge"

# Verify binary executes
& $forge --version
```

---

## 1. CI Status Check (ISSUE-2)

### Steps
1. Navigate to the GitHub repository: `https://github.com/ayushpatil0810/forge/actions`
2. Inspect the latest workflow run on branch `main`.
3. Check the matrix across:
   - `ubuntu-latest` (`1.24.x`, `stable`)
   - `macos-latest` (`1.24.x`, `stable`)
   - `windows-latest` (`1.24.x`, `stable`)
   - `lint` (gofmt, go mod tidy, govulncheck, golangci-lint)

### Expected Result
All jobs pass green. On Windows, MinGW provides `gcc` so `-race` runs cleanly.

### If It Fails
Record in `docs/PROGRESS.md` under **Known issues** (`ISSUE-2`):
- Note runner OS, Go version, and failed step log snippet.

---

## 2. Slice 6: Fresh Machine Check & Demo Recording

### Steps
1. On a clean machine (or VM/sandbox container without Go or developer toolchains installed):
```powershell
# Copy standalone forge archive or binary
$forge = "C:\path\to\forge.exe"

# Verify standalone execution with no external dependencies
& $forge --version

# Run headless demo to verify bundled scripts
& $forge --demo --headless "explore the workspace and summarize README"
```
2. Demo recording with VHS:
```powershell
# Ensure vhs is installed
vhs docs/demo/demo.tape
```

### Expected Result
- Binary executes without missing runtime DLLs or C runtime errors.
- Demo tape records `docs/demo/demo.gif` smoothly within 45–75 seconds.

### If It Fails
Record in `docs/PROGRESS.md`:
- Note OS distribution, missing dependencies, or rendering glitches in VHS.

---

## 3. Slice 7: Checkpoints and Undo

*(Dry-runnable with `--demo`)*

### Steps
```powershell
$scratch = Join-Path $env:TEMP "forge-verify-slice7"
Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $scratch | Out-Null
Set-Location $scratch

# Initialize a git workspace
git init
"initial content" | Set-Content file.txt
git add file.txt
git commit -m "initial commit"

# Run forge with mock/demo to edit file
& $forge --demo --yes --root $scratch "modify file.txt to say updated content"

# Verify checkpoint created in user cache (not workspace)
Test-Path (Join-Path $scratch ".forge/checkpoints") # Must be False

# Verify undo works
& $forge checkpoints list --root $scratch
& $forge undo --root $scratch

# Check content reverted
Get-Content file.txt
```

### Expected Result
- Workspace `.forge/` contains zero checkpoint files (shadow repo is strictly in `%LOCALAPPDATA%\forge\checkpoints`).
- Checkpoint lists previous state.
- `forge undo` cleanly restores `file.txt` to `"initial content"`.

### If It Fails
Record in `docs/PROGRESS.md`:
- Note if files leak into workspace or if shadow repo lock contention occurs.

---

## 4. Slice 8: Setup Flow, Credentials, and Providers

### A. Non-Interactive / Dry-Run Check
```powershell
# Verify invalid --api-key flag is rejected with exit code 2
& $forge --api-key "sk-fake"
Write-Host "Exit code: $LASTEXITCODE" # Must be 2
```

### B. Live Provider Setup (Requires a Real API Key)
*(Cannot be dry-run without a real API key)*

```powershell
# First launch: setup screen asks for a provider, an API key, and a model ID you type
& $forge

# Scripted alternative: store a key without opening the TUI
"sk-proj-your-real-key-here" | & $forge auth login --provider openai --with-key

# Check credential status
& $forge auth status

# Run live query
& $forge --provider openai "Say hello and confirm you can read files."
```

### C. Local Provider Check (Ollama or LM Studio)
*(Optional, if local server running)*

```powershell
# Configure local model endpoint
& $forge config set provider local
& $forge config set base_url http://localhost:11434/v1
& $forge config set model llama3

# Run local query
& $forge "Test local inference."
```

### Expected Result
- Key securely stored in OS credential store (Windows Credential Manager / macOS Keychain / freedesktop Secret Service).
- `forge auth status` displays provider as configured and masked key ID.
- Inference completes successfully without leaking keys in terminal or logs.

### If It Fails
Record in `docs/PROGRESS.md`:
- Note OS credential vault error, keychain daemon error, or endpoint URL normalization issue.

---

## 5. Slice 9: Sessions and Resumption

*(Dry-runnable with `--demo`)*

### Steps
```powershell
$scratch = Join-Path $env:TEMP "forge-verify-slice9"
Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $scratch | Out-Null
Set-Location $scratch
git init

# Run initial session
& $forge --demo --yes --root $scratch "create a file hello.txt containing hello world"

# List sessions
$sessionsOutput = & $forge sessions list
Write-Host $sessionsOutput

# Extract session ID and resume
$sessionId = ($sessionsOutput -split '\n')[1].Split(' ')[0].Trim()
Write-Host "Resuming session: $sessionId"
& $forge resume $sessionId --demo --yes "read hello.txt and append a second line"

# Verify session doctor
& $forge sessions doctor
```

### Expected Result
- Session is automatically titled from first prompt.
- Session persists in SQLite user database (`%LOCALAPPDATA%\forge\forge.db`).
- Resumption restores full context and conversation history.
- `forge sessions doctor` reports `Database integrity OK`.

### If It Fails
Record in `docs/PROGRESS.md`:
- Note SQLite lock or schema mismatch issues.

---

## 6. Slice 9a: Policy, Audit Log, and Secrets

*(Dry-runnable with `--demo`)*

### Steps
```powershell
$scratch = Join-Path $env:TEMP "forge-verify-slice9a"
Remove-Item -Recurse -Force $scratch -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Path $scratch | Out-Null
Set-Location $scratch
git init
New-Item -ItemType Directory -Path (Join-Path $scratch ".forge") | Out-Null

# 1. Project policy with untrusted allow rule + deny rule
$projPolicy = @'
{
  "allow": ["bash(*)"],
  "deny": ["bash(git push:*)"]
}
'@
$projPolicy | Set-Content (Join-Path $scratch ".forge\policy.json")

# 2. Check effective policy: allow rule must be ignored with warning
& $forge policy list --root $scratch

# 3. Policy check CLI
& $forge policy check bash "git push origin main" --root $scratch # Must be denied
& $forge policy check bash "go test ./..." --root $scratch       # Must ask (since allow was ignored)

# 4. Verify hard safety floor: dangerous command triggers ask even if allowed
& $forge policy check bash "go test -exec evil ./..." --root $scratch # Must ask/floor

# 5. Verify audit log and tamper verification
& $forge audit verify
```

### Expected Result
- Output explicitly warns that project `allow` rule `bash(*)` is ignored.
- `git push` is blocked by project `deny` rule.
- `go test -exec` is caught by argument hazard safety floor.
- Audit log is written to `%LOCALAPPDATA%\forge\audit\` with `0600` permissions.
- `forge audit verify` returns 0 with intact SHA-256 tamper-evident hash chain.

### If It Fails
Record in `docs/PROGRESS.md`:
- Note if project policy allow rules were accepted (critical security bug).
- Note if tamper verification failed or audit log failed to append.
