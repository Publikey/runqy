# Task Dependencies (Fan-Out / Fan-In)

Runqy supports task dependencies, allowing you to build complex workflows where tasks wait for other tasks to complete before running. This enables patterns like ETL pipelines, ML training workflows, and multi-step data processing — all without external orchestration.

## Overview

A task can declare one or more **parent tasks** via the `depends_on` field. When parents are specified:

1. The task enters a **waiting** state instead of being enqueued immediately.
2. A background resolver checks every 2 seconds for completed parents.
3. When **all** parents complete, the child task is automatically enqueued.
4. If a parent fails, the child (and its descendants) cascade-fail by default.

Tasks without dependencies behave exactly as before — no changes needed for existing workflows.

## Supported Patterns

```
Chain:    A → B → C
Fan-out:  A → B, A → C, A → D
Fan-in:   A + B + C → D
Diamond:  A → B, A → C, B + C → D
```

---

## API Reference

### POST /queue/add

Enqueue a single task, optionally with dependencies.

**New fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `depends_on` | `[]string` | `null` | Parent task UUIDs that must complete first |
| `on_parent_failure` | `string` | `"fail"` | `"fail"` to cascade-fail, `"ignore"` to proceed anyway |
| `inject_parent_results` | `bool` | `false` | Inject parent results into child payload |

**Request:**

```bash
curl -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "process",
    "data": {"step": "transform"},
    "depends_on": ["<parent-task-uuid>"],
    "on_parent_failure": "fail",
    "inject_parent_results": true
  }'
```

**Response (task waiting on dependencies):**

```json
{
  "task_id": "b2f3e4a5-...",
  "queue": "process",
  "state": "waiting",
  "depends_on": [
    {"id": "a1b2c3d4-...", "state": "pending"}
  ],
  "on_parent_failure": "fail",
  "inject_parent_results": true
}
```

**Response (all parents already completed — enqueued immediately):**

```json
{
  "task_id": "b2f3e4a5-...",
  "queue": "process",
  "state": "pending",
  "depends_on": [
    {"id": "a1b2c3d4-...", "state": "completed"}
  ]
}
```

### POST /queue/add-batch

Submit multiple tasks in one call. Tasks can reference each other within the batch using the `_ref` mechanism.

**New fields per job:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `_ref` | `string` | `null` | Local reference ID for this job within the batch |
| `depends_on_ref` | `[]string` | `null` | Depend on other jobs by their `_ref` in the same batch |
| `depends_on` | `[]string` | `null` | Depend on existing tasks by UUID |
| `on_parent_failure` | `string` | `"fail"` | `"fail"` or `"ignore"` |
| `inject_parent_results` | `bool` | `false` | Inject parent results into child payload |

**Request:**

```bash
curl -X POST http://localhost:3000/queue/add-batch \
  -H "X-API-Key: dev-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "etl-pipeline",
    "jobs": [
      {
        "_ref": "extract",
        "data": {"source": "s3://bucket/raw"}
      },
      {
        "_ref": "transform",
        "depends_on_ref": ["extract"],
        "data": {"format": "parquet"},
        "inject_parent_results": true
      },
      {
        "_ref": "load",
        "depends_on_ref": ["transform"],
        "data": {"dest": "warehouse"},
        "inject_parent_results": true
      }
    ]
  }'
```

**Response:**

```json
{
  "enqueued": 1,
  "waiting": 2,
  "failed": 0,
  "task_ids": [
    "a1b2c3d4-...",
    "b2f3e4a5-...",
    "c3d4e5f6-..."
  ]
}
```

The first job (`extract`) has no dependencies and is enqueued immediately. The other two enter the waiting state.

### GET /queue/{uuid}

Retrieve task status. Tasks with dependencies include additional fields in the response.

**Response (waiting task):**

```json
{
  "task_id": "b2f3e4a5-...",
  "queue": "etl-pipeline",
  "state": "waiting",
  "depends_on": [
    {"id": "a1b2c3d4-...", "state": "pending"}
  ],
  "on_parent_failure": "fail",
  "inject_parent_results": true
}
```

**Response (dependencies resolved, task running):**

```json
{
  "task_id": "b2f3e4a5-...",
  "queue": "etl-pipeline",
  "state": "active",
  "depends_on": [
    {"id": "a1b2c3d4-...", "state": "completed"}
  ]
}
```

---

## Workflow Patterns

### Chain: A → B → C

Sequential processing where each step depends on the previous one. Useful for ETL pipelines.

```bash
# Step 1: Extract
EXTRACT=$(curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{"queue":"etl","data":{"source":"s3://data/raw"}}' | jq -r '.task_id')

# Step 2: Transform (waits for extract)
TRANSFORM=$(curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d "{\"queue\":\"etl\",\"data\":{\"format\":\"parquet\"},\"depends_on\":[\"$EXTRACT\"],\"inject_parent_results\":true}" | jq -r '.task_id')

# Step 3: Load (waits for transform)
curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d "{\"queue\":\"etl\",\"data\":{\"dest\":\"warehouse\"},\"depends_on\":[\"$TRANSFORM\"],\"inject_parent_results\":true}"
```

