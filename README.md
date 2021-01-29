Spew
====

Like conky, generate lines of info to pass to bars like lemonbar.

Build
-----

`make`
`make install`
`PREFIX=~/bin make install`

Usage:
------

```toml
# config.toml
template = "{{ .Date }} Hello {{ .Username }} - {{ .Random }}\n"

[[sources]]
name="date"
type="once"
script="date"

[[sources]]
name="username"
type="once"
script="whoami"

[[sources]]
name="random"
type="listen"
script="bash -c 'while true; do sleep 1; echo $RANDOM; done'"
```

```bash
spew config.toml
```

Use with lemonbar:
```bash
spew config.toml | lemonbar -p
```

TODO
----

- retry before failing
- sandbox failure (each section fails on its own)
