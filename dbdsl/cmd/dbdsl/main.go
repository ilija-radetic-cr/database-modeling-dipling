package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbdsl/internal/evaluation"
	"dbdsl/internal/generate"
	"dbdsl/internal/lint"
	"dbdsl/internal/scaffold"
	"dbdsl/internal/validate"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 2
	}

	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "lint":
		return runLint(args[1:])
	case "dbml":
		return runDBML(args[1:])
	case "sql":
		return runSQL(args[1:])
	case "trace":
		return runTrace(args[1:])
	case "generate":
		return runGenerate(args[1:])
	case "bundle-from-task":
		return runBundleFromTask(args[1:])
	case "llm-plan":
		return runLLMPlan(args[1:])
	case "llm-repair":
		return runLLMRepair(args[1:])
	case "llm-baseline":
		return runLLMBaseline(args[1:])
	case "evaluate":
		return runEvaluate(args[1:])
	case "evaluation-freeze-check":
		return runEvaluationFreezeCheck(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage()
		return 2
	}
}

type commandOptions struct {
	Path    string
	POC     string
	Version string
	Write   bool
	Strict  bool
}

type bundleFromTaskOptions struct {
	TaskPath string
	OutDir   string
	ModelID  string
	Name     string
}

func runValidate(args []string) int {
	opts, ok := parseCommandOptions("validate", args, false, false)
	if !ok {
		return 2
	}
	result := validate.ValidateFile(opts.Path)
	if result.OK() {
		fmt.Println("validation ok")
		return 0
	}
	printValidationFailure(result, os.Stdout)
	return 1
}

func runLint(args []string) int {
	opts, ok := parseCommandOptions("lint", args, false, true)
	if !ok {
		return 2
	}
	validationResult := validate.ValidateFile(opts.Path)
	if !validationResult.OK() {
		printValidationFailure(validationResult, os.Stderr)
		return 1
	}
	result := lint.LintFile(opts.Path)
	printLintResult(result)
	if result.HasErrors() {
		return 1
	}
	if opts.Strict && result.HasWarnings() {
		return 1
	}
	return 0
}

func runSQL(args []string) int {
	opts, ok := parseCommandOptions("sql", args, true, false)
	if !ok {
		return 2
	}
	if !validateBeforeGeneration(opts.Path) {
		return 1
	}
	output, err := generate.PostgreSQLFile(opts.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate sql: %v\n", err)
		return 1
	}
	if opts.Write {
		outputPath, ok := outputPathFor(opts, "sql")
		if !ok {
			return 2
		}
		if err := writeOutput(outputPath, output); err != nil {
			fmt.Fprintf(os.Stderr, "write sql: %v\n", err)
			return 1
		}
		fmt.Printf("wrote %s\n", outputPath)
		return 0
	}
	fmt.Print(output)
	return 0
}

func runDBML(args []string) int {
	opts, ok := parseCommandOptions("dbml", args, true, false)
	if !ok {
		return 2
	}
	if !validateBeforeGeneration(opts.Path) {
		return 1
	}
	output, err := generate.DBMLFile(opts.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate dbml: %v\n", err)
		return 1
	}
	if opts.Write {
		outputPath, ok := outputPathFor(opts, "dbml")
		if !ok {
			return 2
		}
		if err := writeOutput(outputPath, output); err != nil {
			fmt.Fprintf(os.Stderr, "write dbml: %v\n", err)
			return 1
		}
		fmt.Printf("wrote %s\n", outputPath)
		return 0
	}
	fmt.Print(output)
	return 0
}

func runTrace(args []string) int {
	opts, ok := parseCommandOptions("trace", args, true, false)
	if !ok {
		return 2
	}
	if !validateBeforeGeneration(opts.Path) {
		return 1
	}
	output, err := generate.TraceFile(opts.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate trace report: %v\n", err)
		return 1
	}
	if opts.Write {
		outputPath, ok := outputPathFor(opts, "trace")
		if !ok {
			return 2
		}
		if err := writeOutput(outputPath, output); err != nil {
			fmt.Fprintf(os.Stderr, "write trace report: %v\n", err)
			return 1
		}
		fmt.Printf("wrote %s\n", outputPath)
		return 0
	}
	fmt.Print(output)
	return 0
}

