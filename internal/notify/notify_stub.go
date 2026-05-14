//go:build !darwin

package notify

import (
	"fmt"
	"os"
)

// StubNotifier prints notifications to stderr on non-darwin platforms.
type StubNotifier struct{}

// NewStubNotifier creates a stub notifier.
func NewStubNotifier() *StubNotifier { return &StubNotifier{} }

// New returns a platform-specific Notifier (stub on non-darwin).
func New() Notifier { return NewStubNotifier() }

func (n *StubNotifier) Send(title, body string) {
	fmt.Fprintf(os.Stderr, "[notify] %s: %s\n", title, body)
}
