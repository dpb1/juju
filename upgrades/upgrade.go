// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package upgrades

import (
	"fmt"

	"github.com/loggo/loggo"

	"launchpad.net/juju-core/agent"
	"launchpad.net/juju-core/state/api"
	"launchpad.net/juju-core/version"
)

var logger = loggo.GetLogger("juju.upgrade")

// Step defines an idempotent operation that is run to perform
// a specific upgrade step.
type Step interface {
	// Description is a human readable description of what the upgrade step does.
	Description() string

	// Targets returns the target machine types for which the upgrade step is applicable.
	Targets() []Target

	// Run executes the upgrade business logic.
	Run(context Context) error
}

// Operation defines what steps to perform to upgrade to a target version.
type Operation interface {
	// The Juju version for which this operation is applicable.
	// Upgrade operations designed for versions of Juju earlier
	// than we are upgrading from are not run since such steps would
	// already have been used to get to the version we are running now.
	TargetVersion() version.Number

	// Steps to perform during an upgrade.
	Steps() []Step
}

// Target defines the type of machine for which a particular upgrade
// step can be run.
type Target string

const (
	// AllMachines applies to any machine.
	AllMachines = Target("allMachines")

	// HostMachine is a machine on which units are deployed.
	HostMachine = Target("hostMachine")

	// StateServer is a machine participating in a Juju state server cluster.
	StateServer = Target("stateServer")
)

// upgradeToVersion encapsulates the steps which need to be run to
// upgrade any prior version of Juju to targetVersion.
type upgradeToVersion struct {
	targetVersion version.Number
	steps         []Step
}

// Steps is defined on the Operation interface.
func (u upgradeToVersion) Steps() []Step {
	return u.steps
}

// TargetVersion is defined on the Operation interface.
func (u upgradeToVersion) TargetVersion() version.Number {
	return u.targetVersion
}

// Context is used give the upgrade steps attributes needed
// to do their job.
type Context interface {
	// APIState returns an API connection to state.
	APIState() *api.State

	// AgentConfig returns the agent config for the machine that is being upgraded.
	AgentConfig() agent.Config
}

// UpgradeContext is a default Context implementation.
type UpgradeContext struct {
	// Work in progress........
	// Exactly what a context needs is to be determined as the
	// implementation evolves.
	st          *api.State
	agentConfig agent.Config
}

// APIState is defined on the Context interface.
func (c *UpgradeContext) APIState() *api.State {
	return c.st
}

// AgentConfig is defined on the Context interface.
func (c *UpgradeContext) AgentConfig() agent.Config {
	return c.agentConfig
}

// upgradeError records a description of the step being performed and the error.
type upgradeError struct {
	description string
	err         error
}

func (e *upgradeError) Error() string {
	return fmt.Sprintf("%s: %v", e.description, e.err)
}

// PerformUpgrade runs the business logic needed to upgrade the current "from" version to this
// version of Juju on the "target" type of machine.
func PerformUpgrade(from version.Number, target Target, context Context) *upgradeError {
	// If from is not known, it is 1.16.
	if from == version.Zero {
		from = version.MustParse("1.16.0")
	}
	for _, upgradeOps := range upgradeOperations() {
		// Do not run steps for versions of Juju earlier or same as we are upgrading from.
		if upgradeOps.TargetVersion().Compare(from) < 1 {
			continue
		}
		if err := runUpgradeSteps(context, target, upgradeOps); err != nil {
			return err
		}
	}
	return nil
}

// validTarget returns true if target is in step.Targets().
func validTarget(target Target, step Step) bool {
	for _, opTarget := range step.Targets() {
		if target == AllMachines || target == opTarget {
			return true
		}
	}
	return len(step.Targets()) == 0
}

// runUpgradeSteps runs all the upgrade steps relevant to target.
// As soon as any error is encountered, the operation is aborted since
// subsequent steps may required successful completion of earlier ones.
// The steps must be idempotent so that the entire upgrade operation can
// be retried.
func runUpgradeSteps(context Context, target Target, upgradeOp Operation) *upgradeError {
	for _, step := range upgradeOp.Steps() {
		if !validTarget(target, step) {
			continue
		}
		if err := step.Run(context); err != nil {
			return &upgradeError{
				description: step.Description(),
				err:         err,
			}
		}
	}
	return nil
}

type upgradeStep struct {
	description string
	targets     []Target
	run         func(Context) error
}

// Description is defined on the Step interface.
func (step *upgradeStep) Description() string {
	return step.description
}

// Targets is defined on the Step interface.
func (step *upgradeStep) Targets() []Target {
	return step.targets
}

// Run is defined on the Step interface.
func (step *upgradeStep) Run(context Context) error {
	return step.run(context)
}