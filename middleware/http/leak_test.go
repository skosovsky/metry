// revive:disable-next-line var-naming -- package name "http" is intentional for HTTP middleware
package http

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
