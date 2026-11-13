#!/bin/sh
# Records demo/demo.gif with VHS (brew install vhs), using made-up sites: a cached
# inventory, a saved key choice and a stand-in ssh. Nothing talks to SpinupWP or a server.
set -eu
cd "$(dirname "$0")/.."
work="$PWD/demo/.work"
rm -rf "$work"
mkdir -p "$work/bin" "$work/config/spin-ssh" "$work/cache/spin-ssh" "$work/ssh"

go build -ldflags "-X main.version=0.1.0" -o "$work/bin/spin-ssh" .
: >"$work/ssh/id_ed25519"

now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
hour_ago=$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)
day_ago=$(date -u -v-1d +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '1 day ago' +%Y-%m-%dT%H:%M:%SZ)

cat >"$work/config/spin-ssh/config.json" <<EOF
{"ssh_key_id": 1, "ssh_key_name": "Alex's laptop", "identity_file": "$work/ssh/id_ed25519"}
EOF

site() { # id domain user server ip php
	printf '{"id": %s, "domain": "%s", "site_user": "%s", "user_auth": "publickey", "php_version": "%s", "server_name": "%s", "host": "%s", "port": 22, "keys": [{"id": 1, "name": "Alex'"'"'s laptop"}, {"id": 2, "name": "Sam"}, {"id": 3, "name": "Deploy bot"}]}' \
		"$1" "$2" "$3" "$6" "$4" "$5"
}
{
	printf '{"fetched_at": "%s", "sites": [' "$now"
	site 101 acme.dev acme web-1 203.0.113.10 8.3
	printf ','
	site 102 blog.acme.dev blog web-1 203.0.113.10 8.3
	printf ','
	site 103 docs.acme.dev docs web-2 203.0.113.11 8.2
	printf ','
	site 104 shop.acme.dev shop web-2 203.0.113.11 8.3
	printf ','
	site 105 status.acme.dev status web-3 203.0.113.12 8.1
	printf ','
	site 106 staging.acme.dev staging staging-1 198.51.100.20 8.3
	printf ','
	site 107 staging.blog.acme.dev stagingblog staging-1 198.51.100.20 8.3
	printf ','
	site 108 staging.shop.acme.dev stagingshop staging-1 198.51.100.20 8.3
	printf ','
	site 109 wiki.acme.dev wiki web-3 203.0.113.12 8.2
	printf ']}\n'
} >"$work/cache/spin-ssh/inventory.json"
printf '{"104": "%s", "103": "%s"}\n' "$hour_ago" "$day_ago" >"$work/cache/spin-ssh/recent.json"

# Stand-in ssh: a short banner, then a shell in a fake site home.
mkdir -p "$work/site/files/wp-content" "$work/site/logs"
: >"$work/site/files/wp-config.php"
: >"$work/site/files/index.php"
cat >"$work/bin/ssh" <<EOF
#!/bin/sh
for a; do dest=\$a; done
user=\${dest%@*}
printf 'Welcome to Ubuntu 24.04 LTS\n\n'
cd "$work/site"
PS1="\$user@staging-1:/sites/staging.blog.acme.dev\\\$ " exec bash --noprofile --norc -i
EOF
chmod +x "$work/bin/ssh"

cat >"$work/env.sh" <<EOF
export PATH="$work/bin:\$PATH"
export XDG_CONFIG_HOME="$work/config" XDG_CACHE_HOME="$work/cache"
export SPINUPWP_API_TOKEN=demo SPIN_SSH_NO_UPDATE_CHECK=1
export PS1='\$ '
EOF

vhs demo/demo.tape
