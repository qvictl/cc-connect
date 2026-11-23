# Quiet exec commands

Set `quiet = true` for a configured exec command that posts its own result.
By default, exec commands still show progress and report completion normally.

```toml
[[commands]]
name = "deploy"
description = "Deploy and report on its own / 部署并自行回报结果"
exec = "./scripts/deploy.sh {{args}}"
quiet = true
```

Quiet exec commands send no progress message. Successful stdout is sent as
plain text; if stdout is empty, nothing is sent. Failures and timeouts still
produce a reply. If a failed command writes to both stdout and stderr, both
are included within the existing output limit.

开启后不发进度消息；成功时 stdout 原样作为文本发送，为空则不发送；
失败及超时仍会回复。
