package contracts

// Named gives type-safe access to the scenario runtimes.
type Named struct{}

// Storage42 returns the Storage42 runtime bytecode.
func (Named) Storage42() []byte { return mustHex(Storage42Runtime) }

// SelfDestruct returns the SelfDestruct runtime bytecode.
func (Named) SelfDestruct() []byte { return mustHex(SelfDestructRuntime) }

// Echo returns the Echo runtime bytecode.
func (Named) Echo() []byte { return mustHex(EchoRuntime) }

func mustHex(s string) []byte { return FromHex(s) }
