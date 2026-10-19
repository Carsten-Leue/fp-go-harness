// Package cli is the command line of fp-go-harness, built on
// [github.com/urfave/cli/v3].
//
// The configuration is decoded with a codec ([GetConfigCodec]) from the
// variables of a run, merged from these sources, the later one wins:
//
//   - the defaults of the codec
//   - the .env file in the working directory
//   - the process environment
//   - the flags that are set on the command line
package cli

//go:generate go tool gen lens
