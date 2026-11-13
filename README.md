# spin-ssh

Pick a SpinupWP site and SSH to it as its site user. If your key isn't on that site
user yet, spin-ssh adds it through the SpinupWP API first.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/marcossantiago/spin-ssh/main/install.sh | sh
```

This downloads the latest release for your Mac or Linux machine, checks it against
the release's checksums, and installs it to `~/.local/bin` without sudo. It tells you
if that folder isn't on your `PATH` yet. To pick a version or a folder:

```sh
curl -fsSL https://raw.githubusercontent.com/marcossantiago/spin-ssh/main/install.sh \
  | SPIN_SSH_VERSION=v0.2.0 SPIN_SSH_INSTALL_DIR=/usr/local/bin sh
```

If you work on spin-ssh itself: `go install github.com/marcossantiago/spin-ssh@latest`.

### Before the first run

- **Your public key must be on the SpinupWP team** (Team Settings → SSH Keys). Ask an
  admin to add it. spin-ssh never creates team keys.
- **An API token.** On first run spin-ssh asks for your SpinupWP organization (paste
  any SpinupWP URL, or the part after `spinupwp.app/`) and offers to open its API
  settings, `https://spinupwp.app/{ORGANIZATION_SLUG}/settings/api`, where you create a
  token. Paste it in and spin-ssh checks it and saves it, with the organization, to
  `~/.config/spin-ssh/credentials.json`, readable only by you. If
  `SPINUPWP_API_TOKEN` is set, it's used instead. `spin-ssh token` replaces the saved
  token.
  - **Read-only** is enough to list sites and connect, and is the safer choice. When
    your key isn't on a site yet, spin-ssh links straight to the site's SFTP settings
    in SpinupWP (press `o` to open it), waits while you add the key, then checks again
    when you press enter and connects.
  - **Read-write** also lets spin-ssh add your key to a site (and `spin-ssh revoke`
    remove it) for you.

### Updating

```sh
spin-ssh update
```

Once a day spin-ssh checks GitHub in the background. When a new release is out, the
picker's title bar and `spin-ssh --version` say so. If you installed with `go install`,
`spin-ssh update` prints the command to run instead. Set `SPIN_SSH_NO_UPDATE_CHECK=1`
to turn the check off.

## Use

```sh
spin-ssh                      # pick a site, connect
spin-ssh staging shop         # prefilled search; connects straight away on a single match
spin-ssh shop -- -L 3306:localhost:3306   # extra ssh arguments after --
spin-ssh --print shop         # print the ssh command instead of running it
spin-ssh --refresh            # reload servers and sites first
spin-ssh revoke               # remove your key from a site user
spin-ssh key                  # choose which SSH key to use again
spin-ssh token                # save a new SpinupWP API token
spin-ssh update               # update to the latest release
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
  `POST /sites/{id}/site-user/ssh-keys` and waits for the event before connecting. If
  SpinupWP refuses because the token is read-only, spin-ssh remembers that (by a hash of
  the token) and from then on checks the site and explains how to add the key instead.
  SpinupWP has no API for sudo users, so spin-ssh only connects as site users.

## Releasing

Push a version tag. GitHub Actions runs the tests, then GoReleaser builds macOS and
Linux binaries (amd64 and arm64) and publishes them with checksums and a changelog:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The tag becomes the version shown by `spin-ssh --version` and in the picker.
