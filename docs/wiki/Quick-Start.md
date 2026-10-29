# Quick start

This walks through two machines, `desk` (hosts sessions) and your laptop (drives them). The laptop can host sessions too; just run an agent there as well.

## 1. Start an agent on each host

```sh
clawsh agent
```

It listens on `:7420`. On first run it generates a token and logs where it saved it. Print it any time with:

```sh
clawsh token
```

## 2. Tell the hub about your agents

On the machine running the agent there's nothing to do: if `hosts.toml` doesn't exist yet, the agent creates it with a `local` host (this agent's URL and token), so `clawsh ls`, `clawsh attach local/<id>` and `clawsh web` work right away. An existing file is never changed.

To drive other machines, on the machine you drive from, run `clawsh help` to see the config path (it ends in `clawsh/hosts.toml`), then create that file:

```toml
[[host]]
name = "desk"
url = "http://192.168.1.20:7420"
token = "paste-token-from-desk"
```

Add one `[[host]]` block per agent. See [Configuration](Configuration.md).

## 3. Check they're reachable

```sh
clawsh hosts
```

Each host shows `ok (...)`, `unreachable: ...`, or `auth failed: ...` (wrong token).

## 4. Start a session

```sh
clawsh new desk --dir ~/src/api
```

This starts `claude` in `~/src/api` on `desk` and attaches your terminal to it. Pass arguments to claude after `--`:

```sh
clawsh new desk --dir ~/src/api -- --resume
```

## 5. Detach and come back

Press **Ctrl-]** to detach. The session keeps running on `desk`.

```sh
clawsh ls                 # every session on every host
clawsh attach desk/8158   # an ID prefix is enough
```

When you re-attach, the agent replays recent output and nudges claude to repaint, so the screen comes back clean.

## 6. Stop it

```sh
clawsh kill desk/8158
```
