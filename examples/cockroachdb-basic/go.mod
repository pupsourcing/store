module github.com/pupsourcing/store/examples/cockroachdb-basic

go 1.24.11

replace github.com/pupsourcing/store => ../..

require (
	github.com/google/uuid v1.6.0
	github.com/lib/pq v1.10.9
	github.com/pupsourcing/store v0.0.0-00010101000000-000000000000
)
