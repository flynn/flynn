#!/bin/sh

exec /bin/prometheus-postgresql \
  --pg.host     "${PGHOST}" \
  --pg.user     "${PGUSER}" \
  --pg.password "${PGPASSWORD}" \
  --pg.database "${PGDATABASE}" \
  --web.listen-address ":${PORT}" \
  --pg.use-timescaledb true
