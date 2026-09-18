package get

type causer interface {
	Cause() error
}

type ignore struct {
	err error
}

// Error for options: version, usage
func (i ignore) Error() string {
	return i.err.Error()
}

func (i ignore) Cause() error {
	return i.err
}

// errTop get important message from wrapped error message
func errTop(err error) error {
	for e := err; e != nil; {
		// NOTE: bind the type switch to a different name (c) than the loop
		// variable (e). With `switch e := e.(type)` the inner e would be
		// *declared* for each case with the matched type, so in `case causer:`
		// e would have the static type causer and `e = e.Cause()` would not
		// compile (and the loop variable would never be updated).
		switch c := e.(type) {
		case ignore:
			return nil
		case causer:
			e = c.Cause()
		default:
			return c
		}
	}

	return nil
}
