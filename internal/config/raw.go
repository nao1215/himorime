package config

// The raw types mirror the YAML document one to one. Every yaml tag here is a
// key a suite may contain, and schema/yahiko.schema.json lists exactly the same
// keys: TestSchemaKeysMatchGo walks these structs and fails on any difference.
// Pointers mark the settings whose absence means "inherit".

// RawFile is the top level of a suite file.
type RawFile struct {
	Version    string         `yaml:"version"`
	Suite      *RawSuite      `yaml:"suite"`
	Defaults   *RawDefaults   `yaml:"defaults"`
	Build      *RawExec       `yaml:"build"`
	Benchmarks []RawBenchmark `yaml:"benchmarks"`
	Report     *RawReport     `yaml:"report"`
}

// RawSuite names the suite.
type RawSuite struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// RawDefaults holds the settings every benchmark inherits.
type RawDefaults struct {
	Warmup     *int              `yaml:"warmup"`
	Runs       *int              `yaml:"runs"`
	MinRuns    *int              `yaml:"min_runs"`
	MaxRuns    *int              `yaml:"max_runs"`
	MinTime    *Duration         `yaml:"min_time"`
	Timeout    *Duration         `yaml:"timeout"`
	Cwd        *string           `yaml:"cwd"`
	Env        map[string]string `yaml:"env"`
	Stdout     *string           `yaml:"stdout"`
	Stderr     *string           `yaml:"stderr"`
	ExitCodes  []int             `yaml:"exit_codes"`
	Regression *RawRegression    `yaml:"regression"`
}

// RawExec is a build step or a hook.
type RawExec struct {
	Command Argv              `yaml:"command"`
	Shell   bool              `yaml:"shell"`
	Cwd     *string           `yaml:"cwd"`
	Env     map[string]string `yaml:"env"`
	Timeout *Duration         `yaml:"timeout"`
}

// RawBenchmark is one benchmark case.
type RawBenchmark struct {
	Name        string               `yaml:"name"`
	Description string               `yaml:"description"`
	Tags        []string             `yaml:"tags"`
	Warmup      *int                 `yaml:"warmup"`
	Runs        *int                 `yaml:"runs"`
	MinRuns     *int                 `yaml:"min_runs"`
	MaxRuns     *int                 `yaml:"max_runs"`
	MinTime     *Duration            `yaml:"min_time"`
	Timeout     *Duration            `yaml:"timeout"`
	Cwd         *string              `yaml:"cwd"`
	Env         map[string]string    `yaml:"env"`
	Stdout      *string              `yaml:"stdout"`
	Stderr      *string              `yaml:"stderr"`
	ExitCodes   []int                `yaml:"exit_codes"`
	Stdin       *StdinSpec           `yaml:"stdin"`
	Setup       []RawExec            `yaml:"setup"`
	PrepareEach []RawExec            `yaml:"prepare_each"`
	Cleanup     []RawExec            `yaml:"cleanup"`
	Baseline    string               `yaml:"baseline"`
	Commands    Commands             `yaml:"commands"`
	Budget      map[string]RawBudget `yaml:"budget"`
	Regression  *RawRegression       `yaml:"regression"`
}

// RawCommand is one named command of a benchmark.
type RawCommand struct {
	Command   Argv              `yaml:"command"`
	Shell     bool              `yaml:"shell"`
	Cwd       *string           `yaml:"cwd"`
	Env       map[string]string `yaml:"env"`
	Timeout   *Duration         `yaml:"timeout"`
	Stdout    *string           `yaml:"stdout"`
	Stderr    *string           `yaml:"stderr"`
	ExitCodes []int             `yaml:"exit_codes"`
}

// RawBudget is the absolute budget of one command.
type RawBudget struct {
	Mean   *BudgetExpr `yaml:"mean"`
	Median *BudgetExpr `yaml:"median"`
	Min    *BudgetExpr `yaml:"min"`
	Max    *BudgetExpr `yaml:"max"`
}

// RawRegression configures how a base revision and the working tree are compared.
type RawRegression struct {
	Metric     *string  `yaml:"metric"`
	MaxPercent *Percent `yaml:"max_percent"`
	Confidence *float64 `yaml:"confidence"`
	MinSamples *int     `yaml:"min_samples"`
	MaxCV      *float64 `yaml:"max_cv"`
	Commands   []string `yaml:"commands"`
}

// RawReport lists the report files written after every run.
type RawReport struct {
	Outputs []RawOutput `yaml:"outputs"`
}

// RawOutput is one report file.
type RawOutput struct {
	Format string `yaml:"format"`
	Path   string `yaml:"path"`
}
