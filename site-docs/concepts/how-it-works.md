# How It Works

`mgtt` encodes your system's dependency graph in a YAML model. A constraint engine walks the graph from the outermost component inward, probing each component and eliminating healthy branches.

The same model serves two phases: simulation (design time) and troubleshooting (runtime). See [Simulation](simulation.md) and [Troubleshooting](troubleshooting.md).
