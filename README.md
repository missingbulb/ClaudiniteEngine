# ClaudiniteEngine
The Claudinite Engine Binary

## Layout

- `cn/`: everything compiled into the shipped `cn` binary.
- `dev/`: the tools that build, test, sign and release `cn`, by stage ([dev/README.md](dev/README.md)). `.github/workflows/` only calls into it.
- `docs/`: the documents below.
- `rewrite-temp/`: tools that exist only for the move off the Node engine: the parity harness, the decision faces only it asks (`cndecide`), the timing probes and `fromnode`, which moves a member off the Node engine (`go run ./rewrite-temp/fromnode --repo DIR`).

## Documents

- [Design](docs/design.md) and its [record](docs/design-record.md): decisions, research, alternatives and the security review.
- [Release testing](docs/release-testing.md) and its [record](docs/release-testing-record.md).
- [Build and migration plan](docs/build-plan.md): phases 0 to 10.
- The license server lives in [ClaudiniteLicenses](https://github.com/missingbulb/ClaudiniteLicenses/tree/main/docs); the engine asks it only for a fleet run's owner plan (docs/design.md, Licensing).
