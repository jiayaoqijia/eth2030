package contracts

// Named gives type-safe access to the scenario runtimes.
type Named struct{}

func mustRuntime(name string) []byte {
	code, ok := Runtime(name)
	if !ok {
		panic("missing scenario contract " + name)
	}
	return code
}
