package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"golang.org/x/term"
)

const usage = `kubecfg - group kubeconfig contexts and hide whole groups from kubectl

Usage:
  kubecfg                   open the interactive TUI
  kubecfg enable <group>    move the group's contexts back into the active kubeconfig
  kubecfg disable <group>   move the group's contexts out of the active kubeconfig
  kubecfg status            show groups, ungrouped contexts and drift
  kubecfg help              show this help

Files:
  active kubeconfig   $KUBECONFIG (single path) or ~/.kube/config
  stash               ~/.config/kubecfg/stash.yaml
  groups              ~/.config/kubecfg/groups.yaml
`

// version is set by release builds via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kubecfg:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		if bi, ok := debug.ReadBuildInfo(); ok && version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version // `go install ...@vX.Y.Z`
		}
		fmt.Printf("kubecfg %s\n\n%s", version, usage)
		return nil
	}
	toggle := len(args) == 2 && (args[0] == "enable" || args[0] == "disable")
	status := len(args) == 1 && args[0] == "status"
	if len(args) == 0 && (!term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd()))) {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("the TUI needs a terminal")
	}
	if len(args) > 0 && !toggle && !status {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command")
	}
	paths, err := defaultPaths()
	if err != nil {
		return err
	}
	st, err := load(paths)
	if err != nil {
		return err
	}
	switch {
	case toggle:
		return st.SetEnabled(args[1], args[0] == "enable")
	case status:
		fmt.Print(st.Status())
		return nil
	}
	return runTUI(paths, st)
}
