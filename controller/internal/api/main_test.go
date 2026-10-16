package api

import (
	"os"
	"testing"

	"github.com/the-asad-iqbal/async-os/controller/internal/auth"
)

func TestMain(m *testing.M) {
	auth.SetHashIterationsForTests(1000)
	os.Exit(m.Run())
}
