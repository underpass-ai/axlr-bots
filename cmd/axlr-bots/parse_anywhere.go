package main

import "flag"

// parseAnywhere parses the flags wherever they stand among the
// positional arguments (`execution artifact <id> <name> --out f` as well
// as `--out f <id> <name>`) and returns the positional ones in order;
// false means the flags were wrong and the error was printed.
func parseAnywhere(flags *flag.FlagSet, args []string) ([]string, bool) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, false
		}
		args = flags.Args()
		if len(args) == 0 {
			return positional, true
		}
		positional, args = append(positional, args[0]), args[1:]
	}
}
