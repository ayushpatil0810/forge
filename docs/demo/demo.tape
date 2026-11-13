# VHS tape for Forge AI coding agent demo
# Requirements: vhs, ffmpeg, ttyd
# --demo-speed controls simulation delay multiplier (1.0 = normal, 0.5 = 2x faster, 0 = instant CI execution)
Output docs/demo/demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 750
Set Theme "TokyoNight"
Set Padding 16

Type "forge --demo --demo-speed 1.0"
Sleep 1s
Enter

Sleep 3s
Type "Fix the failing test in this workspace"
Sleep 1s
Enter

# Step 1: Initial analysis & failing test run
Sleep 14s

# Step 2: Diagnosing boundary error and inspecting code
Sleep 10s

# Step 3: Presenting diff and awaiting approval
Sleep 12s
Type "y"
Sleep 2s

# Step 4: Re-running test and passing verification
Sleep 10s

# Step 5: Final explanation and review
Sleep 6s