func runGenerate(args []string) int {
	opts, ok := parseCommandOptions("generate", args, false, false)
	if !ok {
		return 2
	}
	if opts.POC == "" || opts.Version == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl generate --poc <case> --version <version>")
		return 2
	}
	if !validateBeforeGeneration(opts.Path) {
		return 1
	}
	dbml, err := generate.DBMLFile(opts.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate dbml: %v\n", err)
		return 1
	}
	trace, err := generate.TraceFile(opts.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate trace report: %v\n", err)
		return 1
	}
	dbmlPath, _ := outputPathFor(opts, "dbml")
	tracePath, _ := outputPathFor(opts, "trace")
	if err := writeOutput(dbmlPath, dbml); err != nil {
		fmt.Fprintf(os.Stderr, "write dbml: %v\n", err)
		return 1
	}
	if err := writeOutput(tracePath, trace); err != nil {
		fmt.Fprintf(os.Stderr, "write trace report: %v\n", err)
		return 1
	}
	fmt.Printf("validation ok\n")
	fmt.Printf("wrote %s\n", dbmlPath)
	fmt.Printf("wrote %s\n", tracePath)
	return 0
}

func runBundleFromTask(args []string) int {
	opts, ok := parseBundleFromTaskOptions(args)
	if !ok {
		return 2
	}
	result, err := scaffold.BundleFromTask(opts.TaskPath, opts.OutDir, scaffold.Options{
		ModelID: opts.ModelID,
		Name:    opts.Name,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle-from-task: %v\n", err)
		return 1
	}
	validation := validate.ValidateFile(result.ModelPath)
	if !validation.OK() {
		printValidationFailure(validation, os.Stderr)
		return 1
	}
	lintResult := lint.LintFile(result.ModelPath)
	if lintResult.HasErrors() {
		printLintResult(lintResult)
		return 1
	}
	fmt.Printf("validation ok\n")
	printLintResult(lintResult)
	fmt.Printf("wrote v0.5 bundle to %s\n", opts.OutDir)
	for _, path := range result.Files {
		fmt.Printf("- %s\n", path)
	}
	return 0
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  dbdsl validate <db_model.dsl.yaml>")
	fmt.Fprintln(os.Stderr, "  dbdsl lint <db_model.dsl.yaml> [--strict]")
	fmt.Fprintln(os.Stderr, "  dbdsl dbml <db_model.dsl.yaml>")
	fmt.Fprintln(os.Stderr, "  dbdsl sql <db_model.dsl.yaml>   (PostgreSQL DDL)")
	fmt.Fprintln(os.Stderr, "  dbdsl trace <db_model.dsl.yaml>")
	fmt.Fprintln(os.Stderr, "  dbdsl validate --poc <case> --version <version>")
	fmt.Fprintln(os.Stderr, "  dbdsl lint --poc <case> --version <version> [--strict]")
	fmt.Fprintln(os.Stderr, "  dbdsl dbml --poc <case> --version <version> [--write]")
	fmt.Fprintln(os.Stderr, "  dbdsl trace --poc <case> --version <version> [--write]")
	fmt.Fprintln(os.Stderr, "  dbdsl generate --poc <case> --version <version>")
	fmt.Fprintln(os.Stderr, "  dbdsl bundle-from-task <task.md> --out <dir> [--id <model_id>] [--name <model name>]")
	fmt.Fprintln(os.Stderr, "  dbdsl llm-plan <task.md> --out <dir> [--mock] [--model <model>]")
	fmt.Fprintln(os.Stderr, "  dbdsl llm-repair <bundle_dir> --issue <issue_id> --out <dir> [--mock]")
	fmt.Fprintln(os.Stderr, "  dbdsl llm-baseline <task.md> --out <dir> [--target dbml|sql] [--mock]")
	fmt.Fprintln(os.Stderr, "  dbdsl evaluate <reference_db_model.dsl.yaml> <candidate_db_model.dsl.yaml> --out <dir>")
	fmt.Fprintln(os.Stderr, "  dbdsl evaluation-freeze-check <freeze_manifest.yaml> [--stage preparation|ai-internal|human-signed]")
}

func runEvaluationFreezeCheck(args []string) int {
	manifestPath := ""
	stage := evaluation.FreezeStagePreparation
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--stage" && i+1 < len(args):
			i++
			stage = evaluation.FreezeStage(args[i])
		case strings.HasPrefix(args[i], "--stage="):
			stage = evaluation.FreezeStage(strings.TrimPrefix(args[i], "--stage="))
		case strings.HasPrefix(args[i], "--") || manifestPath != "":
			fmt.Fprintf(os.Stderr, "evaluation-freeze-check: unknown option or extra argument %s\n", args[i])
			return 2
		default:
			manifestPath = args[i]
		}
	}
	if manifestPath == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl evaluation-freeze-check <freeze_manifest.yaml> [--stage preparation|ai-internal|human-signed]")
		return 2
	}
	result, err := evaluation.CheckFreezePackageWithStage(manifestPath, stage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evaluation-freeze-check: %v\n", err)
		return 1
	}
	fmt.Printf("freeze package ok: %s\n", result.PackageID)
	fmt.Printf("domains: %d; verified artifacts: %d\n", result.Domains, result.Artifacts)
	fmt.Printf("source coverage: %d/%d non-empty lines; obligations: %d\n", result.CoveredLines, result.NonEmptyLines, result.Obligations)
	return 0
}

