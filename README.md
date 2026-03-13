<p align="center">
  <img src="assets/logo.svg" alt="runqy logo" width="80" height="80">
</p>

<h1 align="center">runqy</h1>

<p align="center">
  <strong>Open-source task queue for AI workloads. Deploy workers anywhere, from your laptop to the cloud.</strong>
</p>

<p align="center">
  <a href="https://github.com/Publikey/runqy/stargazers"><img src="https://img.shields.io/github/stars/Publikey/runqy?style=flat&logo=github" alt="GitHub Stars"></a>
  <a href="https://github.com/Publikey/runqy/blob/main/LICENSE"><img src="https://img.shields.io/github/license/Publikey/runqy" alt="License"></a>
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go" alt="Go Version"></a>
  <a href="https://github.com/Publikey/runqy-python"><img src="https://img.shields.io/badge/Python_SDK-Available-3776AB?style=flat&logo=python" alt="Python SDK"></a>
  <a href="https://github.com/Publikey/runqy/actions"><img src="https://img.shields.io/github/actions/workflow/status/Publikey/runqy/release.yml" alt="Build Status"></a>
</p>

<p align="center">
  <a href="https://docs.runqy.com"><strong>Documentation</strong></a> · 
  <a href="https://runqy.com"><strong>Website</strong></a> · 
  <a href="#examples">Examples</a> · 
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

<p align="center">
  <img src="assets/demo.gif" alt="Runqy demo — from zero to task result in 90 seconds" width="800">
</p>

---

## Why Runqy?

