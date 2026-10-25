module github.com/denglertai/gonfig/plugins/openbao

go 1.27

require (
	github.com/denglertai/gonfig v0.0.0
	github.com/openbao/openbao/api/v2 v2.7.1
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/go-jose/go-jose/v4 v4.1.6 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.2.0 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)

// The plugin is built against the gonfig sources it is part of, and released with gonfig
replace github.com/denglertai/gonfig => ../..
