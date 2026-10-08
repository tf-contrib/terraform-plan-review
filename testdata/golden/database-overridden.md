<!-- terraform-plan-review:id=default -->
### 🔴 Plan: **1 to replace**, 1 to change, 1 to import

> [!WARNING]
> This plan is partial (`-target` or `-exclude`). Other changes may be pending.

> [!WARNING]
> **Blocking rules overridden by label.**
> - [`aws_db_instance.main`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L3) will be **replaced**: Databases must not be replaced without approval. (`protect-databases`)

> [!WARNING]
> - [`aws_iam_role.ci`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L21) will be **imported**: IAM change: request a security review. (`watch-iam`)

#### Destructive changes

| | Resource | Why |
|---|---|---|
| ♻️ replace | [`aws_db_instance.main`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L3) | `identifier` forces replacement; create before destroy |

<details open><summary><b><code>database</code></b>: 1 to replace, 1 to change, 1 to import</summary>

♻️ **replace** [`aws_db_instance.main`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L3): `identifier` forces replacement; create before destroy

```diff
! identifier = "orders-v1" -> "orders-v2"  # forces replacement
! instance_class = "db.t3.micro" -> "db.t3.small"
! arn = "arn:aws:rds:us-east-1:000000000000:db:orders-v1" -> (known after apply)
```

✏️ **update** [`aws_s3_bucket.logs`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L7)

```diff
! tags.team = "data" -> "platform"
```

📥 **import** [`aws_iam_role.ci`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L21)

<details><summary>1 data source read during apply</summary>

- [`data.aws_iam_policy_document.assume`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L25) configuration depends on values known after apply

</details>

**Outputs**

```diff
! db_password = (sensitive value) -> (sensitive value)
```

<details><summary>⚠️ 1 resource changed outside of Terraform/OpenTofu</summary>

These differences were found while refreshing state. They are not caused by this change, but applying it will reconcile them.

✏️ **update** `aws_security_group.web`

```diff
! ingress[0].cidr_blocks[0] = "10.0.0.0/16" -> "0.0.0.0/0"
```

</details>

_1 change hidden by ignore rules._

</details>

<sub>[terraform-plan-review](https://github.com/tofu-contrib/terraform-plan-review) · Terraform/OpenTofu 1.13.1 · plan for `0123456`</sub>
<!-- terraform-plan-review:state:H4sIAAAAAAAA/zTNS2rEMBCE4bvU2oix1H6MLiNa3e0g4kcYyQlh8N1DhmRXi4/6n/hE7DsIIm69DzSM03znLGoLOjwQn1BunLna7+avmjSnstfGu5jbuOyImEk0z0TL3Y/oXqrwlh7Hak4KIsLN94PSFMz8H6gh5VPerbn1eKuIsLwo0WiL1x7dK+v+nz6Otch30kPOzfbmuNZzM0TIpDJMM3m2gOu6fgIAAP//qUa+2NEAAAA= -->
