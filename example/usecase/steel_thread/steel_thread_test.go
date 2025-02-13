package main

import (
	"os"
	"testing"

	"github.com/extrimian/ssi-sdk/schema"
)

// TestMain is used to set up schema caching in order to load all schemas locally
func TestMain(m *testing.M) {
	localSchemas, err := schema.GetAllLocalSchemas()
	if err != nil {
		os.Exit(1)
	}
	l, err := schema.NewCachingLoader(localSchemas)
	if err != nil {
		os.Exit(1)
	}
	l.EnableHTTPCache()
	os.Exit(m.Run())
}

func TestSteelThreadUseCase(_ *testing.T) {
	// If there is an error in main this test will fail
	main()
}
