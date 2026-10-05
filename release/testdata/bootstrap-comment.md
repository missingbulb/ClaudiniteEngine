The 7 `@claudinite` packages are reserved at `0.0.0`. Each needs its trusted publishers and token refusal set on npmjs.com:

- [ ] `@claudinite/cli` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`, Allow npm dist-tag
- [ ] `@claudinite/cli` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-linux-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-linux-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-linux-arm64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-linux-arm64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-darwin-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-darwin-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-darwin-arm64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-darwin-arm64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-windows-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-windows-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/sdk` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/sdk` Publishing access: Require two-factor authentication and disallow tokens
- [ ] delete `NPM_BOOTSTRAP_TOKEN` from Actions secrets and revoke it on npm

Every box of a package is on its page at `https://www.npmjs.com/package/<name>/access`. Until a package's publisher is attached, a real `release.yml` publish or `promote.yml` promotion of it is a red run whose last line names its box here; before the placeholders existed those runs were dry runs. npm has no CLI or API for trusted publishers (https://docs.npmjs.com/trusted-publishers), so these are clicks.
