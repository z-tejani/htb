# htbTUI

Personal Hack The Box TUI in Go for when you want a fast keyboard-first client instead of the website or MCP.

Project: [github.com/z-tejani/htb](https://github.com/z-tejani/htb)

## Features

- active machine summary
- machine browser with HTB-style categories
- current season, retired, and Starting Point machine groups
- in-app machine name search
- VPN server browser and one-key switching
- VPN config download plus local connect/disconnect actions
- spawn selected machine and wait for its IP
- submit flags from a modal form

## Setup

1. Run `go run .`
2. If no token is configured, press `t` in the TUI and paste `HTB_APP_TOKEN`.
3. The token is saved to `.env`, which overrides `HTB/config/replay.env`.

## Keys

- `tab`: switch between machine and VPN panes
- `[ ]`: switch machine categories
- `/`: search machines by name
- `t`: add or update the HTB app token
- `r`: refresh current pane and active machine
- `s`: spawn the selected machine
- `w`: switch to the selected VPN server
- `d`: download the selected VPN config to `.htbtui/vpn`
- `c`: connect the selected VPN locally with `openvpn`
- `x`: disconnect the local HTB OpenVPN session
- `f`: open the flag submission form
- `enter`: machine details in Machines pane, connect in VPN pane
- `q`: quit

## Notes

- The TUI uses the live HTB v4 API.
- Shell environment wins first, then `.env`, then `HTB/config/replay.env`.
- Machine loading now includes current season, retired, and Starting Point catalogs, so the first refresh can take a few seconds.
- VPN downloads are saved under `.htbtui/vpn` in the project directory.
- Local VPN connect/disconnect uses `openvpn` if it is installed on your machine.
- Preferred VPN values are honored automatically before a spawn request if configured.
- Source code and updates live at [github.com/z-tejani/htb](https://github.com/z-tejani/htb).
