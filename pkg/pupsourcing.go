// Package pupsourcing provides event sourcing capabilities for Go applications.
//
// This package serves as the main entry point for the pupsourcing event store library.
// For the core event sourcing functionality, see the store package and its subpackages:
//
//	store           - Core types (Event, PersistedEvent) and store interfaces (EventStore, EventReader)
//	store/consumer  - Consumer interfaces
//	store/postgres  - PostgreSQL implementation
//	store/migrations - Migration generation
//
// Quick Start:
//
//  1. Generate migrations:
//     go run github.com/pupsourcing/store/cmd/migrate-gen -output migrations
//
//  2. Create store and append events:
//     eventStore := postgres.NewStore(postgres.DefaultStoreConfig())
//     tx, _ := db.BeginTx(ctx, nil)
//     result, err := eventStore.Append(ctx, tx, store.NoStream(), events)
//     tx.Commit()
//
//  3. Read events:
//     stream, err := eventStore.ReadAggregateStream(ctx, tx, "User", userID, nil, nil)
//     events, err := eventStore.ReadEvents(ctx, tx, 0, 100)
//
// See the examples directory for complete working examples.
package pupsourcing

// Version returns the current version of the library.
func Version() string {
	return "0.1.0-dev"
}
