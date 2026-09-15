package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

type llmPlanCLIOptions struct {
	TaskPath          string
	OutDir            string
	ModelID           string
	Name              string
	Model             string
	ReasoningEffort   string
	Temperature       float64
	MaxOutputTokens   int
	MaxRepairAttempts int
	Mock              bool
}

type llmRepairCLIOptions struct {
	BundleDir       string
	Issue           string
	OutDir          string
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
	Mock            bool
}

type llmBaselineCLIOptions struct {
	TaskPath        string
	OutDir          string
	Target          string
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
	Mock            bool
}

func runLLMPlan(args []string) int {
	opts, ok := parseLLMPlanOptions(args)
	if !ok {
		return 2
	}
	client, err := makeLLMClient(opts.Mock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm-plan: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	result, err := llmpipeline.RunPlan(ctx, client, llmpipeline.PlanOptions{
		TaskPath:          opts.TaskPath,
		OutDir:            opts.OutDir,
		ModelID:           opts.ModelID,
		Name:              opts.Name,
		Model:             opts.Model,
		ReasoningEffort:   opts.ReasoningEffort,
		Temperature:       opts.Temperature,
		MaxOutputTokens:   opts.MaxOutputTokens,
		MaxRepairAttempts: opts.MaxRepairAttempts,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm-plan: %v\n", err)
		return 1
	}
	fmt.Printf("wrote LLM-assisted v0.5 bundle to %s\n", result.OutDir)
	if result.ValidationReport.OK() {
		fmt.Println("validation ok")
	} else {
		printValidationFailure(result.ValidationReport, os.Stdout)
		return 1
	}
	printLintResult(result.LintReport)
	if result.LintReport.HasErrors() {
		return 1
	}
	if result.GeneratedDBML {
		fmt.Printf("wrote %s\n", result.DBMLPath)
		fmt.Printf("wrote %s\n", result.TracePath)
	}
	return 0
}

func runLLMRepair(args []string) int {
	opts, ok := parseLLMRepairOptions(args)
	if !ok {
		return 2
	}
	client, err := makeLLMClient(opts.Mock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm-repair: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	err = llmpipeline.RunRepair(ctx, client, llmpipeline.RepairOptions{
		BundleDir:       opts.BundleDir,
		Issue:           opts.Issue,
		OutDir:          opts.OutDir,
		Model:           opts.Model,
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm-repair: %v\n", err)
		return 1
	}
	fmt.Printf("wrote repair proposal to %s\n", opts.OutDir)
	return 0
}

func runLLMBaseline(args []string) int {
	opts, ok := parseLLMBaselineOptions(args)
	if !ok {
		return 2
	}
	client, err := makeLLMClient(opts.Mock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm-baseline: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	err = llmpipeline.RunBaseline(ctx, client, llmpipeline.BaselineOptions{
		TaskPath:        opts.TaskPath,
		OutDir:          opts.OutDir,
		Target:          opts.Target,
		Model:           opts.Model,
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm-baseline: %v\n", err)
		return 1
	}
	fmt.Printf("wrote direct LLM %s baseline to %s\n", opts.Target, opts.OutDir)
	return 0
}

func makeLLMClient(mock bool) (llm.Client, error) {
	if mock {
		return llm.NewDefaultMockClient(), nil
	}
	return llm.NewOpenAIClientFromEnv()
}

func parseLLMPlanOptions(args []string) (llmPlanCLIOptions, bool) {
	opts := llmPlanCLIOptions{MaxRepairAttempts: 1}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mock":
			opts.Mock = true
		case arg == "--out":
			value, ok := optionValue("llm-plan", "--out", args, &i)
			if !ok {
				return opts, false
			}
			opts.OutDir = value
		case strings.HasPrefix(arg, "--out="):
			opts.OutDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--id":
			value, ok := optionValue("llm-plan", "--id", args, &i)
			if !ok {
				return opts, false
			}
			opts.ModelID = value
		case strings.HasPrefix(arg, "--id="):
			opts.ModelID = strings.TrimPrefix(arg, "--id=")
		case arg == "--name":
			value, ok := optionValue("llm-plan", "--name", args, &i)
			if !ok {
				return opts, false
			}
			opts.Name = value
		case strings.HasPrefix(arg, "--name="):
			opts.Name = strings.TrimPrefix(arg, "--name=")
		case arg == "--model":
			value, ok := optionValue("llm-plan", "--model", args, &i)
			if !ok {
				return opts, false
			}
			opts.Model = value
		case strings.HasPrefix(arg, "--model="):
			opts.Model = strings.TrimPrefix(arg, "--model=")
		case arg == "--reasoning-effort":
			value, ok := optionValue("llm-plan", "--reasoning-effort", args, &i)
			if !ok {
				return opts, false
			}
			opts.ReasoningEffort = value
		case strings.HasPrefix(arg, "--reasoning-effort="):
			opts.ReasoningEffort = strings.TrimPrefix(arg, "--reasoning-effort=")
		case arg == "--temperature":
			value, ok := optionValue("llm-plan", "--temperature", args, &i)
			if !ok || !parseFloatOption("llm-plan", "--temperature", value, &opts.Temperature) {
				return opts, false
			}
		case strings.HasPrefix(arg, "--temperature="):
			if !parseFloatOption("llm-plan", "--temperature", strings.TrimPrefix(arg, "--temperature="), &opts.Temperature) {
				return opts, false
			}
		case arg == "--max-output-tokens":
			value, ok := optionValue("llm-plan", "--max-output-tokens", args, &i)
			if !ok || !parseIntOption("llm-plan", "--max-output-tokens", value, &opts.MaxOutputTokens) {
				return opts, false
			}
		case strings.HasPrefix(arg, "--max-output-tokens="):
			if !parseIntOption("llm-plan", "--max-output-tokens", strings.TrimPrefix(arg, "--max-output-tokens="), &opts.MaxOutputTokens) {
				return opts, false
			}
		case arg == "--max-repair-attempts":
			value, ok := optionValue("llm-plan", "--max-repair-attempts", args, &i)
			if !ok || !parseIntOption("llm-plan", "--max-repair-attempts", value, &opts.MaxRepairAttempts) {
				return opts, false
			}
		case strings.HasPrefix(arg, "--max-repair-attempts="):
			if !parseIntOption("llm-plan", "--max-repair-attempts", strings.TrimPrefix(arg, "--max-repair-attempts="), &opts.MaxRepairAttempts) {
				return opts, false
			}
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(os.Stderr, "llm-plan: unknown option %s\n", arg)
			return opts, false
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || opts.OutDir == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl llm-plan <task.md> --out <dir> [--mock] [--model <model>]")
		return opts, false
	}
	opts.TaskPath = positional[0]
	return opts, true
}

func parseLLMRepairOptions(args []string) (llmRepairCLIOptions, bool) {
	var opts llmRepairCLIOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mock":
			opts.Mock = true
		case arg == "--out":
			value, ok := optionValue("llm-repair", "--out", args, &i)
			if !ok {
				return opts, false
			}
			opts.OutDir = value
		case strings.HasPrefix(arg, "--out="):
			opts.OutDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--issue":
			value, ok := optionValue("llm-repair", "--issue", args, &i)
			if !ok {
				return opts, false
			}
			opts.Issue = value
		case strings.HasPrefix(arg, "--issue="):
			opts.Issue = strings.TrimPrefix(arg, "--issue=")
		case arg == "--model":
			value, ok := optionValue("llm-repair", "--model", args, &i)
			if !ok {
				return opts, false
			}
			opts.Model = value
		case strings.HasPrefix(arg, "--model="):
			opts.Model = strings.TrimPrefix(arg, "--model=")
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(os.Stderr, "llm-repair: unknown option %s\n", arg)
			return opts, false
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || opts.OutDir == "" || opts.Issue == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl llm-repair <bundle_dir> --issue <issue_id> --out <dir> [--mock]")
		return opts, false
	}
	opts.BundleDir = positional[0]
	return opts, true
}

func parseLLMBaselineOptions(args []string) (llmBaselineCLIOptions, bool) {
	var opts llmBaselineCLIOptions
	opts.Target = "dbml"
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--mock":
			opts.Mock = true
		case arg == "--out":
			value, ok := optionValue("llm-baseline", "--out", args, &i)
			if !ok {
				return opts, false
			}
			opts.OutDir = value
		case strings.HasPrefix(arg, "--out="):
			opts.OutDir = strings.TrimPrefix(arg, "--out=")
		case arg == "--target":
			value, ok := optionValue("llm-baseline", "--target", args, &i)
			if !ok {
				return opts, false
			}
			opts.Target = value
		case strings.HasPrefix(arg, "--target="):
			opts.Target = strings.TrimPrefix(arg, "--target=")
		case arg == "--model":
			value, ok := optionValue("llm-baseline", "--model", args, &i)
			if !ok {
				return opts, false
			}
			opts.Model = value
		case strings.HasPrefix(arg, "--model="):
			opts.Model = strings.TrimPrefix(arg, "--model=")
		case strings.HasPrefix(arg, "--"):
			fmt.Fprintf(os.Stderr, "llm-baseline: unknown option %s\n", arg)
			return opts, false
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || opts.OutDir == "" {
		fmt.Fprintln(os.Stderr, "usage: dbdsl llm-baseline <task.md> --out <dir> [--target dbml|sql] [--mock]")
		return opts, false
	}
	if opts.Target != "dbml" && opts.Target != "sql" {
		fmt.Fprintln(os.Stderr, "llm-baseline: --target must be dbml or sql")
		return opts, false
	}
	opts.TaskPath = positional[0]
	return opts, true
}

func optionValue(command, option string, args []string, index *int) (string, bool) {
	if *index+1 >= len(args) {
		fmt.Fprintf(os.Stderr, "%s: %s requires a value\n", command, option)
		return "", false
	}
	*index = *index + 1
	return args[*index], true
}

func parseFloatOption(command, option, value string, target *float64) bool {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s requires a numeric value\n", command, option)
		return false
	}
	*target = parsed
	return true
}

func parseIntOption(command, option, value string, target *int) bool {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s requires an integer value\n", command, option)
		return false
	}
	*target = parsed
	return true
}
