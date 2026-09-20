package config

// The raw types mirror the YAML document one to one. Every yaml tag here is a
// key a suite may contain, and schema/himorime.schema.json lists exactly the same
// keys: TestSchemaKeysMatchGo walks these structs and fails on any difference.
// Pointers mark the settings whose absence means "inherit".

// RawFile is the top level of a suite file.
type RawFile struct {
	Version     string         `yaml:"version"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Defaults    *RawDefaults   `yaml:"defaults"`
	Build       *RawExec       `yaml:"build"`
	Benchmarks  []RawBenchmark `yaml:"benchmarks"`
	Report      *RawReport     `yaml:"report"`
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
	Metrics    *RawMetrics       `yaml:"metrics"`
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
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Tags        []string          `yaml:"tags"`
	Warmup      *int              `yaml:"warmup"`
	Runs        *int              `yaml:"runs"`
	MinRuns     *int              `yaml:"min_runs"`
	MaxRuns     *int              `yaml:"max_runs"`
	MinTime     *Duration         `yaml:"min_time"`
	Timeout     *Duration         `yaml:"timeout"`
	Cwd         *string           `yaml:"cwd"`
	Env         map[string]string `yaml:"env"`
	Stdout      *string           `yaml:"stdout"`
	Stderr      *string           `yaml:"stderr"`
	ExitCodes   []int             `yaml:"exit_codes"`
	Stdin       *StdinSpec        `yaml:"stdin"`
	Terminal    bool              `yaml:"terminal"`
	Setup       []RawExec         `yaml:"setup"`
	PrepareEach []RawExec         `yaml:"prepare_each"`
	Cleanup     []RawExec         `yaml:"cleanup"`
	Baseline    string            `yaml:"baseline"`
	Commands    Commands          `yaml:"commands"`
	Metrics     *RawMetrics       `yaml:"metrics"`
	Regression  *RawRegression    `yaml:"regression"`
}

// RawCommand is one named command of a benchmark.
type RawCommand struct {
	Command   Argv                         `yaml:"command"`
	Shell     bool                         `yaml:"shell"`
	Cwd       *string                      `yaml:"cwd"`
	Env       map[string]string            `yaml:"env"`
	Timeout   *Duration                    `yaml:"timeout"`
	Stdout    *string                      `yaml:"stdout"`
	Stderr    *string                      `yaml:"stderr"`
	ExitCodes []int                        `yaml:"exit_codes"`
	Budget    map[string]map[string]string `yaml:"budget"`
}

// RawMetrics selects what a benchmark measures besides latency. In defaults
// it holds everything but throughput, whose work belongs to one benchmark.
type RawMetrics struct {
	Throughput  *RawThroughput `yaml:"throughput"`
	CPU         *bool          `yaml:"cpu"`
	Memory      *bool          `yaml:"memory"`
	Unsupported *string        `yaml:"unsupported"`
}

// RawThroughput declares the work one run of the benchmark does.
type RawThroughput struct {
	Work RawWork `yaml:"work"`
}

// RawWork is the amount of work of one run: a number, or the size of a file.
type RawWork struct {
	Value    *float64 `yaml:"value"`
	FileSize *string  `yaml:"file_size"`
	Unit     *string  `yaml:"unit"`
}

// RawRegression configures how a base revision and the working tree are
// compared. confidence, min_samples, max_cv and commands apply to every
// independent metric; throughput derives its verdict from latency.
type RawRegression struct {
	Confidence *float64             `yaml:"confidence"`
	MinSamples *int                 `yaml:"min_samples"`
	MaxCV      *float64             `yaml:"max_cv"`
	Commands   []string             `yaml:"commands"`
	Latency    *RawMetricRegression `yaml:"latency"`
	CPUTotal   *RawMetricRegression `yaml:"cpu_total"`
	PeakRSS    *RawMetricRegression `yaml:"peak_rss"`
}

// RawMetricRegression is the tolerance of one metric in a comparison, and
// whether its verdict gates the result.
type RawMetricRegression struct {
	Statistic     *string  `yaml:"statistic"`
	MaxPercent    *Percent `yaml:"max_percent"`
	MinDifference *string  `yaml:"min_difference"`
	Gate          *bool    `yaml:"gate"`
}

// RawReport lists the report files written after every run, and the tools
// whose versions reports record.
type RawReport struct {
	Outputs  []RawOutput `yaml:"outputs"`
	Versions Versions    `yaml:"versions"`
}

// RawOutput is one report file. Section names the part of an existing
// Markdown file the report replaces.
type RawOutput struct {
	Format  string `yaml:"format"`
	Path    string `yaml:"path"`
	Section string `yaml:"section"`
}
