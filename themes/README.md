# Forge Themes

Forge supports customizable semantic color themes for its interactive terminal interface (`T-8`, `T-11`).

## Available Built-In Themes

- `dark`: Default modern dark theme with Catppuccin-inspired palette.
- `light`: High-contrast, clean light theme for bright environments.
- `catppuccin-mocha`: Authentic Catppuccin Mocha theme.
- `tokyo-night`: Celebrated Tokyo Night colorway.
- `auto`: Automatically selects dark or light based on terminal background color.
- `monochrome`: Strips ANSI color codes for accessibility or dumb terminals (`NO_COLOR`).

## Selecting a Theme

Via CLI flag:
```bash
forge --theme tokyo-night
```

Via environment variable:
```bash
export FORGE_THEME=tokyo-night
```

Via configuration file (`~/.forge/config.json` or `.forge/config.json`):
```json
{
  "theme": "tokyo-night"
}
```

## Theme Schema

All themes define semantic tokens in a JSON format:

| Token | Description |
| :--- | :--- |
| `name` | Unique theme identifier |
| `is_dark` | Boolean flag indicating whether the theme has a dark background |
| `background` | Terminal canvas background color |
| `foreground` | Primary text content color |
| `primary` | User prompts, active indicators, and focus highlights |
| `secondary` | Auxiliary metadata, badges, and accents |
| `accent` | Highlighted keywords and emphasis |
| `muted` | Subtle captions, timestamps, and dim info |
| `error` | Failure indicators, alerts, and destructive actions |
| `success` | Passed checks, approved actions, and completions |
| `warning` | Cautions, dangerous command warnings, and rate limit notices |
| `border` | Card and modal borders |
| `glamour_style` | Markdown renderer style (`dark` or `light`) |
