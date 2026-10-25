package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/openbao/openbao/api/v2"
)

// envPrefix is the prefix of the variables configuring the plugin beyond the BAO_* and VAULT_* variables of the client
const envPrefix = "GONFIG_OPENBAO_"

// defaultJWTFile is the service account token Kubernetes mounts into pods
const defaultJWTFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// authenticate sets the token of the client for the method in GONFIG_OPENBAO_AUTH:
//   - token (default): BAO_TOKEN or VAULT_TOKEN, which the client reads itself, or the file in GONFIG_OPENBAO_TOKEN_FILE
//   - approle: logs in with GONFIG_OPENBAO_ROLE_ID and GONFIG_OPENBAO_SECRET_ID, or their _FILE variants
//   - kubernetes: logs in with the role GONFIG_OPENBAO_ROLE and the service account token of the pod, or the file in
//     GONFIG_OPENBAO_JWT_FILE
//
// GONFIG_OPENBAO_AUTH_MOUNT sets the mount of the auth method, which defaults to its name.
func authenticate(ctx context.Context, client *api.Client) error {
	method := os.Getenv(envPrefix + "AUTH")
	mount := strings.Trim(os.Getenv(envPrefix+"AUTH_MOUNT"), "/")
	if mount == "" {
		mount = method
	}

	switch method {
	case "", "token":
		token, err := setting("TOKEN", false)
		if err != nil {
			return err
		}
		if token != "" {
			client.SetToken(token)
		}
		if client.Token() == "" {
			return errors.New("no token, set BAO_TOKEN, VAULT_TOKEN or GONFIG_OPENBAO_TOKEN_FILE, or another GONFIG_OPENBAO_AUTH")
		}
		return nil
	case "approle":
		roleID, err := setting("ROLE_ID", true)
		if err != nil {
			return err
		}
		secretID, err := setting("SECRET_ID", true)
		if err != nil {
			return err
		}
		return login(ctx, client, mount, map[string]any{"role_id": roleID, "secret_id": secretID})
	case "kubernetes":
		role := os.Getenv(envPrefix + "ROLE")
		if role == "" {
			return errors.New("no role, set GONFIG_OPENBAO_ROLE")
		}
		file := os.Getenv(envPrefix + "JWT_FILE")
		if file == "" {
			file = defaultJWTFile
		}
		jwt, err := readSetting(file)
		if err != nil {
			return fmt.Errorf("failed to read the service account token: %w", err)
		}
		return login(ctx, client, mount, map[string]any{"role": role, "jwt": jwt})
	default:
		return fmt.Errorf("unknown GONFIG_OPENBAO_AUTH %q, use token, approle or kubernetes", method)
	}
}

// login logs in with the auth method at the mount and sets the token of the client
func login(ctx context.Context, client *api.Client, mount string, data map[string]any) error {
	// A token of the environment would be sent with the login, which doesn't need one
	client.ClearToken()

	secret, err := client.Logical().WriteWithContext(ctx, "auth/"+mount+"/login", data)
	if err != nil {
		return fmt.Errorf("failed to log in with %s: %w", mount, describe(err))
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return fmt.Errorf("failed to log in with %s: the response contains no token", mount)
	}
	client.SetToken(secret.Auth.ClientToken)
	return nil
}

// setting returns the value of GONFIG_OPENBAO_<name>, or the content of the file in GONFIG_OPENBAO_<name>_FILE.
// Setting both is an error, and so is setting none if the setting is required.
func setting(name string, required bool) (string, error) {
	value := os.Getenv(envPrefix + name)
	file := os.Getenv(envPrefix + name + "_FILE")

	switch {
	case value != "" && file != "":
		return "", fmt.Errorf("set either %s%s or %s%s_FILE, not both", envPrefix, name, envPrefix, name)
	case file != "":
		content, err := readSetting(file)
		if err != nil {
			return "", fmt.Errorf("failed to read %s%s_FILE: %w", envPrefix, name, err)
		}
		return content, nil
	case value == "" && required:
		return "", fmt.Errorf("no %s, set %s%s or %s%s_FILE", strings.ToLower(strings.ReplaceAll(name, "_", " ")), envPrefix, name, envPrefix, name)
	}
	return value, nil
}

// readSetting returns the content of the file without surrounding whitespace, e.g. a trailing line break.
// An empty file is an error, since it would log in without credentials.
func readSetting(file string) (string, error) {
	content, err := os.ReadFile(file) //nolint:gosec // the files of the settings are given by the user
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", fmt.Errorf("%s is empty", file)
	}
	return value, nil
}
