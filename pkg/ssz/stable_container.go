// EIP-7495: SSZ StableContainer
//
// A StableContainer is a fixed-capacity container where fields can be
// optional (wrapped in Optional[T]). This enables forward-compatible
// consensus structures: new optional fields can be appended without
// changing the Merkle tree shape of existing fields.
//
// The hash tree root is computed by:
//  1. Collecting the hash tree root of each field (or zero for inactive).
//  2. Merkleizing the field roots padded to the container's capacity.
//  3. Mixing in the active bitvector as a hash.
//
// A Profile is a subtype of StableContainer with a fixed set of active
// fields and no optional fields exposed, providing a concrete schema on
// top of the stable Merkle structure.
//
// Spec: https://eips.ethereum.org/EIPS/eip-7495
package ssz

import (
	"errors"
	"fmt"
)

// Errors specific to StableContainer operations.
var (
	ErrCapacityExceeded = errors.New("ssz: stable container capacity exceeded")
	ErrFieldIndexOOB    = errors.New("ssz: field index out of bounds")
	ErrProfileMismatch  = errors.New("ssz: profile field count does not match container")
)

// FieldDef describes a single field within a StableContainer.
type FieldDef struct {
	// Name is a human-readable label for the field.
	Name string
	// TypeTag is a short string identifying the SSZ type (e.g. "uint64",
	// "Bytes32", "Container"). Used for documentation; not enforced.
	TypeTag string
	// Optional indicates whether the field may be absent. When a field
	// is optional and inactive, its hash tree root is treated as zero.
	Optional bool
}

// StableContainer implements EIP-7495.
type StableContainer struct {
	capacity int        // maximum number of fields (fixed at creation)
	fields   []FieldDef // definitions of added fields (up to capacity)
	values   [][32]byte // hash tree root of each field value
	active   []bool     // per-field active flag
}

// NewStableContainer creates a new StableContainer with the given maximum
// field capacity. Capacity must be positive.
func NewStableContainer(capacity int) *StableContainer {
	if capacity <= 0 {
		capacity = 1
	}
	// EIP-7495 caps active_fields at 256 bits so the mix-in fits a single
	// 32-byte chunk; larger capacities are clamped.
	if capacity > 256 {
		capacity = 256
	}
	return &StableContainer{
		capacity: capacity,
		fields:   make([]FieldDef, 0, capacity),
		values:   make([][32]byte, 0, capacity),
		active:   make([]bool, 0, capacity),
	}
}

// Capacity returns the maximum number of fields.
func (sc *StableContainer) Capacity() int {
	return sc.capacity
}

// Len returns the current number of defined fields.
func (sc *StableContainer) Len() int {
	return len(sc.fields)
}

// AddField appends a field to the container. If optional is true the field
// starts as inactive; otherwise it starts as active. The value must be a
// 32-byte hash tree root of the field's content.
//
// Returns an error if the container is already at capacity.
func (sc *StableContainer) AddField(name string, value [32]byte, optional bool) error {
	if len(sc.fields) >= sc.capacity {
		return fmt.Errorf("%w: %d fields already defined (capacity %d)",
			ErrCapacityExceeded, len(sc.fields), sc.capacity)
	}
	sc.fields = append(sc.fields, FieldDef{
		Name:     name,
		TypeTag:  "",
		Optional: optional,
	})
	sc.values = append(sc.values, value)
	// Non-optional fields are always active; optional fields start inactive.
	sc.active = append(sc.active, !optional)
	return nil
}

// AddFieldWithTag is like AddField but also records a type tag.
func (sc *StableContainer) AddFieldWithTag(name, typeTag string, value [32]byte, optional bool) error {
	if len(sc.fields) >= sc.capacity {
		return fmt.Errorf("%w: %d fields already defined (capacity %d)",
			ErrCapacityExceeded, len(sc.fields), sc.capacity)
	}
	sc.fields = append(sc.fields, FieldDef{
		Name:     name,
		TypeTag:  typeTag,
		Optional: optional,
	})
	sc.values = append(sc.values, value)
	sc.active = append(sc.active, !optional)
	return nil
}

// SetActive sets the active flag for the field at the given index.
func (sc *StableContainer) SetActive(index int, active bool) {
	if index >= 0 && index < len(sc.active) {
		sc.active[index] = active
	}
}

