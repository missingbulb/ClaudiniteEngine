The 13 `@claudinite` packages are reserved at `0.0.0`. Each needs its trusted publisher and token refusal set on npmjs.com:

- [ ] `@claudinite/cli-rc` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-rc` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-rc-linux-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-rc-linux-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-rc-linux-arm64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-rc-linux-arm64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-rc-darwin-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-rc-darwin-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-rc-darwin-arm64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-rc-darwin-arm64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-rc-windows-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `release.yml`, environment `release`
- [ ] `@claudinite/cli-rc-windows-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/cli` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-linux-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/cli-linux-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-linux-arm64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/cli-linux-arm64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-darwin-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/cli-darwin-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-darwin-arm64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/cli-darwin-arm64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/cli-windows-x64` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/cli-windows-x64` Publishing access: Require two-factor authentication and disallow tokens
- [ ] `@claudinite/sdk` trusted publisher: GitHub Actions, organization or user `missingbulb`, repository `ClaudiniteEngine`, workflow filename `promote.yml`, environment `promote`
- [ ] `@claudinite/sdk` Publishing access: Require two-factor authentication and disallow tokens
- [ ] delete `NPM_BOOTSTRAP_TOKEN` from Actions secrets and revoke it on npm

Both boxes of a package are on its page at `https://www.npmjs.com/package/<name>/access`. Until a package's publisher is attached, a real `release.yml` or `promote.yml` publish of it is a red run whose last line names its box here; before the placeholders existed those runs were dry runs. npm has no CLI or API for trusted publishers (https://docs.npmjs.com/trusted-publishers), so these are clicks.
