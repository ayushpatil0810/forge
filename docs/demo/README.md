# Forge Demo Recording

This directory contains assets and scripts for generating the Forge animated demonstration GIF using [Charm VHS](https://github.com/charmbracelet/vhs).

## Recording the Demo

Run via Makefile:
```bash
make demo-gif
```

Or run `vhs` directly:
```bash
vhs docs/demo/demo.tape
```

The output will be rendered to `docs/demo/demo.gif`.

## Prerequisites

### Windows
Install VHS and its dependencies using `winget` or `scoop`:
```powershell
# Using winget
winget install charmbracelet.vhs
winget install Gyan.FFmpeg
winget install ttyd

# Or using scoop
scoop install vhs ffmpeg ttyd
```

### macOS
```bash
brew install vhs ffmpeg ttyd
```

### Linux (Ubuntu/Debian)
```bash
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://repo.charm.sh/apt/gpg.key | sudo gpg --dearmor -o /etc/apt/keyrings/charm.gpg
echo "deb [signed-by=/etc/apt/keyrings/charm.gpg] https://repo.charm.sh/apt/ * *" | sudo tee /etc/apt/sources.list.d/charm.list
sudo apt update && sudo apt install vhs ffmpeg ttyd
```
