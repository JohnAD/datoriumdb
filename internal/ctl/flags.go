package ctl

import "strconv"

// extractFlag scans args for a "--name value" pair anywhere in the slice,
// removes it, and returns the value plus the remaining args.
func extractFlag(args []string, name string) (string, []string, bool) {
	out := make([]string, 0, len(args))
	value := ""
	found := false
	for i := 0; i < len(args); i++ {
		if args[i] == name && i+1 < len(args) && !found {
			value = args[i+1]
			found = true
			i++
			continue
		}
		out = append(out, args[i])
	}
	return value, out, found
}

// extractBoolFlag scans args for a bare "--name" switch, removes it, and
// reports whether it was present.
func extractBoolFlag(args []string, name string) (bool, []string) {
	out := make([]string, 0, len(args))
	found := false
	for _, a := range args {
		if a == name {
			found = true
			continue
		}
		out = append(out, a)
	}
	return found, out
}

// extractIntFlag scans args for a "--name value" pair with an integer value.
func extractIntFlag(args []string, name string) (*int, []string, error) {
	raw, rest, found := extractFlag(args, name)
	if !found {
		return nil, rest, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return nil, rest, err
	}
	return &n, rest, nil
}