🌍 **Workers run anywhere** — Your laptop, on-prem servers, AWS, Azure, Runpod, any machine with an internet connection. [Learn more →](https://docs.runqy.com/worker/)  
🚀 **Zero-touch deployment** — Workers pull code from Git, install dependencies, and start processing automatically. No manual setup. [Learn more →](https://docs.runqy.com/worker/deployment/)  
📄 **Simple YAML config** — Define a queue in a few lines. One YAML file, one queue. [Learn more →](https://docs.runqy.com/server/configuration/#queue-worker-definitions-yaml)  
🔐 **Built-in secrets** — Pass secrets to workers via encrypted env vars. [Learn more →](https://docs.runqy.com/server/configuration/#configuration-methods)  
🐍 **Go server + Python SDK** — Robust Go server, familiar Python developer experience. [Learn more →](https://docs.runqy.com/python-sdk/)  
📊 **Web monitoring UI** — Real-time dashboard with Prometheus metrics. [Learn more →](https://docs.runqy.com/guides/monitoring/)  

🔗 **Task Dependencies** — Build multi-step workflows with fan-out/fan-in patterns. Chain tasks, run them in parallel, and aggregate results — all with simple dependency declarations. [Learn more →](docs/task-dependencies.md)

### Feature Comparison

| Feature | Runqy | Celery | Temporal | Modal | BullMQ | Inngest |
|---------|-------|--------|----------|-------|--------|---------|
| **Self-hosted** | ✅ | ✅ | ✅ | ❌ | ✅ | ❌ |
| **Workers anywhere** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Auto-deploy from Git** | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ |
| **Deployment YAML** | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ |
| **Built-in secrets** | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ |
| **Monitoring UI** | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| **Task Dependencies** | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ |

---

## Quick Start

Get Runqy running in under 60 seconds:

```bash
# 1. Start the stack
curl -O https://raw.githubusercontent.com/Publikey/runqy/main/docker-compose.quickstart.yml
docker-compose -f docker-compose.quickstart.yml up -d

# 2. Enqueue a task
pip install runqy-python
python -c "
from runqy_python import RunqyClient
client = RunqyClient('http://localhost:3000', api_key='dev-api-key')
task = client.enqueue('quickstart-oneshot', {'message': 'Hello World!'})
print(f'Task ID: {task.task_id}')
"

# 3. Check results
open http://localhost:3000/monitoring/
```

See the [Quickstart Guide](https://docs.runqy.com/getting-started/quickstart/) for the full walkthrough.

## Define a Queue

A queue is a simple YAML file:

```yaml
queues:
  image-resize:
    priority: 5
    deployment:
      # Worker code: https://github.com/acme/image-worker
      git_url: "https://github.com/acme/image-worker.git"
      branch: "main"
      startup_cmd: "python main.py"
      mode: "one_shot"
```

Deploy it:

```bash
runqy config create -f queue.yaml
```

See the [Queue Configuration Reference](https://docs.runqy.com/server/configuration/#queue-worker-definitions-yaml) for all options.

## Write a Task

```python
from runqy import task, load

@load
def setup():
    """Load models once when worker starts"""
    import torch
    return torch.load('my_model.pt')

@task
def process_image(image_url: str, model) -> dict:
    """Runs on every task execution"""
    result = model.predict(image_url)
    return {"prediction": result, "confidence": 0.95}
```

See the [Python SDK Reference](https://docs.runqy.com/python-sdk/) for the full API.

## Enqueue Tasks

Three ways to enqueue:

```bash
# CLI
runqy task enqueue -q image-resize -p '{"image":"img001.jpg","size":256}'

# REST API
curl -s POST localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{"queue":"image-resize","data":{"image":"img002.jpg"}}'

# Python SDK
from runqy_python import RunqyClient
client = RunqyClient('http://localhost:3000', api_key='dev-api-key')
task = client.enqueue('image-resize', {'image': 'img003.jpg'})
```

See the [API Reference](https://docs.runqy.com/server/api/) for all endpoints.

---

## Examples

Explore real-world use cases:

- **[quickstart-oneshot](examples/quickstart-oneshot/)** — Simple task execution
- **[quickstart-longrunning](examples/quickstart-longrunning/)** — Long-running worker processes
- **[data-pipeline](examples/data-pipeline/)** — Multi-step data processing (API calls, ETL)
- **[webhook-processor](examples/webhook-processor/)** — Event-driven webhook handling (Stripe, GitHub)
- **[scheduled-tasks](examples/scheduled-tasks/)** — Cron-like healthchecks, reports, and cleanup
- **[multi-queue](examples/multi-queue/)** — Priority-based routing (critical, standard, bulk)
- **[gpu-inference](examples/gpu-inference/)** — GPU-accelerated image generation with Stable Diffusion
- **[star-runqy](examples/star-runqy/)** — Vault secrets management tutorial

---

## Installation

### Quick Install

**Linux/macOS:**
```bash
curl -fsSL https://raw.githubusercontent.com/publikey/runqy/main/install.sh | sh
```

**Windows (PowerShell):**
```powershell
iwr https://raw.githubusercontent.com/publikey/runqy/main/install.ps1 -useb | iex
```

### Docker

```bash
docker pull ghcr.io/publikey/runqy:latest
```

### From Source

```bash
git clone https://github.com/Publikey/runqy.git
cd runqy
go build -o runqy ./app
```

See the [Installation Guide](https://docs.runqy.com/getting-started/quickstart/) for detailed instructions.

## Requirements

- Redis + PostgreSQL

## Server Configuration

Configure the server via environment variables:

```bash
export REDIS_HOST=localhost:6379
export RUNQY_API_KEY=your-secret-key
```

See the [Configuration Reference](https://docs.runqy.com/server/configuration/?h=redis_host#1-environment-variables-envsecret-file) for all options.

## CLI Reference

Manage your deployment locally or remotely:

```bash
runqy queue list                    # List all queues
runqy config create -f queue.yaml   # Deploy a queue

runqy task enqueue -q myqueue -p '{"key":"value"}'  # Enqueue task
runqy task list myqueue                              # List tasks
runqy task get myqueue <task_id>                     # Get task result

runqy worker list                   # List active workers
```

See the [CLI Reference](https://docs.runqy.com/server/cli/) for all commands.

## Monitoring

Access the built-in web dashboard at `/monitoring`:

<p align="center">
  <img src="docs/images/monitoring-dashboard.png" alt="Runqy Dashboard" width="700">
</p>

<details>
<summary>📊 More screenshots</summary>

**Queue Overview** — Status, pending/active/completed counts, latency per queue:

<p align="center">
  <img src="docs/images/monitoring-queues.png" alt="Runqy Queues" width="700">
</p>

**Workers** — CPU/RAM usage, assigned queues, heartbeat status:

<p align="center">
  <img src="docs/images/monitoring-workers.png" alt="Runqy Workers" width="700">
</p>

</details>

Runqy also exposes Prometheus metrics at `/metrics`. See the [Monitoring Guide](https://docs.runqy.com/guides/monitoring/) for Grafana dashboards and alerting.

## Architecture

Tasks flow from clients → runqy server → queues → workers running anywhere. Workers are stateless and pull code from Git on startup.

<p align="center">
  <img src="assets/architecture.png" alt="runqy architecture" width="700">
</p>

**Zero-touch Deployment:** Workers connect to the server, pull your code from Git, install dependencies, and start processing — no manual setup required.

<p align="center">
  <img src="assets/code_pull.png" alt="zero-touch deployment" width="700">
</p>

## Links

- 📖 **[Documentation](https://docs.runqy.com)** — Complete guides and API reference
- 🌐 **[Website](https://runqy.com)** — Project homepage  
- 🐍 **[Python SDK](https://github.com/Publikey/runqy-python)** — Client library
- 🔧 **[Worker Runtime](https://github.com/Publikey/runqy-worker)** — Task processor
- 🤝 **[Contributing](CONTRIBUTING.md)** — How to contribute
- 📄 **[License](LICENSE)** — MIT License

---

<p align="center">
  <strong>Your workers, your machines, your rules.</strong><br>
  Built on <a href="https://github.com/hibiken/asynq">asynq</a> • Made with ❤️ for AI developers
</p>
