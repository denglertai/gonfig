package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/openbao/openbao/api/v2"
)

// provider reads secrets of the KV secrets engine of OpenBao or Vault, see secret.Provider
type provider struct {
	client *api.Client
}

// Params returns the params of the filter: the mount of the secrets engine, its KV version and the version of a secret
func (p *provider) Params() []string {
	return []string{"mount", "kv", "version"}
}

// Connect creates the client from the BAO_* or VAULT_* variables and authenticates it, see authenticate
func (p *provider) Connect(ctx context.Context) error {
	// The client would use https://127.0.0.1:8200 without an address, which hides a missing configuration
	if api.ReadBaoVariable(api.EnvVaultAddress) == "" && api.ReadBaoVariable(api.EnvVaultAgentAddr) == "" {
		return errors.New("no address, set BAO_ADDR or VAULT_ADDR")
	}

	config := api.DefaultConfig()
	if config.Error != nil {
		return fmt.Errorf("invalid configuration: %w", config.Error)
	}
	client, err := api.NewClient(config)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	if err := authenticate(ctx, client); err != nil {
		return err
	}
	p.client = client
	return nil
}

// Read reads the data of the secret at the reference, e.g. "secret/app/db" for the secret app/db of the mount secret
func (p *provider) Read(ctx context.Context, ref string, params map[string]string) (any, error) {
	mount, path, err := splitReference(ref, params["mount"])
	if err != nil {
		return nil, err
	}

	switch params["kv"] {
	case "1":
		if params["version"] != "" {
			return nil, errors.New("versions require the KV version 2")
		}
		secret, err := p.client.KVv1(mount).Get(ctx, path)
		if err != nil {
			return nil, describe(err)
		}
		return secret.Data, nil
	case "", "2":
		var secret *api.KVSecret
		if params["version"] == "" {
			secret, err = p.client.KVv2(mount).Get(ctx, path)
		} else {
			version, convErr := strconv.Atoi(params["version"])
			if convErr != nil || version < 1 {
				return nil, fmt.Errorf("invalid version %q, use a positive number", params["version"])
			}
			secret, err = p.client.KVv2(mount).GetVersion(ctx, path, version)
		}
		if err != nil {
			return nil, describe(err)
		}
		// Deleted versions are returned without data, but with their metadata
		if secret.Data == nil {
			return nil, errors.New("the secret is deleted")
		}
		return secret.Data, nil
	default:
		return nil, fmt.Errorf("invalid KV version %q, use 1 or 2", params["kv"])
	}
}

// splitReference splits the reference into the mount of the secrets engine and the path of the secret within it.
// The mount is the first segment of the reference, unless a mount is given, which the reference has to start with,
// e.g. for mounts with several segments like "kv/team".
func splitReference(ref string, mount string) (string, string, error) {
	ref = strings.Trim(ref, "/")
	mount = strings.Trim(mount, "/")

	if mount == "" {
		mount, path, _ := strings.Cut(ref, "/")
		if path == "" {
			return "", "", errors.New("no path after the mount, e.g. secret/app/db")
		}
		return mount, path, nil
	}

	path, found := strings.CutPrefix(ref, mount+"/")
	if !found || path == "" {
		return "", "", fmt.Errorf("the reference has to start with the mount %q, followed by the path", mount)
	}
	return mount, path, nil
}

// describe returns the error without the URL and response bodies of the client's response errors, which may be long
// or contain anything a proxy returns, keeping the status code and the messages of OpenBao. Errors of requests without
// a response, e.g. a refused connection, are reduced to the error of net/http, which names the method and the URL of
// the request, since the address helps to find the cause. net/http replaces a password in the URL by ***, and tokens
// are sent in a header, so the URL contains no secrets.
func describe(err error) error {
	if errors.Is(err, api.ErrSecretNotFound) {
		return errors.New("not found")
	}

	var response *api.ResponseError
	if errors.As(err, &response) {
		if response.RawError || len(response.Errors) == 0 {
			return fmt.Errorf("status %d", response.StatusCode)
		}
		var messages []string
		for _, message := range response.Errors {
			messages = append(messages, unwrapMessages(message)...)
		}
		return fmt.Errorf("status %d: %s", response.StatusCode, strings.Join(messages, "; "))
	}

	// The KV client wraps these errors with the path, which the reference in the error of the filter names already
	var request *url.Error
	if errors.As(err, &request) {
		return request
	}
	return err
}

// unwrapMessages returns the messages of an error message of OpenBao, which wraps several messages in a list,
// e.g. "1 error occurred:\n\t* permission denied\n\n". Other messages are returned as they are.
func unwrapMessages(message string) []string {
	header, list, found := strings.Cut(message, " occurred:\n")
	if !found || strings.ContainsAny(header, "\n") {
		return []string{message}
	}

	var messages []string
	for _, line := range strings.Split(list, "\n") {
		if item, ok := strings.CutPrefix(strings.TrimSpace(line), "* "); ok {
			messages = append(messages, item)
		}
	}
	if len(messages) == 0 {
		return []string{message}
	}
	return messages
}
