package main

import (
	"context"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// failOnceCredential returns the first error of the credential it wraps for every later request. DefaultAzureCredential
// tries every credential of its chain on each request, which takes about a second without Azure, e.g. for the timeout
// of managed identity. A run of gonfig would otherwise wait that long for every secret, although the result doesn't
// change. Tokens are cached by the credential itself.
type failOnceCredential struct {
	credential azcore.TokenCredential

	mu  sync.Mutex
	err error
}

// GetToken returns a token of the wrapped credential, or the error of its first failure
func (c *failOnceCredential) GetToken(ctx context.Context, options policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return azcore.AccessToken{}, c.err
	}
	token, err := c.credential.GetToken(ctx, options)
	if err != nil {
		c.err = err
	}
	return token, err
}