### Fan-Out: A → B, C, D

One parent spawns multiple independent children. Useful for processing data in parallel.

```bash
# Parent: split a dataset
SPLIT=$(curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{"queue":"data","data":{"action":"split","chunks":3}}' | jq -r '.task_id')

# Fan-out: three parallel processing tasks
for SHARD in 0 1 2; do
  curl -s -X POST http://localhost:3000/queue/add \
    -H "X-API-Key: dev-api-key" \
    -d "{\"queue\":\"data\",\"data\":{\"shard\":$SHARD},\"depends_on\":[\"$SPLIT\"],\"inject_parent_results\":true}"
done
```

### Fan-In: A + B + C → D

Multiple parents converge into a single child. Useful for aggregation after parallel work.

```bash
# Three independent tasks
TASK_A=$(curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{"queue":"ml","data":{"model":"bert"}}' | jq -r '.task_id')

TASK_B=$(curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{"queue":"ml","data":{"model":"gpt2"}}' | jq -r '.task_id')

TASK_C=$(curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{"queue":"ml","data":{"model":"t5"}}' | jq -r '.task_id')

# Aggregator: waits for all three
curl -s -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d "{\"queue\":\"ml\",\"data\":{\"action\":\"ensemble\"},\"depends_on\":[\"$TASK_A\",\"$TASK_B\",\"$TASK_C\"],\"inject_parent_results\":true}"
```

### Diamond: A → B, C; B + C → D

Combines fan-out and fan-in. A common pattern for image processing pipelines.

```bash
curl -X POST http://localhost:3000/queue/add-batch \
  -H "X-API-Key: dev-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "queue": "image-pipeline",
    "jobs": [
      {
        "_ref": "download",
        "data": {"url": "https://example.com/photo.jpg"}
      },
      {
        "_ref": "resize",
        "depends_on_ref": ["download"],
        "data": {"width": 1024, "height": 768}
      },
      {
        "_ref": "watermark",
        "depends_on_ref": ["download"],
        "data": {"text": "© 2026 Acme"}
      },
      {
        "_ref": "composite",
        "depends_on_ref": ["resize", "watermark"],
        "data": {"output": "s3://bucket/final.jpg"},
        "inject_parent_results": true
      }
    ]
  }'
```

This submits a full diamond DAG in a single HTTP call. `download` runs first, then `resize` and `watermark` run in parallel, and `composite` runs after both finish.

---

## Batch Submission with `_ref`

The `_ref` / `depends_on_ref` mechanism lets you define entire workflows in a single batch request without needing to know task UUIDs ahead of time.

**How it works:**

1. Assign a `_ref` string to any job in the batch.
2. Other jobs reference it via `depends_on_ref`.
3. Runqy pre-generates UUIDs for all jobs and resolves refs to real IDs before storing dependencies.

You can mix `depends_on` (existing task UUIDs) and `depends_on_ref` (batch-local refs) in the same job:

```json
{
  "_ref": "step3",
  "depends_on": ["existing-task-uuid-from-earlier"],
  "depends_on_ref": ["step1", "step2"],
  "data": {"merge": true}
}
```

---

## Parent Result Injection

When `inject_parent_results` is `true`, the child task receives all parent results in its payload under the `_parent_results` key.

**Example payload received by child:**

```json
{
  "action": "ensemble",
  "_parent_results": {
    "a1b2c3d4-...": {"model": "bert", "accuracy": 0.92},
    "b2f3e4a5-...": {"model": "gpt2", "accuracy": 0.89},
    "c3d4e5f6-...": {"model": "t5", "accuracy": 0.91}
  }
}
```

Each key in `_parent_results` is the parent's task UUID, and the value is whatever that parent returned as its result. If a parent result is not valid JSON, it is included as a raw string.

**Worker code reading parent results:**

```python
from runqy import task

@task
def ensemble(action: str, _parent_results: dict = None) -> dict:
    scores = {pid: r["accuracy"] for pid, r in _parent_results.items()}
    best = max(scores, key=scores.get)
    return {"best_parent": best, "accuracy": scores[best]}
```

---

## Failure Handling

### Cascade Failure (default)

When `on_parent_failure` is `"fail"` (the default), a parent failure cascades through the entire dependency chain:

1. Parent task fails.
2. Resolver detects the failure.
3. Child is removed from the waiting queue and marked as cascade-failed.
4. If the child has its own dependents, they cascade-fail too.

```
A (fails) → B (cascade-fails) → C (cascade-fails)
```

This is the safe default — if your extract step fails, you don't want to run the transform.

### Ignore Failure

When `on_parent_failure` is `"ignore"`, the child proceeds regardless of parent outcomes. The child is enqueued once all parents reach a terminal state (completed, failed, or archived).

