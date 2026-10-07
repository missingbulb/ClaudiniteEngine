# ClaudiniteEngine
The Claudinite Engine Binary

## Layout

- `cn/`: everything compiled into the shipped `cn` binary.
- `dev/`: the tools that build, sign and release `cn`: the release pipeline (`dev/release`), the key-ceremony tool (`dev/cn-keys`) and the public development keys (`dev/testkeys`).
- `docs/`: the documents below.
- `rewrite-temp/`: tools that exist only for the move off the Node engine: the parity harness, the decision faces only it asks (`cndecide`) and the timing probes.

## Documents

- [Design](docs/design.md) and its [record](docs/design-record.md): decisions, research, alternatives and the security review.
- [Release testing](docs/release-testing.md) and its [record](docs/release-testing-record.md).
- [Build and migration plan](docs/build-plan.md): phases 0 to 10.
- The license server lives in [ClaudiniteLicenses](https://github.com/missingbulb/ClaudiniteLicenses/tree/main/docs); the engine asks it nothing (docs/design.md, Licensing).
