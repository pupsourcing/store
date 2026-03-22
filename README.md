# pupsourcing/store

[![CI](https://github.com/pupsourcing/store/actions/workflows/ci.yml/badge.svg)](https://github.com/pupsourcing/store/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/pupsourcing/store)](https://goreportcard.com/report/github.com/pupsourcing/store)
[![GoDoc](https://godoc.org/github.com/pupsourcing/store?status.svg)](https://godoc.org/github.com/pupsourcing/store)

A minimal, production-ready event store for Go.

## Features

- **PostgreSQL-backed event store** — append-only, immutable event log with BIGSERIAL global positions
- **Optimistic concurrency control** — via expected versions enforced at the application and database level
- **Aggregate stream reads** — load a full or partial event history with optional version ranges
- **Sequential event reading** — read events by global position for building consumers and projections
- **Transaction-first design** — all operations accept `*sql.Tx`; you control transaction boundaries
- **Consumer interfaces** — `Consumer` and `ScopedConsumer` for event processing
- **SQL migration generator** — `cmd/migrate-gen` generates a ready-to-apply `.sql` file
- **Event mapping code generator** — `cmd/eventmap-gen` generates type-safe domain event mappings
- **CockroachDB compatible** — the PostgreSQL implementation works unmodified against CockroachDB

## Quick Start

### 1. Install

```bash
go get github.com/pupsourcing/store
```

Choose your PostgreSQL driver:

```bash
go get github.com/lib/pq
```

### 2. Generate Migrations

```bash
go run github.com/pupsourcing/store/cmd/migrate-gen -output migrations
```

Apply the generated file with your preferred migration tool:

```bash
psql -h localhost -U postgres -d mydb -f migrations/*_init_event_sourcing.sql
```

### 3. Append and Read Events

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "log"
    "time"

    "github.com/google/uuid"
    _ "github.com/lib/pq"

    "github.com/pupsourcing/store"
    "github.com/pupsourcing/store/postgres"
)

func main() {
    db, err := sql.Open("postgres", "host=localhost user=postgres dbname=mydb sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    ctx := context.Background()
    s := postgres.NewStore(postgres.DefaultStoreConfig())

    // Append events to a new aggregate
    userID := uuid.New().String()
    payload, _ := json.Marshal(map[string]string{"email": "alice@example.com", "name": "Alice"})

    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer tx.Rollback() //nolint:errcheck

    result, err := s.Append(ctx, tx, store.NoStream(), []store.Event{
        {
            AggregateType: "User",
            AggregateID:   userID,
            EventID:       uuid.New(),
            EventType:     "UserCreated",
            EventVersion:  1,
            Payload:       payload,
            Metadata:      []byte(`{}`),
            CreatedAt:     time.Now(),
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := tx.Commit(); err != nil {
        log.Fatal(err)
    }

    log.Printf("appended at positions %v, aggregate now at version %d",
        result.GlobalPositions, result.ToVersion())

    // Read the aggregate stream
    tx2, _ := db.BeginTx(ctx, nil)
    defer tx2.Rollback() //nolint:errcheck

    stream, err := s.ReadAggregateStream(ctx, tx2, "User", userID, nil, nil)
    if err != nil {
        log.Fatal(err)
    }
    tx2.Commit() //nolint:errcheck

    log.Printf("stream: %d events, current version %d", stream.Len(), stream.Version())
    for _, e := range stream.Events {
        log.Printf("  v%d  %s  pos=%d", e.AggregateVersion, e.EventType, e.GlobalPosition)
    }
}
```

## Core Concepts

### Events & Aggregates

`store.Event` is an immutable value object that you construct before persisting. The store assigns `AggregateVersion` and `GlobalPosition` during `Append`.

```go
event := store.Event{
    AggregateType: "Order",         // logical category of the aggregate
    AggregateID:   orderID,         // string identifier — UUID, email, slug, etc.
    EventID:       uuid.New(),      // idempotency key for the event itself
    EventType:     "OrderPlaced",   // discriminator used by consumers
    EventVersion:  1,               // schema version of the payload
    Payload:       payload,         // serialized domain data (JSON, proto, etc.)
    Metadata:      metadata,        // cross-cutting concerns (request ID, actor, etc.)
    CreatedAt:     time.Now(),
    // optional tracing fields:
    TraceID:       store.NullString{String: traceID, Valid: true},
    CorrelationID: store.NullString{String: corrID, Valid: true},
    CausationID:   store.NullString{String: causID, Valid: true},
}
```

`store.PersistedEvent` is what you read back. It adds `GlobalPosition` and `AggregateVersion`.

`store.Stream` wraps the full ordered history of a single aggregate along with helper methods:

```go
stream.Version()  // current aggregate version (0 if empty)
stream.IsEmpty()  // true if no events were found
stream.Len()      // number of events in the stream
```

`store.AppendResult` describes the outcome of a write:

```go
result.ToVersion()       // aggregate version after the append
result.FromVersion()     // aggregate version before the append
result.GlobalPositions   // global positions assigned to each event
result.Events            // persisted events with all fields populated
```

### Expected Versions

Expected versions are the mechanism for optimistic concurrency. You declare the state you expect the aggregate to be in before writing.

| Constructor | When to use |
|---|---|
| `store.NoStream()` | Creating a new aggregate — fails if it already exists |
| `store.Exact(n)` | Updating an existing aggregate at a known version — fails on conflict |
| `store.Any()` | Unconditional write — skips version validation entirely |

Conflicts return `store.ErrOptimisticConcurrency`. The database unique constraint on `(aggregate_type, aggregate_id, aggregate_version)` acts as a final safety net even if two transactions pass the application-level check simultaneously.

```go
// Create — must not already exist
result, err := s.Append(ctx, tx, store.NoStream(), events)

// Update at a known version
result, err := s.Append(ctx, tx, store.Exact(stream.Version()), events)

// Unconditional
result, err := s.Append(ctx, tx, store.Any(), events)

if errors.Is(err, store.ErrOptimisticConcurrency) {
    // reload, reapply command, retry
}
```

### Aggregate Streams

`ReadAggregateStream` returns the ordered event history for a single aggregate instance. Both version bounds are optional and inclusive.

```go
// Full history
stream, err := s.ReadAggregateStream(ctx, tx, "User", userID, nil, nil)

// From a specific version onwards (e.g., to skip already-processed events)
from := int64(5)
stream, err = s.ReadAggregateStream(ctx, tx, "User", userID, &from, nil)

// A version window
from, to := int64(5), int64(10)
stream, err = s.ReadAggregateStream(ctx, tx, "User", userID, &from, &to)
```

### Sequential Reads

`ReadEvents` reads from the global log in position order, which is the foundation for building consumers and projections.

```go
// Read up to 500 events after global position 0
events, err := s.ReadEvents(ctx, tx, 0, 500)

// Continue from last processed position
events, err = s.ReadEvents(ctx, tx, lastPosition, 500)
```

To filter by aggregate type at the SQL level, use `ReadEventsWithScope`:

```go
events, err := s.ReadEventsWithScope(ctx, tx, checkpoint, 500, postgres.ReadEventsScope{
    AggregateTypes: []string{"User", "Order"},
})
```

`GetLatestGlobalPosition` returns the highest position currently in the log — useful for lightweight polling checks without fetching full batches.

```go
latest, err := s.GetLatestGlobalPosition(ctx, tx)
```

### Consumers

The `consumer` package defines the interfaces for event processing.

`consumer.Consumer` is the base interface:

```go
type AuditLogConsumer struct{}

func (c *AuditLogConsumer) Name() string { return "audit_log.v1" }

func (c *AuditLogConsumer) Handle(ctx context.Context, tx *sql.Tx, event store.PersistedEvent) error {
    // tx is the processor's transaction — use it for atomic read model + checkpoint updates.
    // Never call tx.Commit() or tx.Rollback() here; the processor owns that.
    _, err := tx.ExecContext(ctx,
        "INSERT INTO audit_log (event_id, event_type, occurred_at) VALUES ($1, $2, $3)",
        event.EventID, event.EventType, event.CreatedAt,
    )
    return err
}
```

`consumer.ScopedConsumer` narrows delivery to specific aggregate types. Consumers that implement only `Consumer` receive all events.

```go
type UserReadModel struct{}

func (p *UserReadModel) Name() string              { return "user_read_model.v1" }
func (p *UserReadModel) AggregateTypes() []string  { return []string{"User"} }

func (p *UserReadModel) Handle(ctx context.Context, tx *sql.Tx, event store.PersistedEvent) error {
    // Only receives events where AggregateType == "User"
    return nil
}
```

## PostgreSQL Implementation

### Configuration

`postgres.NewStore` accepts a `*postgres.StoreConfig` built with functional options:

```go
s := postgres.NewStore(postgres.NewStoreConfig(
    postgres.WithEventsTable("my_events"),           // default: "events"
    postgres.WithAggregateHeadsTable("agg_heads"),   // default: "aggregate_heads"
    postgres.WithLogger(myLogger),                   // optional; nil disables logging
))
```

`postgres.DefaultStoreConfig()` returns a ready-to-use configuration with default table names and no logger.

### NOTIFY Support

Configure the store to issue a `pg_notify` call inside each `Append` transaction. The notification fires only when the transaction commits — no phantom wakes.

```go
s := postgres.NewStore(postgres.NewStoreConfig(
    postgres.WithNotifyChannel("pupsourcing_events"),
))
```

Consumers can `LISTEN` on the same channel to wake up immediately instead of polling on a fixed interval.

### CockroachDB

The `postgres` package is compatible with CockroachDB. Use the PostgreSQL driver with a CockroachDB connection string — no separate implementation is needed.

```go
db, err := sql.Open("postgres", "postgresql://root@localhost:26257/mydb?sslmode=disable")
s := postgres.NewStore(postgres.DefaultStoreConfig())
```

See the [`cockroachdb-basic`](./examples/cockroachdb-basic/) example for a full walkthrough including transaction retry handling.

## Migration Generator

`cmd/migrate-gen` generates a single `.sql` file that creates all required tables and indexes.

**CLI:**

```bash
go run github.com/pupsourcing/store/cmd/migrate-gen -output migrations
# writes migrations/20060102150405_init_event_sourcing.sql

go run github.com/pupsourcing/store/cmd/migrate-gen \
  -output migrations \
  -filename 001_events.sql \
  -events-table my_events \
  -aggregate-heads-table my_aggregate_heads
```

**`go:generate`:**

```go
//go:generate go run github.com/pupsourcing/store/cmd/migrate-gen -output migrations -filename init.sql
```

The generated migration creates:

- **`events`** — append-only event log with `global_position BIGSERIAL` primary key, `event_id UUID UNIQUE`, and a `UNIQUE (aggregate_type, aggregate_id, aggregate_version)` constraint that enforces optimistic concurrency at the database level
- **`aggregate_heads`** — one row per aggregate tracking its current version for O(1) version lookups during `Append`

## Event Mapping Code Generator

`cmd/eventmap-gen` generates type-safe mapping code between your domain event structs and `store.Event` / `store.PersistedEvent`. This keeps your domain model free of infrastructure dependencies.

```bash
go run github.com/pupsourcing/store/cmd/eventmap-gen \
  -input internal/domain/events \
  -output internal/infrastructure/generated
```

See the [`eventmap-codegen`](./examples/eventmap-codegen/) example for a complete demonstration including versioned events and schema evolution patterns.

## Examples

Complete, runnable examples are in [`examples/`](./examples/):

- **[basic](./examples/basic/)** — connecting, appending events, reading aggregate streams, and reading the global log
- **[cockroachdb-basic](./examples/cockroachdb-basic/)** — using the PostgreSQL implementation against CockroachDB with transaction retry handling
- **[eventmap-codegen](./examples/eventmap-codegen/)** — generating type-safe domain event mappings with `eventmap-gen`, including versioned payloads and projections

## Development

**Unit tests:**

```bash
make test-unit
```

**Integration tests (requires Docker):**

```bash
make test-integration-local
```

This starts a PostgreSQL container via `docker compose`, runs all integration tests, then cleans up.

**Manual integration testing:**

```bash
docker compose up -d
make test-integration
docker compose down
```

**Lint and format:**

```bash
make lint
make fmt
```

## License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
