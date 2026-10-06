# Changelog

## [0.2.0](https://github.com/tofu-contrib/tofu-plan-review/compare/v0.1.0...v0.2.0) (2026-10-06)


### ⚠ BREAKING CHANGES

* the action takes JSON plans (the output of 'tofu show -json'), not binary plan files, and the 'tofu' input is removed. Plan and report paths must be inside the workspace.

### Features

* run the action as a FROM scratch Docker image ([#7](https://github.com/tofu-contrib/tofu-plan-review/issues/7)) ([c27b6cc](https://github.com/tofu-contrib/tofu-plan-review/commit/c27b6ccd724cfcf87a6dd55ed6e1a80041c4e7ec))

## 0.1.0 (2026-10-06)


### Features

* release pipeline, Nix flake and Linux-only action ([#1](https://github.com/tofu-contrib/tofu-plan-review/issues/1)) ([db42617](https://github.com/tofu-contrib/tofu-plan-review/commit/db42617d16471cd3e82f05a7ce49b869bcf440ee))
* urfave/cli commands and .github/tofu/review.hcl rules path ([#4](https://github.com/tofu-contrib/tofu-plan-review/issues/4)) ([d235bb2](https://github.com/tofu-contrib/tofu-plan-review/commit/d235bb26d7e49bd02dbbadba849940da127ad882))