func runEvaluate(args []string) int {
	var positional []string
	outDir := ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--out" && i+1 < len(args):
			i++
			outDir = args[i]
		case strings.HasPrefix(args[i], "--out="):
			outDir = strings.TrimPrefix(args[i], "--out=")
		case strings.HasPrefix(args[i], "--"):
			fmt.Fprintf(os.Stderr, "evaluate: unknown or incomplete option %s\n", args[i])
			return 2
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 || strings.TrimSpace(outDir) == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl evaluate <reference_db_model.dsl.yaml> <candidate_db_model.dsl.yaml> --out <dir>")
		return 2
	}
	report, err := evaluation.Compare(positional[0], positional[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "evaluate: %v\n", err)
		return 1
	}
	if err := evaluation.Write(report, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "evaluate: write report: %v\n", err)
		return 1
	}
	fmt.Printf("wrote evaluation report to %s\n", outDir)
	return 0
}

func validateBeforeGeneration(path string) bool {
	result := validate.ValidateFile(path)
	if result.OK() {
		return true
	}
	printValidationFailure(result, os.Stderr)
	return false
}

func printValidationFailure(result validate.Result, out *os.File) {
	fmt.Fprintln(out, "validation failed:")
	for _, err := range result.Errors {
		fmt.Fprintf(out, "- %s\n", err)
	}
}

func printLintResult(result lint.Result) {
	version := result.Version
	if version == "" {
		version = "v0.2"
	}
	if len(result.Issues) == 0 {
		fmt.Printf("lint %s ok\n", version)
		return
	}
	fmt.Printf("lint %s found %d issue(s):\n", version, len(result.Issues))
	for _, issue := range result.Issues {
		if issue.Element != "" {
			fmt.Printf("- [%s] %s %s: %s\n", issue.Severity, issue.Code, issue.Element, issue.Message)
			continue
		}
		fmt.Printf("- [%s] %s: %s\n", issue.Severity, issue.Code, issue.Message)
	}
}