```bash
curl -X POST http://localhost:3000/queue/add \
  -H "X-API-Key: dev-api-key" \
  -d '{
    "queue": "reporting",
    "data": {"action": "generate-report"},
    "depends_on": ["<task-a>", "<task-b>"],
    "on_parent_failure": "ignore",
    "inject_parent_results": true
  }'
```

Use this for tasks like report generation that should run even if some data sources failed — the child can inspect `_parent_results` to see which parents succeeded.

---

## Python SDK Examples

### Simple Chain

```python
from runqy_python import RunqyClient

client = RunqyClient("http://localhost:3000", api_key="dev-api-key")

# Step 1: Download
download = client.enqueue("image-pipeline", {"url": "https://example.com/photo.jpg"})

# Step 2: Resize (waits for download)
resize = client.enqueue(
    "image-pipeline",
    {"width": 512, "height": 512},
    depends_on=[download.task_id],
    inject_parent_results=True,
)

# Step 3: Upload (waits for resize)
upload = client.enqueue(
    "image-pipeline",
    {"dest": "s3://bucket/output.jpg"},
    depends_on=[resize.task_id],
    inject_parent_results=True,
)
```

### Fan-In with Model Ensemble

```python
from runqy_python import RunqyClient

client = RunqyClient("http://localhost:3000", api_key="dev-api-key")

# Train three models in parallel
models = ["bert", "gpt2", "t5"]
training_tasks = []
for model in models:
    task = client.enqueue("ml-training", {"model": model, "epochs": 10})
    training_tasks.append(task.task_id)

# Ensemble: waits for all training tasks
ensemble = client.enqueue(
    "ml-evaluation",
    {"action": "ensemble"},
    depends_on=training_tasks,
    inject_parent_results=True,
)
```

### Batch Workflow with Refs

```python
from runqy_python import RunqyClient

client = RunqyClient("http://localhost:3000", api_key="dev-api-key")

result = client.enqueue_batch("etl-pipeline", [
    {
        "_ref": "extract",
        "data": {"source": "s3://datalake/raw/2026-03-13"},
    },
    {
        "_ref": "validate",
        "depends_on_ref": ["extract"],
        "data": {"schema": "v2"},
        "inject_parent_results": True,
    },
    {
        "_ref": "transform",
        "depends_on_ref": ["validate"],
        "data": {"format": "parquet"},
        "inject_parent_results": True,
    },
    {
        "_ref": "load",
        "depends_on_ref": ["transform"],
        "data": {"dest": "warehouse.analytics"},
        "inject_parent_results": True,
    },
])

print(f"Enqueued: {result.enqueued}, Waiting: {result.waiting}")
print(f"Task IDs: {result.task_ids}")
```

### Resilient Pipeline (Ignore Failures)

```python
from runqy_python import RunqyClient

client = RunqyClient("http://localhost:3000", api_key="dev-api-key")

# Fetch data from multiple sources (some may fail)
sources = ["api-a", "api-b", "api-c"]
fetch_tasks = []
for src in sources:
    task = client.enqueue("data-fetch", {"source": src})
    fetch_tasks.append(task.task_id)

# Aggregate whatever data we got — runs even if some fetches failed
client.enqueue(
    "data-aggregate",
    {"action": "merge"},
    depends_on=fetch_tasks,
    on_parent_failure="ignore",
    inject_parent_results=True,
)
```

---

## Best Practices

- **Use batch submission for complete workflows.** Submitting a full DAG in one `add-batch` call with `_ref` is more efficient and avoids partial workflow creation if the server goes down mid-submission.

- **Keep dependency chains shallow.** The resolver polls every 2 seconds. Deep chains (A → B → C → D → ...) add latency at each level. If possible, fan-out to parallelize work.

- **Use `inject_parent_results` only when needed.** Parent results are fetched from Redis and merged into the child payload. For large results, this increases payload size. If the child can fetch data from a shared store (S3, database), prefer passing a reference instead.

- **Use `on_parent_failure: "ignore"` for best-effort aggregation.** This is useful when partial results are acceptable, like generating a report from whichever data sources succeeded.

- **Monitor waiting tasks.** Tasks stuck in the waiting state may indicate failed parents or missing dependencies. Use `GET /queue/{uuid}` to check the state of each parent.

## Limitations

- **Cycle detection is not enforced.** Do not create circular dependencies (A → B → A). The resolver will not detect cycles, and the tasks will wait indefinitely.

- **Dependencies are validated at submission time.** All parent UUIDs in `depends_on` must correspond to existing tasks. The request will fail if a parent UUID is not found.

- **Resolver polling interval is 2 seconds.** There is a small delay between a parent completing and the child being enqueued. This is not configurable at runtime.

- **Cross-queue dependencies are supported** but all tasks share the same resolver. A task in queue `A` can depend on a task in queue `B`.

- **Waiting tasks are stored in PostgreSQL/SQLite**, not in Redis. If the database is unavailable, dependency resolution pauses until it recovers.
