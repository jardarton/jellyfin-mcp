# jellyfin-mcp

An [MCP](https://modelcontextprotocol.io) server for [Jellyfin](https://jellyfin.org). It lets an agent search and browse a Jellyfin library, see what people are watching, keep track of watched state, favorites and playlists, remote control clients, and run the usual maintenance: library scans, metadata refreshes and scheduled tasks.

It is a single static Go binary built with the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), and speaks MCP over stdio or Streamable HTTP. It targets Jellyfin 10.10 and later.

## Tools

| Tool | What it does |
| --- | --- |
| `find_items` | Search or browse by title, type, library, genre, year, person, watched state and favorites, with sorting and paging |
| `get_item` | Details of one item: overview, cast and crew, ratings, external ids, audio, video and subtitle tracks, path |
| `libraries` | Libraries and item counts |
| `latest` | Recently added |
| `continue_watching` | Partly watched items and where they stopped |
| `next_up` | Next episode of each series in progress |
| `episodes` | A series' episodes with watched state |
| `similar` | Similar items, for recommendations |
| `playlists` | Playlists, or the items of one |
| `users` | Users and when they were last active |
| `sessions` | Connected clients and what they are playing, including transcode reasons |
| `server_info` | Version and whether a restart is pending |
| `scheduled_tasks` | Scheduled tasks and how their last run went |
| `activity_log` | The server's activity log |
| `mark_played`, `mark_unplayed` | Mark items watched or unwatched |
| `set_favorite` | Add or remove favorites |
| `create_playlist`, `add_to_playlist`, `remove_from_playlist` | Manage playlists |
| `control_playback` | Play, queue, pause, stop, skip or seek on a client |
| `send_message` | Show a message on a client |
| `scan_library` | Scan one or all libraries for new files, as the scheduled scan does |
| `refresh_metadata` | Re-read an item's files and look up missing metadata and images |
| `run_task` | Start a scheduled task |

Items can be given by id or by name, with an optional year: `"Metropolis (1927)"`. When a name matches several items, the error lists them with their ids.

Tools carry MCP annotations, so a client or gateway can ask before risky calls. Everything that only reads is marked read-only. `mark_played` and `mark_unplayed` (which overwrite play counts and resume positions, for everything in a series or album), `refresh_metadata` (re-reading files can overwrite fields, such as names in a library that uses embedded titles, and recalculates runtime and chapters), `remove_from_playlist` and `run_task` (some tasks delete data) are marked destructive.

Watch state, favorites, continue watching, next up and playlists belong to a Jellyfin user. Calls take a `user` name; without one they use `JELLYFIN_USER`, or the server's only user.

## Configuration

Create an API key in Jellyfin under Dashboard → API Keys, then configure the server through the environment:

| Variable | |
| --- | --- |
| `JELLYFIN_URL` | Server URL, such as `http://jellyfin:8096`. Required. |
| `JELLYFIN_API_KEY` | The API key. Required, unless `JELLYFIN_API_KEY_FILE` names a file holding it. |
| `JELLYFIN_USER` | User name or id that tools act for when a call names none. |
| `JELLYFIN_MCP_TRANSPORT` | `stdio` (default) or `http`. The container image defaults to `http`. |
| `JELLYFIN_MCP_ADDR` | Listen address for `http`, default `127.0.0.1:8080`. The container image uses `:8080`. |
| `JELLYFIN_MCP_AUTH_TOKEN` | When set, `http` requires `Authorization: Bearer <token>`. Or use `JELLYFIN_MCP_AUTH_TOKEN_FILE`. |
| `JELLYFIN_MCP_ALLOWED_HOSTS` | Comma-separated host names that `http` answers to, such as `jellyfin-mcp,mcp.example.com`, or `*` for any. Guards against DNS rebinding. |
| `JELLYFIN_MCP_READ_ONLY` | `true` leaves out every tool that changes something. |

Over HTTP, MCP is served at `/mcp` (stateless, JSON responses) and a health check at `/healthz`. Listening beyond loopback requires `JELLYFIN_MCP_AUTH_TOKEN` or `JELLYFIN_MCP_ALLOWED_HOSTS`; without either the server refuses to start.

A Jellyfin API key has administrator rights. Keep the HTTP endpoint on a private network or behind `JELLYFIN_MCP_AUTH_TOKEN`, set `JELLYFIN_MCP_ALLOWED_HOSTS` to the names clients use, and use `JELLYFIN_MCP_READ_ONLY=true` when the agent only needs to look. Cross-origin browser requests are always refused.

## Running

### Container

Images for amd64 and arm64 are published to `ghcr.io/jardarton/jellyfin-mcp`: `latest` and `X.Y.Z` for releases, `main` and `sha-<commit>` for the main branch.

```sh
docker run -p 8080:8080 \
  -e JELLYFIN_URL=http://jellyfin:8096 \
  -e JELLYFIN_API_KEY=... \
  -e JELLYFIN_MCP_AUTH_TOKEN=... \
  ghcr.io/jardarton/jellyfin-mcp:latest
```

The image runs as `65534:65534` and works with a read-only root filesystem.

### Nix

```sh
nix run github:jardarton/jellyfin-mcp
```

### Go

```sh
go install github.com/jardarton/jellyfin-mcp/cmd/jellyfin-mcp@latest
```

### MCP clients

For a client that starts servers over stdio:

```json
{
  "mcpServers": {
    "jellyfin": {
      "command": "jellyfin-mcp",
      "env": {
        "JELLYFIN_URL": "http://jellyfin:8096",
        "JELLYFIN_API_KEY": "..."
      }
    }
  }
}
```

For one that connects over HTTP, point it at `http://<host>:8080/mcp`.

## Development

```sh
nix develop          # or direnv
go test ./...
nix flake check      # tests, gofmt
nix build .#image    # OCI image; docker load < result
```

After changing `go.mod`, update `vendorHash` in `flake.nix`: set it to `pkgs.lib.fakeHash`, run `nix build`, and copy the hash it prints.

To release, set `version` in `flake.nix` and push a matching `vX.Y.Z` tag. CI tests every push and pull request, and publishes images for pushes to `main` and for tags.

## License

[MIT](LICENSE)