func parseCommandOptions(command string, args []string, allowWrite bool, allowStrict bool) (commandOptions, bool) {
	var opts commandOptions
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--poc":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s: --poc requires a value\n", command)
				return opts, false
			}
			i++
			opts.POC = args[i]
		case strings.HasPrefix(arg, "--poc="):
			opts.POC = strings.TrimPrefix(arg, "--poc=")
		case arg == "--version":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "%s: --version requires a value\n", command)
				return opts, false
			}
			i++
			opts.Version = args[i]
		case strings.HasPrefix(arg, "--version="):
			opts.Version = strings.TrimPrefix(arg, "--version=")
		case arg == "--write":
			if !allowWrite {
				fmt.Fprintf(os.Stderr, "%s: --write is not supported\n", command)
				return opts, false
			}
			opts.Write = true
		case arg == "--strict":
			if !allowStrict {
				fmt.Fprintf(os.Stderr, "%s: --strict is not supported\n", command)
				return opts, false
			}
			opts.Strict = true
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(os.Stderr, "%s: unknown option %s\n", command, arg)
			return opts, false
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) > 1 {
		fmt.Fprintf(os.Stderr, "%s: expected at most one path argument\n", command)
		return opts, false
	}
	if len(positional) == 1 {
		if opts.POC != "" || opts.Version != "" {
			fmt.Fprintf(os.Stderr, "%s: use either a path or --poc/--version, not both\n", command)
			return opts, false
		}
		opts.Path = positional[0]
	}
	if opts.Path == "" {
		if opts.POC == "" || opts.Version == "" {
			fmt.Fprintf(os.Stderr, "usage: dbdsl %s <db_model.dsl.yaml>\n", command)
			fmt.Fprintf(os.Stderr, "   or: dbdsl %s --poc <case> --version <version>\n", command)
			return opts, false
		}
		opts.Path = pocModelPath(opts.POC, opts.Version)
	}
	if opts.Write && (opts.POC == "" || opts.Version == "") {
		fmt.Fprintf(os.Stderr, "%s: --write requires --poc and --version\n", command)
		return opts, false
	}
	return opts, true
}

func parseBundleFromTaskOptions(args []string) (bundleFromTaskOptions, bool) {
	var opts bundleFromTaskOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--out":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "bundle-from-task: --out requires a value")
				return opts, false
			}
			i++
			opts.OutDir = args[i]
		case strings.HasPrefix(arg, "--out="):
			opts.OutDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--id":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "bundle-from-task: --id requires a value")
				return opts, false
			}
			i++
			opts.ModelID = args[i]
		case strings.HasPrefix(arg, "--id="):
			opts.ModelID = strings.TrimPrefix(arg, "--id=")
		case arg == "--name":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "bundle-from-task: --name requires a value")
				return opts, false
			}
			i++
			opts.Name = args[i]
		case strings.HasPrefix(arg, "--name="):
			opts.Name = strings.TrimPrefix(arg, "--name=")
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(os.Stderr, "bundle-from-task: unknown option %s\n", arg)
			return opts, false
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || opts.OutDir == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl bundle-from-task <task.md> --out <dir> [--id <model_id>] [--name <model name>]")
		return opts, false
	}
	opts.TaskPath = positional[0]
	return opts, true
}

func pocModelPath(poc, version string) string {
	return filepath.Join("poc", poc, versionDir(version), "db_model.dsl.yaml")
}

func outputPathFor(opts commandOptions, kind string) (string, bool) {
	if opts.POC == "" || opts.Version == "" {
		fmt.Fprintln(os.Stderr, "output path requires --poc and --version")
		return "", false
	}
	dir := filepath.Join("output", "poc", versionDir(opts.Version))
	switch kind {
	case "dbml":
		return filepath.Join(dir, opts.POC+".dbml"), true
	case "trace":
		return filepath.Join(dir, opts.POC+"_traceability_report.md"), true
	case "sql":
		return filepath.Join(dir, opts.POC+".postgresql.sql"), true
	default:
		fmt.Fprintf(os.Stderr, "unknown output kind %s\n", kind)
		return "", false
	}
}

func versionDir(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func writeOutput(path, output string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output), 0o644)
}
