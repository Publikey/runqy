# runqy with Prometheus and Grafana

A complete monitoring setup: runqy with Redis and PostgreSQL, Prometheus with alert rules, and
Grafana with its data source and a **runqy** dashboard already provisioned.

```bash
docker compose up -d
```

| Service | URL |
|---------|-----|
| runqy dashboard | http://localhost:3000/monitoring/ |
| Metrics | http://localhost:3000/metrics |
| Prometheus | http://localhost:9090 (alert rules under **Alerts**) |
| Grafana | http://localhost:3001 (`admin` / `admin`), dashboard **runqy** |

The Grafana dashboard stays empty until a queue exists: metrics are per queue. Create one from
the runqy dashboard (**Queues > Create queue**), start a worker against `http://localhost:3000`
with the `RUNQY_API_KEY` set in `docker-compose.yml`, and enqueue a few tasks.

## Files

| File | Purpose |
|------|---------|
| `docker-compose.yml` | runqy, Redis, PostgreSQL, Prometheus and Grafana |
| `prometheus.yml` | Scrapes `runqy:3000/metrics` every 15s |
| `alerts.yml` | Alert rules: no worker, latency, queue depth, error rate, nothing processed, unhealthy worker |
| `grafana/dashboards/runqy.json` | The dashboard; it can also be imported by hand into an existing Grafana |
| `grafana/provisioning/` | Declares the Prometheus data source and loads the dashboard |

The worker metrics (`runqy_queue_workers`, `runqy_queue_unhealthy_workers`, `runqy_workers`)
need runqy v0.4.5 or later.

Documentation: https://runqy.com/docs/guides/monitoring/
