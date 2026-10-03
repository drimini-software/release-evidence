package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"github.com/drimini-software/release-evidence/internal/packet"
	"github.com/drimini-software/release-evidence/internal/repository"
	"github.com/drimini-software/release-evidence/internal/review"
	"github.com/drimini-software/release-evidence/internal/scanner"
)

var version = "0.1.0-dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "release-evidence %s\n", version)
		return 0
	case "scan":
		return runScan(ctx, args[1:], stdout, stderr)
	case "review":
		return runReview(ctx, args[1:], stdout, stderr)
	case "help", "--help", "-help", "-h":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runReview(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	statePath := flags.String("state", "", "local packet path; defaults to a private per-repository user config path")
	listenAddress := flags.String("listen", "127.0.0.1:0", "loopback review address; port 0 chooses an available port")
	idleTimeout := flags.Duration("idle-timeout", 30*time.Minute, "stop the local review server after this period without activity")
	maxEntries := flags.Int("max-entries", 25_000, "maximum repository entries to visit")
	maxDepth := flags.Int("max-depth", 32, "maximum repository path depth")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "review requires exactly one repository path")
		return 2
	}
	if *maxEntries <= 0 || *maxDepth <= 0 || *idleTimeout <= 0 {
		fmt.Fprintln(stderr, "review limits must be positive")
		return 2
	}

	root := flags.Arg(0)
	if *statePath == "" {
		var err error
		*statePath, err = review.DefaultStatePath(root)
		if err != nil {
			fmt.Fprintf(stderr, "prepare review state: %v\n", err)
			return 1
		}
	}

	repositoryOptions := repository.DefaultOptions()
	repositoryOptions.MaxEntries = *maxEntries
	repositoryOptions.MaxDepth = *maxDepth
	result, err := scanner.Scan(ctx, root, scanner.Options{
		Repository:  repositoryOptions,
		ToolVersion: version,
	})
	if err != nil {
		fmt.Fprintf(stderr, "scan failed: %v\n", err)
		return 1
	}
	result, err = review.RestoreAssessment(result, *statePath)
	if err != nil {
		fmt.Fprintf(stderr, "restore review: %v\n", err)
		return 1
	}
	if err := review.Serve(ctx, result, review.Options{
		ListenAddress: *listenAddress,
		StatePath:     *statePath,
		Writer:        stdout,
		IdleTimeout:   *idleTimeout,
	}); err != nil {
		fmt.Fprintf(stderr, "review failed: %v\n", err)
		return 1
	}
	return 0
}

func runScan(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "-", "write the packet to this file, or - for standard output")
	pretty := flags.Bool("pretty", true, "indent JSON output")
	maxEntries := flags.Int("max-entries", 25_000, "maximum repository entries to visit")
	maxDepth := flags.Int("max-depth", 32, "maximum repository path depth")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "scan requires exactly one repository path")
		return 2
	}
	if *maxEntries <= 0 || *maxDepth <= 0 {
		fmt.Fprintln(stderr, "scan limits must be positive")
		return 2
	}

	options := repository.DefaultOptions()
	options.MaxEntries = *maxEntries
	options.MaxDepth = *maxDepth
	result, err := scanner.Scan(ctx, flags.Arg(0), scanner.Options{
		Repository:  options,
		ToolVersion: version,
	})
	if err != nil {
		fmt.Fprintf(stderr, "scan failed: %v\n", err)
		return 1
	}

	if *output == "-" {
		if err := packet.Encode(stdout, result, *pretty); err != nil {
			fmt.Fprintf(stderr, "write packet: %v\n", err)
			return 1
		}
		return 0
	}

	if err := packet.WriteAtomic(*output, result, *pretty); err != nil {
		fmt.Fprintf(stderr, "write packet: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "wrote release evidence packet to %s\n", *output)
	return 0
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Release Evidence creates a local, deterministic repository evidence packet.")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  release-evidence scan [options] <repository>")
	fmt.Fprintln(writer, "  release-evidence review [options] <repository>")
	fmt.Fprintln(writer, "  release-evidence version")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Scan makes no network request. Review binds only to a temporary loopback session.")
	fmt.Fprintln(writer, "Neither command executes repository content.")
}
