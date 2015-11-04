---
title: How do I use the Postgres appliance?
layout: docs
toc_min_level: 2
---

# How do I use the Postgres appliance?

Flynn includes a highly available Postgres appliance which it uses for internal services and is available for use by installed apps.

When [adding a Postgres resource to an app](/docs/postgres#adding-a-database-to-an-app), Flynn will provision a database and login credentials specifically for that app. That configuration is automatically passed to the app via environment variables:

- `DATABASE_URL` is a Heroku-compatible url with hostname, database name, and login credentials. This will look like `postgres://username:password@hostname:5432/`.
- `PGHOST` is the hostname of the primary Postgres server.
- `PGDATABASE` is the database name.
- `PGUSER` is the username.
- `PGPASSWORD` is the password.

