// Package replay reads recorded Bob session logs (JSON lines) and redacts the
// sensitive values in them, so that recorded traffic can be replayed in tests.
package replay

//go:generate go tool gen lens
