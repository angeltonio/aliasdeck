package app

import (
	"context"
	"fmt"
	"os"

	"github.com/angeltonio/aliasdeck/internal/apply"
	"github.com/angeltonio/aliasdeck/internal/config"
	"github.com/angeltonio/aliasdeck/internal/state"
)

// UninstallOptions configures Uninstall.
type UninstallOptions struct {
	Options

	// Yes skips the confirmation prompt (--yes/-f).
	Yes bool

	// Confirm asks the user a yes/no question. Defaults to prompting over
	// Env's Stdin/Stdout when nil.
	Confirm func(question string) (bool, error)
}

// UninstallReport summarizes what one `uninstall` run did.
type UninstallReport struct {
	Cancelled bool

	OutputPath    string
	OutputRemoved bool

	RCPath           string
	BootstrapRemoved bool
	// BootstrapExact reports whether the rc file was restored byte-for-byte
	// (native-apply spec, "Uninstall Restores Byte-Identical Files"). It is
	// true both when there was nothing to remove and when the exact
	// recorded block was found and cut; it is false only when
	// apply.RemoveBootstrap had to fall back to its marker-scan heuristic
	// because the user had edited inside the block.
	BootstrapExact bool

	StateRemoved bool
}

// Uninstall removes the generated file and the bootstrap sentinel block,
// leaving every other user file byte-identical to before install
// (cli-commands spec, "uninstall Confirms and Restores"). It prompts for
// confirmation unless Yes is set.
func Uninstall(_ context.Context, env Env, opts UninstallOptions) (UninstallReport, error) {
	dc, err := loadDeviceContext(env, opts.Options)
	if err != nil {
		return UninstallReport{}, err
	}

	if !opts.Yes {
		confirm := opts.Confirm
		if confirm == nil {
			confirm = func(q string) (bool, error) { return promptYesNo(env, q) }
		}
		ok, err := confirm(fmt.Sprintf(
			"Remove AliasDeck's generated file and bootstrap line for device %q?", dc.Device.Name))
		if err != nil {
			return UninstallReport{}, fmt.Errorf("reading uninstall confirmation: %w", err)
		}
		if !ok {
			return UninstallReport{Cancelled: true}, nil
		}
	}

	statePath := config.StateFile(dc.Base)
	st, err := state.Load(statePath)
	if err != nil {
		return UninstallReport{}, err
	}

	report := UninstallReport{BootstrapExact: true}

	if outputPath, err := dc.Backend.OutputPath(dc.Device); err == nil {
		report.OutputPath = outputPath
		if rmErr := os.Remove(outputPath); rmErr == nil {
			report.OutputRemoved = true
		} else if !os.IsNotExist(rmErr) {
			return report, fmt.Errorf("removing %s: %w", outputPath, rmErr)
		}
	}

	if st.Bootstrap != nil {
		report.RCPath = st.Bootstrap.RCPath
		exact, err := apply.RemoveBootstrap(st.Bootstrap.RCPath, st.Bootstrap.Block)
		if err != nil {
			return report, fmt.Errorf("removing bootstrap line from %s: %w", st.Bootstrap.RCPath, err)
		}
		report.BootstrapRemoved = true
		report.BootstrapExact = exact
	}

	if err := os.Remove(statePath); err == nil {
		report.StateRemoved = true
	} else if !os.IsNotExist(err) {
		return report, fmt.Errorf("removing %s: %w", statePath, err)
	}

	return report, nil
}
