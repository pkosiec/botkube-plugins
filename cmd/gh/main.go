package main

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log"

	"github.com/hashicorp/go-plugin"
	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/api/executor"
	"github.com/kubeshop/botkube/pkg/pluginx"
)

const (
	pluginName       = "gh"
	logsTailLines    = 150
	defaultNamespace = "default"
)

// GHExecutor implements the Botkube executor plugin interface.
type GHExecutor struct{}

// Commands defines all supported GitHub plugin commands and their flags.
type (
	Commands struct {
		Create *CreateCommand `arg:"subcommand:create"`
	}
	CreateCommand struct {
		Issue *CreateIssueCommand `arg:"subcommand:issue"`
	}
	CreateIssueCommand struct {
		Type      string `arg:"positional"`
		Namespace string `arg:"-n,--namespace"`
	}
)

// Metadata returns details about the GitHub plugin.
func (*GHExecutor) Metadata(context.Context) (api.MetadataOutput, error) {
	return api.MetadataOutput{
		Version:     "v1.0.0",
		Description: "GH creates an issue on GitHub for a related Kubernetes resource.",
	}, nil
}

// Config holds the GitHub executor configuration.
type Config struct {
	GitHub struct {
		Token         string
		Repository    string
		IssueTemplate string
	}
}

// IssueDetails holds all available information about a given issue.
type IssueDetails struct {
	Type      string
	Namespace string
	Logs      string
	Version   string
}

var depsDownloadLinks = map[string]api.Dependency{
	"gh": {
		URLs: map[string]string{
			"darwin/amd64": "https://github.com/cli/cli/releases/download/v2.21.2/gh_2.21.2_macOS_amd64.tar.gz//gh_2.21.2_macOS_amd64/bin",
			"linux/amd64":  "https://github.com/cli/cli/releases/download/v2.21.2/gh_2.21.2_linux_amd64.tar.gz//gh_2.21.2_linux_amd64/bin",
			"linux/arm64":  "https://github.com/cli/cli/releases/download/v2.21.2/gh_2.21.2_linux_arm64.tar.gz//gh_2.21.2_linux_arm64/bin",
			"linux/386":    "https://github.com/cli/cli/releases/download/v2.21.2/gh_2.21.2_linux_386.tar.gz//gh_2.21.2_linux_386/bin",
		},
	},
	"kubectl": {
		URLs: map[string]string{
			"darwin/amd64": "https://dl.k8s.io/release/v1.26.0/bin/darwin/amd64/kubectl",
			"linux/amd64":  "https://dl.k8s.io/release/v1.26.0/bin/linux/amd64/kubectl",
			"linux/arm64":  "https://dl.k8s.io/release/v1.26.0/bin/linux/arm64/kubectl",
			"linux/386":    "https://dl.k8s.io/release/v1.26.0/bin/linux/386/kubectl",
		},
	},
}

// Execute returns a given command as a response.
func (e *GHExecutor) Execute(ctx context.Context, in executor.ExecuteInput) (executor.ExecuteOutput, error) {
	var cfg Config
	pluginx.MergeExecutorConfigs(in.Configs, &cfg)

	var cmd Commands
	pluginx.ParseCommand(pluginName, in.Command, &cmd)

	if cmd.Create == nil || cmd.Create.Issue == nil {
		return executor.ExecuteOutput{
			Data: fmt.Sprintf("Usage: %s create issue KIND/NAME", pluginName),
		}, nil
	}
	issueDetails, _ := getIssueDetails(ctx, cmd.Create.Issue.Namespace, cmd.Create.Issue.Type)
	mdBody, _ := renderIssueBody(cfg.GitHub.IssueTemplate, issueDetails)
	title := fmt.Sprintf("The `%s` malfunctions", cmd.Create.Issue.Type)
	issueURL, _ := createGitHubIssue(cfg, title, mdBody)

	return executor.ExecuteOutput{
		Data: fmt.Sprintf("New issue created successfully! 🎉\n\nIssue URL: %s", issueURL),
	}, nil
}

func main() {
	err := pluginx.DownloadDependencies(depsDownloadLinks)
	if err != nil {
		log.Fatal(err)
	}

	executor.Serve(map[string]plugin.Plugin{
		pluginName: &executor.Plugin{
			Executor: &GHExecutor{},
		},
	})
}

func getIssueDetails(ctx context.Context, namespace, name string) (IssueDetails, error) {
	if namespace == "" {
		namespace = defaultNamespace
	}
	logs, _ := pluginx.ExecuteCommand(ctx, fmt.Sprintf("kubectl logs %s -n %s --tail %d", name, namespace, logsTailLines))
	ver, _ := pluginx.ExecuteCommand(ctx, "kubectl version -o yaml")

	return IssueDetails{
		Type:      name,
		Namespace: namespace,
		Logs:      logs,
		Version:   ver,
	}, nil
}

func renderIssueBody(bodyTpl string, data IssueDetails) (string, error) {
	tmpl, _ := template.New("issue-body").Funcs(template.FuncMap{
		"code": func(syntax, in string) string {
			return fmt.Sprintf("\n```%s\n%s\n```\n", syntax, in)
		},
	}).Parse(bodyTpl)

	var body bytes.Buffer
	tmpl.Execute(&body, data)

	return body.String(), nil
}

func createGitHubIssue(cfg Config, title, mdBody string) (string, error) {
	cmd := fmt.Sprintf(`GH_TOKEN=%s gh issue create --title %q --body '%s' --label bug -R %s`, cfg.GitHub.Token, title, mdBody, cfg.GitHub.Repository)

	return pluginx.ExecuteCommand(context.Background(), cmd)
}
