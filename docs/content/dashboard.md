---
title: Dashboard
layout: docs
toc_min_level: 2
---

# Dashboard

Flynn comes with a web dashboard that supports managing apps and databases
running on Flynn. After installing Flynn, you can reach the dashboard at
`https://dashboard.$CLUSTER_DOMAIN`.

An ephemeral CA certificate is used to secure access to the dashboard, for more
details look at [the security
documentation](/docs/security#dashboard-ca-certificate).

## GitHub Integration

The dashboard supports deploying apps directly from GitHub, without touching the
command line. To link a GitHub account, follow the instructions in the dashboard
to generate an API token and link it.

## Login Token

A secret bearer token used to access the dashboard is generated when Flynn is
installed. The bootstrap command will provide a login token upon completion. If
you lost the token, you can retrieve it using the CLI:

```text
$ flynn -a dashboard env get LOGIN_TOKEN
0ff8d3b563d24c0d02fd25394eb86136
```
