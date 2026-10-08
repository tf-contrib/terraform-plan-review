<!-- terraform-plan-review:id=default -->
### 🔴 Plan: **1 to destroy**, **1 to replace**, 1 to forget, 4 to change, 1 to add, 1 to move

#### Destructive changes

| | Resource | Why |
|---|---|---|
| 🗑️ destroy | `terraform_data.old` | removed from configuration |
| ♻️ replace | [`terraform_data.server`](https://github.com/example/infra/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L7) | `triggers_replace` forces replacement |

<details open><summary><b><code>basic</code></b>: 1 to destroy, 1 to replace, 1 to forget, 4 to change, 1 to add, 1 to move</summary>

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

<sub>[terraform-plan-review](https://github.com/tofu-contrib/terraform-plan-review) · Terraform/OpenTofu 1.13.1 · plan for `0123456`</sub>
<!-- terraform-plan-review:state:H4sIAAAAAAAA/2zPTYrEIBCG4bvUuglGjX+XaUqrHAJJDMbpoWly98HtOLtaPHzF+4EXhPkBCQKIWSq9GOs8xkSc4QEVwgciXmvqx17oe+MJz3NqXCvmUvcnYcOJ+NzKe+ejQQCVnVuySdlJAw/4Q3OpX6U1PiAAxiT1HAXblEe5rVefo6jYRr0YjWlEB/9AACmTsChNFHkZTdmoP+OMQngfRaTRnGVb0xsCWKWNt46sQzWyygfuTM9W+iKK7Kz3BiWO9OJUuQdoLWf0UhNJ/x+rL669U0UZZ2/IZob7vn8DAAD//x6umeubAQAA -->
