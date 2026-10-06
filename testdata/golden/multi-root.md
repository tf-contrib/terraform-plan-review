<!-- tofu-plan-review:id=default -->
### ⛔ OpenTofu plan: **1 to destroy**, **2 to replace**, 1 to forget, 5 to change, 1 to add, 1 to import, 1 to move

> [!WARNING]
> `database` This plan is partial (`-target` or `-exclude`). Other changes may be pending.

> [!CAUTION]
> **Blocked by policy.** Add the `destroy-approved` label to approve.
> - `database` [`aws_db_instance.main`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L3) will be **replaced**: Databases must not be replaced without approval. (`protect-databases`)

> [!WARNING]
> - `database` [`aws_iam_role.ci`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L21) will be **imported**: IAM change: request a security review. (`watch-iam`)

| Root | Destroy | Replace | Change | Add | Other | |
|---|--:|--:|--:|--:|--:|---|
| `basic` | **1** | **1** | 4 | 1 | 2 | 🔴 |
| `database` | · | **1** | 1 | · | 1 | ⛔ blocked |

#### Destructive changes

| | Root | Resource | Why |
|---|---|---|---|
| 🗑️ destroy | `basic` | `terraform_data.old` | removed from configuration |
| ♻️ replace | `basic` | [`terraform_data.server`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L7) | `triggers_replace` forces replacement |
| ♻️ replace | `database` | [`aws_db_instance.main`](https://github.com/example/infra/blob/0123456789abcdef/testdata/handwritten/database/main.tf#L3) | `identifier` forces replacement; create before destroy |

<details><summary><b><code>basic</code></b>: 1 to destroy, 1 to replace, 1 to forget, 4 to change, 1 to add, 1 to move</summary>

🗑️ **destroy** `terraform_data.old`: removed from configuration

♻️ **replace** [`terraform_data.server`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L7): `triggers_replace` forces replacement

```diff
! triggers_replace[0] = "v1" -> "v2"  # forces replacement
! input.size = "small" -> "large"
- input.tags.owner = "team-a"
+ input.tags.team = "platform"
! id = "59345481-e7b1-75ad-695c-91e2698fc62a" -> (known after apply)
! output = {"size":"small","tags":{"env":"dev","owner":"team-a"}} -> (known after apply)
```

👋 **forget** [`terraform_data.forgotten`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L18): removed from state, the real resource is kept

✏️ **update** [`module.app.terraform_data.deployment`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/modules/app/main.tf#L2)

```diff
! input.replicas = 2 -> 3
! output = {"image":"registry.example.com/app:1.0.0","replicas":2} -> (known after apply)
```

✏️ **update** [`terraform_data.list`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L22)

```diff
- input[1] = "10.0.1.0/24"
+ input[2] = "10.0.3.0/24"
! output = ["10.0.0.0/24","10.0.1.0/24","10.0.2.0/24"] -> (known after apply)
```

✏️ **update** [`terraform_data.policy`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L1)

```diff
! input (JSON):
      {
        "Statement": [
          {
-           "Action": "s3:GetObject",
+           "Action": [
+             "s3:GetObject",
+             "s3:PutObject"
+           ],
            "Effect": "Allow",
            "Resource": "arn:aws:s3:::example-bucket/*",
            "Sid": "Read"
      …
! output = "{\"Statement\":[{\"Action\":\"s3:GetObject\",\"Effect\":\"Allow\",\"Resource\":\"arn:aws:s3:::example-bucket/*\",\"Sid\":\"Read\"}],\"Version\":\"2012-10-17\"}" -> (known after apply)
```

✏️ **update** [`terraform_data.secret`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L16)

```diff
! input = (sensitive value) -> (sensitive value)
! output = (sensitive value) -> (known after apply)
```

➕ **create** [`terraform_data.new`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L23)

```diff
+ id = (known after apply)
+ input = (known after apply)
+ output = (known after apply)
```

🚚 **move** [`terraform_data.renamed_to`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L11) (moved from `terraform_data.renamed_from`)

**Outputs**

```diff
+ new_id = (known after apply)
! server_size = "small" -> (known after apply)
```

</details>

<details><summary><b><code>database</code></b>: 1 to replace, 1 to change, 1 to import</summary>

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

<details><summary>⚠️ 1 resource changed outside of OpenTofu</summary>

These differences were found while refreshing state. They are not caused by this change, but applying it will reconcile them.

✏️ **update** `aws_security_group.web`

```diff
! ingress[0].cidr_blocks[0] = "10.0.0.0/16" -> "0.0.0.0/0"
```

</details>

_1 change hidden by ignore rules._

</details>

<sub>tofu-plan-review · OpenTofu 1.13.1 · planned at `0123456`</sub>
<!-- tofu-plan-review:state:H4sIAAAAAAAA/2zRXWrlMAwF4L3o+RIS2/HfZoIsySVMEl/s3JZSsvfB05mXSd8M/jhCR1/wDnF6AEGEcVLazNb5gIlYMjygQvyChG2l/tgLvzYZ8PkcTqkVc6n7wnjiwPLcyucuxwkRdPZ+zpayVxYe8B/Npb6V85QDImAiZaY0iqN8l9vaehwnLS6Z2RqkOzrkAyIoRaNDZdOY57spG/dhknEcQ0hj4rt5lm2lT4jgtLHBeXYe9Z1VOXAXXs7SE3HM3oVgUeGdNqEqfQFj1IRBGWYVfmL1XWrfUyeVpmDZZYHrAf03YZPePH60hdOyHu3Eg2TYce39eUOcvDE5/Gm6qxX3pZZNBlr7KUY1zWycFlF/QdNLetEvOYetvDWIICmzMVay4gm+xw7/kr5bWbjQqx93wNZeu0AEckyz80ahaLiu63cAAAD//0RCYXJIAgAA -->
