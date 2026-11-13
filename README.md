# spin-ssh

Pick a SpinupWP site and SSH to it as its site user. If your key isn't on that site
user yet, spin-ssh adds it through the SpinupWP API first.

## Install

Download a binary for macOS or Linux from
[Releases](https://github.com/marcossantiago/spin-ssh/releases), or build it with Go:

```sh
go install github.com/marcossantiago/spin-ssh@latest
```

On macOS, a downloaded binary may need `xattr -d com.apple.quarantine spin-ssh`
before it will run.

Create an API token in SpinupWP (My Account > API Tokens) and export it:

```sh
export SPINUPWP_API_TOKEN=...
```

Your public key must already be on the SpinupWP team (Team Settings > SSH Keys).
spin-ssh never creates team keys.

## Use

```sh
spin-ssh                      # pick a site, connect
spin-ssh staging shop         # prefilled search; connects straight away on a single match
spin-ssh shop -- -L 3306:localhost:3306   # extra ssh arguments after --
spin-ssh --print shop         # print the ssh command instead of running it
spin-ssh --refresh            # reload servers and sites first
spin-ssh revoke               # remove your key from a site user
spin-ssh key                  # choose which SSH key to use again
spin-ssh --version            # also shown in the picker's title bar
```

In the picker, type to fuzzy-search the domain and server. Space-separated terms
must all match, and matched letters are highlighted. Sites you used recently come
first. The panel beside the list shows the selected site's address, site user,
PHP version and whether your key is already on it.

| Key | Does |
|---|---|
| `↑` `↓` / `ctrl+p` `ctrl+n` / `tab` | move |
| `pgup` `pgdown` | move a page |
| `enter` | connect |
| `ctrl+r` | reload sites from SpinupWP |
| `esc` | clear the search, or quit when it's empty |

## How it works

- **Your key.** On first run spin-ssh reads the private keys in `~/.ssh` and the keys
  in the SSH agent that ssh uses, including `IdentityAgent` from your ssh config (for
  1Password and Secretive). It matches them against the team's keys. One match is used
  without asking. With several you pick one. With none you pick your team key and type
  the path to its private key. The choice is saved in `~/.config/spin-ssh/config.json`.
- **Pinned key.** It runs `ssh -i <key> -o IdentitiesOnly=yes -p <port> <site_user>@<ip>`.
  For an agent-only key, `<key>` is its `.pub` file and ssh uses the agent's private key.
- **Cache.** Sites and servers are cached in `~/.cache/spin-ssh/inventory.json`. The
  picker opens from the cache, and if the cache is older than 15 minutes it refreshes in
  the background. When you last used each site is kept in `recent.json` next to it.
- **Access.** Picking a site your key isn't on yet calls
  `POST /sites/{id}/site-user/ssh-keys` and waits for the event before connecting.
  SpinupWP has no API for sudo users, so spin-ssh only connects as site users.

## Releasing

Push a version tag. GitHub Actions runs the tests, then GoReleaser builds macOS and
Linux binaries (amd64 and arm64) and publishes them with checksums and a changelog:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The tag becomes the version shown by `spin-ssh --version` and in the picker.
