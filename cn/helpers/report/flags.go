package report

import (
	"flag"
	"io"
)

// ParseFlags parses a command's flags, any fault a Usage error; a command
// takes no positional arguments past them.
func ParseFlags(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return Wrap(Usage, fs.Name(), err)
	}
	if fs.NArg() != 0 {
		return New(Usage, fs.Name()+" takes no positional arguments")
	}
	return nil
}
