// chain_config_forks.go provides a structured fork schedule representation,
// fork transition detection, and chain config comparison utilities. It extends
// ChainConfig with methods for querying fork activation blocks/timestamps,
// enumerating active forks, and computing configuration differences between
// two chain configs (e.g., for detecting incompatible fork changes).
package config

import (
	"fmt"
	"math/big"
)

// ForkID identifies a fork by name and activation point (block or timestamp).
type ForkID struct {
	Name      string
	Block     *big.Int // non-nil for block-number forks
	Timestamp *uint64  // non-nil for timestamp forks
}

// String returns a human-readable representation of the fork.
func (f ForkID) String() string {
	if f.Block != nil {
		return fmt.Sprintf("%s@block:%s", f.Name, f.Block.String())
	}
	if f.Timestamp != nil {
		return fmt.Sprintf("%s@time:%d", f.Name, *f.Timestamp)
	}
	return fmt.Sprintf("%s@pending", f.Name)
}

// IsActive returns true if the fork is active at the given block number and
// timestamp. Block-number forks check against num; timestamp forks check
// against time.
func (f ForkID) IsActive(num *big.Int, time uint64) bool {
	if f.Block != nil {
		return num != nil && f.Block.Cmp(num) <= 0
	}
	if f.Timestamp != nil {
		return *f.Timestamp <= time
	}
	return false
}

// ForkSchedule returns the complete ordered list of forks defined in the
// chain configuration, including both block-number and timestamp forks.
// Forks with nil activation are included but marked as pending.
func (c *ChainConfig) ForkSchedule() []ForkID {
	schedule := []ForkID{
		{Name: "Homestead", Block: c.HomesteadBlock},
		{Name: "EIP150", Block: c.EIP150Block},
		{Name: "EIP155", Block: c.EIP155Block},
		{Name: "EIP158", Block: c.EIP158Block},
		{Name: "Byzantium", Block: c.ByzantiumBlock},
		{Name: "Constantinople", Block: c.ConstantinopleBlock},
		{Name: "Petersburg", Block: c.PetersburgBlock},
		{Name: "Istanbul", Block: c.IstanbulBlock},
		{Name: "Berlin", Block: c.BerlinBlock},
		{Name: "London", Block: c.LondonBlock},
		{Name: "Shanghai", Timestamp: c.ShanghaiTime},
		{Name: "Cancun", Timestamp: c.CancunTime},
		{Name: "Prague", Timestamp: c.PragueTime},
		{Name: "Amsterdam", Timestamp: c.AmsterdamTime},
		{Name: "Glamsterdan", Timestamp: c.GlamsterdanTime},
		{Name: "Hogota", Timestamp: c.HogotaTime},
		{Name: "SSZBlocks", Timestamp: c.SSZBlockTime},
	}
	return schedule
}

// ActiveForks returns only the forks that are active at the given block number
// and timestamp.
func (c *ChainConfig) ActiveForks(num *big.Int, time uint64) []ForkID {
	all := c.ForkSchedule()
	var active []ForkID
	for _, f := range all {
		if f.IsActive(num, time) {
			active = append(active, f)
		}
	}
	return active
}

// PendingForks returns forks that have activation points set but are not yet
// active at the given block number and timestamp.
func (c *ChainConfig) PendingForks(num *big.Int, time uint64) []ForkID {
	all := c.ForkSchedule()
	var pending []ForkID
	for _, f := range all {
		if (f.Block != nil || f.Timestamp != nil) && !f.IsActive(num, time) {
			pending = append(pending, f)
		}
	}
	return pending
}

// UnscheduledForks returns forks with no activation point (nil block and nil
// timestamp). These are future forks not yet scheduled.
func (c *ChainConfig) UnscheduledForks() []ForkID {
	all := c.ForkSchedule()
	var unscheduled []ForkID
	for _, f := range all {
		if f.Block == nil && f.Timestamp == nil {
			unscheduled = append(unscheduled, f)
		}
	}
	return unscheduled
}

// ForkConfigDiff represents a difference between two chain configs for a
// specific fork.
type ForkConfigDiff struct {
	ForkName string
	Local    string // local value (e.g., "block:100" or "time:1000")
	Remote   string // remote value
}

// ConfigDiff compares two chain configurations and returns a list of forks
// where the activation points differ. This is useful for detecting
// incompatible chain configs when syncing with peers.
func ConfigDiff(local, remote *ChainConfig) []ForkConfigDiff {
	if local == nil || remote == nil {
		return nil
	}

	var diffs []ForkConfigDiff

	localForks := local.ForkSchedule()
	remoteForks := remote.ForkSchedule()

	for i := 0; i < len(localForks) && i < len(remoteForks); i++ {
		lf := localForks[i]
		rf := remoteForks[i]

		if lf.Name != rf.Name {
			continue // should not happen with same fork order
		}

		lStr := forkPointString(lf)
		rStr := forkPointString(rf)

		if lStr != rStr {
			diffs = append(diffs, ForkConfigDiff{
				ForkName: lf.Name,
				Local:    lStr,
				Remote:   rStr,
			})
		}
	}

	return diffs
}

// forkPointString returns a string representation of a fork's activation point.
func forkPointString(f ForkID) string {
	if f.Block != nil {
		return fmt.Sprintf("block:%s", f.Block.String())
	}
	if f.Timestamp != nil {
		return fmt.Sprintf("time:%d", *f.Timestamp)
	}
	return "nil"
}

// ConfigCompatError represents an incompatibility between two chain configs
// at a specific fork.
type ConfigCompatError struct {
	ForkName  string
	LocalVal  string
	RemoteVal string
	HeadBlock uint64
	HeadTime  uint64
}

// Error implements the error interface.
func (e *ConfigCompatError) Error() string {
	return fmt.Sprintf("incompatible fork %q: local=%s remote=%s (head block=%d time=%d)",
		e.ForkName, e.LocalVal, e.RemoteVal, e.HeadBlock, e.HeadTime)
}

// CheckConfigCompatible verifies that two chain configs are compatible up to
// the given head block number and timestamp. It returns the first incompatible
// fork found, or nil if the configs are compatible.
func CheckConfigCompatible(local, remote *ChainConfig, headNum uint64, headTime uint64) *ConfigCompatError {
	if local == nil || remote == nil {
		return nil
	}

	diffs := ConfigDiff(local, remote)
	num := new(big.Int).SetUint64(headNum)

	for _, d := range diffs {
		// Find the fork in the local schedule.
		for _, f := range local.ForkSchedule() {
			if f.Name != d.ForkName {
				continue
			}
			// Only report incompatibility if the fork is already active.
			if f.IsActive(num, headTime) {
				return &ConfigCompatError{
					ForkName:  d.ForkName,
					LocalVal:  d.Local,
					RemoteVal: d.Remote,
					HeadBlock: headNum,
					HeadTime:  headTime,
				}
			}
			break
		}
	}

	return nil
}

// NextForkAfter returns the name and activation time/block of the next fork
// that will activate after the given block number and timestamp. Returns an
// empty ForkID if no future forks are scheduled.
func (c *ChainConfig) NextForkAfter(num *big.Int, time uint64) ForkID {
	pending := c.PendingForks(num, time)
	if len(pending) == 0 {
		return ForkID{}
	}
	return pending[0]
}
