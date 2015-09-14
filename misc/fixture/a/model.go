package a

// A is struct
// +test
type A struct {
}

type (
	// +test
	B struct{}
	// C is struct
	// +test: opts
	C struct{}
)
