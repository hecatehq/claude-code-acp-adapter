package app

import (
	"io"

	"github.com/hecatehq/acp-adapter-kit/adaptercli"
	"github.com/hecatehq/claude-code-acp-adapter/claudecodeadapter"
)

const (
	Name  = claudecodeadapter.Name
	Title = claudecodeadapter.Title
)

var Version = "0.0.0-dev"

func Run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	return adaptercli.Run(args, adapterSpec(stdin, stdout, stderr))
}

func adapterSpec(stdin io.Reader, stdout io.Writer, stderr io.Writer) adaptercli.Spec {
	return claudecodeadapter.NewCLISpec(Version, stdin, stdout, stderr)
}
