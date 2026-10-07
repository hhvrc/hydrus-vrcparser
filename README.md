# hydrus-tagger

Tags files in a [Hydrus](https://hydrusnetwork.github.io/hydrus/) client from:

- **VRChat screenshots** -- the metadata VRChat and companion tools embed in PNG
  iTXt chunks, in any of three formats: VRCX JSON, VRChat's XMP, and the older
  pipe-delimited "line" format.
- **Twitter/X URLs** -- the account each known URL points at.

It is a single executable with no runtime to install, and only ever adds tags.
State is kept in a local SQLite cache (`vrchat.db`) so a scheduled run only
reads new files and only pushes tags that changed.

## Tags

| Source | Tags |
|---|---|
| VRChat | `vrchat`, `vrchat-author-id:`, `vrchat-author-name:`, `vrchat-world-id:`, `vrchat-world-name:`, `vrchat-world-instanceId:`, `vrchat-user-id:`, `vrchat-user-name:` (players present), `vrchat-date:`, `creator_tool:` (the XMP CreatorTool, verbatim), `editor:` (the app that edited the image, if any) |
| Twitter/X | `twitter-username:` -- from twitter.com, x.com and the fxtwitter/vxtwitter/fixupx/fixvx mirrors |

## Setup

1. In Hydrus, create a client API key (services > review services > client
   api) with permission to search files, read metadata and add tags.
2. Create `%APPDATA%\hydrus-tagger\config.json`:

   ```json
   {
     "api_key": "<key>",
     "service_name": "my tags",
     "data_directory": "\\\\server\\hydrus\\files"
   }
   ```

   `data_directory` is the Hydrus files directory (the one with the `f00`..`fff`
   folders); the VRChat tagger reads PNGs from it. Other settings, all optional:
   `hydrus_address` (default `http://127.0.0.1:45869`), `database` (default
   `vrchat.db`, relative to where you run the tool), `vrchat_selector` /
   `twitter_selector` (search overrides), `twitter_namespace`, `timeout` (per
   request, e.g. `"2m"`), `max_retries` (default 3; 0 disables retries).
   Unknown keys are an error, so a typo cannot silently fall back to a default.

   The key can also come from the `HYDRUS_API_KEY` environment variable.
   Flags override both.

## Usage

```powershell
hydrus-tagger run --dry-run   # preview, against a throwaway copy of the database
hydrus-tagger run             # derive and push
hydrus-tagger run --only twitter
hydrus-tagger status          # what the cache database holds (read-only)
hydrus-tagger correlate       # suggest which vrchat-user-id and -name tags are the same person
```

`run` checks Hydrus is reachable and the tag service exists before touching the
database. Ctrl+C stops it after saving progress; nothing done so far is redone.

## How it works

Each tagger names a Hydrus search, and the host does the rest:

1. **Discover** candidates with the tagger's search.
2. **Extract** (VRChat only): read new files' iTXt chunks off disk and cache
   them. Versioned separately, so improving a parser never re-reads the share.
3. **Derive** tags for files whose version is stale, or every file for taggers
   whose input is live Hydrus metadata (Twitter: a file can gain a URL).
4. **Push** only tag sets whose hash differs from the ledger of what was last
   pushed, grouping identical sets into one request.

Progress is saved after every extract chunk, after deriving and after every
push batch.

## Building

Requires Go 1.27.

```powershell
go build -o dist\hydrus-tagger.exe ./cmd/hydrus-tagger
go test ./...
go vet ./...
```

## License

[GNU General Public License v2](LICENSE)
