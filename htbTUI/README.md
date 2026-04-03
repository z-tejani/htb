# htbTUI

Personal Hack The Box TUI in Go for when you want a fast keyboard-first client instead of the website or MCP.

## Features

- active machine summary
- machine browser with release ordering
- VPN server browser and one-key switching
- spawn selected machine and wait for its IP
- submit flags from a modal form

## Setup

1. Run `go run .`
2. If no token is configured, press `t` in the TUI and paste `HTB_APP_TOKEN`.
3. The token is saved to `htbTUI/.env`, which overrides `HTB/config/replay.env`.

## Keys

- `tab`: switch between machine and VPN panes
- `t`: add or update the HTB app token
- `r`: refresh current pane and active machine
- `s`: spawn the selected machine
- `w`: switch to the selected VPN server
- `f`: open the flag submission form
- `enter`: load details for the selected machine
- `q`: quit

## Notes

- The TUI uses the live HTB v4 API.
- Shell environment wins first, then `htbTUI/.env`, then `HTB/config/replay.env`.
- Preferred VPN values are honored automatically before a spawn request if configured.
