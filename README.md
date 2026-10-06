# tofu-plan-review

Readable [OpenTofu](https://opentofu.org) plan reviews on pull requests.

Most plan commenters paste the text output of `tofu plan` into a comment.
`tofu-plan-review` reads the structured JSON plan instead, which lets it show
reviewers what matters:

- **Destructive changes first.** Destroys and replacements are listed at the
  top, with the attribute that forces each replacement.
- **Readable diffs of embedded documents.** IAM policies, container
  definitions and other JSON strings are diffed line by line instead of
  shown as one replaced blob. Multi-line strings get the same treatment.
- **Links and annotations on source lines.** Every change links to the
  `resource` block that causes it, and appears as an annotation in the
  pull request's *Files changed* view.
- **What changed since the last push.** The comment is updated in place and
  says which changes are new, gone or different since the previous plan.
- **Many roots, one comment.** Plan roots in a job matrix and get a single
  combined comment with a per-root summary table.
- **Always fits.** Large plans degrade gracefully to stay under GitHub's
  65,536-character comment limit; the full review goes to the job summary.
- **Policy rules.** Block or warn on risky changes (e.g. replacing a
  database) and hide known noise (e.g. `tags_all`), configured in HCL.
- **Secrets stay secret.** Sensitive values are redacted using the plan's
  sensitivity marks, *and* any copy of a sensitive value that appears in an
  unmarked attribute is redacted too.

Terraform JSON plans use the same format and should work too, but are not
tested yet. The action runs on Linux runners (x86-64 and ARM64).

## Quick start

```yaml
on: pull_request

permissions:
  contents: read
  pull-requests: write

jobs:
  plan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: opentofu/setup-opentofu@v1
        with:
          tofu_wrapper: false
      - run: tofu init -input=false && tofu plan -input=false -out=tfplan
        working-directory: infra
      - uses: tofu-contrib/tofu-plan-review@main
        with:
          plan: infra/tfplan
          working-directory: infra
```

`plan` accepts a binary plan file (converted with `tofu show -json` in
`working-directory`, so state and plan encryption settings apply) or the JSON
output of `tofu show -json`.

## Multiple roots

Analyze each root in its own job and combine the reports. Reports contain no
sensitive values, so unlike plan files they are safe to upload as artifacts.

```yaml
jobs:
  plan:
    strategy:
      matrix:
        root: [network, database, app]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: opentofu/setup-opentofu@v1
        with:
          tofu_wrapper: false
      - run: tofu init -input=false && tofu plan -input=false -out=tfplan
        working-directory: infra/${{ matrix.root }}
      - uses: tofu-contrib/tofu-plan-review@main
        with:
          mode: analyze
          plan: infra/${{ matrix.root }}/tfplan
          working-directory: infra/${{ matrix.root }}
          report: report-${{ matrix.root }}.json
      - uses: actions/upload-artifact@v4
        with:
          name: report-${{ matrix.root }}
          path: report-${{ matrix.root }}.json

  review:
    needs: plan
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - uses: actions/checkout@v5 # for the rules file
      - uses: actions/download-artifact@v4
        with:
          pattern: report-*
          merge-multiple: true
          path: reports
      - uses: tofu-contrib/tofu-plan-review@main
        with:
          plan: |
            reports/report-network.json
            reports/report-database.json
            reports/report-app.json
```

## Rules

Put rules in `.github/tofu/review.hcl`:

```hcl
# Adding this label to the pull request downgrades blocks to warnings.
override_label = "destroy-approved"

rule "protect-databases" {
  severity       = "block" # fails the check until the label is added
  message        = "Databases must not be destroyed without approval."
  resource_types = ["aws_db_instance", "aws_rds_*"]
  actions        = ["delete", "replace"]
}

rule "iam-review" {
  severity       = "warn"
  message        = "IAM change: request a security review."
  resource_types = ["aws_iam_*"]
}

rule "hide-tags-all" {
  severity   = "ignore"
  attributes = ["tags_all"]
}
```

| Field            | Meaning                                                                              |
| ---------------- | ------------------------------------------------------------------------------------ |
| `severity`       | `block`, `warn` or `ignore`                                                          |
| `resource_types` | Glob patterns on the resource type                                                   |
| `addresses`      | Glob patterns on the full address, e.g. `module.prod.*`                              |
| `actions`        | Any of `create`, `update`, `replace`, `delete`, `forget`, `move`, `import`, `read`   |
| `attributes`     | Attribute paths, e.g. `tags_all` or `ingress[*].cidr_blocks`; covers nested paths    |

All fields of a rule must match. In globs, `*` matches anything, including
dots and brackets. Data source reads only match rules that list `read`.

`ignore` rules with `attributes` hide those attributes; an update left with
nothing to show is hidden entirely. Without `attributes` they hide the whole
change. A change with a `block` or `warn` finding is never hidden.

Labels are read from the event payload, so add `labeled` and `unlabeled` to
the `pull_request` trigger types for the override label to take effect
without a new push.

## Inputs

| Input               | Default                | Description                                                              |
| ------------------- | ---------------------- | ------------------------------------------------------------------------ |
| `plan`              | (required)             | Plan files or reports, one per line                                      |
| `mode`              | `comment`              | `comment`, or `analyze` to write a report for combining roots            |
| `working-directory` | `.`                    | Root module directory                                                    |
| `name`              | `working-directory`    | Display name of the root                                                 |
| `report`            | `tofu-plan-review.json`| Report path in `analyze` mode                                            |
| `config`            | `.github/tofu/review.hcl` | Rules file                                                            |
| `id`                | `default`              | Comment identifier, for independent comments on one pull request         |
| `title`             | `OpenTofu plan`        | Comment title                                                            |
| `annotate`          | `true`                 | Annotate source lines in the pull request diff                           |
| `summary`           | `true`                 | Write the full review to the job summary                                 |
| `fail-on-block`     | `true`                 | Fail the step when a blocking rule matches                               |
| `tofu`              | `tofu`                 | Binary used to convert binary plans                                      |
| `github-token`      | `github.token`         | Token with `pull-requests: write`                                        |

Outputs: `has-changes`, `destructive`, `blocked`, `to-add`, `to-change`,
`to-replace`, `to-destroy`.

## Local use

```sh
go install github.com/tofu-contrib/tofu-plan-review/cmd/tofu-plan-review@latest

tofu plan -out=tfplan
tofu-plan-review render tfplan > review.md
```

Run `tofu-plan-review <command> --help` for all options. Every option can also
be set with a `TOFU_PLAN_REVIEW_<OPTION>` environment variable, e.g.
`TOFU_PLAN_REVIEW_CONFIG` for `--config`.

## Notes and limitations

- Pull requests from forks get a read-only `GITHUB_TOKEN`, so the comment
  cannot be posted. The job summary is still written.
- GitHub shows at most 10 annotations of each level per step. Errors
  (blocked changes) and warnings (destructive changes) are chosen first.
- Source locations are found for resources in the root module and in local
  modules (`./` or `../` sources). Resources in registry modules link to
  nothing.
- Content-based redaction skips sensitive values shorter than 4 characters
  to avoid redacting unrelated text.
- Linux runners only. At a release tag (`@vX.Y.Z`, or that tag's commit
  SHA) the action downloads the release binary and verifies its checksum.
  At other refs, such as `@main`, it builds from source with Go (about 20
  seconds).

## Development

The toolchain (Go, OpenTofu) comes from the Nix flake: run `nix develop`, or
open the repository in the devcontainer.

```sh
nix build                          # build the binary into ./result
go test ./...
go test ./internal/render -update  # rewrite golden files after rendering changes
./scripts/gen-fixtures.sh          # regenerate plans from testdata/scenarios
```

`testdata/scenarios/<name>/{v1,v2}` are configurations using only the built-in
`terraform_data` resource: `v1` is applied, then `v2` is planned.
`testdata/plans/database.json` is written by hand for cases that need real
providers (drift, imports, partial plans); its configuration and rules are in
`testdata/handwritten/database`. All values in fixtures are made up.

## License

[MPL-2.0](LICENSE)
