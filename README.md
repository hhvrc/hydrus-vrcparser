# Hydrus VRChat Parser

Extract embedded metadata from VRChat screenshots stored in [Hydrus](https://hydrusnetwork.github.io/hydrus/) and push standardized tags back for organization and searchability.

VRChat embeds metadata in PNG iTXt chunks. This tool reads those chunks, parses them across three formats (JSON, XMP/XML, and legacy pipe-delimited), normalizes the data, and pushes tags to a Hydrus tag service.

## What gets tagged

- **Author** -- VRChat user ID and display name
- **World** -- world ID, instance ID, and world name
- **Players** -- list of players present in the instance
- **Date** -- screenshot capture date (from XMP metadata)
- **Position** -- camera coordinates at time of capture
- **Render quality** setting

## Requirements

- Python 3.9+
- A running [Hydrus client](https://hydrusnetwork.github.io/hydrus/) with the API enabled
- Hydrus API access key (Client > Services > Review services > local > client api)

## Installation

```bash
git clone https://github.com/hhvrc/hydrus-vrcparser.git
cd hydrus-vrcparser
python -m venv .venv

# Windows
.venv\Scripts\activate

# Linux/macOS
source .venv/bin/activate

pip install -r requirements.txt
```

## Usage

```bash
python hydrus-vrcparser.py \
    --api-key YOUR_API_KEY \
    --hydrus-addr http://localhost:45869 \
    --data-dir /path/to/hydrus/db/client_files \
    --service-name "my tags"
```

CLI arguments override values in `config.json`. The merged result is persisted back to the config file for subsequent runs, so you only need to pass arguments once.

After the first run, simply execute: `python hydrus-vrcparser.py`

### CLI options

| Flag | Description | Default |
|---|---|---|
| `--api-key` | Hydrus API key | from config.json |
| `--hydrus-addr` | Hydrus client API address | from config.json |
| `--data-dir` | Path to Hydrus `client_files` directory | from config.json |
| `--service-name` | Hydrus local tag service name | auto-detect if only one |
| `--db` | SQLite database path | `./vrchat.db` |
| `--config` | Config file path | `config.json` |

## How it works

1. **Discover** -- Queries Hydrus for PNGs with embedded metadata
2. **Extract** -- Reads PNG iTXt chunks from disk; caches chunks to skip redundant disk I/O on subsequent runs
3. **Recover** -- Scans `broken_metadata/` for previously failed files; retries with lenient parsing
4. **Parse** -- Detects format (JSON > XMP > legacy) and parses with field-level error handling
5. **Normalize** -- Converts all formats to common schema
6. **Tag** -- Builds tag mappings from normalized metadata
7. **Push** -- Pushes tags to Hydrus only when changed (SHA256 comparison)

Metadata that fails to parse is saved to `broken_metadata/` for manual inspection.

## Supported metadata formats

| Format | Source | Parsed Fields |
|---|---|---|
| **JSON** | VRCX screenshot manager | Author, world, instance, players |
| **XMP/XML** | VRChat native (normal & compact forms) | Author, world, capture date |
| **Legacy pipe** | screenshotmanager / lfs | Author, world, instance, position, players, render quality |

All formats normalized to a common schema before tag generation.

## Testing

```bash
python -m unittest discover -s tests -v
```

## Building a standalone executable

```bash
pip install pyinstaller
pyinstaller hydrus-vrcparser.spec
```

Output: `dist/hydrus-vrcparser.exe`

## Development

See [CLAUDE.md](CLAUDE.md) for architecture details, conventions, and code guidance.

## License

[GNU General Public License v2](LICENSE)
