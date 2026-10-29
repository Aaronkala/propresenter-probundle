# propresenter-probundle

Read, and later generate, ProPresenter `.probundle` song files.

- Format spec: [`docs/probundle-format.md`](docs/probundle-format.md)
- Sample: `samples/Suuruutes Äärellä.probundle` (ProPresenter 21.x export)
- Schema: `vendor/ProPresenter7-Proto/autogen-proto` (see `VENDORED.txt`)

## Setup

```sh
uv venv .venv && uv pip install --python .venv protobuf blake3
./scripts/gen-protos.sh          # writes Python bindings to gen/
```

## Inspect a bundle

```sh
.venv/bin/python tools/inspect_probundle.py "samples/Suuruutes Äärellä.probundle"          # summary + hash check
.venv/bin/python tools/inspect_probundle.py "samples/Suuruutes Äärellä.probundle" --json   # full .pro as JSON
```