// IsActive reports whether the field at index is active.
func (sc *StableContainer) IsActive(index int) bool {
	if index < 0 || index >= len(sc.active) {
		return false
	}
	return sc.active[index]
}

// SetValue updates the 32-byte hash tree root for the field at index.
func (sc *StableContainer) SetValue(index int, value [32]byte) {
	if index >= 0 && index < len(sc.values) {
		sc.values[index] = value
	}
}

// ActiveBitvector returns the bitvector of active fields, packed into
// bytes with the least significant bit first, padded to the container's
// full capacity. Bit i is 1 if field i is active.
func (sc *StableContainer) ActiveBitvector() []byte {
	bits := make([]bool, sc.capacity)
	for i := 0; i < len(sc.active) && i < sc.capacity; i++ {
		bits[i] = sc.active[i]
	}
	return MarshalBitvector(bits)
}

// HashTreeRoot computes the EIP-7495 hash tree root of the container:
//
//	mix_in_active_fields(merkleize_progressive([field roots]), active_fields)
//
// Field i occupies progressive-tree position i (stable generalized index
// across versions); inactive fields contribute zero chunks. The active
// fields bitvector is mixed in as its packed-bits chunk per
// mix_in_active_fields(root, pack_bits(active_fields)).
func (sc *StableContainer) HashTreeRoot() [32]byte {
	// Build the capacity-length chunk list; active_fields cannot end in 0
	// per the EIP's illegal-type rules, so the last position is meaningful.
	chunks := make([][32]byte, sc.capacity)
	for i := 0; i < len(sc.values) && i < sc.capacity; i++ {
		if sc.active[i] {
			chunks[i] = sc.values[i]
		}
		// Inactive fields remain zero.
	}

	// Progressive Merkleization per EIP-7916: stable gindices as the field
	// count grows across versions.
	merkleRoot := merkleizeProgressive(chunks, 1)

	// mix_in_active_fields: hash(root, pack_bits(active_fields)). For
	// capacity <= 256 the packed bits fit a single 32-byte chunk;
	// ActiveBitvector already returns the packed bytes.
	bvBytes := sc.ActiveBitvector()
	var packedChunk [32]byte
	copy(packedChunk[:], bvBytes)

	return hash(merkleRoot, packedChunk)
}

// Field returns the definition of the field at index.
func (sc *StableContainer) Field(index int) (FieldDef, error) {
	if index < 0 || index >= len(sc.fields) {
		return FieldDef{}, fmt.Errorf("%w: index %d, len %d",
			ErrFieldIndexOOB, index, len(sc.fields))
	}
	return sc.fields[index], nil
}

// --- Profile ---

// Profile wraps a StableContainer with a fixed schema. All fields
// referenced by the profile must be active. A Profile is useful for
// concrete message types that always include a known set of fields
// while still being Merkle-compatible with the underlying
// StableContainer capacity.
type Profile struct {
	container *StableContainer
	// activeIndices lists the field indices that this profile requires.
	activeIndices []int
}

// NewProfile creates a Profile over the given StableContainer.
// activeIndices specifies which fields must be active (all others are
// forced inactive). Returns an error if any index is out of bounds.
func NewProfile(sc *StableContainer, activeIndices []int) (*Profile, error) {
	for _, idx := range activeIndices {
		if idx < 0 || idx >= sc.Len() {
			return nil, fmt.Errorf("%w: index %d out of range [0, %d)",
				ErrProfileMismatch, idx, sc.Len())
		}
	}

	// Deactivate all fields, then activate only the profile's set.
	for i := 0; i < sc.Len(); i++ {
		sc.SetActive(i, false)
	}
	for _, idx := range activeIndices {
		sc.SetActive(idx, true)
	}

	return &Profile{
		container:     sc,
		activeIndices: activeIndices,
	}, nil
}

// HashTreeRoot returns the hash tree root of the underlying container
// with only the profile's fields active.
func (p *Profile) HashTreeRoot() [32]byte {
	return p.container.HashTreeRoot()
}

// ActiveBitvector returns the bitvector from the underlying container.
func (p *Profile) ActiveBitvector() []byte {
	return p.container.ActiveBitvector()
}

// Container returns the underlying StableContainer.
func (p *Profile) Container() *StableContainer {
	return p.container
}
