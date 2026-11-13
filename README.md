# spin-ssh

Search your SpinupWP sites and SSH in as the site user. If your key isn't on the site
yet, spin-ssh adds it for you.

![spin-ssh demo](demo/demo.gif)

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/marcossantiago/spin-ssh/main/install.sh | sh
```

Installs to `~/.local/bin` (macOS and Linux). Update with `spin-ssh update`.

## First run

You need:

- **Your public key on the SpinupWP team** (Team Settings → SSH Keys).
- **A SpinupWP API token.** spin-ssh asks for it and opens the page to create one.
  Read-only is enough; read-write also lets spin-ssh add your key to sites.

spin-ssh finds which of your local SSH keys is on the team and asks you to confirm it.

## Use

```sh
spin-ssh                    # pick a site
spin-ssh shop               # skip the picker when one site matches
spin-ssh shop -- -L 3306:localhost:3306   # extra ssh arguments
spin-ssh revoke             # remove your key from a site
spin-ssh key                # choose a different SSH key
spin-ssh token              # replace the API token
```

<details>
<summary>More</summary>

- `--print` prints the ssh command instead of running it. `--refresh` reloads sites.
- The token comes from `SPINUPWP_API_TOKEN` if set, else `~/.config/spin-ssh/credentials.json`.
- Sites are cached in `~/.cache/spin-ssh` and refresh in the background after 15 minutes.
- spin-ssh always connects with your chosen key only (`-i <key> -o IdentitiesOnly=yes`).
- SpinupWP's API can't manage sudo users, so spin-ssh only connects as site users.
- `SPIN_SSH_NO_UPDATE_CHECK=1` turns off the daily update check.
- The demo GIF is made with `demo/record.sh` and fake sites.

</details>

## Releasing

```sh
git tag v0.2.0 && git push origin v0.2.0
```

GitHub Actions builds and publishes the release.
