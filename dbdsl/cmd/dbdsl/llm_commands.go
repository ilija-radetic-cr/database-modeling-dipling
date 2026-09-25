package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

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
