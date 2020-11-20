module github.com/cgbaker/nomad-deploy-notifier

go 1.15

require (
	github.com/hashicorp/go-hclog v0.14.1
	github.com/hashicorp/nomad/api v0.0.0-20201115152218-974039ba8bf6
	github.com/slack-go/slack v0.7.2
)

replace github.com/hashicorp/nomad/api => github.com/hashicorp/nomad-enterprise/api v0.0.0-20201120110713-e4f0f01000fe
